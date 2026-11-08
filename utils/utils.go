package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/alwqx/sec/version"
)

const (
	// 解析 metal 命令中的时间参数格式
	ParseMetalCmdArgTimeLayout = "20060102"
	LayoutYYMMDD               = "2006-01-02"
	StandardTimeLayout         = "2006-01-02 15:04:05"

	defaultHttpTimeout = 10 * time.Second
)

// StandardTimeString 返回标准格式的时间字符串
func StandardTimeString(t time.Time) string {
	return t.Format(StandardTimeLayout)
}

// TimeYYMMDDString 返回YYMMDD格式的时间字符串
func TimeYYMMDDString(t time.Time) string {
	return t.Format(LayoutYYMMDD)
}

// ParseBeginEnd parses --begin/--end CLI flags into formatted date strings.
// If beginStr or endStr is empty, defaults are computed as:
//
//	end   = now
//	begin = end - defaultDays days
//
// parseLayout is used to decode the user-provided strings (e.g. "20060102").
// outputLayout is used to format the returned strings (e.g. "2006-01-02").
// Returns an error if begin > end or if parsing fails.
func ParseBeginEnd(beginStr, endStr string, defaultDays int, parseLayout, outputLayout string) (string, string, error) {
	end := time.Now()
	begin := end.Add(-time.Duration(defaultDays) * 24 * time.Hour)

	var err error
	if beginStr != "" {
		begin, err = time.Parse(parseLayout, beginStr)
		if err != nil {
			return "", "", err
		}
	}
	if endStr != "" {
		end, err = time.Parse(parseLayout, endStr)
		if err != nil {
			return "", "", err
		}
	}

	if end.Before(begin) {
		return "", "", fmt.Errorf("invalid time range: begin=%s end=%s", begin.Format(outputLayout), end.Format(outputLayout))
	}

	return begin.Format(outputLayout), end.Format(outputLayout), nil
}

// ShanghaiToday 返回上海时区的今天，表示为 UTC 零点，和 time.Parse 解析出的日期可以直接比较。
// 国内行情按北京时间划分交易日，和 Desktop 的默认日期一致，不随本机时区变化。
func ShanghaiToday() time.Time {
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// ParseRange 解析 --begin、--end（YYYYMMDD）。没有指定 begin 时从 end 往前 defaultDays 个日历日，没有指定 end 时是上海时区的今天。
// 和 ParseBeginEnd 不同，默认开始日期从结束日期往前推，只给 --end 时也是有效区间；只比较日期，不受当天时刻影响。
func ParseRange(beginText, endText string, defaultDays int) (time.Time, time.Time, error) {
	end := ShanghaiToday()
	var err error
	if endText != "" {
		if end, err = time.Parse(ParseMetalCmdArgTimeLayout, endText); err != nil {
			return end, end, fmt.Errorf("--end 须为 YYYYMMDD: %s", endText)
		}
	}
	begin := end.AddDate(0, 0, -defaultDays)
	if beginText != "" {
		if begin, err = time.Parse(ParseMetalCmdArgTimeLayout, beginText); err != nil {
			return begin, end, fmt.Errorf("--begin 须为 YYYYMMDD: %s", beginText)
		}
	}
	if begin.After(end) {
		return begin, end, fmt.Errorf("--begin %s 晚于 --end %s", begin.Format(ParseMetalCmdArgTimeLayout), end.Format(ParseMetalCmdArgTimeLayout))
	}
	return begin, end, nil
}

// UserAgent 生成 user-agent
func UserAgent() string {
	return fmt.Sprintf("sec/%s (%s %s) Go/%s", version.Version, runtime.GOARCH, runtime.GOOS, runtime.Version())
}

// retryDelay 是请求失败后重试前的等待时间，测试时调小。
var retryDelay = 500 * time.Millisecond

// MakeRequest 发送 http 请求，返回 *http.Response
// timeout 为请求超时时间（含响应体读取），0 表示使用默认的 10 秒。
// 没有请求体的 GET、HEAD 在连接失败或超时时重试一次；POST 等请求和 HTTP 错误状态不重试。
func MakeRequest(ctx context.Context, method, reqURL string, headers http.Header, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	// 调用方的请求头不能覆盖掉默认的 User-Agent。
	if headers != nil {
		req.Header = headers.Clone()
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent())
	}

	var client *http.Client
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	} else {
		client = &http.Client{Timeout: defaultHttpTimeout}
	}
	resp, err := client.Do(req)
	if err == nil || !retryable(req) {
		return resp, err
	}
	slog.DebugContext(ctx, "请求失败，稍后重试", "url", reqURL, "error", err)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(retryDelay):
	}
	return client.Do(req)
}

// retryable 判断失败的请求能不能重发：没有请求体的 GET、HEAD，且调用方的 context 没有取消或到期。
// 只重试连接失败和单次超时，HTTP 错误状态多半不是一两秒内能恢复的，直接交给调用方。
func retryable(req *http.Request) bool {
	if req.Body != nil || (req.Method != http.MethodGet && req.Method != http.MethodHead) {
		return false
	}
	return req.Context().Err() == nil
}

// CheckStatus 在响应不是 200 时返回错误，避免把错误页当成数据解析。
func CheckStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	host := "上游"
	if resp.Request != nil && resp.Request.URL != nil {
		host = resp.Request.URL.Host
	}
	return fmt.Errorf("%s 返回 HTTP %d", host, resp.StatusCode)
}

// PositiveOrNil 返回 value 的指针，value 不大于 0 时返回 nil。上游用 0 表示缺失的价格、指标，JSON 输出里应为 null。
func PositiveOrNil(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	return &value
}

// PrintJSON 把数据按缩进格式写到 out，供命令的 --json 输出使用。
func PrintJSON(out io.Writer, data any) error {
	encoder := json.NewEncoder(out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// WriteJson 把数据结构转换成 json 格式写到文件中
func WriteJson(data interface{}, filePath string) error {
	if data == nil {
		return nil
	}

	v, err := json.Marshal(data)
	if err != nil {
		return err
	}

	f, err := os.Create(filePath)
	if err != nil {
		return err
	}

	_, err = f.Write(v)
	return err
}

// SecDir returns the path to the ~/.sec directory, creating it and any
// specified subdirectories if they don't exist.
func SecDir(sub ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(append([]string{home, ".sec"}, sub...)...)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// TruncateRunes 把 text 截到最多 n 个字符（按 Unicode 字符计，不切开多字节字符），截断时末尾加「…」。
func TruncateRunes(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}

// JSONify json 序列化
func JSONify(data interface{}) string {
	v, err := json.Marshal(data)
	if err != nil {
		slog.Error("JSONify", "error", err)
		return ""
	}
	return string(v)
}

func HumanByte(cap float64) (res string) {
	if cap <= 0.0 {
		res = " - "
	} else if cap >= 1_073_741_824.0 { // 1024 * 1024 * 1024
		res = fmt.Sprintf("%-.2f GB", cap/1_073_741_824.0)
	} else if cap >= 1_048_576.0 { // 1024 * 1024
		res = fmt.Sprintf("%-.2f MB", cap/1_048_576.0)
	} else if cap >= 1_024.0 {
		res = fmt.Sprintf("%-.2f KB", cap/1_024.0)
	} else {
		res = fmt.Sprintf("%-.0f B", cap)
	}
	return
}

func HumanNum(cap float64) (res string) {
	if cap <= 0.0 {
		res = " - "
	} else if cap > 100_000_000.0 {
		res = fmt.Sprintf("%-.2f亿", cap/100_000_000.0)
	} else {
		res = fmt.Sprintf("%-.2f万", cap/10_000.0)
	}
	return
}

// HumanRatio 格式化估值倍数，0 表示不可用，显示为 -。
func HumanRatio(value float64) string {
	if value == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", value)
}
