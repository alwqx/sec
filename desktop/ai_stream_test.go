package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/internal/sse"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/valuation"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func streamValuationRequest(srv *desktopServer, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/valuation", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, r)
	return rr
}

func streamEvents(t *testing.T, r io.Reader) []sse.Event {
	t.Helper()
	d := sse.NewDecoder(r, 16<<20)
	var events []sse.Event
	for {
		event, err := d.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		events = append(events, event)
	}
	return events
}

func TestAIValuationStream(t *testing.T) {
	defer gock.Off()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, srv)
	// 配置与输入错误发生在流开始前，仍返回 JSON 和原来的 HTTP 状态码。
	rr := streamValuationRequest(srv, `{"code":"invalid"}`)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	mockValuationUpstream(valuationBody, performanceBody)
	gock.New("https://model.example").Post("/v1/chat/completions").AddMatcher(func(r *http.Request, _ *gock.Request) (bool, error) {
		var input map[string]any
		err := json.NewDecoder(r.Body).Decode(&input)
		return input["stream"] == true && input["model"] == "first", err
	}).Reply(200).SetHeader("Content-Type", "text/event-stream").BodyString(
		`data: {"model":"served","choices":[{"delta":{"content":"PB 0.8 倍，股息率 99%。"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n")
	rr = streamValuationRequest(srv, `{"code":"SH600036"}`)
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Header().Get("Content-Type"), "text/event-stream")
	events := streamEvents(t, rr.Body)
	require.Equal(t, "status", events[0].Type)
	last := events[len(events)-1]
	require.Equal(t, "done", last.Type)
	var got llm.Explanation
	require.NoError(t, json.Unmarshal([]byte(last.Data), &got))
	require.Equal(t, "served", got.Model)
	require.NotEmpty(t, got.Check.Unsourced)
	require.False(t, got.Cached)
	require.NotContains(t, last.Data, aiValuationKey)
	// JSON 入口可以命中流式生成的结果。
	mockValuationUpstream(valuationBody, performanceBody)
	rr = aiValuationRequest(srv, `{"code":"SH600036"}`)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.True(t, got.Cached)
	// SSE 缓存命中只发送元信息和最终结果，不重新请求模型。
	mockValuationUpstream(valuationBody, performanceBody)
	events = streamEvents(t, streamValuationRequest(srv, `{"code":"SH600036"}`).Body)
	for _, event := range events {
		require.NotEqual(t, "delta", event.Type)
	}
	require.Equal(t, "done", events[len(events)-1].Type)
	require.True(t, gock.IsDone())
}

func TestAIValuationStreamFailure(t *testing.T) {
	for _, wire := range []string{
		`data: {"choices":[{"delta":{"content":"半篇"}}]}` + "\n\n",
		`event: error` + "\n" + `data: {"error":{"message":"bad key ` + aiValuationKey + `"}}` + "\n\n",
	} {
		t.Run(wire[:10], func(t *testing.T) {
			defer gock.Off()
			srv := &desktopServer{dev: true}
			configureAIValuation(t, srv)
			mockValuationUpstream(valuationBody, performanceBody)
			gock.New("https://model.example").Post("/v1/chat/completions").Reply(200).SetHeader("Content-Type", "text/event-stream").BodyString(wire)
			rr := streamValuationRequest(srv, `{"code":"SH600036"}`)
			require.Equal(t, http.StatusOK, rr.Code, "响应头已发送，后续错误用事件表达")
			require.NotContains(t, rr.Body.String(), aiValuationKey)
			events := streamEvents(t, rr.Body)
			require.Equal(t, "error", events[len(events)-1].Type)
			for _, event := range events {
				require.NotEqual(t, "done", event.Type)
			}
			// 断流没有缓存，下次 JSON 请求仍会调用模型。
			mockValuationUpstream(valuationBody, performanceBody)
			gock.New("https://model.example").Post("/v1/chat/completions").Reply(200).BodyString(aiExplanationReply)
			require.Equal(t, http.StatusOK, aiValuationRequest(srv, `{"code":"SH600036"}`).Code)
			require.True(t, gock.IsDone())
		})
	}
}

type gatedExplanation struct {
	llm.Fake
	release chan struct{}
	stopped chan struct{}
}

func (f *gatedExplanation) ChatStream(ctx context.Context, _ llm.Request, emit func(llm.StreamEvent) error) (*llm.Response, error) {
	defer close(f.stopped)
	if err := emit(llm.StreamEvent{Type: llm.StreamText, Text: "首段"}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.release:
	}
	return llm.FakeText("首段").Response, nil
}

func TestAIStreamFlushAndCancel(t *testing.T) {
	for _, cancelAfterDelta := range []bool{false, true} {
		t.Run(map[bool]string{false: "实时输出", true: "取消上游"}[cancelAfterDelta], func(t *testing.T) {
			defer gock.Off()
			t.Setenv("HOME", t.TempDir())
			t.Setenv("USERPROFILE", t.TempDir())
			mockValuationUpstream(valuationBody, performanceBody)
			fake := &gatedExplanation{release: make(chan struct{}), stopped: make(chan struct{})}
			srv := &desktopServer{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				srv.streamAIValuation(w, r, "SH600036", valuation.DefaultDCFParams(), nil, fake, llm.Model{Name: "m"})
			}))
			defer server.Close()
			transport := &http.Transport{}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			r, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			response, err := client.Do(r)
			require.NoError(t, err)
			defer response.Body.Close()
			d := sse.NewDecoder(response.Body, 16<<20)
			for {
				event, err := d.Next()
				require.NoError(t, err)
				if event.Type == "delta" {
					require.Contains(t, event.Data, "首段")
					break
				}
			}
			// 首段到达时模型仍在等待，证明 HTTP 响应没有缓冲整篇。
			select {
			case <-fake.stopped:
				t.Fatal("模型过早结束")
			default:
			}
			if cancelAfterDelta {
				cancel()
			} else {
				close(fake.release)
				for {
					event, err := d.Next()
					require.NoError(t, err)
					if event.Type == "done" {
						break
					}
				}
			}
			select {
			case <-fake.stopped:
			case <-time.After(3 * time.Second):
				t.Fatal("上游没有退出")
			}
		})
	}
}

type brokenStreamWriter struct{ http.ResponseWriter }

func (brokenStreamWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAIStreamWriteFailureCancels(t *testing.T) {
	s := newAIStream(brokenStreamWriter{httptest.NewRecorder()}, context.Background())
	defer s.close()
	require.ErrorIs(t, s.send("delta", map[string]string{"text": "x"}), io.ErrClosedPipe)
	require.ErrorIs(t, s.ctx.Err(), context.Canceled)
}
