package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/utils"
)

// ProbeTimeout 是连接测试时每个请求的超时。思考模型即使只回 OK 也可能想一会儿。
const ProbeTimeout = 2 * time.Minute

// probeMaxTokens 是测试请求的输出上限：要给思考模型留出推理的余量，否则可能在回答前就被截断。
const probeMaxTokens = 1024

// pingTool 是测试工具调用用的工具，不会真的执行。
var pingTool = Tool{
	Name:        "ping",
	Description: "连接测试用的工具，原样返回 text。",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
}

// 工具调用测试的结果。
const (
	ToolsSupported = "supported"  // 调用了 ping
	ToolsNotCalled = "not_called" // 请求成功但没有调用工具
	ToolsError     = "error"      // 带工具的请求被拒绝，比如模型不支持工具调用
	ToolsUnknown   = "unknown"    // 带工具的请求临时失败（限流、5xx、超时、网络中断），没测出来
)

// ProbeUsage 是测试请求的 token 用量。
type ProbeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ProbeResult 是一次连接测试的结果，sec config ai test --json 和 Desktop 的测试接口原样输出。
type ProbeResult struct {
	Name        string     `json:"name"`
	Provider    string     `json:"provider"`
	API         string     `json:"api"`
	BaseURL     string     `json:"base_url"`
	Model       string     `json:"model"`
	Key         string     `json:"key"`
	Local       bool       `json:"local"`
	OK          bool       `json:"ok"`
	Error       string     `json:"error,omitempty"`
	Hint        string     `json:"hint,omitempty"`
	Sent        bool       `json:"sent"` // 是否真的发出了请求；读取 Key、创建客户端时就失败的不会发出
	LatencyMS   int64      `json:"latency_ms"`
	Warning     string     `json:"warning,omitempty"`
	ReplyModel  string     `json:"reply_model,omitempty"`
	Reply       string     `json:"reply,omitempty"`
	Usage       ProbeUsage `json:"usage"`
	Tools       string     `json:"tools,omitempty"`
	ToolsDetail string     `json:"tools_detail,omitempty"`
}

// ProbeProfile 用一个配置发两个测试请求：一句固定的「请只回复 OK」检查地址、Key 和模型，再让模型调用测试工具，
// 判断是否支持工具调用。不含用户数据，启用 AI 功能前也可以用。结果和失败原因都写在返回值里，CLI 和 Desktop 共用。
// 配置必须存在；getenv 一般传 os.Getenv。
func ProbeProfile(ctx context.Context, ai *config.AIConfig, name string, secret *config.AISecret, getenv func(string) string) ProbeResult {
	p := ai.Profiles[name]
	provider, _ := config.LookupAIProvider(p.Provider)
	// 只读取一次 Key：显示文字和创建客户端用同一个结果。
	key, keyErr := ai.ResolveKey(secret, name, getenv)
	result := ProbeResult{
		Name: name, Provider: provider.Name, API: p.EffectiveAPI(), BaseURL: p.EffectiveBaseURL(), Model: p.Model,
		Key: ai.DescribeKey(name, key, keyErr), Local: p.IsLocal(),
	}
	fail := func(err error) ProbeResult {
		result.Error, result.Hint = err.Error(), probeHint(err, p)
		return result
	}
	if keyErr != nil {
		return fail(keyErr)
	}
	client, err := New(p, key.Value)
	if err != nil {
		return fail(err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	start := time.Now()
	result.Sent = true
	resp, err := client.Chat(reqCtx, Request{
		System:    "这是 sec 的连接测试。",
		Messages:  []Message{{Role: RoleUser, Text: "请只回复 OK。"}},
		MaxTokens: probeMaxTokens,
	})
	if err != nil {
		return fail(err)
	}
	result.OK, result.LatencyMS = true, time.Since(start).Milliseconds()
	result.ReplyModel, result.Reply = resp.Model, utils.TruncateRunes(resp.Message.Text, 80)
	result.Usage = ProbeUsage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
	// 连接是通的，但思考模型可能把输出上限都用在推理上，回复被截断或为空。
	switch {
	// 关闭思考没有生效时最需要知道：用户正是为了避免推理用完输出上限才关闭的。
	case resp.ThinkingIgnored:
		result.Warning = ThinkingIgnoredHint(name)
	case resp.StopReason == StopMaxTokens || strings.TrimSpace(resp.Message.Text) == "":
		result.Warning = fmt.Sprintf("回复被截断或为空（停止原因 %s），模型可能需要更大的输出上限，或者模型本身有问题", resp.RawStopReason)
	}

	toolCtx, cancelTool := context.WithTimeout(ctx, ProbeTimeout)
	defer cancelTool()
	resp, err = client.Chat(toolCtx, Request{
		System:    "这是 sec 的连接测试。需要时调用工具。",
		Messages:  []Message{{Role: RoleUser, Text: "请调用 ping 工具，参数 text 填 sec。"}},
		Tools:     []Tool{pingTool},
		MaxTokens: probeMaxTokens,
	})
	var apiErr *APIError
	switch {
	// 只有厂商明确拒绝（如 400、422）才可能是不支持工具调用；网络错误、被取消、可重试的状态码都算没测出来。
	case err != nil && (toolCtx.Err() != nil || !errors.As(err, &apiErr) || apiErr.Retryable()):
		result.Tools, result.ToolsDetail = ToolsUnknown, err.Error()
	case err != nil:
		result.Tools, result.ToolsDetail = ToolsError, err.Error()
	case slices.ContainsFunc(resp.Message.ToolCalls, func(c ToolCall) bool { return c.Name == pingTool.Name }):
		result.Tools = ToolsSupported
	default:
		result.Tools, result.ToolsDetail = ToolsNotCalled, "停止原因 "+resp.RawStopReason
	}
	return result
}

// probeHint 根据失败原因给出下一步怎么做，用词不限定 CLI 或 Desktop。
func probeHint(err error, p config.AIProfile) string {
	var apiErr *APIError
	switch {
	case errors.Is(err, ErrMissingKey):
		return "" // 错误信息里已经说了怎么设置
	case errors.Is(err, config.ErrSecretMissing), errors.Is(err, config.ErrKeyUndecryptable), errors.Is(err, config.ErrSecretCorrupt):
		return "保存的 Key 无法解密，请重新设置 Key"
	case errors.Is(err, config.ErrSecretPermissions):
		return "secret.key 权限过宽，按错误信息里的提示运行 chmod 600"
	case errors.As(err, &apiErr):
		switch {
		case apiErr.Status == 401 || apiErr.Status == 403:
			return "Key 不对或没有权限，请重新设置 Key；国际区域和中国区域的 Key 不通用"
		case apiErr.Status == 404:
			return "地址或模型名可能不对，检查接口地址（base_url）和模型"
		case apiErr.Status == 400:
			return "请求被拒绝，检查模型名和接口协议（api）是否和厂商一致"
		case apiErr.Status == 402 || apiErr.Status == 429:
			return "被限流或余额不足，稍后再试或检查账户"
		case apiErr.Status >= 500:
			return "厂商服务暂时不可用，稍后再试"
		}
	case connRefused(err) && p.IsLocal():
		return "本机模型服务可能没有启动，比如先运行 ollama serve"
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("%v 内没有回复，模型可能太慢或地址不通", ProbeTimeout)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "连不上接口地址，检查网络、代理或接口地址（base_url）"
	}
	return ""
}

// connRefused 判断是不是连接被拒绝。Windows 上是 WSAECONNREFUSED（10061），和 syscall.ECONNREFUSED 不是同一个值。
func connRefused(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == syscall.ECONNREFUSED || errno == wsaeConnRefused)
}

// wsaeConnRefused 是 Windows 的 WSAECONNREFUSED。
const wsaeConnRefused = syscall.Errno(10061)
