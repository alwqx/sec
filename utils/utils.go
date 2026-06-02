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

// UserAgent 生成 user-agent
func UserAgent() string {
	return fmt.Sprintf("sec/%s (%s %s) Go/%s", version.Version, runtime.GOARCH, runtime.GOOS, runtime.Version())
}

// MakeRequest 发送 http 请求，返回 *http.Response
// timeout 为请求超时时间（含响应体读取），0 表示不设置超时
func MakeRequest(ctx context.Context, method, reqURL string, headers http.Header, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent())
	if headers != nil {
		req.Header = headers
	}

	var client *http.Client
	if timeout > 0 {
		client = &http.Client{Timeout: timeout}
	} else {
		client = &http.Client{Timeout: defaultHttpTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
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
