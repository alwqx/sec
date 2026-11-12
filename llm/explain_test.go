package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/config"
	"github.com/stretchr/testify/require"
)

// valuationResult 是 get_valuation 结果的简化版：写明金额单位是亿，带免责声明。
func valuationResult(asOf string) *aitool.Result {
	r := aitool.NewResult("get_valuation", "东方财富估值与业绩报表", map[string]any{
		"ex_code": "SH600519", "name": "贵州茅台", "price": 1523.45, "market_cap": 19137.2,
		"pe": map[string]any{"current": 21.34, "percentile": 18.6, "median": 28.9},
	})
	r.AsOf = asOf
	r.Notes = append(r.Notes, "金额单位是亿元，比率和分位单位是 %")
	r.Disclaimer = "估值结果由规则和假设机械计算，不构成投资建议。"
	return r
}

// stubFetch 返回固定的估值结果，并记下收到的参数。
func stubFetch(asOf *string, inputs *[]string) func(context.Context, json.RawMessage) (*aitool.Result, error) {
	return func(_ context.Context, input json.RawMessage) (*aitool.Result, error) {
		*inputs = append(*inputs, string(input))
		return valuationResult(*asOf), nil
	}
}

const explainText = "估值水平：当前 PE 21.34 倍，市值约 1.91 万亿元，规则判定估值偏低。\n历史位置：PE 处于 18.6% 分位，低于中位数 28.9。\n数据口径：截至 2026-11-06，股息率 3.8%。"

func TestValuationExplain(t *testing.T) {
	asOf, inputs := "2026-11-06", []string{}
	fake := &Fake{Replies: []FakeReply{FakeText("  " + explainText + "\n")}}
	explainer := &ValuationExplainer{Client: fake, Model: Model{Profile: "ds", API: "openai", BaseURL: "https://x", Name: "m"}, CacheDir: t.TempDir(), Fetch: stubFetch(&asOf, &inputs)}

	got, err := explainer.Explain(context.Background(), json.RawMessage(`{"code":"600519"}`))
	require.NoError(t, err)
	require.Equal(t, []string{`{"code":"600519"}`}, inputs, "参数原样交给 get_valuation")
	require.Equal(t, "SH600519", got.ExCode)
	require.Equal(t, "贵州茅台", got.Name)
	require.Equal(t, "2026-11-06", got.AsOf)
	require.Equal(t, explainText, got.Text, "去掉首尾空白")
	require.Equal(t, "估值结果由规则和假设机械计算，不构成投资建议。", got.Disclaimer, "免责声明来自工具结果，由代码附上")
	require.Equal(t, "m", got.Model, "厂商没返回模型名时用配置的模型")
	require.Equal(t, "ds", got.Profile)
	require.Equal(t, ValuationPromptVersion, got.PromptVersion)
	require.False(t, got.Cached)
	require.False(t, got.Truncated)

	// 数字核对：1.91 万亿对得上 19137.2 亿，编造的股息率 3.8% 对不上。
	require.Equal(t, []string{"3.8%"}, unsourcedTexts(got.Check))

	// 单轮请求，不开放工具；系统提示词有核心约束，用户消息是任务说明和工具结果。
	req := fake.Requests[0]
	require.Empty(t, req.Tools)
	require.Equal(t, explainMaxTokens, req.MaxTokens)
	require.Contains(t, req.System, "只能使用工具结果里的数字")
	require.Contains(t, req.System, "不给买入、卖出")
	require.Len(t, req.Messages, 1)
	require.Contains(t, req.Messages[0].Text, "估值水平")
	require.Contains(t, req.Messages[0].Text, `"ex_code":"SH600519"`)
	require.Contains(t, req.Messages[0].Text, "金额单位是亿元", "notes 交给模型")

	data, err := json.Marshal(got)
	require.NoError(t, err)
	require.Contains(t, string(data), `"check":{"numbers":`)
	require.Contains(t, string(data), `"advice_terms":[]`)
}

func TestValuationExplainCache(t *testing.T) {
	asOf, inputs := "2026-11-06", []string{}
	dir := t.TempDir()
	fake := &Fake{Replies: []FakeReply{FakeText("第一次"), FakeText("换了日期"), FakeText("换了参数"), FakeText("换了模型")}}
	model := Model{Profile: "original", API: "openai", BaseURL: "https://x", Name: "m"}
	explainer := &ValuationExplainer{Client: fake, Model: model, CacheDir: dir, Fetch: stubFetch(&asOf, &inputs)}
	explain := func(input string) *Explanation {
		got, err := explainer.Explain(context.Background(), json.RawMessage(input))
		require.NoError(t, err)
		return got
	}

	require.Equal(t, "第一次", explain(`{"code":"600519"}`).Text)
	got := explain(`{"code":"SH600519"}`)
	require.True(t, got.Cached, "同一只证券、同一天、同一个模型：不再请求模型")
	require.Equal(t, "第一次", got.Text)
	require.Len(t, fake.Requests, 1)

	asOf = "2026-11-09"
	require.Equal(t, "换了日期", explain(`{"code":"600519"}`).Text, "数据日期变了重新生成")
	require.Equal(t, "换了参数", explain(`{"code":"600519","wacc":10}`).Text, "DCF 参数不同重新生成")
	require.True(t, explain(`{"wacc":10,"code":"600519"}`).Cached, "参数顺序不影响")
	explainer.Model.Name = "m2"
	require.Equal(t, "换了模型", explain(`{"code":"600519"}`).Text, "换了模型重新生成")
	explainer.Model = Model{Profile: "renamed", API: "openai", BaseURL: "https://x", Name: "m"}
	got = explain(`{"code":"600519"}`)
	require.True(t, got.Cached, "配置改名不影响缓存")
	require.Equal(t, "renamed", got.Profile, "缓存命中时返回当前配置名")
	require.Len(t, fake.Requests, 4)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 4)
}

func TestValuationExplainNoCache(t *testing.T) {
	asOf, inputs := "2026-11-06", []string{}
	// 被截断的不缓存；CacheDir 为空时不缓存；没有 as_of 时不缓存。
	truncated := FakeText("估值水平：")
	truncated.Response.StopReason = StopMaxTokens
	fake := &Fake{Replies: []FakeReply{truncated, FakeText("完整"), FakeText("不缓存 1"), FakeText("不缓存 2")}}
	dir := t.TempDir()
	explainer := &ValuationExplainer{Client: fake, CacheDir: dir, Fetch: stubFetch(&asOf, &inputs)}
	got, err := explainer.Explain(context.Background(), json.RawMessage(`{"code":"600519"}`))
	require.NoError(t, err)
	require.True(t, got.Truncated)
	got, err = explainer.Explain(context.Background(), json.RawMessage(`{"code":"600519"}`))
	require.NoError(t, err)
	require.Equal(t, "完整", got.Text, "截断的结果没有缓存")

	explainer.CacheDir = ""
	for _, want := range []string{"不缓存 1", "不缓存 2"} {
		got, err = explainer.Explain(context.Background(), json.RawMessage(`{"code":"600519"}`))
		require.NoError(t, err)
		require.Equal(t, want, got.Text)
	}
}

func TestValuationExplainErrors(t *testing.T) {
	asOf, inputs := "2026-11-06", []string{}
	refusal := FakeText("")
	refusal.Response.StopReason = StopRefusal
	empty := FakeText("  ")
	empty.Response.RawStopReason = "end_turn"
	fake := &Fake{Replies: []FakeReply{refusal, empty, {Err: errors.New("HTTP 503")}}}
	explainer := &ValuationExplainer{Client: fake, Fetch: stubFetch(&asOf, &inputs)}
	for _, want := range []string{"模型拒绝了这个请求", "模型没有给出解读（停止原因 end_turn）", "HTTP 503"} {
		_, err := explainer.Explain(context.Background(), json.RawMessage(`{"code":"600519"}`))
		require.ErrorContains(t, err, want)
	}

	// 取估值数据失败（比如代码不对）原样返回，不调用模型。
	explainer.Fetch = func(context.Context, json.RawMessage) (*aitool.Result, error) {
		return nil, aitool.Inputf("没有找到证券 x")
	}
	_, err := explainer.Explain(context.Background(), json.RawMessage(`{"code":"x"}`))
	require.True(t, aitool.IsInputError(err))
	require.Len(t, fake.Requests, 3)
}

func TestOpen(t *testing.T) {
	secret := config.NewAISecret(t.TempDir() + "/secret.key")
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	var ai config.AIConfig
	_, _, err := Open(&ai, "", secret, getenv)
	require.ErrorIs(t, err, ErrNoProfile)

	require.NoError(t, ai.Add("local", config.AIProfile{Provider: "ollama", Model: "qwen3"}))
	require.NoError(t, ai.Add("ds", config.AIProfile{Provider: "deepseek", Model: "deepseek-chat"}))
	_, _, err = Open(&ai, "", secret, getenv)
	require.ErrorIs(t, err, ErrAIDisabled, "默认关闭，没启用时不调用模型")

	require.NoError(t, ai.SetEnabled(true))
	client, model, err := Open(&ai, "", secret, getenv)
	require.NoError(t, err, "不写名字时用 default")
	require.NotNil(t, client)
	require.Equal(t, Model{Profile: "local", Provider: "ollama", API: "openai", BaseURL: "http://localhost:11434/v1", Name: "qwen3"}, model)
	require.Equal(t, "openai http://localhost:11434/v1 qwen3", model.ID())

	_, _, err = Open(&ai, "nosuch", secret, getenv)
	require.ErrorIs(t, err, config.ErrAIProfileNotFound)
	require.True(t, strings.Contains(err.Error(), "local") && strings.Contains(err.Error(), "ds"), "列出可选的配置")
	_, _, err = Open(&ai, "ds", secret, getenv)
	require.ErrorIs(t, err, ErrMissingKey, "远程厂商没有 Key")
	env["DEEPSEEK_API_KEY"] = "sk-test-1234567890"
	_, model, err = Open(&ai, "ds", secret, getenv)
	require.NoError(t, err)
	require.Equal(t, "ds", model.Profile)
	require.True(t, model.ThinkingOffSupported, "DeepSeek 官方地址能关闭思考，出错时据此给建议")
	require.False(t, model.ThinkingOff)
	require.NoError(t, ai.Edit("ds", func(p *config.AIProfile) { p.Thinking = config.AIThinkingOff }))
	_, off, err := Open(&ai, "ds", secret, getenv)
	require.NoError(t, err)
	require.True(t, off.ThinkingOff)
	require.Equal(t, model.ID(), off.ID(), "思考设置不进缓存键")
}

type failingStream struct {
	Fake
	err error
}

func (f *failingStream) ChatStream(ctx context.Context, _ Request, emit func(StreamEvent) error) (*Response, error) {
	if err := emitStream(ctx, emit, StreamEvent{Type: StreamText, Text: "半篇草稿"}); err != nil {
		return nil, err
	}
	return nil, f.err
}

func TestExplainStreamChecksAndCache(t *testing.T) {
	asOf, inputs := "2026-11-06", []string{}
	fake := &Fake{Replies: []FakeReply{FakeText(explainText)}}
	e := &ValuationExplainer{Client: fake, CacheDir: t.TempDir(), Fetch: stubFetch(&asOf, &inputs)}
	var events []ExplanationEvent
	got, err := e.ExplainStream(context.Background(), nil, func(event ExplanationEvent) error {
		events = append(events, event)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, "meta", events[0].Type)
	require.Contains(t, events[0].Meta.Disclaimer, "不构成投资建议")
	require.Equal(t, "delta", events[1].Type)
	require.Equal(t, explainText, events[1].Text)
	require.Equal(t, "status", events[2].Type)
	require.Equal(t, []string{"3.8%"}, unsourcedTexts(got.Check), "流式完成后仍做完整数字核对")
	jsonResult, err := e.Explain(context.Background(), nil)
	require.NoError(t, err)
	require.True(t, jsonResult.Cached, "流式与 JSON 共用缓存")
	events = nil
	got, err = e.ExplainStream(context.Background(), nil, func(event ExplanationEvent) error { events = append(events, event); return nil })
	require.NoError(t, err)
	require.True(t, got.Cached)
	require.Len(t, events, 1, "缓存直接返回完整结果，不伪造增量")
	require.True(t, events[0].Meta.Cached)
	require.Len(t, fake.Requests, 1)
}

func TestExplainStreamDoesNotCacheDraft(t *testing.T) {
	for _, mode := range []string{"断流", "回调失败", "取消", "截断"} {
		t.Run(mode, func(t *testing.T) {
			asOf, inputs := "2026-11-06", []string{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := errors.New("断流")
			var client Client = &failingStream{err: want}
			if mode != "断流" {
				reply := FakeText(explainText)
				if mode == "截断" {
					reply.Response.StopReason = StopMaxTokens
				}
				client = &Fake{Replies: []FakeReply{reply}}
			}
			dir := t.TempDir()
			e := &ValuationExplainer{Client: client, CacheDir: dir, Fetch: stubFetch(&asOf, &inputs)}
			got, err := e.ExplainStream(ctx, nil, func(event ExplanationEvent) error {
				if event.Type == "delta" {
					if mode == "取消" {
						cancel()
					}
					if mode == "回调失败" {
						return want
					}
				}
				return nil
			})
			if mode == "截断" {
				require.NoError(t, err)
				require.True(t, got.Truncated)
			} else {
				require.Error(t, err)
				require.Nil(t, got)
				if mode == "取消" {
					require.ErrorIs(t, err, context.Canceled)
				} else {
					require.ErrorIs(t, err, want)
				}
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
