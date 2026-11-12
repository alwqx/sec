package llm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"

	"github.com/alwqx/sec/config"
)

// openAIClient 调用 OpenAI 兼容的 Chat Completions（POST {base}/chat/completions），覆盖 OpenAI、DeepSeek、通义千问、
// Kimi、智谱和本地 Ollama。各家的差异按厂商处理，见 maxTokensField 和推理内容的处理。
type openAIClient struct {
	name     string // 厂商显示名，用于错误信息
	provider string // 厂商 id
	url      string
	model    string
	key      string // 本机模型可以为空，为空时不发 Authorization
	// extra 是按配置加到每个请求里的参数，比如关闭思考，见 openAIExtraParams。
	extra map[string]any
	// thinkingOff 表示配置关闭了思考，用于发现被模型忽略的情况，见 Response.ThinkingIgnored。
	thinkingOff bool
}

type openAIMessage struct {
	Role    string  `json:"role"`
	Content *string `json:"content"` // 只有工具调用的助手消息是 null
	// ReasoningContent 是 DeepSeek、Kimi 等思考模型的推理内容，要原样发回，见 openAIMessages。
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	ToolCalls        []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON 字符串
	} `json:"function"`
}

type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type openAIChoice struct {
	Message      openAIReply `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens            int `json:"prompt_tokens"`
	CompletionTokens        int `json:"completion_tokens"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

type openAIResponse struct {
	Model   string         `json:"model"`
	Choices []openAIChoice `json:"choices"`
	Usage   openAIUsage    `json:"usage"`
}

// openAIReply 是回复里的消息。content 通常是字符串，部分兼容网关和多模态模型返回内容块数组，见 replyText。
type openAIReply struct {
	Content          json.RawMessage  `json:"content"`
	ReasoningContent string           `json:"reasoning_content"`
	ToolCalls        []openAIToolCall `json:"tool_calls"`
}

// replyText 取出回复的文字：字符串原样返回；内容块数组按顺序拼接 text 块；null 或缺失时为空。
func replyText(content json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var text string
		err := json.Unmarshal(trimmed, &text)
		return text, err
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(trimmed, &parts); err != nil {
		return "", fmt.Errorf("content 既不是字符串也不是内容块数组：%w", err)
	}
	var text strings.Builder
	for _, part := range parts {
		if part.Type == "text" || part.Type == "output_text" {
			text.WriteString(part.Text)
		}
	}
	return text.String(), nil
}

// maxTokensField 是输出上限的参数名：OpenAI 的新模型只接受 max_completion_tokens，其他兼容接口用 max_tokens。
func (c *openAIClient) maxTokensField() string {
	if c.provider == "openai" {
		return "max_completion_tokens"
	}
	return "max_tokens"
}

// openAIExtraParams 是按配置加到每个请求里的参数。关闭思考的参数各家不同，能否关闭由 config.AIProfile.ThinkingOffSupported
// 判断，配置校验已拒绝其他厂商；这里遇到未知厂商不加参数，不猜。两处的一致性由 TestThinkingOffSupportedSendsParams 检查。
func openAIExtraParams(profile config.AIProfile) map[string]any {
	if profile.Thinking != config.AIThinkingOff {
		return nil
	}
	switch profile.Provider {
	case "deepseek", "zhipu":
		return map[string]any{"thinking": map[string]string{"type": "disabled"}}
	case "qwen":
		return map[string]any{"enable_thinking": false}
	}
	return nil
}

func (c *openAIClient) Chat(ctx context.Context, req Request) (*Response, error) {
	body, headers, err := c.request(req)
	if err != nil {
		return nil, err
	}
	var resp openAIResponse
	if err := postJSON(ctx, c.url, headers, body, &resp, c.name, parseOpenAIError); err != nil {
		return nil, err
	}
	return c.response(ctx, resp)
}

func (c *openAIClient) request(req Request) (map[string]any, http.Header, error) {
	if err := validateRequest(req); err != nil {
		return nil, nil, err
	}
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: &req.System})
	}
	for _, m := range req.Messages {
		messages = append(messages, c.openAIMessages(m)...)
	}
	body := map[string]any{"model": c.model, "messages": messages, c.maxTokensField(): maxTokens(req)}
	if len(req.Tools) > 0 {
		tools := make([]openAITool, 0, len(req.Tools))
		for _, t := range req.Tools {
			tool := openAITool{Type: "function"}
			tool.Function.Name, tool.Function.Description, tool.Function.Parameters = t.Name, t.Description, toolSchema(t)
			tools = append(tools, tool)
		}
		body["tools"] = tools
	}
	maps.Copy(body, c.extra)
	headers := http.Header{}
	if c.key != "" {
		headers.Set("Authorization", "Bearer "+c.key)
	}
	return body, headers, nil
}

func (c *openAIClient) response(ctx context.Context, resp openAIResponse) (*Response, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("%s 的回复里没有内容（choices 为空）", c.name)
	}
	choice := resp.Choices[0]
	text, err := replyText(choice.Message.Content)
	if err != nil {
		return nil, fmt.Errorf("%s 的回复：%w", c.name, err)
	}
	msg := Message{Role: RoleAssistant, Text: text}
	if choice.Message.ReasoningContent != "" {
		msg.reasoning, msg.reasoningProvider = choice.Message.ReasoningContent, c.provider
	}
	for _, call := range choice.Message.ToolCalls {
		id := call.ID
		if id == "" {
			// 部分本机模型不给 id。随机生成，不同轮的调用不会重名，结果发回时对得上就行。
			id = randomCallID()
		}
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: id, Name: call.Function.Name, Input: toolInput(call.Function.Arguments)})
	}
	stop := openAIStopReason(choice.FinishReason)
	// 有的兼容接口（如 Ollama）调用工具时 finish_reason 仍是 stop，以是否有工具调用为准。
	if len(msg.ToolCalls) > 0 {
		stop = StopToolUse
	}
	if stop == StopOther {
		slog.WarnContext(ctx, "模型返回了未处理的停止原因", "provider", c.name, "finish_reason", choice.FinishReason)
	}
	// 关闭参数按厂商文档发出，但只思考的模型（如部分 qwq、GLM-4.7）可能忽略它；从回复里发现这种情况，不让用户以为已经关闭。
	ignored := c.thinkingOff && (choice.Message.ReasoningContent != "" || resp.Usage.CompletionTokensDetails.ReasoningTokens > 0)
	if ignored {
		slog.WarnContext(ctx, "配置关闭了思考，但模型仍在思考，这个模型可能不支持关闭", "provider", c.name, "model", resp.Model)
	}
	return &Response{
		Message: msg, StopReason: stop, RawStopReason: choice.FinishReason, Model: resp.Model,
		Usage:           Usage{InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens},
		ThinkingIgnored: ignored,
	}, nil
}

// openAIMessages 把一条消息换成 Chat Completions 的消息：每个工具结果是一条 tool 消息，放在用户文字之前。
// 没有「出错」标记，出错的结果在内容前加说明。
//
// 推理内容按 DeepSeek 和 Kimi 的要求原样发回：请求带工具时，DeepSeek 要求发回之前所有助手消息的
// reasoning_content，否则返回 400；Kimi 要求原样保留每条助手消息的推理内容。只发回给产生它的厂商，
// 别的厂商可能不认这个字段。
func (c *openAIClient) openAIMessages(m Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(m.ToolResults)+1)
	for _, r := range m.ToolResults {
		content := r.Content
		if r.IsError {
			content = "工具调用出错：" + content
		}
		out = append(out, openAIMessage{Role: "tool", ToolCallID: r.CallID, Content: &content})
	}
	if m.Role == RoleAssistant {
		msg := openAIMessage{Role: RoleAssistant}
		if m.Text != "" || len(m.ToolCalls) == 0 {
			text := m.Text
			msg.Content = &text
		}
		if m.reasoningProvider == c.provider {
			msg.ReasoningContent = m.reasoning
		}
		for _, call := range m.ToolCalls {
			tc := openAIToolCall{ID: call.ID, Type: "function"}
			tc.Function.Name, tc.Function.Arguments = call.Name, toolArguments(call.Input)
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
		return append(out, msg)
	}
	if m.Text != "" || len(m.ToolResults) == 0 {
		text := m.Text
		out = append(out, openAIMessage{Role: RoleUser, Content: &text})
	}
	return out
}

// toolInput 把参数字符串换成 JSON。空字符串当作没有参数；不是合法 JSON 时保存成 JSON 字符串，
// 交给工具参数校验报错，让模型看到错误后修正，而不是在这里丢掉。发回时见 toolArguments。
func toolInput(arguments string) json.RawMessage {
	if arguments == "" {
		return emptyObject
	}
	if json.Valid([]byte(arguments)) {
		return json.RawMessage(arguments)
	}
	quoted, _ := json.Marshal(arguments)
	return quoted
}

// toolArguments 是发回时的参数字符串：toolInput 保存成字符串的不合法参数还原成模型原来给的样子，不多套一层引号。
func toolArguments(input json.RawMessage) string {
	trimmed := bytes.TrimSpace(input)
	if len(trimmed) == 0 {
		return "{}"
	}
	if trimmed[0] == '"' {
		var original string
		if json.Unmarshal(trimmed, &original) == nil {
			return original
		}
	}
	return string(input)
}

func randomCallID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "call_" + hex.EncodeToString(b)
}

func openAIStopReason(reason string) string {
	switch reason {
	case "stop":
		return StopEnd
	case "length":
		return StopMaxTokens
	case "tool_calls", "function_call":
		return StopToolUse
	case "content_filter":
		return StopRefusal
	}
	return StopOther
}

// parseOpenAIError 从 {"error":{"message":"...","type":"...","code":...}} 里取出错误类型和信息；没有 type 时用 code。
func parseOpenAIError(payload []byte) (string, string) {
	var body struct {
		Error struct {
			Message string          `json:"message"`
			Type    string          `json:"type"`
			Code    json.RawMessage `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return "", ""
	}
	kind := body.Error.Type
	if kind == "" && len(body.Error.Code) > 0 && string(body.Error.Code) != "null" {
		var code string
		if json.Unmarshal(body.Error.Code, &code) != nil {
			code = string(body.Error.Code)
		}
		kind = code
	}
	return kind, body.Error.Message
}
