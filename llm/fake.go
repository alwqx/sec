package llm

import (
	"context"
	"errors"
	"sync"
)

// Fake 是测试用的客户端：按顺序返回预先给定的回复，并记下收到的请求。CI 不调用真实模型，用它测试工具调用循环和提示词组装。
type Fake struct {
	mu sync.Mutex
	// Replies 依次返回。每一项是一个回复或一个错误。
	Replies []FakeReply
	// Requests 是收到的请求，按顺序。
	Requests []Request
}

// FakeReply 是 Fake 的一次回复。
type FakeReply struct {
	Response *Response
	Err      error
}

// ErrFakeExhausted 表示 Fake 的回复用完了。
var ErrFakeExhausted = errors.New("llm.Fake：没有更多预设回复")

// ErrFakeEmptyReply 表示预设的回复既没有 Response 也没有 Err。
var ErrFakeEmptyReply = errors.New("llm.Fake：预设回复既没有 Response 也没有 Err")

// FakeText 返回一个正常结束的文字回复。
func FakeText(text string) FakeReply {
	return FakeReply{Response: &Response{Message: Message{Role: RoleAssistant, Text: text}, StopReason: StopEnd}}
}

// FakeToolCalls 返回一个调用工具的回复。
func FakeToolCalls(calls ...ToolCall) FakeReply {
	return FakeReply{Response: &Response{Message: Message{Role: RoleAssistant, ToolCalls: calls}, StopReason: StopToolUse}}
}

func (f *Fake) Chat(ctx context.Context, req Request) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, req)
	if len(f.Replies) == 0 {
		return nil, ErrFakeExhausted
	}
	reply := f.Replies[0]
	f.Replies = f.Replies[1:]
	// 写成 FakeReply{} 时报清楚的错误，不让被测代码拿到 nil 回复后 panic。
	if reply.Response == nil && reply.Err == nil {
		return nil, ErrFakeEmptyReply
	}
	return reply.Response, reply.Err
}
