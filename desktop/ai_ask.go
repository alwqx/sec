package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/cmd/macro"
	"github.com/alwqx/sec/llm"
)

// askTimeout 是一次问答的整体时限，和 sec ask 一致。
const askTimeout = 10 * time.Minute

// maxAskConversations 是内存里最多保留的对话数，超过时丢掉最久没用的。
const maxAskConversations = 20

// conversationID 是浏览器生成的对话 id。
var conversationID = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// askBusyWait 是同一个对话上一个问题还没结束时最多等多久。页面取消后马上追问时，上一个请求已经随连接断开取消，
// 只是还在收尾（停下正在执行的工具调用），等它结束就可以继续。
var askBusyWait = 5 * time.Second

// errAskBusy 表示同一个对话上一个问题还没回答完。
var errAskBusy = errors.New("这个对话上一个问题还在回答，请等它完成或取消")

// askConversations 是对话面板的对话，只在内存里，关闭 Desktop 即清空。每个对话保存最近 llm.MaxAskHistory 轮的
// llm.AskTurn（问答和本机留作核对来源的工具结果）：历史在服务端，浏览器只提交对话 id，不能伪造前面的回答或工具结果。
type askConversations struct {
	mu    sync.Mutex
	items map[string]*askConversation
}

type askConversation struct {
	turns []llm.AskTurn
	// done 在回答问题时不为 nil，回答结束时关闭。
	done chan struct{}
	used time.Time
}

// begin 开始回答对话 id 里的一个问题，返回前几轮；没有这个对话时新建一个（新对话，或者已经被丢掉的旧对话）。
// 同一个对话一次只回答一个问题：上一个问题还没结束时最多等 askBusyWait，ctx 取消或等不到时返回 errAskBusy。
// 回答结束后必须调用 finish。
func (c *askConversations) begin(ctx context.Context, id string) ([]llm.AskTurn, error) {
	ctx, cancel := context.WithTimeout(ctx, askBusyWait)
	defer cancel()
	for {
		turns, done := c.tryBegin(id)
		if done == nil {
			return turns, nil
		}
		select {
		case <-done:
		case <-ctx.Done():
			return nil, errAskBusy
		}
	}
}

// tryBegin 在对话空闲时开始回答，返回前几轮；正在回答时返回它的 done。
func (c *askConversations) tryBegin(id string) ([]llm.AskTurn, chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = map[string]*askConversation{}
	}
	conv, ok := c.items[id]
	if !ok {
		conv = &askConversation{}
		c.items[id] = conv
		c.evict(id)
	}
	if conv.done != nil {
		return nil, conv.done
	}
	conv.done, conv.used = make(chan struct{}), time.Now()
	return slices.Clone(conv.turns), nil
}

// finish 结束一个问题；answer 不为 nil（回答已经送到页面）时把这一轮记下来，只留最近 llm.MaxAskHistory 轮。
func (c *askConversations) finish(id string, answer *llm.Answer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	conv, ok := c.items[id]
	if !ok {
		return
	}
	close(conv.done)
	conv.done, conv.used = nil, time.Now()
	if answer != nil {
		conv.turns = append(conv.turns, answer.Turn())
		conv.turns = conv.turns[max(0, len(conv.turns)-llm.MaxAskHistory):]
	}
}

// evict 在对话超过 maxAskConversations 个时丢掉最久没用、没在回答的对话，不丢刚建的 keep。
func (c *askConversations) evict(keep string) {
	for len(c.items) > maxAskConversations {
		oldest := ""
		for id, conv := range c.items {
			if id != keep && conv.done == nil && (oldest == "" || conv.used.Before(c.items[oldest].used)) {
				oldest = id
			}
		}
		if oldest == "" {
			return
		}
		delete(c.items, oldest)
	}
}

// handleAIAsk 是 Desktop 对话面板的一次问答，和 sec ask 共用 llm.Asker 和 aitool 的工具。只接收问题、可选的证券上下文、
// 临时模型配置和可选的对话 id，不接受浏览器提交的历史回答、工具结果或提示词。带对话 id 时是追问：带上这个对话在服务端
// 保存的前几轮（见 askConversations），回答送到页面后记下这一轮；不带时单独回答。问题、证券和对话 id 在读取模型配置之前校验。
// 请求 Accept: text/event-stream 时逐条发送工具调用（step），最后发送 done 或 error；否则返回 JSON 的 llm.Answer。
func (s *desktopServer) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question     string `json:"question"`
		Code         string `json:"code"`
		Profile      string `json:"profile"`
		Conversation string `json:"conversation"`
	}
	if !s.readAIRequest(w, r, &in) {
		return
	}
	question, err := llm.NormalizeAskQuestion(in.Question)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	code, err := llm.NormalizeAskCode(in.Code)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Conversation != "" && !conversationID.MatchString(in.Conversation) {
		writeError(w, http.StatusBadRequest, "对话 id 不合法")
		return
	}
	client, model, _, ok := s.openAIModel(w, r, in.Profile)
	if !ok {
		return
	}
	asker := &llm.Asker{Client: client, Model: model, Tools: aitool.Default(macro.AISource())}
	// delivered 是已经送到页面的回答，结束时记进对话；失败、取消或没送到时为 nil，前几轮不变。
	var delivered *llm.Answer
	if in.Conversation != "" {
		if asker.History, err = s.asks.begin(r.Context(), in.Conversation); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		defer func() { s.asks.finish(in.Conversation, delivered) }()
	}
	ctx, cancel := context.WithTimeout(r.Context(), askTimeout)
	defer cancel()
	steps := 0
	if !wantsAIStream(r) {
		asker.OnStep = func(llm.ToolStep) { steps++ }
		answer, err := asker.Ask(ctx, question, code)
		if err != nil {
			if r.Context().Err() == nil {
				status, message := askFailure(ctx, err, steps == 0)
				writeError(w, status, message)
			}
			return
		}
		writeJSON(w, answer)
		if r.Context().Err() == nil {
			delivered = answer
		}
		return
	}
	stream := newAIStream(w, ctx)
	defer func() {
		if recover() != nil {
			_ = stream.send("error", map[string]any{"message": "问答异常，请稍后重试", "status": http.StatusInternalServerError})
		}
		stream.close()
	}()
	if stream.send("status", map[string]string{"text": "正在请求模型…"}) != nil {
		return
	}
	// 写出失败（窗口关闭、客户端断开）时 stream 取消 ctx，问答随之停止。
	asker.OnStep = func(step llm.ToolStep) {
		steps++
		_ = stream.send("step", step)
	}
	answer, err := asker.Ask(stream.ctx, question, code)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, message := askFailure(ctx, err, steps == 0)
		_ = stream.send("error", map[string]any{"message": message, "status": status})
		return
	}
	if stream.send("done", answer) == nil {
		delivered = answer
	}
}

// askFailure 返回问答失败的状态码和提示。模型错误只给 llm.PublicError 的部分（厂商错误正文可能回显 Key）；
// 还没调用过工具时请求就被拒绝（400、422），提示模型可能不支持工具调用，和 sec ask 一致。
func askFailure(ctx context.Context, err error, firstRound bool) (int, string) {
	if ctx.Err() != nil {
		return http.StatusGatewayTimeout, fmt.Sprintf("问答超过 %d 分钟没有完成，请把问题问得具体一些或选择其他模型", int(askTimeout.Minutes()))
	}
	var apiErr *llm.APIError
	if public, ok := llm.PublicError(err); ok {
		message := public + "，请检查模型配置或稍后重试"
		if firstRound && errors.As(err, &apiErr) && (apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusUnprocessableEntity) {
			message += "；模型可能不支持工具调用，可以在设置页「AI 模型」测试连接"
		}
		return http.StatusBadGateway, message
	}
	// 问答自己的错误（模型拒绝、没有给出回答、超过轮数）由代码生成，可以直接显示。
	if errors.Is(err, llm.ErrAsk) {
		return http.StatusBadGateway, err.Error()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "模型响应超时，请稍后重试或选择其他模型"
	}
	return http.StatusBadGateway, "问答失败，请重试或选择其他模型"
}
