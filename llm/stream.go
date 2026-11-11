package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const (
	StreamText     = "text_delta"
	StreamThinking = "thinking"
)

// StreamEvent 只传正文或状态，不把思考过程和未完成的工具参数暴露给界面。
type StreamEvent struct {
	Type string
	Text string
}

// StreamingClient 是可选能力；Chat 保持完整回复接口。成功时返回与 Chat 相同的完整消息。
type StreamingClient interface {
	Client
	ChatStream(context.Context, Request, func(StreamEvent) error) (*Response, error)
}

var ErrStreamUnsupported = errors.New("这个模型客户端不支持流式输出")

// StreamError 是 HTTP 成功后流内发生的模型错误，不伪造 HTTP 状态码。
type StreamError struct{ Provider, Type, Message string }

func (e *StreamError) Error() string {
	return fmt.Sprintf("%s 流式错误（%s）：%s", e.Provider, e.Type, e.Message)
}

func streamReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("模型事件流未完整结束：%w", err)
}

func streamAPIError(provider string, data []byte, parse func([]byte) (string, string)) error {
	kind, message := parse(data)
	return &StreamError{Provider: provider, Type: kind, Message: message}
}

func emitStream(ctx context.Context, emit func(StreamEvent) error, event StreamEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if emit != nil {
		return emit(event)
	}
	return nil
}

// openStream 保留普通 HTTP 错误；开始 SSE 后由各协议解释流内错误。不重试 POST。
func openStream(ctx context.Context, url string, headers http.Header, body any, provider string, parseError func([]byte) (string, string)) (io.ReadCloser, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "text/event-stream")
	resp, err := utils.MakeRequest(ctx, http.MethodPost, url, headers, bytes.NewReader(data), requestTimeout)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败：%w", provider, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)+1))
		if err != nil {
			return nil, err
		}
		if len(payload) > maxResponseBytes {
			return nil, errors.New("模型错误回复超过大小限制")
		}
		kind, message := parseError(payload)
		if message == "" {
			message = utils.TruncateRunes(strings.TrimSpace(string(payload)), 300)
		}
		return nil, &APIError{Provider: provider, Status: resp.StatusCode, Type: kind, Message: message, RetryAfter: retryAfter(resp.Header, time.Now())}
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if contentType != "text/event-stream" {
		resp.Body.Close()
		return nil, fmt.Errorf("%s 没有返回 SSE 流，请检查模型或网关是否支持流式输出", provider)
	}
	return resp.Body, nil
}
