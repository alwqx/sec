package utils

import (
	"fmt"
	"io"
	"net/http"
	"runtime"

	"github.com/alwqx/sec/version"
)

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
