package eastmoney

import (
	"context"
	"io"
	"net/http"

	"github.com/alwqx/sec/utils"
)

// getBody 发送 GET 请求，状态码不是 200 时报错，返回响应正文。K 线、F10 报表和数据中心共用。
// 不记日志：F10 由外层重试时记 WARN，其余调用方自己记 ERROR，避免同一次失败记两遍。
func getBody(ctx context.Context, reqURL string) ([]byte, error) {
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqURL, nil, nil, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

// RowNumber 读取 F10 报表或数据中心一行中的数值字段，字段缺失或不是数字时返回 nil。
func RowNumber(row map[string]any, key string) *float64 {
	if value, ok := row[key].(float64); ok {
		return &value
	}
	return nil
}
