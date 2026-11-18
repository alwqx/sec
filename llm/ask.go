package llm

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alwqx/sec/aitool"
)

// 问答（sec ask，以后 Desktop 对话面板复用）：模型按问题调用 aitool 的只读工具，工具结果发回后继续，直到给出回答。
// 设计见 docs/ai.md 的「问答：sec ask 与 Desktop 对话面板」。

//go:embed prompts/ask.md
var askPrompt string

// AskPromptVersion 是问答提示词的版本，写进结果。
const AskPromptVersion = "ask-v2"

const (
	// MaxAskRounds 是一次问答最多请求模型的轮数，包括给出回答的那一轮。
	MaxAskRounds = 8
	// maxAskQuestionRunes 是问题的字数上限。
	maxAskQuestionRunes = 500
	// maxAskCodeRunes 是证券上下文（代码或名称）的字数上限。
	maxAskCodeRunes = 32
	// lastRoundNote 和工具结果一起发给模型，下一轮是最后一轮：让模型用已有的数据回答，不丢掉已经查到的结果。
	lastRoundNote = "下一轮是最后一轮，不能再调用工具：请用已经取到的数据回答，没有取到的数据在回答里说明。"
)

// askToolTimeout 是单次工具调用的超时，和 sec mcp 一致。测试时调小。
var askToolTimeout = 30 * time.Second

// ErrAsk 表示问答本身失败（问题不能用、模型没有给出回答），不是模型接口或网络的错误。
var ErrAsk = errors.New("问答失败")

// ToolStep 是问答里的一次工具调用，CLI 逐行显示，--json 原样输出。
type ToolStep struct {
	Round int             `json:"round"` // 第几轮请求模型时调用的，从 1 开始
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
	// AsOf 是工具结果的截止日期；结果里的数据不是同一个时点时为空，各自的日期在工具结果里。
	AsOf      string `json:"as_of,omitempty"`
	Source    string `json:"source,omitempty"`
	Error     string `json:"error,omitempty"` // 调用失败的原因，原样交给了模型
	ElapsedMS int64  `json:"elapsed_ms"`
}

// Answer 是一次问答的结果。
type Answer struct {
	Question string     `json:"question"`
	Code     string     `json:"code,omitempty"` // --code 带入的证券
	Text     string     `json:"text"`
	Steps    []ToolStep `json:"steps"`
	Check    Check      `json:"check"` // 和问题、本次成功的工具结果核对
	// DataDates 是成功的工具调用的「工具 截止日期」，去重，按调用顺序；没有截止日期的结果不列。
	DataDates []string `json:"data_dates"`
	// Notes 是代码给出的说明，比如用到的数据截止日期不同、模型没有调用工具。
	Notes          []string  `json:"notes"`
	Disclaimer     string    `json:"disclaimer"` // 工具结果自带的免责声明加上通用说明，由代码附上
	Truncated      bool      `json:"truncated"`
	TruncationHint string    `json:"truncation_hint,omitempty"`
	Rounds         int       `json:"rounds"` // 请求模型的轮数
	Model          string    `json:"model"`
	Profile        string    `json:"profile"`
	PromptVersion  string    `json:"prompt_version"`
	CreatedAt      time.Time `json:"created_at"`
}

// Asker 用 aitool 的工具回答问题。不缓存：行情等数据随时在变，同一个问题每次都重新查询。
type Asker struct {
	Client Client
	Model  Model
	Tools  *aitool.Registry
	// OnStep 在每次工具调用结束后调用，CLI 用它在 stderr 显示进度；为 nil 时不调用。
	OnStep func(ToolStep)
}

// NormalizeAskCode 去掉证券上下文首尾的空白，检查长度和换行。CLI 在读模型配置之前调用。
func NormalizeAskCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if utf8.RuneCountInString(code) > maxAskCodeRunes || strings.ContainsAny(code, "\r\n") {
		return "", fmt.Errorf("%w：证券代码或名称不能超过 %d 字，也不能换行", ErrAsk, maxAskCodeRunes)
	}
	return code, nil
}

// NormalizeAskQuestion 合并问题里的空白，检查是否为空或太长。CLI 在读模型配置之前调用。
func NormalizeAskQuestion(question string) (string, error) {
	question = strings.Join(strings.Fields(question), " ")
	switch n := utf8.RuneCountInString(question); {
	case n == 0:
		return "", fmt.Errorf("%w：问题不能为空", ErrAsk)
	case n > maxAskQuestionRunes:
		return "", fmt.Errorf("%w：问题超过 %d 字，请缩短", ErrAsk, maxAskQuestionRunes)
	}
	return question, nil
}

// Ask 回答问题。code 是可选的证券上下文（代码或名称），会写进问题里，由模型决定怎么查。
// 最多请求模型 MaxAskRounds 轮；ctx 取消时立即停止，正在执行的工具调用也会取消。
func (a *Asker) Ask(ctx context.Context, question, code string) (*Answer, error) {
	question, err := NormalizeAskQuestion(question)
	if err != nil {
		return nil, err
	}
	if code, err = NormalizeAskCode(code); err != nil {
		return nil, err
	}
	prompt := question
	if code != "" {
		prompt = fmt.Sprintf("问题涉及的证券：%s（代码或名称，不确定时先用 search_security 查）\n\n%s", code, question)
	}
	var tools []Tool
	for _, t := range a.Tools.Tools() {
		tools = append(tools, Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	messages := []Message{{Role: RoleUser, Text: prompt}}
	steps := []ToolStep{}
	var results []*aitool.Result
	for round := 1; round <= MaxAskRounds; round++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, err := a.Client.Chat(ctx, Request{System: systemPrompt + "\n\n" + askPrompt, Messages: messages, Tools: tools})
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("%w：模型没有返回回复，请重试或更换模型", ErrAsk)
		}
		if len(resp.Message.ToolCalls) == 0 {
			return a.answer(resp, question, code, round, messages, steps, results)
		}
		if round == MaxAskRounds {
			break
		}
		// 部分本机模型调用工具时停止原因仍是 stop，以有没有工具调用为准。
		messages = append(messages, resp.Message)
		reply := Message{Role: RoleUser}
		for _, call := range resp.Message.ToolCalls {
			result, step, content, err := a.call(ctx, round, call)
			if err != nil {
				return nil, err
			}
			steps = append(steps, step)
			if result != nil {
				results = append(results, result)
			}
			reply.ToolResults = append(reply.ToolResults, ToolResult{CallID: call.ID, Content: content, IsError: step.Error != ""})
			if a.OnStep != nil {
				a.OnStep(step)
			}
		}
		if round == MaxAskRounds-1 {
			reply.Text = lastRoundNote
		}
		messages = append(messages, reply)
	}
	return nil, fmt.Errorf("%w：请求模型 %d 轮后仍在调用工具，没有给出回答，请把问题问得具体一些或换一个模型", ErrAsk, MaxAskRounds)
}

// call 执行一次工具调用，返回结果（失败时为 nil）、调用记录和发给模型的内容。工具失败不算问答失败，
// 原因交给模型，让它改参数或在回答里说明；只有 ctx 取消时返回 error。
func (a *Asker) call(ctx context.Context, round int, call ToolCall) (*aitool.Result, ToolStep, string, error) {
	step := ToolStep{Round: round, Tool: call.Name, Input: jsonInput(call.Input)}
	start := time.Now()
	fail := func(message string) (*aitool.Result, ToolStep, string, error) {
		step.Error, step.ElapsedMS = message, time.Since(start).Milliseconds()
		return nil, step, message, nil
	}
	tool, ok := a.Tools.Lookup(call.Name)
	if !ok {
		var names []string
		for _, t := range a.Tools.Tools() {
			names = append(names, t.Name)
		}
		return fail(fmt.Sprintf("没有这个工具：%s，可用的工具：%s", call.Name, strings.Join(names, "、")))
	}
	toolCtx, cancel := context.WithTimeout(ctx, askToolTimeout)
	defer cancel()
	result, err := tool.Call(toolCtx, call.Input)
	switch {
	case ctx.Err() != nil:
		return nil, step, "", ctx.Err()
	case err != nil && toolCtx.Err() != nil:
		return fail(fmt.Sprintf("%s 超过 %d 秒没有返回", call.Name, int(askToolTimeout.Seconds())))
	case err != nil:
		if !aitool.IsInputError(err) {
			slog.ErrorContext(ctx, "tool call failed", "tool", call.Name, "error", err)
		}
		return fail(err.Error())
	case result == nil:
		return fail(call.Name + " 没有返回结果")
	}
	// 不转义 HTML：「MD&A」「S&P 500」转义成 \u0026 后，数字核对会把 0026 当作来源，也多费 token。和 sec mcp 一致。
	var content bytes.Buffer
	encoder := json.NewEncoder(&content)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		return fail(fmt.Sprintf("%s 的结果无法编码：%v", call.Name, err))
	}
	step.AsOf, step.Source, step.ElapsedMS = result.AsOf, result.Source, time.Since(start).Milliseconds()
	return result, step, strings.TrimSuffix(content.String(), "\n"), nil
}

// jsonInput 返回可以写进 JSON 的参数：OpenAI 兼容接口返回的不合法参数保留成了字符串，这里编码成 JSON 字符串。
func jsonInput(input json.RawMessage) json.RawMessage {
	if len(input) == 0 {
		return emptyObject
	}
	if json.Valid(input) {
		return input
	}
	data, _ := json.Marshal(string(input))
	return data
}

// answer 检查最后一轮的回复，核对数字，附上说明和免责声明。
func (a *Asker) answer(resp *Response, question, code string, round int, messages []Message, steps []ToolStep, results []*aitool.Result) (*Answer, error) {
	text := strings.TrimSpace(resp.Message.Text)
	switch {
	case resp.StopReason == StopRefusal:
		return nil, fmt.Errorf("%w：模型拒绝了这个问题，换一个模型配置再试", ErrAsk)
	case text == "" && resp.StopReason == StopMaxTokens:
		return nil, fmt.Errorf("%w：回答超过输出上限被截断%s", ErrAsk, a.Model.TruncationAdvice(resp, "回答超出上限"))
	case text == "":
		return nil, fmt.Errorf("%w：模型没有给出回答（停止原因 %s）", ErrAsk, resp.RawStopReason)
	case resp.StopReason != StopEnd && resp.StopReason != StopMaxTokens:
		return nil, fmt.Errorf("%w：模型没有正常完成回答（停止原因 %s）", ErrAsk, resp.RawStopReason)
	}
	dates := dataDates(steps)
	answer := &Answer{
		Question: question, Code: code, Text: text, Steps: steps, Check: Verify(text, askSources(messages)...), DataDates: dates,
		Notes: askNotes(steps, dates), Disclaimer: askDisclaimer(results), Truncated: resp.StopReason == StopMaxTokens,
		Rounds: round, Model: resp.Model, Profile: a.Model.Profile, PromptVersion: AskPromptVersion, CreatedAt: time.Now(),
	}
	if answer.Truncated {
		answer.TruncationHint = a.Model.TruncationAdvice(resp, "回答超出上限")
	}
	if answer.Model == "" {
		answer.Model = a.Model.Name
	}
	return answer, nil
}

// askSources 是数字核对的来源：用户消息的文字和成功的工具结果。出错的结果不算，错误说明里常有模型自己给的参数
// （如「begin（2027-01-01）不能晚于 end」），算进来源会让模型编的数字也能核对上。
func askSources(messages []Message) []string {
	var out []string
	for _, m := range messages {
		if m.Role == RoleUser && m.Text != "" {
			out = append(out, m.Text)
		}
		for _, r := range m.ToolResults {
			if !r.IsError {
				out = append(out, r.Content)
			}
		}
	}
	return out
}

// dataDates 是成功的工具调用的「工具 截止日期」，去重，按调用顺序。
func dataDates(steps []ToolStep) []string {
	dates := []string{}
	for _, step := range steps {
		if label := step.Tool + " " + step.AsOf; step.Error == "" && step.AsOf != "" && !slices.Contains(dates, label) {
			dates = append(dates, label)
		}
	}
	return dates
}

// askNotes 是代码给出的说明：模型没有调用工具时回答没有用到 sec 的数据；用到的数据截止日期不同时逐项列出，
// 提醒不要把不同时点的数据放在一起比较。dates 见 dataDates。
func askNotes(steps []ToolStep, dates []string) []string {
	notes := []string{}
	if len(steps) == 0 {
		return append(notes, "模型没有调用工具，回答没有用到 sec 查询的数据。")
	}
	distinct := map[string]bool{}
	for _, step := range steps {
		if step.Error == "" && step.AsOf != "" {
			distinct[step.AsOf] = true
		}
	}
	if len(distinct) > 1 {
		notes = append(notes, "回答用到的数据截止日期不同（"+strings.Join(dates, "、")+"），不同日期的数据不能直接放在一起比较或计算。")
	}
	if slices.ContainsFunc(steps, func(s ToolStep) bool { return s.Error != "" }) {
		notes = append(notes, "有工具调用失败，回答可能缺少这部分数据，失败原因见调用记录。")
	}
	return notes
}

// askDisclaimer 是工具结果自带的免责声明（去重）加上通用说明。
func askDisclaimer(results []*aitool.Result) string {
	var parts []string
	for _, r := range results {
		if r.Disclaimer != "" && !slices.Contains(parts, r.Disclaimer) {
			parts = append(parts, r.Disclaimer)
		}
	}
	return strings.Join(append(parts, defaultDisclaimer), "")
}
