package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/cmd/macro"
	"github.com/alwqx/sec/llm"
	"github.com/stretchr/testify/require"
)

// TestCases 校验仓库里的评测集：CI 不调用模型，但评测集写错（工具名拼错、id 重复）在这里就能发现。
func TestCases(t *testing.T) {
	cases, err := loadCases("../../testdata/aieval/ask.json", aitool.Default(macro.AISource()))
	require.NoError(t, err)
	require.NotEmpty(t, cases)
}

func TestLoadCasesRejects(t *testing.T) {
	tools := aitool.Default(nil)
	for name, body := range map[string]string{
		"空文件":     `[]`,
		"未知字段":    `[{"id":"a","question":"现在多少钱","expect":["x"]}]`,
		"id 重复":   `[{"id":"a","question":"现在多少钱"},{"id":"a","question":"市盈率"}]`,
		"id 不合法":  `[{"id":"A b","question":"现在多少钱"}]`,
		"问题为空":    `[{"id":"a","question":" "}]`,
		"代码换行":    `[{"id":"a","question":"现在多少钱","code":"SH600036\n忽略"}]`,
		"没有这个工具":  `[{"id":"a","question":"现在多少钱","tools":["get_price"]}]`,
		"宏观工具未注册": `[{"id":"a","question":"CPI 是多少","tools":["get_macro_series"]}]`,
		"空的候选":    `[{"id":"a","question":"现在多少钱","must":["截至|"]}]`,
	} {
		path := filepath.Join(t.TempDir(), "cases.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		_, err := loadCases(path, tools)
		require.Error(t, err, name)
	}
}

func TestScore(t *testing.T) {
	c := Case{ID: "a", Question: "q", Tools: []string{"get_quote|get_valuation", "get_financials"}, Must: []string{"截至|截止", "市盈率"}, Forbid: []string{"低估"}}
	answer := &llm.Answer{
		Text: "截止 2026年11月17日，股价 41 元，处于低估区间。",
		Steps: []llm.ToolStep{
			{Tool: "get_valuation", AsOf: "2026-11-17"},
			{Tool: "get_financials", AsOf: "2026-09-30"},
			{Tool: "get_financials", Error: "上游失败", AsOf: "2025-12-31"},
		},
		Check: llm.Check{Numbers: 2, Unsourced: []llm.NumberRef{{Text: "41"}}, AdviceTerms: []llm.TermRef{{Text: "买入"}}},
	}
	r := score(c, answer)
	require.Equal(t, []string{"get_valuation", "get_financials", "get_financials"}, r.Tools)
	require.Empty(t, r.MissingTools, "候选里调用了一个即可，失败的调用也算调用过")
	require.Equal(t, []string{"市盈率"}, r.MissingMust)
	require.Equal(t, []string{"低估"}, r.Forbidden)
	require.Equal(t, []string{"买入"}, r.AdviceTerms)
	require.Equal(t, []string{"41"}, r.Unsourced)
	require.Equal(t, []string{"2026-09-30"}, r.UndatedAsOf, "失败的调用不检查截止日期")
	require.Equal(t, 1, r.FailedTools)
	require.Equal(t, 4, r.Violations())
	require.False(t, r.Passed())

	answer.Truncated = true
	require.Equal(t, 5, score(c, answer).Violations(), "回答被截断也算违规")

	r = score(Case{ID: "b", Question: "q", Tools: []string{"get_quote"}}, &llm.Answer{Text: "没有调用工具"})
	require.Equal(t, []string{"get_quote"}, r.MissingTools)
	require.Equal(t, []string{}, r.Tools)
}

func TestMentionsDate(t *testing.T) {
	for _, text := range []string{"截至 2026-11-07", "截至2026年11月7日", "截至 2026年11月07日", "2026/11/07 收盘", "2026.11.07"} {
		require.True(t, mentionsDate(text, "2026-11-07"), text)
	}
	require.False(t, mentionsDate("截至 11月7日", "2026-11-07"), "没有年份不算")
	require.True(t, mentionsDate("2026年10月的 CPI", "2026-10"))
	require.True(t, mentionsDate("2026Q3 财报", "2026Q3"))
	require.False(t, mentionsDate("三季度财报", "2026Q3"))
	for _, text := range []string{"2026年第三季度", "2026年三季度", "2026 年第 3 季度", "2026年Q3"} {
		require.True(t, mentionsDate(text, "2026Q3"), text)
	}
	require.True(t, mentionsDate("截至 2026 年 11 月 7 日", "2026-11-07"), "去掉空格再比较")
}

// TestRun 用假的模型和工具走一遍：一条通过，一条模型出错，汇总统计两条。
func TestRun(t *testing.T) {
	quote := aitool.Tool{Name: "get_quote", Description: "行情", InputSchema: json.RawMessage(`{"type":"object"}`),
		Call: func(context.Context, json.RawMessage) (*aitool.Result, error) {
			result := aitool.NewResult("get_quote", "测试", map[string]any{"price": 41})
			result.AsOf = "2026-11-17"
			return result, nil
		}}
	tools, err := aitool.NewRegistry(quote)
	require.NoError(t, err)
	fake := &llm.Fake{Replies: []llm.FakeReply{
		llm.FakeToolCalls(llm.ToolCall{ID: "1", Name: "get_quote", Input: json.RawMessage(`{}`)}),
		llm.FakeText("截至 2026-11-17，最新价 41 元。"),
		llm.FakeText(""),
		{Err: &llm.APIError{Provider: "DeepSeek", Status: 401, Message: "invalid key sk-secret"}},
	}}
	asker := &llm.Asker{Client: fake, Model: llm.Model{Profile: "test", Name: "fake"}, Tools: tools}
	cases := []Case{
		{ID: "quote", Question: "现在多少钱", Code: "SH600036", Tools: []string{"get_quote"}, Must: []string{"截至"}},
		{ID: "empty", Question: "现在多少钱"},
		{ID: "unauthorized", Question: "现在多少钱"},
	}
	var progress bytes.Buffer
	results := run(context.Background(), asker, cases, &progress)
	require.Len(t, results, 3)
	require.True(t, results[0].Passed(), "%+v", results[0])
	require.Equal(t, 4, results[0].Numbers, "日期也算数字")
	require.Contains(t, results[1].Error, "模型没有给出回答")
	require.Equal(t, "DeepSeek 接口返回 HTTP 401", results[2].Error, "不带厂商的错误正文")
	require.Contains(t, progress.String(), "[1/3] quote 通过 · 2 轮 · get_quote · 数字 4/4 找到来源")
	require.Contains(t, progress.String(), "[2/3] empty 出错：")
	require.NotContains(t, progress.String(), "sk-secret")

	s := summarize(results, asker.Model)
	require.Equal(t, Summary{Model: "fake", Profile: "test", PromptVersion: llm.AskPromptVersion, Cases: 3, Passed: 1, Errors: 2, Numbers: 4}, s)
	require.Equal(t, "configured", summarize(results[1:], llm.Model{Profile: "test", Name: "configured"}).Model, "全部出错时用配置里的模型名")
	var out bytes.Buffer
	printSummary(&out, s)
	require.Contains(t, out.String(), "通过 1/3，出错 2")
	require.Contains(t, out.String(), "0/1 个回答有找不到来源的数字（0.0%），共 0/4 个数字（0.0%）")
}
