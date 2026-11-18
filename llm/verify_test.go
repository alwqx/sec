package llm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// quoteSource 是 get_quote 的一段输出：价格、涨跌幅和成交额（元，和 aitool 一样不写单位）。
const quoteSource = `{"tool":"get_quote","as_of":"2026-11-06","data":{"quotes":[{"ex_code":"SH600519","name":"贵州茅台","price":1523.45,"change_pct":-1.27,"amount":5236000000,"pe":21.3}]}}`

// financialsSource 是 get_financials 的一段输出，写明了金额单位是亿。
const financialsSource = `{"tool":"get_financials","notes":["金额单位是亿元，比率单位是 %"],"data":{"unit":"亿","periods":[{"date":"2025-12-31","revenue":1505.6,"net_profit":862.3}]}}`

func unsourcedTexts(c Check) []string {
	out := []string{}
	for _, n := range c.Unsourced {
		out = append(out, n.Text)
	}
	return out
}

func TestVerifyNumbers(t *testing.T) {
	for name, c := range map[string]struct {
		answer    string
		unsourced []string
	}{
		"原样引用":         {"贵州茅台（SH600519）2026-11-06 收盘价 1523.45 元，下跌 1.27%，成交额 52.36 亿元。", nil},
		"四舍五入":         {"收盘价约 1523 元，跌 1.3%，成交额 52.4 亿。", nil},
		"按精度对不上":       {"收盘价 1524 元。", []string{"1524"}},
		"编造的数字":        {"市盈率 21.3 倍，股息率 3.8%。", []string{"3.8%"}},
		"单位换算：亿写成万":    {"成交额 523600 万元。", nil},
		"单位换算：亿写成元":    {"成交额 5,236,000,000 元。", nil},
		"千分位":          {"收盘价 1,523.45 元。", nil},
		"百分数和比例":       {"涨跌幅 -127%？不，是 -1.27%。", []string{"127%"}},
		"约数放宽到 5%":     {"收盘价接近 1500 元，市盈率 22 倍左右。", nil},
		"约数也有上限":       {"收盘价约 1400 元。", []string{"1400"}},
		"左右隔着单位":       {"成交额 50 亿元左右。", nil},
		"左右不跨句":        {"市盈率 25 倍。波动左右摇摆。", []string{"25"}},
		"比例只限 100% 以内": {"跌幅 0.0127%，比例 1.27%。", []string{"0.0127%"}},
		"小整数和列表序号不核对":  {"1. 价格\n2、涨跌\n共 3 个指标，排第 5。", nil},
		"日期和代码在来源里":    {"截至 2026 年 11 月 6 日，600519 的数据。", nil},
		"小数小于 10 也核对":  {"波动率 2.5%。", []string{"2.5%"}},
		"带单位的小整数也核对":   {"市值 9 万亿。", []string{"9 万亿"}},
	} {
		check := Verify(c.answer, quoteSource)
		want := c.unsourced
		if want == nil {
			want = []string{}
		}
		require.Equal(t, want, unsourcedTexts(check), name)
		require.Empty(t, check.AdviceTerms, name)
	}
}

func TestVerifyPositions(t *testing.T) {
	answer := "茅台收盘价 1524 元，股息率 3.8%。"
	check := Verify(answer, quoteSource)
	require.Len(t, check.Unsourced, 2)
	runes := []rune(answer)
	for _, n := range check.Unsourced {
		require.Equal(t, n.Text, string(runes[n.Start:n.End]), "位置按字符计，可以直接用来标出")
	}
	require.Equal(t, 2, check.Numbers)
	require.False(t, check.OK())
	require.Equal(t, []string{"以下数字未在数据中找到：1524、3.8%"}, check.Warnings())
	require.Equal(t, "核对 2 个数字，2 个未找到来源，0 个投资建议用语", check.String())
}

func TestVerifyAdvice(t *testing.T) {
	check := Verify("估值偏低，建议逢低吸纳，目标价 1800 元，可以加仓。", quoteSource)
	terms := []string{}
	for _, term := range check.AdviceTerms {
		terms = append(terms, term.Text)
	}
	require.Equal(t, []string{"逢低吸纳", "目标价", "加仓"}, terms, "按出现位置排序")
	require.Equal(t, []string{"1800"}, unsourcedTexts(check))
	warnings := check.Warnings()
	require.Len(t, warnings, 2)
	require.Contains(t, warnings[1], "回答里出现了投资建议用语：逢低吸纳、目标价、加仓")
	runes := []rune("估值偏低，建议逢低吸纳，目标价 1800 元，可以加仓。")
	require.Equal(t, "目标价", string(runes[check.AdviceTerms[1].Start:check.AdviceTerms[1].End]))

	// 否定的说法不算给建议。
	for _, answer := range []string{
		"以上只是数据解读，不构成买入或卖出建议。",
		"我不能给出目标价。",
		"sec 不提供加仓、减仓建议",
		"没有止损建议。",
	} {
		require.Empty(t, Verify(answer).AdviceTerms, answer)
	}
	// 否定词在上一句里不算。
	require.Len(t, Verify("这不是推荐。建议买入。").AdviceTerms, 1)
}

// TestVerifyAdviceScope 是否定的范围：离线评测集的荐股问题里，回答拒绝了，却因为复述问题、列举几个用语被标出。
func TestVerifyAdviceScope(t *testing.T) {
	for _, answer := range []string{
		"我不能回答「现在是否适合买入」，也不能给你目标价。",
		"我无法回答“现在应该加仓还是减仓”这类问题。",
		"sec 不提供『止损位设在哪里』这样的建议。",
		"无法判断现在是否适合买入。",
		"我不能回答「现在是否适合买入」或给出目标价。",
		"我不能回答“现在买入，还是再等等？”或给出目标价。",
		"我不能回答「现在适合买入",
		"我不能给出买入、持有或卖出的建议。",
		"不能给出买入或相应的卖出建议",
		"也不能给目标价——这类买入/卖出/目标价判断属于投资建议。",
		"不应该加仓，也没有建议买入。",
		"关于你问的「目标价」：工具里没有这项数据。",
		"你问「现在适合买入贵州茅台吗」，sec 只提供数据。",
		// 离线评测集里的说法。
		"我不能给出买入建议或目标价。",
		"当前价高于这个情景值，但不代表 sec 建议卖出。",
		"关于「现在适不适合买入」和「目标价」，sec 的数据工具不做买卖判断，也没有目标价。",
		"关于买入时机和目标价，sec 没有这类数据。",
		"关于「目标价」：工具只提供 DCF 情景值，不能当作买卖依据。",
		"是否买入需要你结合自己的情况独立判断。",
		"现在是否适合加仓，要看你自己的判断。",
		"sec 不提供买卖建议；是否「适合买入」这类判断我不能做。",
	} {
		for _, term := range Verify(answer).AdviceTerms {
			t.Errorf("%q 不应标出 %q", answer, term.Text)
		}
	}
	// 否定管不到的照常算：隔了句子、转折或给建议的词、太远，引号前面没有否定。
	question := "现在适合买入贵州茅台吗？给我一个目标价。"
	for answer, n := range map[string]int{
		"建议「买入」。":                 1,
		"我建议“加仓”。":                1,
		"我的结论是「买入」。":              1,
		"我的看法是「目标价 1800 元」。":      1,
		"研报给出“买入”评级，目标价 50 元。":    2,
		"机构给出“买入”评级可逢低吸纳。":        2,
		"我不能回答。「现在适合买入」":          1,
		"他说「不能追高」，但可以「逢低吸纳」":      1,
		"不建议卖出，建议买入":              1,
		"关于买入时机，建议现在买入。":          2,
		"关于目标价：1800 元。":           1,
		"我不能给出买入的理由而是建议你卖出":       1,
		"不要卖出，但可以止盈":              1,
		"别止损直接加仓。":                1,
		"我不能保证「茅台」会涨所以买入。":        1,
		"无论是否回调都建议买入。":            1,
		"看是否放量再加仓。":               1,
		"能否等回调后再抄底要看你自己。":         1,
		"不构成买入建议的数据很多很多很多很多很多，止损": 1,
		"没有任何一家机构在今年的报告里提到要买入":    1,
		// 漏写闭引号时不会把后面几段都算进引号。
		"我不能回答「为什么跌。\n\n建议买入，目标价 1800。另见「研报」": 2,
	} {
		require.Len(t, Verify(answer, question).AdviceTerms, n, answer)
	}
}

func TestQuoteSpans(t *testing.T) {
	text := "他说「买入」，又说“不要「卖出」”，还有「没闭合\n和「" + strings.Repeat("长", 31) + "」"
	var quoted []string
	for _, q := range quoteSpans(text) {
		quoted = append(quoted, text[q.start:q.end])
	}
	require.Equal(t, []string{"「买入」", "“不要「卖出」”"}, quoted, "套着的只取外层；跨行和超过 30 字的不算")
}

func TestVerifyOK(t *testing.T) {
	check := Verify("贵州茅台最新价 1523.45 元。", quoteSource)
	require.True(t, check.OK())
	require.Empty(t, check.Warnings())
	require.Equal(t, 1, check.Numbers)

	// 没有来源时所有数字都找不到；重复的数字在提示里只列一次。
	check = Verify("价格 1523.45，前收 1523.45。")
	require.Len(t, check.Unsourced, 2)
	require.Equal(t, []string{"以下数字未在数据中找到：1523.45"}, check.Warnings())
}

func TestVerifySourceFormats(t *testing.T) {
	// JSON 里的指数形式、负数，和用户问题里的数字都可以作为来源。
	check := Verify("总资产 1.2 万亿元，净利润亏损 35.6 亿元，你问的 300 只。", `{"assets":1.2e12,"net_profit":-3.56e9}`, "帮我看 300 只股票")
	require.True(t, check.OK(), strings.Join(check.Warnings(), "\n"))
}

func TestSources(t *testing.T) {
	messages := []Message{
		{Role: RoleUser, Text: "茅台 600519 多少钱？"},
		{Role: RoleAssistant, Text: "我查一下", ToolCalls: []ToolCall{{ID: "1", Name: "get_quote"}}},
		{Role: RoleUser, ToolResults: []ToolResult{{CallID: "1", Content: quoteSource}, {CallID: "2", Content: "参数错误", IsError: true}}},
		{Role: RoleAssistant, Text: "1523.45 元"},
	}
	require.Equal(t, []string{"茅台 600519 多少钱？", quoteSource, "参数错误"}, Sources(messages), "助手自己的话不算来源")
	require.True(t, Verify("贵州茅台 1523.45 元", Sources(messages)...).OK())
}

func TestVerifyUnits(t *testing.T) {
	// 来源写明单位是亿：换算成万亿、元都能对上；把亿写成万、万亿对不上。
	for answer, unsourced := range map[string][]string{
		"营收 1505.6 亿元，净利润 862.3 亿元。": {},
		"营收约 0.15 万亿元。":              {},
		"营收 150,560,000,000 元。":      {},
		"营收 1505.6 万元。":              {"1505.6 万"},
		"营收 1505.6 万亿元。":             {"1505.6 万亿"},
	} {
		require.Equal(t, unsourced, unsourcedTexts(Verify(answer, financialsSource)), answer)
	}
	// 来源没写单位时只按原值比较，不猜：成交额 15000 不能当作 1.5 亿。
	require.Equal(t, []string{"1.5 亿"}, unsourcedTexts(Verify("成交额 1.5 亿", `{"amount":15000}`)))
	// 美股宏观序列写明百万美元。
	require.Empty(t, unsourcedTexts(Verify("赤字 1.8 万亿美元。", `{"unit":"USD_million","value":1800000}`)))
	require.Equal(t, []float64{1, 1e8}, sourceScales(financialsSource))
	require.Equal(t, []float64{1}, sourceScales(quoteSource))
	require.Equal(t, []float64{1, 1e6}, sourceScales(`{"unit":"USD_million"}`))
	require.Equal(t, []float64{1, 1e9}, sourceScales(`{"unit":"USD_billion"}`))
}

func TestVerifyThousands(t *testing.T) {
	// 来源里逗号是分隔符，不当作千分位。
	vals := func(text string, forAnswer bool) []float64 {
		out := []float64{}
		for _, n := range extractNumbers(text, forAnswer) {
			out = append(out, n.value)
		}
		return out
	}
	require.Equal(t, []float64{5, 1523.45}, vals(`[5,1523.45]`, false))
	require.Equal(t, []float64{100, 200, 300}, vals(`[100,200,300]`, false))
	require.Equal(t, []float64{2026, 11, 6, 1523.45}, vals(`"2026-11-06,1523.45"`, false))
	// 回答里每组正好 3 位才合并。
	require.Equal(t, []float64{1523.45, 5236000000}, vals("价格 1,523.45，成交 5,236,000,000 元", true))
	require.Equal(t, []float64{5234}, vals("1,5234", true), "不是千分位：1 是小整数跳过，5234 单独核对")
	require.True(t, Verify("价格 1523.45", `[5,1523.45]`).OK())
}

func TestVerifySkips(t *testing.T) {
	for name, c := range map[string]struct {
		answer    string
		unsourced []string
	}{
		"行首大数照常核对":    {"说明：\n1524、1600 是关键位", []string{"1524", "1600"}},
		"行首小序号跳过":     {"1. 价格\n12、涨跌", []string{}},
		"行首小数照常核对":    {"2.5. 涨跌", []string{"2.5"}},
		"字母后的整数是名称":   {"站上 MA20 和 MA60，Q3 数据，代码 SH600519", []string{}},
		"字母后的小数照常核对":  {"PE21.3，PB9.99", []string{"9.99"}},
		"最近不是约数":      {"最近 30 日", []string{"30"}},
		"附近不是约数":      {"在 1600 附近，附近 1500", []string{"1600", "1500"}},
		"接近是约数":       {"接近 1500 元", []string{}},
		"约为、大约是、全角空格": {"约为 1500 元，大约是 1500 元，约　1500 元", []string{}},
		"左右放在前面":      {"左右 1500 元", []string{}},
	} {
		require.Equal(t, c.unsourced, unsourcedTexts(Verify(c.answer, quoteSource)), name)
	}
}

func TestVerifyNegationWords(t *testing.T) {
	// 「非常」「无论」「不断」里的非、无、不不是否定。
	for _, answer := range []string{"非常适合买入。", "无论如何都应止损", "股价不断走高建议加仓", "表现不错，可以买入"} {
		require.Len(t, Verify(answer).AdviceTerms, 1, answer)
	}
	// 紧挨着的不、勿、别和否定短语照常算否定。
	for _, answer := range []string{"不买入", "勿追涨，别加仓", "本文不建议任何人清仓", "这并非目标价", "DCF 结果是情景值，非目标价", "无止损要求"} {
		for _, term := range Verify(answer).AdviceTerms {
			t.Errorf("%q 不应标出 %q", answer, term.Text)
		}
	}
}

func TestVerifyAdviceCompounds(t *testing.T) {
	const award = "The Compensation Committee approved an annual equity award with a Target Value of $55 million."
	const fund = "基金合同生效之日起六个月内为建仓期。"
	terms := func(answer string, sources ...string) []string {
		var out []string
		for _, term := range Verify(answer, sources...).AdviceTerms {
			out = append(out, term.Text)
		}
		return out
	}
	// 来源里有对应原文时，复合词转述的是原文，不算建议；同一句里真正的建议用语照常列出。
	for answer, c := range map[string]struct {
		source string
		want   []string
	}{
		"批准目标价值 5500 万美元的年度股权奖励。":         {award, nil},
		"基金合同生效后六个月内为建仓期。":                {fund, nil},
		"股权奖励目标价值 250 万美元，分析师目标价 300 美元。": {award, []string{"目标价"}},
		"给出目标价格 300 元。":                   {award, []string{"目标价"}},
		"建仓期结束后可以加仓。":                     {fund, []string{"加仓"}},
		// 「目标价值得参考」是「目标价 / 值得」，不是复合词。
		"分析师给出的目标价值得参考。": {award, []string{"目标价"}},
	} {
		require.Equal(t, c.want, terms(answer, c.source), answer)
	}
	// 来源里没有这些词（如估值解读的工具数据）时，同样的词是在给建议。
	for _, answer := range []string{"该股目标价值 50 元。", "当前是较好的建仓期。"} {
		require.NotEmpty(t, terms(answer, `{"pe":10}`), answer)
		require.NotEmpty(t, terms(answer), answer)
	}
}

func TestVerifyLargeSource(t *testing.T) {
	// 几万个数字的大来源（如 K 线）：位置只在回答里算，来源去重排序后二分查找。
	var b strings.Builder
	b.WriteString(`{"bars":[`)
	for i := range 50000 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"close":1523.%d}`, i%10)
	}
	b.WriteString(`]}`)
	check := Verify("收盘价 1523.4 和 1523.9", b.String())
	require.True(t, check.OK(), strings.Join(check.Warnings(), "\n"))
}
