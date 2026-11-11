package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/alwqx/sec/internal/sse"
)

type anthropicStreamChunk struct {
	Type         string                     `json:"type"`
	Index        int                        `json:"index"`
	Message      anthropicResponse          `json:"message"`
	ContentBlock map[string]json.RawMessage `json:"content_block"`
	Delta        struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		Thinking   string `json:"thinking"`
		Signature  string `json:"signature"`
		JSON       string `json:"partial_json"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		Input  *int `json:"input_tokens"`
		Output *int `json:"output_tokens"`
	} `json:"usage"`
}

func appendBlockText(block map[string]json.RawMessage, field, delta string) error {
	var previous string
	if raw := block[field]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &previous); err != nil {
			return err
		}
	}
	block[field], _ = json.Marshal(previous + delta)
	return nil
}

func (c *anthropicClient) ChatStream(ctx context.Context, req Request, emit func(StreamEvent) error) (*Response, error) {
	body, headers, err := c.request(req)
	if err != nil {
		return nil, err
	}
	body.Stream = true
	stream, err := openStream(ctx, c.url, headers, body, c.name, parseAnthropicError)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	decoder := sse.NewDecoder(stream, int64(maxResponseBytes))
	var response anthropicResponse
	type blockState struct {
		raw    map[string]json.RawMessage
		input  strings.Builder
		closed bool
	}
	var blocks []*blockState
	started, thinking := false, false
	for {
		event, err := decoder.Next()
		if err != nil {
			return nil, streamReadError(ctx, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var chunk anthropicStreamChunk
		if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
			return nil, errors.New("模型流式事件不是有效的 JSON")
		}
		switch chunk.Type {
		case "error":
			return nil, streamAPIError(c.name, []byte(event.Data), parseAnthropicError)
		case "message_start":
			if started {
				return nil, errors.New("模型重复开始了一条流式消息")
			}
			started, response = true, chunk.Message
		case "content_block_start":
			if !started || chunk.Index != len(blocks) || len(blocks) >= 1024 || chunk.ContentBlock == nil {
				return nil, errors.New("模型内容块顺序无效")
			}
			blocks = append(blocks, &blockState{raw: chunk.ContentBlock})
			var text string
			if json.Unmarshal(chunk.ContentBlock["text"], &text) == nil && text != "" {
				if err := emitStream(ctx, emit, StreamEvent{Type: StreamText, Text: text}); err != nil {
					return nil, err
				}
			}
		case "content_block_delta", "content_block_stop":
			if chunk.Index < 0 || chunk.Index >= len(blocks) || blocks[chunk.Index].closed {
				return nil, errors.New("模型内容块编号无效")
			}
			block := blocks[chunk.Index]
			if chunk.Type == "content_block_stop" {
				if block.input.Len() > 0 {
					if !json.Valid([]byte(block.input.String())) {
						return nil, errors.New("模型工具参数不是完整的 JSON")
					}
					block.raw["input"] = json.RawMessage(block.input.String())
				}
				block.closed = true
				continue
			}
			switch chunk.Delta.Type {
			case "text_delta":
				if err := appendBlockText(block.raw, "text", chunk.Delta.Text); err != nil {
					return nil, err
				}
				if err := emitStream(ctx, emit, StreamEvent{Type: StreamText, Text: chunk.Delta.Text}); err != nil {
					return nil, err
				}
			case "input_json_delta":
				block.input.WriteString(chunk.Delta.JSON)
			case "thinking_delta":
				if err := appendBlockText(block.raw, "thinking", chunk.Delta.Thinking); err != nil {
					return nil, err
				}
				if !thinking {
					thinking = true
					if err := emitStream(ctx, emit, StreamEvent{Type: StreamThinking}); err != nil {
						return nil, err
					}
				}
			case "signature_delta":
				if err := appendBlockText(block.raw, "signature", chunk.Delta.Signature); err != nil {
					return nil, err
				}
			}
		case "message_delta":
			if chunk.Delta.StopReason != "" {
				response.StopReason = chunk.Delta.StopReason
			}
			if chunk.Usage.Input != nil {
				response.Usage.InputTokens = *chunk.Usage.Input
			}
			if chunk.Usage.Output != nil {
				response.Usage.OutputTokens = *chunk.Usage.Output
			}
		case "message_stop":
			if !started || response.StopReason == "" {
				return nil, streamReadError(ctx, io.ErrUnexpectedEOF)
			}
			content := make([]map[string]json.RawMessage, 0, len(blocks))
			for _, block := range blocks {
				if !block.closed {
					return nil, streamReadError(ctx, io.ErrUnexpectedEOF)
				}
				content = append(content, block.raw)
			}
			response.Content, err = json.Marshal(content)
			if err != nil {
				return nil, err
			}
			return c.response(ctx, response)
		}
	}
}
