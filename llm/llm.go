// Package llm 是调用大模型的抽象，CLI 和 Desktop 共用：Anthropic Messages API 和 OpenAI 兼容的 Chat Completions，
// 都用 net/http 直接调用，不引入厂商 SDK。StreamingClient 提供流式能力。设计见 docs/ai.md 的「模型接入（llm）」。
//
// 本包只负责一轮请求：把 Request 换成各协议的格式发出去，把回复换回 Response。工具调用的循环、数字核对和提示词在别处。
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/utils"
)

// 角色。
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// 停止原因，两种协议统一成这几种。
const (
	StopEnd       = "end"        // 正常结束
	StopMaxTokens = "max_tokens" // 输出达到 MaxTokens 被截断
	StopToolUse   = "tool_use"   // 要调用工具，执行后把结果发回去继续
	StopRefusal   = "refusal"    // 模型或厂商的安全策略拒绝回答
	StopOther     = "other"      // 其他，原始值见 Response.RawStopReason
)

// DefaultMaxTokens 是没有指定 MaxTokens 时的输出上限。非流式请求不宜过大，否则容易超时。
const DefaultMaxTokens = 16000

// requestTimeout 是一次请求的超时（含读取回复）。非流式请求要等模型生成完，长回答可能要几分钟。
const requestTimeout = 5 * time.Minute

// maxResponseBytes 是读取回复的上限，防止异常的服务把内存占满。测试时调小。
var maxResponseBytes = 16 << 20

// Client 是一个模型配置对应的客户端。
type Client interface {
	// Chat 发一轮请求，返回文本或工具调用。
	Chat(ctx context.Context, req Request) (*Response, error)
}

// Tool 是提供给模型的工具：名字、说明和参数的 JSON Schema（顶层是 object）。
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ToolCall 是模型要调用的一个工具。
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage // 参数 JSON 对象
}

// ToolResult 是一个工具调用的结果，CallID 对应 ToolCall.ID。
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// Message 是对话里的一条消息。用户消息有 Text 或 ToolResults；助手消息有 Text 和 ToolCalls。
type Message struct {
	Role        string
	Text        string
	ToolCalls   []ToolCall
	ToolResults []ToolResult

	// raw 是助手消息在原协议里的原始内容，由产生它的客户端设置。发回同一种协议时原样发送，
	// 不重新拼装：Anthropic 的 thinking 块绑定了对话，改动或丢掉会被拒绝或丢失推理。
	// rawSig 记下收到时的文字和工具调用，调用方改过时按改过的内容重新拼装。
	raw         json.RawMessage
	rawProto    string
	rawProvider string
	rawSig      string
	// reasoning 是 OpenAI 兼容接口返回的推理内容（reasoning_content），只发回给产生它的厂商。
	reasoning         string
	reasoningProvider string
}

// signature 是消息的文字和工具调用，用来判断收到后有没有被改过。
func signature(m Message) string {
	data, _ := json.Marshal(struct {
		Text  string
		Calls []ToolCall
	}{m.Text, m.ToolCalls})
	return string(data)
}

// Request 是一轮请求。
type Request struct {
	System    string
	Messages  []Message
	Tools     []Tool
	MaxTokens int // 0 表示 DefaultMaxTokens
}

// Usage 是这轮请求的 token 用量，厂商没有返回时为 0。
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// Response 是模型的回复。Message 可以直接追加到 Request.Messages 里继续对话。
type Response struct {
	Message       Message
	StopReason    string // StopEnd 等
	RawStopReason string // 厂商返回的原始值
	Model         string // 厂商返回的实际模型
	Usage         Usage
}

// APIError 是模型接口返回的错误。不含 Key。
type APIError struct {
	Provider   string // 厂商显示名
	Status     int
	Type       string // 厂商的错误类型，如 rate_limit_error，可能为空
	Message    string
	RetryAfter time.Duration // 厂商给出的重试等待时间，没有时为 0
}

func (e *APIError) Error() string {
	text := fmt.Sprintf("%s 接口返回 HTTP %d", e.Provider, e.Status)
	if e.Type != "" {
		text += "（" + e.Type + "）"
	}
	if e.Message != "" {
		text += "：" + e.Message
	}
	return text
}

// Retryable 判断稍后重试是否可能成功：限流、超时、过载和服务端错误可以，参数错误、认证失败不行。
func (e *APIError) Retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status == http.StatusRequestTimeout || e.Status >= 500
}

// ErrMissingKey 表示需要 Key 的接口没有 Key。
var ErrMissingKey = errors.New("没有 API Key")

// New 按模型配置创建客户端。key 来自 config.AIConfig.ResolveKey，本机模型可以为空。
func New(profile config.AIProfile, key string) (Client, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	provider, _ := config.LookupAIProvider(profile.Provider)
	// 本机模型（Ollama、LM Studio 等）和自定义厂商（自建网关可能不需要认证）可以不用 Key，其他厂商没有 Key 时
	// 直接报错，和设错了 Key 的 401 区分开。
	if key == "" && !profile.IsLocal() && profile.Provider != config.AIProviderCustom {
		return nil, fmt.Errorf("%w：%s 需要 Key，用 sec config ai set-key 设置，或设置环境变量", ErrMissingKey, provider.Name)
	}
	switch profile.EffectiveAPI() {
	case config.AIAPIAnthropic:
		// SDK 的惯例是地址不带 /v1；用户填了 OpenAI 风格、以 /v1 结尾的地址时不再重复。
		path := "v1/messages"
		if base, err := url.Parse(profile.EffectiveBaseURL()); err == nil && strings.HasSuffix(strings.TrimRight(base.Path, "/"), "/v1") {
			path = "messages"
		}
		endpoint, err := joinURL(profile.EffectiveBaseURL(), path)
		if err != nil {
			return nil, err
		}
		return &anthropicClient{name: provider.Name, provider: profile.Provider, url: endpoint, model: profile.Model, key: key}, nil
	case config.AIAPIOpenAI:
		endpoint, err := joinURL(profile.EffectiveBaseURL(), "chat/completions")
		if err != nil {
			return nil, err
		}
		return &openAIClient{name: provider.Name, provider: profile.Provider, url: endpoint, model: profile.Model, key: key, extra: openAIExtraParams(profile)}, nil
	}
	return nil, fmt.Errorf("不支持的接口协议：%s", profile.EffectiveAPI())
}

// joinURL 在地址的路径后面加上 path，保留查询参数（如 Azure 等网关要的 ?api-version=）。
func joinURL(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("接口地址无效：%w", err)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + path
	u.RawPath = ""
	return u.String(), nil
}

// postJSON 发送 JSON 请求，把成功的回复解码到 out；HTTP 错误换成 APIError，由 parseError 从回复里取出厂商的错误信息。
func postJSON(ctx context.Context, url string, headers http.Header, body any, out any, provider string, parseError func([]byte) (string, string)) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	headers.Set("Content-Type", "application/json")
	resp, err := utils.MakeRequest(ctx, http.MethodPost, url, headers, bytes.NewReader(data), requestTimeout)
	if err != nil {
		return fmt.Errorf("请求 %s 失败：%w", provider, err)
	}
	defer resp.Body.Close()
	// 多读 1 字节，超过上限时明确报错，而不是截断后报「不是有效的 JSON」。
	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)+1))
	if err != nil {
		return fmt.Errorf("读取 %s 的回复失败：%w", provider, err)
	}
	if len(payload) > maxResponseBytes {
		return fmt.Errorf("%s 的回复超过 %s，不读取", provider, utils.HumanByte(float64(maxResponseBytes)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Provider: provider, Status: resp.StatusCode}
		apiErr.Type, apiErr.Message = parseError(payload)
		if apiErr.Message == "" {
			apiErr.Message = utils.TruncateRunes(strings.TrimSpace(string(payload)), 300)
		}
		apiErr.RetryAfter = retryAfter(resp.Header, time.Now())
		return apiErr
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%s 的回复不是有效的 JSON：%w", provider, err)
	}
	return nil
}

// retryAfter 解析重试等待时间：retry-after-ms（毫秒），或 Retry-After 的秒数或 HTTP 日期。没有或无法解析时为 0。
func retryAfter(header http.Header, now time.Time) time.Duration {
	if ms, err := strconv.ParseFloat(header.Get("retry-after-ms"), 64); err == nil && ms > 0 {
		return time.Duration(ms * float64(time.Millisecond))
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}

// IsRetryable 判断一次请求失败后稍后重试是否可能成功：APIError 按 Retryable；网络错误里只有超时、
// 连接被重置或中断、读到一半断开算。连接被拒绝、证书错误、域名找不到重试也不会好，不算。
// ctx 已取消或超时（调用方不再等）时不重试。
func IsRetryable(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}
	// *url.Error 本身就实现了 net.Error，不能只看类型，要看是不是超时。
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE)
}

// validateRequest 在本地检查空消息：没有内容的消息厂商会拒绝，但报错不说是哪一条。只有原始内容
// （如只有 thinking 块的被截断回复）或推理内容的助手消息也算有内容，换成协议格式后各适配器再检查一次。
func validateRequest(req Request) error {
	for i, m := range req.Messages {
		if m.Text == "" && len(m.ToolCalls) == 0 && len(m.ToolResults) == 0 && len(m.raw) == 0 && m.reasoning == "" {
			return emptyMessage(i, m)
		}
	}
	return nil
}

func emptyMessage(i int, m Message) error {
	return fmt.Errorf("第 %d 条消息（%s）是空的：没有文字、工具调用或工具结果", i+1, m.Role)
}

// toolSchema 是工具的参数 Schema，没有设置时用空的 object：两种接口都不接受 null。
func toolSchema(t Tool) json.RawMessage {
	if len(bytes.TrimSpace(t.InputSchema)) == 0 || string(bytes.TrimSpace(t.InputSchema)) == "null" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return t.InputSchema
}

func maxTokens(req Request) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return DefaultMaxTokens
}

// emptyObject 是没有参数的工具调用的输入。
var emptyObject = json.RawMessage(`{}`)
