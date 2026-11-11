package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/alwqx/sec/internal/sse"
)

type openAIStreamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   json.RawMessage `json:"content"`
			Reasoning string          `json:"reasoning_content"`
			Refusal   string          `json:"refusal"`
			ToolCalls []struct {
				Index int `json:"index"`
				openAIToolCall
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *openAIUsage    `json:"usage"`
	Error json.RawMessage `json:"error"`
}

func (c *openAIClient) ChatStream(ctx context.Context, req Request, emit func(StreamEvent) error) (*Response, error) {
	body, headers, err := c.request(req)
	if err != nil {
		return nil, err
	}
	body["stream"] = true
	// 自定义网关可能不认识 stream_options；原生兼容厂商按文档请求用量。
	if c.provider != "custom" {
		body["stream_options"] = map[string]any{"include_usage": true}
	}
	stream, err := openStream(ctx, c.url, headers, body, c.name, parseOpenAIError)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	decoder := sse.NewDecoder(stream, int64(maxResponseBytes))
	var text, reasoning strings.Builder
	var model, finish string
	var usage openAIUsage
	type callParts struct {
		call      openAIToolCall
		arguments strings.Builder
	}
	calls := map[int]*callParts{}
	thinking, refused := false, false
	for {
		event, err := decoder.Next()
		if err != nil {
			return nil, streamReadError(ctx, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if event.Data == "[DONE]" {
			if finish == "" {
				return nil, streamReadError(ctx, io.ErrUnexpectedEOF)
			}
			if refused {
				finish = "content_filter"
			}
			content, _ := json.Marshal(text.String())
			reply := openAIReply{Content: content, ReasoningContent: reasoning.String()}
			indices := make([]int, 0, len(calls))
			for index := range calls {
				indices = append(indices, index)
			}
			slices.Sort(indices)
			for _, index := range indices {
				part := calls[index]
				part.call.Function.Arguments = part.arguments.String()
				reply.ToolCalls = append(reply.ToolCalls, part.call)
			}
			return c.response(ctx, openAIResponse{Model: model, Choices: []openAIChoice{{Message: reply, FinishReason: finish}}, Usage: usage})
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return nil, errors.New("模型流式事件不是有效的 JSON")
		}
		if event.Type == "error" || (len(chunk.Error) > 0 && string(chunk.Error) != "null") {
			return nil, streamAPIError(c.name, []byte(event.Data), parseOpenAIError)
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		for _, choice := range chunk.Choices {
			if choice.Index != 0 {
				continue
			}
			delta, err := replyText(choice.Delta.Content)
			if err != nil {
				return nil, err
			}
			if delta != "" {
				text.WriteString(delta)
				if err := emitStream(ctx, emit, StreamEvent{Type: StreamText, Text: delta}); err != nil {
					return nil, err
				}
			}
			if choice.Delta.Refusal != "" {
				refused = true
			}
			if choice.Delta.Reasoning != "" {
				reasoning.WriteString(choice.Delta.Reasoning)
				if !thinking {
					thinking = true
					if err := emitStream(ctx, emit, StreamEvent{Type: StreamThinking}); err != nil {
						return nil, err
					}
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				if delta.Index < 0 || delta.Index >= 1024 {
					return nil, errors.New("工具调用编号超出限制")
				}
				part := calls[delta.Index]
				if part == nil {
					part = &callParts{}
					calls[delta.Index] = part
				}
				if delta.ID != "" {
					part.call.ID = delta.ID
				}
				if delta.Type != "" {
					part.call.Type = delta.Type
				}
				part.call.Function.Name += delta.Function.Name
				part.arguments.WriteString(delta.Function.Arguments)
			}
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}
	}
}
