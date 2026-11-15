package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
)

const riskURL = "https://www.sec.gov/Archives/edgar/data/320193/000032019326000026/aapl-20260926.htm"

func riskChanges() *edgar.RiskChanges {
	return &edgar.RiskChanges{
		Current:  edgar.RiskFiling{Accession: "0000320193-26-000026", Form: "10-K", FilingDate: "2026-10-30", ReportDate: "2026-09-26", URL: riskURL, Count: 3},
		Previous: edgar.RiskFiling{Accession: "0000320193-25-000079", Form: "10-K", FilingDate: "2025-10-31", ReportDate: "2025-09-27", URL: "https://www.sec.gov/prev.htm", Count: 3},
		RiskComparison: edgar.RiskComparison{
			Added:   []edgar.RiskFactor{{Category: "Business Risks", Title: "Issues related to AI may harm the Company.", Text: "AI outputs may be inaccurate. The Company invested $5 billion."}},
			Removed: []edgar.RiskFactor{{Title: "ESG scrutiny may result in costs.", Text: "Stakeholders scrutinize ESG practices."}},
			Revised: []edgar.RiskRevision{{
				Old: edgar.RiskFactor{Title: "The Company depends on components.", Text: "Supply is limited. Prices may rise."},
				New: edgar.RiskFactor{Category: "Business Risks", Title: "The Company depends on components and compute capacity.", Text: "Supply is limited. Compute capacity is constrained in 2026."},
			}},
			Kept: []edgar.RiskFactor{{Title: "The stock price may be volatile."}},
		},
		Notes: []string{"对比说明"},
	}
}

func riskReply(sections ...RiskSection) FakeReply {
	data, _ := json.Marshal(map[string]any{"sections": sections})
	return FakeText(string(data))
}

func TestRiskExplain(t *testing.T) {
	fake := &Fake{Replies: []FakeReply{riskReply(
		RiskSection{Text: "2025 财年 10-K 的 3 条风险因素到今年仍是 3 条，今年新增人工智能风险，公司投入 50 亿美元。", Refs: []string{"新增 1"}},
		RiskSection{Text: "供应风险扩展到 2026 年受限的算力，改写 1 不再提价格上涨；ESG 风险不再单独列出。", Refs: []string{"删除1", "改写1"}},
	)}}
	reads := 0
	e := RiskExplainer{Client: fake, Model: Model{Profile: "p", Name: "m"}, CacheDir: t.TempDir(), Read: func(ctx context.Context, raw string) (*edgar.RiskChanges, error) {
		reads++
		return riskChanges(), ctx.Err()
	}}
	result, err := e.Explain(context.Background(), riskURL)
	require.NoError(t, err)
	require.Equal(t, []string{"新增 1"}, result.Sections[0].Refs)
	require.Equal(t, []string{"删除 1", "改写 1"}, result.Sections[1].Refs, "「改写1」规范成输入里的标签；引用按新增、删除、改写的顺序排列")
	require.True(t, strings.HasSuffix(result.Text, "（删除 1、改写 1）"), result.Text)
	require.True(t, result.Check.OK(), "billion 换算成亿、年份、两份 10-K 的报告期和条数都能找到来源，正文里写的标签序号不核对：%v", result.Check)
	require.Equal(t, "m", result.Model)
	require.Equal(t, RiskPromptVersion, result.PromptVersion)
	require.Contains(t, result.Notes[0], "改写只看两年正文里不同的句子")

	req := fake.Requests[0]
	require.Equal(t, riskPrompt, req.System)
	input := req.Messages[0].Text
	for _, text := range []string{`"label":"新增 1"`, `"kind":"removed"`, `"added_text":"Compute capacity is constrained in 2026."`, `"removed_text":"Prices may rise."`, `"old_title":"The Company depends on components."`} {
		require.Contains(t, input, text)
	}
	require.NotContains(t, input, "Supply is limited", "两年相同的句子不发送")
	require.NotContains(t, input, "volatile", "保留的风险因素不发送")
	require.NotContains(t, input, "sec.gov", "不把地址发给模型")

	// 缓存命中不读 EDGAR、不请求模型。
	cached, err := e.Explain(context.Background(), riskURL)
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Equal(t, 1, reads)
	require.Equal(t, "0000320193-25-000079", cached.Changes.Previous.Accession)

	// 对比规则或模型输入的版本改了，缓存失效。
	require.NotEqual(t, riskPromptHash, promptHash(RiskPromptVersion+"\x00"+edgar.RiskComparisonVersion+"\x00x", riskPrompt))

	// 类别里的数字也能找到来源。
	c := riskChanges()
	c.Added[0].Category = "Risks Related to the 2030 Notes"
	fake = &Fake{Replies: []FakeReply{riskReply(RiskSection{Text: "新增与 2030 年票据相关的风险。", Refs: []string{"新增 1"}})}}
	e = RiskExplainer{Client: fake, Read: func(context.Context, string) (*edgar.RiskChanges, error) { return c, nil }}
	result, err = e.Explain(context.Background(), riskURL)
	require.NoError(t, err)
	require.True(t, result.Check.OK(), result.Check)
}

func TestRiskExplainNoChanges(t *testing.T) {
	fake := &Fake{}
	e := RiskExplainer{Client: fake, Read: func(context.Context, string) (*edgar.RiskChanges, error) {
		c := riskChanges()
		c.Added, c.Removed, c.Revised = []edgar.RiskFactor{}, []edgar.RiskFactor{}, []edgar.RiskRevision{}
		return c, nil
	}}
	result, err := e.Explain(context.Background(), riskURL)
	require.NoError(t, err)
	require.Empty(t, result.Sections)
	require.Contains(t, strings.Join(result.Notes, ""), "没有请求模型")
	require.Empty(t, fake.Requests)
}

func TestRiskExplainFailures(t *testing.T) {
	read := func(context.Context, string) (*edgar.RiskChanges, error) { return riskChanges(), nil }
	e := RiskExplainer{Client: &Fake{Replies: []FakeReply{riskReply(RiskSection{Text: "事项", Refs: []string{"改写 2"}})}}, Read: read}
	_, err := e.Explain(context.Background(), riskURL)
	require.ErrorIs(t, err, ErrRiskExplain)
	require.ErrorContains(t, err, "不存在的变化")

	e = RiskExplainer{Client: &Fake{}, Read: func(context.Context, string) (*edgar.RiskChanges, error) { return nil, edgar.ErrDocument }}
	_, err = e.Explain(context.Background(), riskURL)
	require.ErrorIs(t, err, edgar.ErrDocument)
	e.Read = nil
	_, err = e.Explain(context.Background(), riskURL)
	require.ErrorIs(t, err, ErrRiskExplain)
	_, err = e.Explain(context.Background(), "https://evil.test/a.htm")
	require.ErrorIs(t, err, edgar.ErrDocument)
}

func TestSentenceDiff(t *testing.T) {
	require.Equal(t, []string{"The U.S.", "Government may act.", "Sales rose 5%.", "Costs fell?", "Next line"},
		sentences("The U.S. Government may act. Sales rose 5%. Costs fell?\nNext line"), "缩写后面跟大写时也切开，两年切法相同，比较不受影响")
	added, removed := sentenceDiff("Supply is limited. Prices may rise.", "SUPPLY  is limited. Compute is constrained.")
	require.Equal(t, "Compute is constrained.", added, "比较时不计大小写和空白")
	require.Equal(t, "Prices may rise.", removed)
}

func TestPackRiskChanges(t *testing.T) {
	entry := func(n int) riskChange { return riskChange{Label: "改写", AddedText: strings.Repeat("a", n)} }
	chunks := packRiskChanges([]riskChange{entry(30), entry(30), entry(50), entry(10), entry(90)}, 70)
	require.Len(t, chunks, 3, "一条变化不拆开，装不下就放到下一块；超过一块的单独成块")
	require.Len(t, chunks[0], 2)
	require.Len(t, chunks[1], 2)
	require.Equal(t, 50, utf8.RuneCountInString(chunks[1][0].AddedText))
	require.Equal(t, 90, utf8.RuneCountInString(chunks[2][0].AddedText))
}

func TestMaskRiskLabels(t *testing.T) {
	labels := map[string]int{"新增 1": 0, "改写 7": 1, "改写 13": 2}
	text := "改写13和改写 7、新增 1 增长 12%；改写 9 不存在，新增 1 条风险，新增 1 亿美元"
	masked := maskRiskLabels(text, labels)
	require.Equal(t, "改写##和改写 #、新增 # 增长 12%；改写 9 不存在，新增 1 条风险，新增 1 亿美元", masked,
		"只换输入里有的标签；后面跟量词或单位的是数量或金额，照常核对")
	require.Equal(t, utf8.RuneCountInString(text), utf8.RuneCountInString(masked), "字符数不变，核对结果的位置对得上")
}

func TestNormalizeRiskLabel(t *testing.T) {
	for in, want := range map[string]string{"改写3": "改写 3", "新增  12": "新增 12", "删除 1": "删除 1", "Item 1A": "Item 1A", "改写 3 和改写 4": "改写 3 和改写 4"} {
		require.Equal(t, want, normalizeRiskLabel(in), in)
	}
}
