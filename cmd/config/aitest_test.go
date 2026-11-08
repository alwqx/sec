package config

import (
	"bytes"
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
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/stretchr/testify/require"
)

// fakeModel 是假的 OpenAI 兼容接口：按顺序返回预设的状态码和回复，记下请求体。
type fakeModel struct {
	mu      sync.Mutex
	bodies  []string
	replies []fakeModelReply
}

type fakeModelReply struct {
	status int
	body   string
}

func (f *fakeModel) serve(t *testing.T) string {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		data, _ := io.ReadAll(r.Body)
		f.bodies = append(f.bodies, string(data))
		if len(f.replies) == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		reply := f.replies[0]
		f.replies = f.replies[1:]
		w.WriteHeader(reply.status)
		_, _ = w.Write([]byte(reply.body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func okReply(text string) fakeModelReply {
	return fakeModelReply{200, `{"model":"served-model","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"` + text + `"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`}
}

var pingReply = fakeModelReply{200, `{"choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"ping","arguments":"{\"text\":\"sec\"}"}}]}}]}`}

// setupTestProfile 写一个指向假接口的自定义配置并设为 default。
func setupTestProfile(t *testing.T, baseURL string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, err := run(t, "ai", "add", "gw", "--provider", "custom", "--api", "openai", "--base-url", baseURL, "--model", "test-model")
	require.NoError(t, err)
	return home
}

func TestConfigAITest(t *testing.T) {
	model := &fakeModel{replies: []fakeModelReply{okReply("OK"), pingReply, okReply("OK"), pingReply}}
	setupTestProfile(t, model.serve(t))

	output, err := run(t, "ai", "test")
	require.NoError(t, err, "不传名字时测试 default")
	for _, want := range []string{"测试模型配置 gw（自定义）", "协议：openai", "模型：test-model", "数据：本机模型，不出本机",
		"连接正常：", "实际模型 served-model", `回复 "OK"`, "输入 10 / 输出 2 tokens", "工具调用：支持"} {
		require.Contains(t, output, want)
	}
	require.Contains(t, model.bodies[0], "请只回复 OK", "只发固定的测试文字")
	require.NotContains(t, model.bodies[0], `"tools"`)
	require.Contains(t, model.bodies[1], `"name":"ping"`, "第二个请求测试工具调用")
	require.Contains(t, model.bodies[1], `"max_tokens":1024`)

	output, err = run(t, "ai", "test", "gw", "--json")
	require.NoError(t, err)
	var result aiTestResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	require.True(t, result.OK)
	require.Equal(t, aiToolsSupported, result.Tools)
	require.Equal(t, aiTestUsage{InputTokens: 10, OutputTokens: 2}, result.Usage)
	require.Contains(t, output, `"input_tokens": 10`)
	require.True(t, result.Sent)
	require.Contains(t, output, `"latency_ms"`, "字段固定输出")
	require.Empty(t, result.Warning)
	require.Equal(t, "served-model", result.ReplyModel)
	require.True(t, result.Local)

	_, err = run(t, "ai", "test", "nosuch")
	require.ErrorIs(t, err, config.ErrAIProfileNotFound)
}

func TestConfigAITestTools(t *testing.T) {
	// 不调用工具或带工具的请求出错：连接仍算正常，提示可能不支持工具调用。
	model := &fakeModel{replies: []fakeModelReply{
		okReply("OK"), okReply("我不会调用工具"),
		okReply("OK"), {400, `{"error":{"message":"model does not support tools","type":"invalid_request_error"}}`},
	}}
	setupTestProfile(t, model.serve(t))
	output, err := run(t, "ai", "test")
	require.NoError(t, err)
	require.Contains(t, output, "工具调用：模型没有调用测试工具（停止原因 stop）")
	require.Contains(t, output, "sec ask 和 Desktop 对话会用不了")

	output, err = run(t, "ai", "test")
	require.NoError(t, err, "工具调用出错不算连接失败")
	require.Contains(t, output, "工具调用：带工具的请求被拒绝")
	require.Contains(t, output, "does not support tools")
}

func TestConfigAITestFailures(t *testing.T) {
	for status, hint := range map[int]string{401: "Key 不对", 404: "地址或模型名可能不对", 429: "被限流或余额不足", 503: "厂商服务暂时不可用"} {
		model := &fakeModel{replies: []fakeModelReply{{status, `{"error":{"message":"nope"}}`}}}
		setupTestProfile(t, model.serve(t))
		output, err := run(t, "ai", "test")
		require.EqualError(t, err, "模型配置 gw 测试失败", "返回的错误只说结论，原因在输出里")
		require.Contains(t, output, "连接失败：自定义 接口返回 HTTP "+fmt.Sprint(status), status)
		require.Contains(t, output, "建议："+hint, status)
		require.Len(t, model.bodies, 1, "连接失败时不再测工具调用")

		model = &fakeModel{replies: []fakeModelReply{{status, `{}`}}}
		setupTestProfile(t, model.serve(t))
		output, err = run(t, "ai", "test", "--json")
		require.Error(t, err)
		var result aiTestResult
		require.NoError(t, json.Unmarshal([]byte(output), &result))
		require.False(t, result.OK)
		require.Contains(t, result.Hint, hint)
	}

	// 本机模型服务没启动：用 discard 端口 9，本机通常没有服务，连接会立即被拒绝。
	// 不复用关掉的 httptest 端口，并行测试时它可能被别的监听占用。
	setupTestProfile(t, "http://127.0.0.1:9")
	output, err := run(t, "ai", "test")
	require.Error(t, err)
	require.Contains(t, output, "本机模型服务可能没有启动")

	// 远程厂商没有 Key：错误里已经说了怎么设置，不重复给建议。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEEPSEEK_API_KEY", "")
	_, err = run(t, "ai", "add", "ds", "--provider", "deepseek", "--model", "deepseek-chat")
	require.NoError(t, err)
	output, err = run(t, "ai", "test")
	require.Error(t, err)
	require.Contains(t, output, "数据：没有发出请求", "读取 Key 时就失败了，没有发出请求")
	require.NotContains(t, output, "发送给 DeepSeek")
	require.Contains(t, output, "DeepSeek 需要 Key，用 sec config ai set-key 设置")
	require.NotContains(t, output, "建议：")

	// 没有配置、ai 段无效。
	t.Setenv("HOME", t.TempDir())
	_, err = run(t, "ai", "test")
	require.ErrorContains(t, err, "还没有模型配置")
	writeConfig(t, home, `{"ai":{"default":"x"}}`)
	t.Setenv("HOME", home)
	_, err = run(t, "ai", "test")
	require.ErrorIs(t, err, config.ErrInvalidAI)
}

func TestAITestHint(t *testing.T) {
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
		"403":           {&llm.APIError{Status: 403}, remote, "Key 不对"},
		"400":           {&llm.APIError{Status: 400}, remote, "检查模型名和协议"},
		"402":           {&llm.APIError{Status: 402}, remote, "余额不足"},
		"本机连接被拒绝":       {urlErr(&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}), local, "ollama serve"},
		"Windows 连接被拒绝": {urlErr(&net.OpError{Op: "dial", Err: &os.SyscallError{Syscall: "connectex", Err: wsaeConnRefused}}), local, "ollama serve"},
		"远程连接被拒绝":       {urlErr(&net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}), remote, "检查网络、代理"},
		"超时":            {fmt.Errorf("x：%w", context.DeadlineExceeded), remote, "内没有回复"},
		"没有 Key":        {llm.ErrMissingKey, remote, ""},
		"其他":            {errors.New("boom"), remote, ""},
	} {
		got := aiTestHint(c.err, c.profile)
		if c.want == "" {
			require.Empty(t, got, name)
		} else {
			require.Contains(t, got, c.want, name)
		}
	}
}

func TestPrintAITest(t *testing.T) {
	var out bytes.Buffer
	printAITest(&out, aiTestResult{Name: "ds", Provider: "DeepSeek", Model: "deepseek-chat", ReplyModel: "deepseek-chat", Sent: true, OK: true, LatencyMS: 1234, Reply: "OK", Tools: aiToolsSupported})
	text := out.String()
	require.Contains(t, text, "数据：发送给 DeepSeek")
	require.Contains(t, text, "连接正常：1.2 秒，回复")
	require.NotContains(t, text, "实际模型", "和配置的模型一样时不重复")
	require.True(t, strings.HasSuffix(text, "工具调用：支持\n"))
}

func TestConfigAITestToolsUnknown(t *testing.T) {
	// 带工具的请求遇到限流、5xx：没测出来，不说模型不支持工具调用。
	for _, status := range []int{429, 503} {
		model := &fakeModel{replies: []fakeModelReply{okReply("OK"), {status, `{"error":{"message":"busy"}}`}}}
		setupTestProfile(t, model.serve(t))
		output, err := run(t, "ai", "test")
		require.NoError(t, err, status)
		require.Contains(t, output, "工具调用：没测出来，带工具的请求临时失败", status)
		require.NotContains(t, output, "可能不支持工具调用", status)
	}
}

func TestConfigAITestTruncated(t *testing.T) {
	// 思考模型把输出上限用在推理上，回复被截断且为空：连接正常，但提醒。
	model := &fakeModel{replies: []fakeModelReply{
		{200, `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":""}}]}`}, pingReply,
	}}
	setupTestProfile(t, model.serve(t))
	output, err := run(t, "ai", "test")
	require.NoError(t, err)
	require.Contains(t, output, "连接正常")
	require.Contains(t, output, "注意：回复被截断或为空（停止原因 length）")
}

func TestConnRefused(t *testing.T) {
	require.True(t, connRefused(&net.OpError{Err: syscall.ECONNREFUSED}))
	require.True(t, connRefused(&net.OpError{Err: &os.SyscallError{Err: wsaeConnRefused}}), "Windows 的 WSAECONNREFUSED")
	require.False(t, connRefused(&net.OpError{Err: syscall.ECONNRESET}))
	require.False(t, connRefused(errors.New("x")))
}

func TestPrintAITestNotSent(t *testing.T) {
	var out bytes.Buffer
	printAITest(&out, aiTestResult{Name: "ds", Provider: "DeepSeek", Error: "没有 API Key"})
	require.Contains(t, out.String(), "数据：没有发出请求")
	require.NotContains(t, out.String(), "发送给")
}
