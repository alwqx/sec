package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIStreamArrivesBeforeDone(t *testing.T) {
	release := make(chan struct{})
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"model\":\"served\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"招商\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"银行\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	client, err := New(config.AIProfile{Provider: "openai", BaseURL: server.URL, Model: "m"}, "k")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var text string
	resp, err := client.(StreamingClient).ChatStream(ctx, Request{Messages: []Message{{Role: RoleUser, Text: "测试"}}}, func(e StreamEvent) error {
		text += e.Text
		if text == "招商" {
			close(release)
		}
		return nil
	})
	require.NoError(t, err, "服务器收到首段回调前不会结束响应，证明没有缓冲整篇")
	require.Equal(t, "招商银行", text)
	require.Equal(t, text, resp.Message.Text)
	require.Equal(t, Usage{InputTokens: 7, OutputTokens: 2}, resp.Usage)
	require.Equal(t, true, body["stream"])
	require.Contains(t, body, "max_completion_tokens")
	require.Equal(t, map[string]any{"include_usage": true}, body["stream_options"])
}

func TestOpenAIStreamToolsAndReasoning(t *testing.T) {
	rec := &recorder{t: t, extra: http.Header{"Content-Type": {"text/event-stream"}}, replies: []string{
		`data: {"choices":[{"delta":{"reasoning_content":"私有推理","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"ping","arguments":"{\"text\":"}}]}}]}` + "\n\n" +
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"sec\"}"}}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":9}}` + "\n\ndata: [DONE]\n\n",
	}}
	srv := rec.serve()
	client, err := New(config.AIProfile{Provider: "deepseek", BaseURL: srv.URL, Model: "m"}, "k")
	require.NoError(t, err)
	var events []StreamEvent
	resp, err := client.(StreamingClient).ChatStream(context.Background(), Request{}, func(e StreamEvent) error { events = append(events, e); return nil })
	require.NoError(t, err)
	require.Equal(t, StopToolUse, resp.StopReason)
	require.JSONEq(t, `{"text":"sec"}`, string(resp.Message.ToolCalls[0].Input))
	require.Equal(t, "私有推理", resp.Message.reasoning)
	require.Equal(t, []StreamEvent{{Type: StreamThinking}}, events, "不向界面发送推理或半截工具参数")
	require.Equal(t, 9, resp.Usage.OutputTokens)
}

func TestStreamFailures(t *testing.T) {
	for name, reply := range map[string]string{
		"没有结束标记":  "data: {\"choices\":[{\"delta\":{\"content\":\"半篇\"}}]}\n\n",
		"没有停止原因":  "data: [DONE]\n\n",
		"无效 JSON": "data: invalid\n\n",
		"流内错误":    "data: {\"error\":{\"type\":\"overloaded\",\"message\":\"failed\"}}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{t: t, extra: http.Header{"Content-Type": {"text/event-stream"}}, replies: []string{reply}}
			srv := rec.serve()
			c, err := New(custom(config.AIAPIOpenAI, srv.URL), "")
			require.NoError(t, err)
			resp, err := c.(StreamingClient).ChatStream(context.Background(), Request{}, nil)
			require.Error(t, err)
			require.Nil(t, resp)
		})
	}
}

func TestStreamCancelClosesUpstream(t *testing.T) {
	for _, protocol := range []string{config.AIAPIOpenAI, config.AIAPIAnthropic} {
		t.Run(protocol, func(t *testing.T) {
			closed := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				wire := "data: {\"choices\":[{\"delta\":{\"content\":\"首段\"}}]}\n\n"
				if protocol == config.AIAPIAnthropic {
					wire = "data: {\"type\":\"message_start\",\"message\":{}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"首段\"}}\n\n"
				}
				_, _ = io.WriteString(w, wire)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(closed)
			}))
			defer server.Close()
			c, err := New(custom(protocol, server.URL), "")
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err = c.(StreamingClient).ChatStream(ctx, Request{}, func(StreamEvent) error { cancel(); return nil })
			require.ErrorIs(t, err, context.Canceled)
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("模型连接没有关闭")
			}
		})
	}
}

func TestStreamHTTPAndProtocolErrors(t *testing.T) {
	for _, status := range []int{200, 429} {
		rec := &recorder{t: t, statuses: []int{status}, extra: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"3"}}, replies: []string{`{"error":{"message":"failed"}}`}}
		srv := rec.serve()
		c, err := New(custom(config.AIAPIOpenAI, srv.URL), "")
		require.NoError(t, err)
		_, err = c.(StreamingClient).ChatStream(context.Background(), Request{}, nil)
		if status == 200 {
			require.ErrorContains(t, err, "没有返回 SSE")
		} else {
			var apiErr *APIError
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, 429, apiErr.Status)
			require.Equal(t, 3*time.Second, apiErr.RetryAfter)
		}
		require.Len(t, rec.bodies, 1)
	}
}

func TestAnthropicStreamIncomplete(t *testing.T) {
	for _, wire := range []string{
		`{"type":"message_stop"}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"x"}}`,
		`{"type":"error","error":{"type":"overloaded_error","message":"failed"}}`,
		`{"type":"message_start","message":{}}`,
	} {
		rec := &recorder{t: t, extra: http.Header{"Content-Type": {"text/event-stream"}}, replies: []string{"data: " + wire + "\n\n"}}
		srv := rec.serve()
		c, err := New(custom(config.AIAPIAnthropic, srv.URL), "")
		require.NoError(t, err)
		resp, err := c.(StreamingClient).ChatStream(context.Background(), Request{}, nil)
		require.Error(t, err)
		require.Nil(t, resp)
	}
}

func TestStreamCallbackFailure(t *testing.T) {
	rec := &recorder{t: t, extra: http.Header{"Content-Type": {"text/event-stream"}}, replies: []string{"data: {\"choices\":[{\"delta\":{\"content\":\"正文\"}}]}\n\ndata: [DONE]\n\n"}}
	srv := rec.serve()
	c, err := New(custom(config.AIAPIOpenAI, srv.URL), "")
	require.NoError(t, err)
	want := errors.New("下游关闭")
	_, err = c.(StreamingClient).ChatStream(context.Background(), Request{}, func(StreamEvent) error { return want })
	require.ErrorIs(t, err, want)
	require.Len(t, rec.bodies, 1, "增量发送后不自动重试")
}

func TestAnthropicStream(t *testing.T) {
	frames := []string{
		`{"type":"message_start","message":{"model":"served","usage":{"input_tokens":10,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"私有推理"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"正文"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"c1","name":"ping","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"text\":"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"sec\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`,
		`{"type":"message_delta","delta":{},"usage":{"output_tokens":12}}`,
		`{"type":"message_stop"}`,
	}
	var wire strings.Builder
	for _, frame := range frames {
		wire.WriteString("data: " + frame + "\n\n")
	}
	rec := &recorder{t: t, extra: http.Header{"Content-Type": {"text/event-stream"}}, replies: []string{wire.String()}}
	srv := rec.serve()
	client, err := New(custom(config.AIAPIAnthropic, srv.URL), "")
	require.NoError(t, err)
	c := client.(*anthropicClient)
	var text string
	resp, err := c.ChatStream(context.Background(), Request{}, func(e StreamEvent) error { text += e.Text; return nil })
	require.NoError(t, err)
	require.Equal(t, "正文", text)
	require.Equal(t, text, resp.Message.Text)
	require.Equal(t, Usage{InputTokens: 10, OutputTokens: 12}, resp.Usage, "累计值不相加")
	require.Equal(t, StopToolUse, resp.StopReason)
	require.JSONEq(t, `{"text":"sec"}`, string(resp.Message.ToolCalls[0].Input))
	content, empty, err := c.content(resp.Message)
	require.NoError(t, err)
	require.False(t, empty)
	require.Contains(t, string(content), `"signature":"signed"`)
	require.Contains(t, string(content), "私有推理")
}
