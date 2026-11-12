package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// anthropicVersion 是 Messages API 的版本。这个版本不接受 temperature、top_p、top_k，本包也不发送这些参数。
const anthropicVersion = "2026-10-21"

const protoAnthropic = "anthropic"

// anthropicClient 调用 Anthropic Messages API（POST {base}/v1/messages），也用于厂商提供的 Anthropic 兼容接口。
type anthropicClient struct {
	name     string // 厂商显示名，用于错误信息
	provider string // 厂商 id：原始内容（含 thinking 签名）只发回给产生它的厂商
	url      string
	model    string
	key      string // 本机的兼容接口可以为空，为空时不发 x-api-key
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
	Stream    bool               `json:"stream,omitempty"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // 内容块数组
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicBlock 是内容块。只解析用到的字段；thinking 等其他块原样保存在 Message.raw 里。
type anthropicBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthropicResponse struct {
	Model      string          `json:"model"`
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (c *anthropicClient) Chat(ctx context.Context, req Request) (*Response, error) {
	body, headers, err := c.request(req)
	if err != nil {
		return nil, err
	}
	var resp anthropicResponse
	if err := postJSON(ctx, c.url, headers, body, &resp, c.name, parseAnthropicError); err != nil {
		return nil, err
	}
	return c.response(ctx, resp)
}

func (c *anthropicClient) request(req Request) (anthropicRequest, http.Header, error) {
	if err := validateRequest(req); err != nil {
		return anthropicRequest{}, nil, err
	}
	body := anthropicRequest{Model: c.model, MaxTokens: maxTokens(req), System: req.System, Messages: make([]anthropicMessage, 0, len(req.Messages))}
	for _, t := range req.Tools {
		body.Tools = append(body.Tools, anthropicTool{Name: t.Name, Description: t.Description, InputSchema: toolSchema(t)})
	}
	for i, m := range req.Messages {
		content, empty, err := c.content(m)
		if err != nil {
			return anthropicRequest{}, nil, err
		}
		// 别的厂商产生、只有推理内容的消息重新拼装后可能是空的。
		if empty {
			return anthropicRequest{}, nil, emptyMessage(i, m)
		}
		body.Messages = append(body.Messages, anthropicMessage{Role: m.Role, Content: content})
	}
	headers := http.Header{}
	if c.key != "" {
		headers.Set("x-api-key", c.key)
	}
	headers.Set("anthropic-version", anthropicVersion)
	return body, headers, nil
}

func (c *anthropicClient) response(ctx context.Context, resp anthropicResponse) (*Response, error) {
	var blocks []anthropicBlock
	if len(resp.Content) > 0 && string(resp.Content) != "null" {
		if err := json.Unmarshal(resp.Content, &blocks); err != nil {
			return nil, err
		}
	}
	msg := Message{Role: RoleAssistant}
	var text []string
	reasoned := false
	for _, b := range blocks {
		switch b.Type {
		case "thinking", "redacted_thinking":
			reasoned = true
		case "text":
			text = append(text, b.Text)
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: b.ID, Name: b.Name, Input: objectOrEmpty(b.Input)})
		}
	}
	msg.Text = strings.Join(text, "")
	// 只保存非空的原始内容：拒绝或截断时 content 可能是空的，原样发回会被拒绝。
	if len(blocks) > 0 {
		msg.raw, msg.rawProto, msg.rawProvider, msg.rawSig = resp.Content, protoAnthropic, c.provider, signature(msg)
	}
	stop := anthropicStopReason(resp.StopReason)
	if stop == StopOther {
		slog.WarnContext(ctx, "模型返回了未处理的停止原因", "provider", c.name, "stop_reason", resp.StopReason)
	}
	return &Response{
		Message: msg, StopReason: stop, RawStopReason: resp.StopReason, Model: resp.Model,
		Usage:    Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens},
		Reasoned: reasoned,
	}, nil
}

// content 把消息换成内容块，empty 表示没有任何块。同一厂商的 Anthropic 接口产生、没被改过的助手消息原样发回
// （含 thinking 块）；换了厂商（签名对不上）或调用方改了文字、工具调用时按文字和工具调用重新拼装，不再带 thinking 块。
// 工具结果放在同一条用户消息里。
func (c *anthropicClient) content(m Message) (json.RawMessage, bool, error) {
	if m.Role == RoleAssistant && m.rawProto == protoAnthropic && m.rawProvider == c.provider && len(m.raw) > 0 && m.rawSig == signature(m) {
		return m.raw, false, nil
	}
	blocks := make([]anthropicBlock, 0, len(m.ToolResults)+len(m.ToolCalls)+1)
	for _, r := range m.ToolResults {
		blocks = append(blocks, anthropicBlock{Type: "tool_result", ToolUseID: r.CallID, Content: r.Content, IsError: r.IsError})
	}
	if m.Text != "" {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: m.Text})
	}
	for _, call := range m.ToolCalls {
		// 参数必须是对象；OpenAI 兼容接口返回的不合法参数保存成了字符串，这里发空对象。
		blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: call.ID, Name: call.Name, Input: objectOrEmpty(call.Input)})
	}
	data, err := json.Marshal(blocks)
	return data, len(blocks) == 0, err
}

// objectOrEmpty 返回 JSON 对象形式的参数，不是对象时返回空对象。
func objectOrEmpty(input json.RawMessage) json.RawMessage {
	if trimmed := bytes.TrimSpace(input); len(trimmed) > 0 && trimmed[0] == '{' {
		return input
	}
	return emptyObject
}

func anthropicStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return StopEnd
	case "max_tokens":
		return StopMaxTokens
	case "tool_use":
		return StopToolUse
	case "refusal":
		return StopRefusal
	}
	return StopOther
}

// parseAnthropicError 从 {"type":"error","error":{"type":"...","message":"..."}} 里取出错误类型和信息。
func parseAnthropicError(payload []byte) (string, string) {
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return "", ""
	}
	return body.Error.Type, body.Error.Message
}
