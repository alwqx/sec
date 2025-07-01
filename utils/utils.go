package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/alwqx/sec/version"
)

const (
	LayoutYYMMDD       = "2006-01-02"
	StandardTimeLayout = "2006-01-02 15:04:05"
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
func MakeRequest(method, reqURL string, headers http.Header, body io.Reader) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}
	if headers != nil {
		req.Header = headers
	}

	req.Header.Set("User-Agent", UserAgent())
	resp, err = http.DefaultClient.Do(req)
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
