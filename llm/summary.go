package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// 公告和申报摘要共用的部分：分块、并发请求、按停止原因说明错误、去掉 Markdown 围栏。

// summaryConcurrency 是同时请求模型的分块数上限。
const summaryConcurrency = 4

// summaryMaxTokens 是分块和合并请求的输出上限。思考模型的推理也计入上限，8000 不够长公告的一块。
// 按实测 deepseek-v4-pro 约 90 token/s，16000 约需 3 分钟，在 5 分钟的请求超时以内；更慢的模型可能先超时。
const summaryMaxTokens = 16000

// summaryCall 是摘要的一次模型请求。
type summaryCall struct {
	client Client
	model  Model
	system string
	err    error  // 错误前缀，如 ErrSummary
	what   string // 文档名称，用于错误说明，如「公告」
}

// chat 请求一次分块摘要或合并（merge），按停止原因给出可操作的错误；返回去掉围栏的回复正文和实际模型名。
func (c summaryCall) chat(ctx context.Context, input any, merge bool) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", "", err
	}
	resp, err := c.client.Chat(ctx, Request{System: c.system, Messages: []Message{{Role: RoleUser, Text: string(data)}}, MaxTokens: summaryMaxTokens})
	if err != nil {
		return "", "", err
	}
	switch {
	case resp == nil:
		return "", "", fmt.Errorf("%w：模型没有返回回复，请重试或更换模型", c.err)
	case resp.StopReason == StopMaxTokens:
		stage := "分块摘要"
		if merge {
			stage = "合并分块摘要"
		}
		return "", "", fmt.Errorf("%w：%s时回复超过 %d token 的输出上限被截断%s", c.err, stage, summaryMaxTokens, c.model.TruncationAdvice(resp, "摘要正文超出上限，"+c.what+"可能太长"))
	case len(resp.Message.ToolCalls) > 0:
		return "", "", fmt.Errorf("%w：模型调用了工具，%s摘要不提供工具，请重试或更换模型", c.err, c.what)
	case resp.StopReason != StopEnd:
		reason := ""
		if resp.RawStopReason != "" {
			reason = "（停止原因 " + resp.RawStopReason + "）"
		}
		return "", "", fmt.Errorf("%w：模型拒绝或未正常结束%s，请重试或更换模型", c.err, reason)
	}
	return stripJSONFence(resp.Message.Text), resp.Model, nil
}

// summarizeChunks 并发处理各块，结果按块的顺序返回，避免长文档串行调用超过整体时限。
// 任一块失败就取消其余请求。模型名取第一个返回了模型名的块，没有则用 fallback。
func summarizeChunks[C, R any](ctx context.Context, chunks []C, fallback string, run func(context.Context, C) (R, string, error)) ([]R, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	parts := make([]R, len(chunks))
	models := make([]string, len(chunks))
	sem := make(chan struct{}, summaryConcurrency)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for i, chunk := range chunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				once.Do(func() { firstErr = ctx.Err() })
				return
			}
			part, model, err := run(ctx, chunk)
			if err != nil {
				once.Do(func() { firstErr = err; cancel() })
				return
			}
			parts[i], models[i] = part, model
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, "", firstErr
	}
	model := fallback
	for _, m := range models {
		if m != "" {
			model = m
			break
		}
	}
	return parts, model, nil
}

// chunkText 把带位置（页码、章节）的文本按 size 个字符分块，保留位置；单个位置太长时按 rune 分开，
// 避免切坏中文字符。text 取出文字，with 用一段文字生成同一位置的新元素。
func chunkText[T any](items []T, size int, text func(T) string, with func(T, string) T) [][]T {
	var chunks [][]T
	var chunk []T
	n := 0
	for _, item := range items {
		runes := []rune(strings.TrimSpace(text(item)))
		for len(runes) > 0 {
			if n == size {
				chunks, chunk, n = append(chunks, chunk), nil, 0
			}
			k := min(len(runes), size-n)
			chunk = append(chunk, with(item, string(runes[:k])))
			runes, n = runes[k:], n+k
		}
	}
	if len(chunk) > 0 {
		chunks = append(chunks, chunk)
	}
	return chunks
}

// summaryNumberSource 准备数字核对的来源：原文是自然语言，逗号可能是千分位；仅合并完整的三位分组，
// 不改变共用 JSON 数字核对的规则。
func summaryNumberSource(text string) string {
	matches := mergeThousands(text, numberPattern.FindAllStringIndex(text, -1))
	var out strings.Builder
	last := 0
	for _, loc := range matches {
		out.WriteString(text[last:loc[0]])
		out.WriteString(strings.ReplaceAll(text[loc[0]:loc[1]], ",", ""))
		last = loc[1]
	}
	out.WriteString(text[last:])
	return out.String()
}

// stripJSONFence 去掉部分兼容模型加的 Markdown 围栏（``` 或 ```json，大小写和 CRLF 均可），
// 不允许围栏以外的文字。
func stripJSONFence(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if !strings.HasPrefix(text, "```") || !strings.HasSuffix(text, "```") || len(text) < 6 {
		return text
	}
	body := strings.TrimSuffix(text[3:], "```")
	newline := strings.IndexByte(body, '\n')
	if newline < 0 {
		return text
	}
	if lang := strings.TrimSpace(body[:newline]); lang != "" && !strings.EqualFold(lang, "json") {
		return text
	}
	return strings.TrimSpace(body[newline+1:])
}
