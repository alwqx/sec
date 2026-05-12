package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
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

// JSONify json 序列化
func JSONify(data interface{}) string {
	v, err := json.Marshal(data)
	if err != nil {
		slog.Error("JSONify", "error", err)
		return ""
	}
	return string(v)
}
