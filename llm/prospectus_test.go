package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/stretchr/testify/require"
)

const prospectusURL = "https://static.cninfo.com.cn/finalpage/2025-11-28/1224820000.PDF"

func TestQuestionTerms(t *testing.T) {
	require.Equal(t, []string{"最近", "近三", "三年", "营业", "业收", "收入", "净利", "利润"}, questionTerms("公司最近三年的营业收入和净利润是多少？"),
		"虚字处断开，「公司」和问句里的「多少」不计")
	require.Equal(t, []string{"2024", "gpu", "研发", "发投", "投入"}, questionTerms("2024年GPU研发投入"), "英文小写、数字整词，单个汉字不算")
	require.Equal(t, []string{"对赌", "赌协", "协议"}, questionTerms("是否存在对赌协议"), "先去掉问句说法，「对」不切断「对赌」")
	require.Equal(t, []string{"子公"}, questionTerms("有哪些子公司？"), "「子公司」不被「公司」拆开")
	require.Equal(t, []string{"亏损"}, questionTerms("为什么亏损"))
	require.Equal(t, []string{"实际", "际控", "控制", "制人"}, questionTerms("实际控制人是谁"))
	require.Empty(t, questionTerms("请问是什么？"))
}

func prospectusPages() []cninfo.DocumentPage {
	return []cninfo.DocumentPage{
		{Number: 1, Text: "招股说明书 保荐人"},
		{Number: 2, Text: "营业收入 净利润 营业收入 净利润 单位：万元 营业收入 12,398.19 43,845.95"},
		{Number: 3, Text: "续表 净利润 -161,828.83"},
		{Number: 4, Text: "募集资金运用 项目"},
		{Number: 5, Text: "营业收入"},
	}
}

func TestRankAndSelectPages(t *testing.T) {
	pages := prospectusPages()
	ranked := rankPages(pages, "营业收入和净利润")
	require.Equal(t, []int{1, 4, 2}, ranked, "按分数排序（BM25 按页长归一，短页分高），没有词的页不返回")
	var numbers []int
	for _, p := range selectPages(pages, ranked, 1000, 10) {
		numbers = append(numbers, p.Number)
	}
	require.Equal(t, []int{2, 3, 4, 5}, numbers, "取到的页也带上下一页，按页码排序")
	require.Len(t, selectPages(pages, ranked, 1000, 1), 1, "最多 maxPages 页")
	require.Len(t, selectPages(pages, ranked, 10, 10), 1, "超过字数预算的页不取")
}

func TestProspectusAsk(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"sections": []SummarySection{{Text: "营业收入 43,845.95 万元，净利润 -161,828.83 万元。", Pages: []int{3, 2}}}})
	fake := &Fake{Replies: []FakeReply{FakeText(string(data))}}
	reads := 0
	a := ProspectusAnswerer{Client: fake, Model: Model{Name: "m"}, CacheDir: t.TempDir(), Read: func(ctx context.Context, raw string) (*cninfo.Document, error) {
		reads++
		return &cninfo.Document{ID: "1224820000", URL: raw, AsOf: "2025-11-28", Pages: prospectusPages()}, ctx.Err()
	}}
	result, err := a.Ask(context.Background(), prospectusURL, "  营业收入和  净利润是多少？ ")
	require.NoError(t, err)
	require.Equal(t, "营业收入和 净利润是多少？", result.Question, "合并空白")
	require.Equal(t, []int{2, 3, 4, 5}, result.Pages)
	require.Equal(t, 5, result.PageCount)
	require.Equal(t, []int{2, 3}, result.Sections[0].Pages)
	require.True(t, strings.HasSuffix(result.Text, "（PDF 第 2、3 页）"), result.Text)
	require.True(t, result.Check.OK(), "万元换算和跨页表格（续表带上一页的单位）的数字都能找到来源：%v", result.Check)
	input := fake.Requests[0].Messages[0].Text
	require.Contains(t, input, `"question":"营业收入和 净利润是多少？"`)
	require.NotContains(t, input, "保荐人", "没有挑中的页不发送")
	require.Equal(t, prospectusPrompt, fake.Requests[0].System)

	// 同一个问题命中缓存，不读 PDF、不请求模型。
	cached, err := a.Ask(context.Background(), prospectusURL, "营业收入和 净利润是多少？")
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Equal(t, 1, reads)

	// 换个问题要请求模型，但页已经缓存，不再读 PDF。
	fake.Replies = append(fake.Replies, FakeText(`{"sections":[{"text":"募集资金用于项目。","pages":[4]}]}`))
	other, err := a.Ask(context.Background(), prospectusURL, "募集资金运用")
	require.NoError(t, err)
	require.False(t, other.Cached)
	require.Equal(t, 1, reads)
	require.Len(t, fake.Requests, 2)
}

func TestPageNumberSources(t *testing.T) {
	pages := []cninfo.DocumentPage{
		{Number: 1, Text: "营业收入（万\n元） 12,398.19"},
		{Number: 2, Text: "续表 43,845.95"},
		{Number: 3, Text: "续表 单位：% 毛利率 12.5"},
		{Number: 4, Text: "续表（元） 净利润 -161,828.83"},
		{Number: 5, Text: "另一张表"},
	}
	sources := pageNumberSources(pages, pages)
	for _, n := range []int{1, 2, 3, 4} {
		require.True(t, strings.HasSuffix(sources[n], "\n（万元）"), "拆到两行的单位、前 3 页内的单位都带上：%d %q", n, sources[n])
	}
	require.Equal(t, "另一张表\n", sources[5], "第 1 页的单位超过 3 页，不带")
	require.True(t, Verify("净利润 43,845.95 万元", sources[2]).OK())
	require.Equal(t, []float64{1}, sourceScales("采购 2 万\n元器件"), "共用的数字核对不跨空白拼单位")
}

func TestProspectusAskFailures(t *testing.T) {
	read := func(ctx context.Context, raw string) (*cninfo.Document, error) {
		return &cninfo.Document{ID: "1", URL: raw, Pages: prospectusPages()}, nil
	}
	a := ProspectusAnswerer{Client: &Fake{Replies: []FakeReply{FakeText(`{"sections":[{"text":"事项","pages":[1]}]}`)}}, Read: read}
	_, err := a.Ask(context.Background(), prospectusURL, "营业收入")
	require.ErrorContains(t, err, "不存在的 PDF 页码", "只能引用挑出来的页")

	_, err = a.Ask(context.Background(), prospectusURL, "董事会秘书")
	require.ErrorIs(t, err, ErrProspectusAsk)
	require.ErrorIs(t, err, ErrNoRelatedPages)
	require.ErrorContains(t, err, "没有和问题相关的页，请换个说法")
	_, err = a.Ask(context.Background(), prospectusURL, " ")
	require.ErrorContains(t, err, "问题不能为空")
	_, err = a.Ask(context.Background(), prospectusURL, strings.Repeat("问", maxQuestionRunes+1))
	require.ErrorContains(t, err, "超过 200 字")
	_, err = a.Ask(context.Background(), "https://evil.test/a.pdf", "营业收入")
	require.ErrorIs(t, err, cninfo.ErrDocument)
}

func TestUnitHints(t *testing.T) {
	require.Equal(t, "单位：万元", unitHints("表 单位：万元、%\n项目"))
	require.Equal(t, "（万元）", unitHints("销售金额（万\n元）"), "单位拆到两行")
	require.Equal(t, "单位：人民币亿元", unitHints("单位：人民币亿元"))
	require.Empty(t, unitHints("续表 净利润 -161,828.83"))
	require.Empty(t, unitHints("单位：% 数量（股） 价格（元）"), "不放大数字的单位不算")
}
