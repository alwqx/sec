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
	"os"
	"syscall"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestProbeHint(t *testing.T) {
	local := config.AIProfile{Provider: "ollama", Model: "m"}
	remote := config.AIProfile{Provider: "deepseek", Model: "m"}
	urlErr := func(err error) error { return &url.Error{Op: "Post", URL: "http://x", Err: err} }
	for name, c := range map[string]struct {
		err     error
		profile config.AIProfile
		want    string
	}{
		"Key 无法解密":      {config.ErrKeyUndecryptable, remote, "重新设置"},
		"密钥丢失":          {config.ErrSecretMissing, remote, "重新设置"},
		"权限过宽":          {config.ErrSecretPermissions, remote, "chmod 600"},
		"403":           {&APIError{Status: 403}, remote, "Key 不对"},
		"400":           {&APIError{Status: 400}, remote, "检查模型名和接口协议"},
		"402":           {&APIError{Status: 402}, remote, "余额不足"},
		"本机连接被拒绝":       {urlErr(&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}), local, "ollama serve"},
		"Windows 连接被拒绝": {urlErr(&net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connectex", Err: wsaeConnRefused}}), local, "ollama serve"},
		"远程连接被拒绝":       {urlErr(&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}), remote, "检查网络、代理"},
		"超时":            {fmt.Errorf("x：%w", context.DeadlineExceeded), remote, "内没有回复"},
		"没有 Key":        {ErrMissingKey, remote, ""},
		"其他":            {errors.New("boom"), remote, ""},
	} {
		got := probeHint(c.err, c.profile)
		if c.want == "" {
			require.Empty(t, got, name)
		} else {
			require.Contains(t, got, c.want, name)
		}
	}
}

func TestConnRefused(t *testing.T) {
	require.True(t, connRefused(&net.OpError{Err: syscall.ECONNREFUSED}))
	require.True(t, connRefused(&net.OpError{Err: &os.SyscallError{Err: wsaeConnRefused}}), "Windows 的 WSAECONNREFUSED")
	require.False(t, connRefused(&net.OpError{Err: syscall.ECONNRESET}))
	require.False(t, connRefused(errors.New("x")))
}

func TestProbeProfile(t *testing.T) {
	rec := &recorder{t: t, replies: []string{
		`{"model":"served","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`,
		`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"ping","arguments":"{}"}}]}}]}`,
	}}
	server := rec.serve()
	var ai config.AIConfig
	require.NoError(t, ai.Add("gw", custom(config.AIAPIOpenAI, server.URL)))
	secret := config.NewAISecret(t.TempDir() + "/secret.key")
	got := ProbeProfile(context.Background(), &ai, "gw", secret, func(string) string { return "" })
	require.True(t, got.OK, got.Error)
	require.True(t, got.Sent)
	require.Equal(t, "served", got.ReplyModel)
	require.Equal(t, ProbeUsage{InputTokens: 10, OutputTokens: 2}, got.Usage)
	require.Equal(t, ToolsSupported, got.Tools)
	require.True(t, got.Local, "测试服务在本机")
	require.Len(t, rec.bodies, 2)

	// 远程厂商没有 Key：不发请求。
	require.NoError(t, ai.Add("ds", config.AIProfile{Provider: "deepseek", Model: "m"}))
	got = ProbeProfile(context.Background(), &ai, "ds", secret, func(string) string { return "" })
	require.False(t, got.OK)
	require.False(t, got.Sent)
	require.Contains(t, got.Error, "Key")
	require.Len(t, rec.bodies, 2)
}

// 连接测试是确认关闭思考是否生效的地方：模型忽略了关闭参数时给出警告。关闭思考只允许官方地址，用 gock 拦截。
func TestProbeProfileThinkingIgnored(t *testing.T) {
	defer gock.Off()
	gock.New("https://api.deepseek.com").Post("/chat/completions").Reply(200).
		BodyString(`{"model":"deepseek-reasoner","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"OK","reasoning_content":"想一想"}}]}`)
	gock.New("https://api.deepseek.com").Post("/chat/completions").Reply(200).
		BodyString(`{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"ping","arguments":"{}"}}]}}]}`)
	var ai config.AIConfig
	require.NoError(t, ai.Add("ds", config.AIProfile{Provider: "deepseek", Model: "deepseek-reasoner", Thinking: config.AIThinkingOff}))
	secret := config.NewAISecret(t.TempDir() + "/secret.key")
	got := ProbeProfile(context.Background(), &ai, "ds", secret, func(string) string { return "sk-test-abcdef123456" })
	require.True(t, got.OK, got.Error)
	require.Contains(t, got.Warning, "仍在思考")
	require.True(t, gock.IsDone())
}

func TestProbeProfileAnthropic(t *testing.T) {
	rec := &recorder{t: t, replies: []string{
		`{"model":"served","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`,
		`{"content":[{"type":"tool_use","id":"c1","name":"ping","input":{"text":"sec"}}],"stop_reason":"tool_use"}`,
	}}
	server := rec.serve()
	var ai config.AIConfig
	profile := custom(config.AIAPIAnthropic, server.URL)
	profile.APIKeyEnv = "TEST_AI_KEY"
	require.NoError(t, ai.Add("gw", profile))
	key := "sk-test-private-123456"
	reads := 0
	got := ProbeProfile(context.Background(), &ai, "gw", nil, func(env string) string {
		require.Equal(t, "TEST_AI_KEY", env)
		reads++
		return key
	})
	require.True(t, got.OK, got.Error)
	require.Equal(t, ToolsSupported, got.Tools)
	require.Equal(t, ProbeUsage{InputTokens: 10, OutputTokens: 2}, got.Usage)
	require.Equal(t, 1, reads, "状态显示和请求使用同一次读取的 Key")
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), key)
	require.Contains(t, got.Key, "3456")
	require.Len(t, rec.bodies, 2, "只测工具调用，不执行工具或继续对话")
	require.Equal(t, []string{"/v1/messages", "/v1/messages"}, rec.paths)
	for i, body := range rec.bodies {
		require.Equal(t, key, rec.headers[i].Get("x-api-key"))
		require.EqualValues(t, probeMaxTokens, body["max_tokens"])
		require.Len(t, body["messages"], 1, "两次请求只含各自固定的测试消息")
	}
	require.NotContains(t, rec.bodies[0], "tools")
	require.Len(t, rec.bodies[1]["tools"], 1)
}

func TestProbeProfileToolsFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"未调用", 200, `{"choices":[{"finish_reason":"stop","message":{"content":"OK"}}]}`, ToolsNotCalled},
		{"调用了其他工具", 200, `{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"function":{"name":"other","arguments":"{}"}}]}}]}`, ToolsNotCalled},
		{"不支持工具", 400, `{"error":{"message":"tools unsupported"}}`, ToolsError},
		{"限流", 429, `{"error":{"message":"rate limited"}}`, ToolsUnknown},
		{"上游超时", 408, `{}`, ToolsUnknown},
		{"上游故障", 503, `{}`, ToolsUnknown},
		{"无效响应", 200, `not JSON`, ToolsUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{t: t, statuses: []int{200, tc.status}, replies: []string{
				`{"choices":[{"finish_reason":"stop","message":{"content":"OK"}}]}`, tc.body,
			}}
			server := rec.serve()
			var ai config.AIConfig
			require.NoError(t, ai.Add("gw", custom(config.AIAPIOpenAI, server.URL)))
			got := ProbeProfile(context.Background(), &ai, "gw", nil, func(string) string { return "" })
			require.True(t, got.OK, "工具测试失败不改变连接成功的结论")
			require.Equal(t, tc.want, got.Tools)
			require.NotEmpty(t, got.ToolsDetail)
			require.Empty(t, got.Error)
			require.Len(t, rec.bodies, 2)
		})
	}
}

func TestProbeProfileCancelTools(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests++
		if requests == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"OK"}}]}`))
			return
		}
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	var ai config.AIConfig
	require.NoError(t, ai.Add("gw", custom(config.AIAPIOpenAI, server.URL)))
	got := ProbeProfile(ctx, &ai, "gw", nil, func(string) string { return "" })
	require.True(t, got.OK)
	require.Equal(t, ToolsUnknown, got.Tools)
	require.Contains(t, got.ToolsDetail, "context canceled")
}
