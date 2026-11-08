package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/stretchr/testify/require"
)

// recorder 是假的模型接口：记下每次请求的路径、请求头和请求体，按顺序返回预设的回复。
// 处理函数在服务器的 goroutine 里运行，不能调用 require；出错时记下来，测试结束时在测试 goroutine 里报告。
type recorder struct {
	t        *testing.T
	mu       sync.Mutex
	paths    []string
	headers  []http.Header
	bodies   []map[string]any
	replies  []string
	statuses []int
	extra    http.Header
	problems []string
}

func (r *recorder) serve() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		data, err := io.ReadAll(req.Body)
		var body map[string]any
		if err == nil {
			err = json.Unmarshal(data, &body)
		}
		if err != nil {
			r.problems = append(r.problems, "请求体不是 JSON："+err.Error())
		}
		r.paths, r.headers, r.bodies = append(r.paths, req.URL.Path), append(r.headers, req.Header.Clone()), append(r.bodies, body)
		if len(r.replies) == 0 {
			r.problems = append(r.problems, "请求比预设的回复多")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		status := http.StatusOK
		if len(r.statuses) > 0 {
			status, r.statuses = r.statuses[0], r.statuses[1:]
		}
		for k, v := range r.extra {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		reply := r.replies[0]
		r.replies = r.replies[1:]
		_, _ = w.Write([]byte(reply))
	}))
	r.t.Cleanup(func() {
		server.Close()
		for _, problem := range r.problems {
			r.t.Error(problem)
		}
	})
	return server
}

func custom(api, url string) config.AIProfile {
	return config.AIProfile{Provider: config.AIProviderCustom, API: api, BaseURL: url, Model: "test-model"}
}

var quoteTool = Tool{Name: "get_quote", Description: "查询行情", InputSchema: json.RawMessage(`{"type":"object","properties":{"codes":{"type":"array"}}}`)}

func TestErrors(t *testing.T) {
	rec := &recorder{t: t, statuses: []int{429, 400, 503, 401}, extra: http.Header{"Retry-After": {"7"}}, replies: []string{
		`{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
		`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`,
		`<html>gateway down</html>`,
		`{"error":{"message":"invalid key","type":null,"code":"invalid_api_key"}}`,
	}}
	server := rec.serve()
	anthropic, err := New(custom(config.AIAPIAnthropic, server.URL), "sk-secret-value")
	require.NoError(t, err)
	req := Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}}

	_, err = anthropic.Chat(context.Background(), req)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, &APIError{Provider: "自定义", Status: 429, Type: "rate_limit_error", Message: "slow down", RetryAfter: 7 * time.Second}, apiErr)
	require.True(t, apiErr.Retryable())
	require.Equal(t, "自定义 接口返回 HTTP 429（rate_limit_error）：slow down", apiErr.Error())
	require.NotContains(t, err.Error(), "sk-secret-value", "错误里没有 Key")

	_, err = anthropic.Chat(context.Background(), req)
	require.ErrorAs(t, err, &apiErr)
	require.False(t, apiErr.Retryable(), "参数错误不重试")

	_, err = anthropic.Chat(context.Background(), req)
	require.ErrorAs(t, err, &apiErr)
	require.True(t, apiErr.Retryable())
	require.Equal(t, "<html>gateway down</html>", apiErr.Message, "不是 JSON 时用原文")

	openai, err := New(custom(config.AIAPIOpenAI, server.URL), "k")
	require.NoError(t, err)
	_, err = openai.Chat(context.Background(), req)
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "invalid_api_key", apiErr.Type, "没有 type 时用 code")
	require.Equal(t, "invalid key", apiErr.Message)
}

func TestNew(t *testing.T) {
	_, err := New(config.AIProfile{Provider: "anthropic", Model: "m"}, "")
	require.ErrorIs(t, err, ErrMissingKey, "Anthropic 接口必须有 Key")
	_, err = New(config.AIProfile{Provider: "gemini", Model: "m"}, "k")
	require.ErrorContains(t, err, "厂商无效", "先校验配置")

	// 地址由厂商的官方地址或填的地址拼出来，含 DeepSeek 的 Anthropic 兼容接口。
	for profile, want := range map[config.AIProfile]string{
		{Provider: "anthropic", Model: "m"}: "https://api.anthropic.com/v1/messages",
		{Provider: "deepseek", Model: "m"}:  "https://api.deepseek.com/chat/completions",
		{Provider: "deepseek", API: config.AIAPIAnthropic, BaseURL: "https://api.deepseek.com/anthropic", Model: "m"}: "https://api.deepseek.com/anthropic/v1/messages",
		{Provider: "ollama", Model: "m"}: "http://localhost:11434/v1/chat/completions",
	} {
		client, err := New(profile, "k")
		require.NoError(t, err)
		switch c := client.(type) {
		case *anthropicClient:
			require.Equal(t, want, c.url)
		case *openAIClient:
			require.Equal(t, want, c.url)
		}
	}
}

func TestCancel(t *testing.T) {
	// 取消上下文时请求立即结束，CLI 按 Ctrl-C、Desktop 切换页面都靠它。
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	t.Cleanup(func() { close(block); server.Close() })
	client, err := New(custom(config.AIAPIOpenAI, server.URL), "")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = client.Chat(ctx, Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline"), err)
}

func TestFake(t *testing.T) {
	fake := &Fake{Replies: []FakeReply{
		FakeToolCalls(ToolCall{ID: "1", Name: "get_quote", Input: json.RawMessage(`{}`)}),
		FakeText("好的"),
		{Err: errors.New("boom")},
	}}
	resp, err := fake.Chat(context.Background(), Request{System: "s"})
	require.NoError(t, err)
	require.Equal(t, StopToolUse, resp.StopReason)
	resp, err = fake.Chat(context.Background(), Request{})
	require.NoError(t, err)
	require.Equal(t, "好的", resp.Message.Text)
	_, err = fake.Chat(context.Background(), Request{})
	require.EqualError(t, err, "boom")
	_, err = fake.Chat(context.Background(), Request{})
	require.ErrorIs(t, err, ErrFakeExhausted)
	require.Len(t, fake.Requests, 4)
	require.Equal(t, "s", fake.Requests[0].System)
}

func TestToolSchemaDefault(t *testing.T) {
	require.JSONEq(t, `{"type":"object","properties":{}}`, string(toolSchema(Tool{Name: "x"})))
	require.JSONEq(t, `{"type":"object","properties":{}}`, string(toolSchema(Tool{Name: "x", InputSchema: json.RawMessage("null")})))
	require.Equal(t, quoteTool.InputSchema, toolSchema(quoteTool))
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 8, 8, 0, 0, 0, time.UTC)
	for header, want := range map[string]time.Duration{
		"Retry-After: 7":   7 * time.Second,
		"Retry-After: 1.5": 1500 * time.Millisecond,
		"Retry-After: Sun, 08 Nov 2026 08:00:30 GMT": 30 * time.Second,
		"Retry-After: Sun, 08 Nov 2026 07:00:00 GMT": 0,
		"retry-after-ms: 250":                        250 * time.Millisecond,
		"Retry-After: soon":                          0,
	} {
		name, value, _ := strings.Cut(header, ": ")
		h := http.Header{}
		h.Set(name, value)
		require.Equal(t, want, retryAfter(h, now), header)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestIsRetryable(t *testing.T) {
	ctx := context.Background()
	require.True(t, IsRetryable(ctx, &APIError{Status: 503}))
	require.True(t, IsRetryable(ctx, &APIError{Status: 529}))
	require.False(t, IsRetryable(ctx, &APIError{Status: 400}))
	require.True(t, IsRetryable(ctx, fmt.Errorf("请求失败：%w", timeoutError{})), "网络超时可以重试")
	require.True(t, IsRetryable(ctx, fmt.Errorf("x：%w", io.ErrUnexpectedEOF)))
	require.False(t, IsRetryable(ctx, errors.New("bad")))
	require.False(t, IsRetryable(ctx, context.Canceled))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.False(t, IsRetryable(cancelled, &APIError{Status: 503}), "调用方已取消时不重试")
}

func TestResponseTooLarge(t *testing.T) {
	original := maxResponseBytes
	t.Cleanup(func() { maxResponseBytes = original })
	maxResponseBytes = 64
	rec := &recorder{t: t, replies: []string{`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"` + strings.Repeat("x", 100) + `"}}]}`}}
	server := rec.serve()
	client, err := New(custom(config.AIAPIOpenAI, server.URL), "")
	require.NoError(t, err)
	_, err = client.Chat(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	require.ErrorContains(t, err, "回复超过")
}

func TestEndpoints(t *testing.T) {
	for _, c := range []struct {
		profile config.AIProfile
		want    string
	}{
		{custom(config.AIAPIOpenAI, "https://gw.example.com/openai?api-version=2024-10-01"), "https://gw.example.com/openai/chat/completions?api-version=2024-10-01"},
		{custom(config.AIAPIAnthropic, "https://gw.example.com/claude/?x=1"), "https://gw.example.com/claude/v1/messages?x=1"},
		{custom(config.AIAPIAnthropic, "http://localhost:11434/v1"), "http://localhost:11434/v1/messages"},
		{custom(config.AIAPIAnthropic, "http://localhost:11434/v1/"), "http://localhost:11434/v1/messages"},
	} {
		client, err := New(c.profile, "k")
		require.NoError(t, err)
		switch cl := client.(type) {
		case *anthropicClient:
			require.Equal(t, c.want, cl.url, "查询参数保留、/v1 不重复")
		case *openAIClient:
			require.Equal(t, c.want, cl.url, "查询参数保留")
		}
	}
}

func TestIsRetryableNetwork(t *testing.T) {
	ctx := context.Background()
	wrap := func(err error) error {
		return fmt.Errorf("请求失败：%w", &url.Error{Op: "Post", URL: "http://x", Err: err})
	}
	require.False(t, IsRetryable(ctx, wrap(&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED})), "连接被拒绝不重试")
	require.False(t, IsRetryable(ctx, wrap(errors.New("x509: certificate signed by unknown authority"))), "证书错误不重试")
	require.False(t, IsRetryable(ctx, wrap(&net.DNSError{Err: "no such host", Name: "x", IsNotFound: true})), "域名找不到不重试")
	require.True(t, IsRetryable(ctx, wrap(timeoutError{})), "超时重试")
	require.True(t, IsRetryable(ctx, wrap(&net.OpError{Op: "read", Err: syscall.ECONNRESET})), "连接被重置重试")
}

func TestFakeEmptyReply(t *testing.T) {
	fake := &Fake{Replies: []FakeReply{{}}}
	_, err := fake.Chat(context.Background(), Request{})
	require.ErrorIs(t, err, ErrFakeEmptyReply)
}
