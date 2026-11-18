package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/cmd/macro"
	"github.com/alwqx/sec/llm"
)

// askTimeout 是一次问答的整体时限，和 sec ask 一致。
const askTimeout = 10 * time.Minute

// handleAIAsk 是 Desktop 对话面板的一次问答，和 sec ask 共用 llm.Asker 和 aitool 的工具。只接收问题、可选的证券上下文和
// 临时模型配置，不接受浏览器提交的历史回答、工具结果或提示词：每个问题单独回答。问题和证券在读取模型配置之前校验。
// 请求 Accept: text/event-stream 时逐条发送工具调用（step），最后发送 done 或 error；否则返回 JSON 的 llm.Answer。
func (s *desktopServer) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Question string `json:"question"`
		Code     string `json:"code"`
		Profile  string `json:"profile"`
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
	client, model, _, ok := s.openAIModel(w, r, in.Profile)
	if !ok {
		return
	}
	asker := &llm.Asker{Client: client, Model: model, Tools: aitool.Default(macro.AISource())}
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
	_ = stream.send("done", answer)
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
