package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const aiValuationKey = "sk-valuation-test-private-123456"

func configureAIValuation(t *testing.T, srv *desktopServer) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := config.Default()
	for _, name := range []string{"first", "second"} {
		require.NoError(t, cfg.AI.Add(name, config.AIProfile{Provider: "custom", API: "openai", BaseURL: "https://model.example/v1", Model: name}))
	}
	secret, err := srv.aiSecret()
	require.NoError(t, err)
	_, err = cfg.AI.SetKey(secret, "first", aiValuationKey)
	require.NoError(t, err)
	require.NoError(t, cfg.AI.SetEnabled(true))
	path, err := srv.configPath()
	require.NoError(t, err)
	require.NoError(t, config.SaveFile(context.Background(), path, cfg))
	return path
}

func aiValuationRequest(srv *desktopServer, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/valuation", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, r)
	return rr
}

const aiExplanationReply = `{"model":"served","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"估值水平：PB 0.8 倍，股息率 99%。"}}]}`

func TestAIValuation(t *testing.T) {
	defer gock.Off()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	path := configureAIValuation(t, srv)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	mockValuationUpstream(valuationBody, performanceBody)
	var sent map[string]any
	capture := func(r *http.Request, _ *gock.Request) (bool, error) {
		body, err := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err == nil {
			err = json.Unmarshal(body, &sent)
		}
		return err == nil, err
	}
	gock.New("https://model.example").Post("/v1/chat/completions").MatchHeader("Authorization", "Bearer "+aiValuationKey).
		AddMatcher(capture).Reply(200).BodyString(aiExplanationReply)
	rr := aiValuationRequest(srv, `{"code":"sh600036"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got llm.Explanation
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, "SH600036", got.ExCode)
	require.Equal(t, "2026-10-30", got.AsOf)
	require.Equal(t, "first", got.Profile, "不指定配置时用 default")
	require.Equal(t, "served", got.Model)
	require.Contains(t, got.Disclaimer, "不构成投资建议")
	require.NotEmpty(t, got.Check.Unsourced)
	require.False(t, got.Cached)
	require.NotContains(t, rr.Body.String(), aiValuationKey)
	require.NotContains(t, sent, "tools", "估值解读只发一轮精简数据，不开放工具调用")
	messages := sent["messages"].([]any)
	source := messages[1].(map[string]any)["content"].(string)
	require.Contains(t, source, `"ex_code":"SH600036"`)
	require.Contains(t, source, `"notes":`)
	require.Contains(t, source, "金额单位是亿元")
	require.NotContains(t, source, `"series":`, "不向模型发送逐日历史")
	require.Equal(t, "first", sent["model"])
	require.True(t, gock.IsDone())

	// 同一份数据与 CLI 共用 llm 缓存，不再请求模型。
	mockValuationUpstream(valuationBody, performanceBody)
	rr = aiValuationRequest(srv, `{"code":"SH600036","profile":"first"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.True(t, got.Cached)

	// 临时选其他配置、修改参数：用新模型重新生成，不写 default。
	mockValuationUpstream(valuationBody, performanceBody)
	gock.New("https://model.example").Post("/v1/chat/completions").AddMatcher(capture).Reply(200).BodyString(aiExplanationReply)
	rr = aiValuationRequest(srv, `{"code":"SH600036","profile":"second","growth":0,"terminal_growth":0,"wacc":9,"margin_of_safety":0}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, "second", got.Profile)
	require.False(t, got.Cached)
	require.Equal(t, "second", sent["model"])
	source = sent["messages"].([]any)[1].(map[string]any)["content"].(string)
	for _, param := range []string{`"growth":0`, `"terminal_growth":0`, `"wacc":9`, `"margin_of_safety":0`} {
		require.Contains(t, source, param)
	}
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "选择配置、生成解读均不修改共享配置或 default")
	require.True(t, gock.IsDone())
}

func TestAIValuationRejectsBeforeUpstream(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	path := configureAIValuation(t, srv)
	for _, body := range []string{
		`{"code":"600036"}`, `{"code":"HK00700"}`, `{"code":"SH600036","wacc":0}`, `{"code":"SH600036","growth":101}`,
		`{"code":"SH600036","wacc":"8"}`, `{"code":"SH600036","prompt":"user prompt"}`, `{} {}`, `null`,
	} {
		require.Equal(t, http.StatusBadRequest, aiValuationRequest(srv, body).Code, body)
	}
	require.Equal(t, http.StatusConflict, aiValuationRequest(srv, `{"code":"SH600036","profile":"missing"}`).Code)
	require.NoError(t, config.UpdateAIFile(context.Background(), path, func(ai *config.AIConfig) error { return ai.SetEnabled(false) }))
	rr := aiValuationRequest(srv, `{"code":"SH600036"}`)
	require.Equal(t, http.StatusConflict, rr.Code)
	require.Contains(t, rr.Body.String(), "AI 功能没有启用")
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{"enabled":"yes"}}`), 0o600))
	require.Equal(t, http.StatusConflict, aiValuationRequest(srv, `{"code":"SH600036"}`).Code)
	require.False(t, gock.HasUnmatchedRequest(), "配置、输入有问题时不请求模型或估值数据")

	srv.dev = false
	srv.token = "test-token"
	require.Equal(t, http.StatusForbidden, aiValuationRequest(srv, `{"code":"SH600036"}`).Code)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/valuation", strings.NewReader(`{"code":"SH600036"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(&http.Cookie{Name: "token", Value: srv.token})
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, r)
	require.Equal(t, http.StatusForbidden, rr.Code)
}

func TestAIValuationModelFailure(t *testing.T) {
	defer gock.Off()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, srv)
	mockValuationUpstream(valuationBody, performanceBody)
	gock.New("https://model.example").Post("/v1/chat/completions").Reply(401).BodyString(`{"error":{"message":"bad key ` + aiValuationKey + `"}}`)
	rr := aiValuationRequest(srv, `{"code":"SH600036"}`)
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "HTTP 401")
	require.NotContains(t, rr.Body.String(), aiValuationKey)

	mockValuationUpstream(emptyBody, emptyBody)
	rr = aiValuationRequest(srv, `{"code":"SH600036"}`)
	require.Equal(t, http.StatusNotFound, rr.Code, "没有估值数据时不调用模型")
}

func TestAIValuationInput(t *testing.T) {
	in := aiValuationInput{Code: "SH600036"}
	input, err := in.toolInput()
	require.NoError(t, err)
	require.JSONEq(t, `{"code":"SH600036"}`, string(input))
	require.Equal(t, 8.0, in.params().WACC)
	zero, nine := 0.0, 9.0
	in.Growth, in.TerminalGrowth, in.WACC, in.MarginOfSafety = &zero, &zero, &nine, &zero
	input, err = in.toolInput()
	require.NoError(t, err)
	require.JSONEq(t, `{"code":"SH600036","growth":0,"terminal_growth":0,"wacc":9,"margin_of_safety":0}`, string(input))
	require.NoError(t, in.params().Validate())
}

func TestAIValuationUS(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	configureAIValuation(t, srv)
	mockUSValuation()
	gock.New("https://model.example").Post("/v1/chat/completions").Reply(200).BodyString(aiExplanationReply)
	rr := aiValuationRequest(srv, `{"code":"$aapl"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got llm.Explanation
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Equal(t, "$AAPL", got.ExCode)
	require.Equal(t, "2026-03-03", got.AsOf)
	require.NotEmpty(t, got.Disclaimer)
	require.True(t, gock.IsDone())
}

type aiValuationTransport func(*http.Request) (*http.Response, error)

func (f aiValuationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAIValuationCancel(t *testing.T) {
	defer gock.Off()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	configureAIValuation(t, srv)
	mockValuationUpstream(valuationBody, performanceBody)
	// 不能直接替换 http.DefaultTransport：gock 只在它等于 gock.DefaultTransport 时拦截，否则所有请求都会走真实网络，
	// 测试快慢取决于 CI 能否连上东方财富。这里让 gock 继续拦截，只把模型地址放行到一直等到取消的假传输。
	native := gock.DefaultTransport.Transport
	started := make(chan struct{})
	gock.DefaultTransport.Transport = aiValuationTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	gock.EnableNetworking()
	gock.NetworkingFilter(func(r *http.Request) bool { return r.URL.Host == "model.example" })
	defer func() {
		gock.DisableNetworkingFilters()
		gock.DisableNetworking()
		gock.DefaultTransport.Transport = native
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ai/valuation", strings.NewReader(`{"code":"SH600036"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { srv.Handler().ServeHTTP(rr, r); close(done) }()
	select {
	case <-started:
	case <-done:
		t.Fatalf("没有请求模型：%s", rr.Body.String())
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("等待模型请求超时")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后模型请求没有退出")
	}
	require.Empty(t, rr.Body.String(), "取消时不返回可缓存的解读或误报失败")
	require.True(t, gock.IsDone(), "估值数据应由 gock 提供，不能请求真实网络")
}
