package edgar

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// earningsPage 按 SEC 利润表页的格式生成表格：项目名和脚注各占一列（表头跨两列），rows 是 {概念, 脚注, 第 1 年, 第 2 年}。
func earningsPage(title string, dates []string, rows ...[4]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<table class="report"><tr><th class="tl" colspan="2" rowspan="2"><div><strong>%s</strong></div></th><th class="th" colspan="%d">12 Months Ended</th></tr><tr>`, title, len(dates))
	for _, d := range dates {
		fmt.Fprintf(&b, `<th class="th"><div>%s</div></th>`, d)
	}
	b.WriteString("</tr>")
	for _, r := range rows {
		fmt.Fprintf(&b, `<tr class="re"><td class="pl"><a onclick="Show.showAR( this, 'defref_%s', window );">x</a></td><td class="th"><sup>%s</sup></td><td class="nump">%s<span></span></td><td class="nump">%s<span></span></td></tr>`, r[0], r[1], r[2], r[3])
	}
	b.WriteString(`</table><table><tr><td>Name:</td><td>us-gaap_EarningsPerShareDiluted</td></tr></table>`)
	return b.String()
}

// Visa：各类别都有稀释每股收益和加权股数，股数以百万计；其他维度下的行不计入，稀释数优先于基本数。
func TestParseClassEarnings(t *testing.T) {
	page := earningsPage("CONSOLIDATED STATEMENTS OF OPERATIONS - USD ($) shares in Millions, $ in Millions", []string{"Sep. 30, 2025", "Sep. 30, 2024"},
		[4]string{"us-gaap_NetIncomeLoss", "", "$ 20,058", "$ 19,743"},
		[4]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassAMember", "", "", ""},
		[4]string{"us-gaap_EarningsPerShareBasic", "", "$ 10.22", "$ 9.74"},
		[4]string{"us-gaap_WeightedAverageNumberOfSharesOutstandingBasic", "", "1,714", "1,621"},
		[4]string{"us-gaap_EarningsPerShareDiluted", "", "$ 10.20", "$ 9.73"},
		[4]string{"us-gaap_WeightedAverageNumberOfDilutedSharesOutstanding", "", "1,966", "2,029"},
		[4]string{"us-gaap_StatementClassOfStockAxis=v_CommonClassB2Member", "", "", ""},
		[4]string{"us-gaap_EarningsPerShareDiluted", "[1]", "$ 15.70", ""},
		[4]string{"us-gaap_WeightedAverageNumberOfDilutedSharesOutstanding", "[1]", "120", ""},
		[4]string{"srt_ProductOrServiceAxis=v_ServiceMember", "", "", ""},
		[4]string{"us-gaap_EarningsPerShareDiluted", "", "$ 1.00", "$ 1.00"},
		[4]string{"us-gaap_WeightedAverageNumberOfDilutedSharesOutstanding", "", "1", "1"},
	)
	require.Equal(t, []ClassEarnings{
		{End: "2025-09-30", Member: "us-gaap_CommonClassAMember", EPS: 10.20, Shares: 1966e6},
		{End: "2024-09-30", Member: "us-gaap_CommonClassAMember", EPS: 9.73, Shares: 2029e6},
		{End: "2025-09-30", Member: "v_CommonClassB2Member", EPS: 15.70, Shares: 120e6},
	}, parseClassEarnings(page))

	// 伯克希尔：只列基本数，亏损用括号表示，股数没有单位说明。
	page = earningsPage("Consolidated Statements of Earnings - USD ($) $ in Millions", []string{"Dec. 31, 2022", "Dec. 31, 2021"},
		[4]string{"us-gaap_StatementClassOfStockAxis=brka_EquivalentClassBMember", "", "", ""},
		[4]string{"us-gaap_EarningsPerShareBasic", "[1]", "$ (10.33)", "$ 39.70"},
		[4]string{"us-gaap_WeightedAverageNumberOfSharesOutstandingBasic", "", "2,203,513,566", "2,253,431,412"},
	)
	require.Equal(t, []ClassEarnings{
		{End: "2022-12-31", Member: "brka_EquivalentClassBMember", EPS: -10.33, Shares: 2203513566},
		{End: "2021-12-31", Member: "brka_EquivalentClassBMember", EPS: 39.70, Shares: 2253431412},
	}, parseClassEarnings(page))

	// 表头不单独注明股数单位（"In Millions, except Per Share data"）时股数也按百万计；单元格里的脚注标记不影响数值。
	page = earningsPage("CONSOLIDATED STATEMENTS OF INCOME - USD ($) In Millions, except Per Share data", []string{"Dec. 31, 2025", "Dec. 31, 2024"},
		[4]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassAMember", "", "", ""},
		[4]string{"us-gaap_EarningsPerShareDiluted", "", "$ 5.63 [1]", "$ 5.00"},
		[4]string{"us-gaap_WeightedAverageNumberOfDilutedSharesOutstanding", "", "2,165", "2,200"},
	)
	require.Equal(t, []ClassEarnings{
		{End: "2025-12-31", Member: "us-gaap_CommonClassAMember", EPS: 5.63, Shares: 2165e6},
		{End: "2024-12-31", Member: "us-gaap_CommonClassAMember", EPS: 5.00, Shares: 2200e6},
	}, parseClassEarnings(page))

	require.Empty(t, parseClassEarnings("<html>not a statement</html>"))
}

func TestIncomeStatementPage(t *testing.T) {
	summary := func(reports ...[3]string) []byte {
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="utf-8"?><FilingSummary><MyReports>`)
		for _, r := range reports {
			fmt.Fprintf(&b, `<Report instance="x.htm"><HtmlFileName>%s</HtmlFileName><LongName>%s</LongName><MenuCategory>%s</MenuCategory></Report>`, r[0], r[1], r[2])
		}
		b.WriteString(`</MyReports></FilingSummary>`)
		return []byte(b.String())
	}
	require.Equal(t, "R4.htm", incomeStatementPage(summary(
		[3]string{"R1.htm", "0000001 - Document - Cover", "Cover"},
		[3]string{"R2.htm", "9952151 - Statement - CONSOLIDATED BALANCE SHEETS", "Statements"},
		[3]string{"R3.htm", "9952152 - Statement - CONSOLIDATED STATEMENTS OF OPERATIONS (Parenthetical)", "Statements"},
		[3]string{"R4.htm", "9952153 - Statement - CONSOLIDATED STATEMENTS OF OPERATIONS", "Statements"},
		[3]string{"R5.htm", "9952154 - Statement - CONSOLIDATED STATEMENTS OF COMPREHENSIVE INCOME", "Statements"},
		[3]string{"R6.htm", "9952155 - Statement - CONSOLIDATED STATEMENTS OF SHAREHOLDERS' EQUITY AND RETAINED EARNINGS", "Statements"},
		[3]string{"R9.htm", "9952158 - Disclosure - Earnings Per Share", "Notes"},
	)))
	require.Equal(t, "R4.htm", incomeStatementPage(summary(
		[3]string{"R3.htm", "100 - Statement - Consolidated Statements of Changes in Equity and Retained Earnings", "Statements"},
		[3]string{"R4.htm", "100 - Statement - Consolidated Statements of Income and Comprehensive Income", "Statements"},
	)), "利润表和综合收益表合并时用合并的表；名称含 earnings 的股东权益表不算")
	require.Empty(t, incomeStatementPage([]byte("not xml")))
}

func TestClassEarnings(t *testing.T) {
	defer gock.Off()
	c := client(t)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0001403161.json").Reply(200).
		BodyString(`{"name":"VISA","filings":{"recent":{"accessionNumber":["0002-25-000002","0002-25-000001","0002-24-000001","0002-15-000001"],` +
			`"filingDate":["2025-11-06","2025-07-29","2024-11-13","2015-11-19"],"form":["10-K","10-Q","10-K","10-K"]}}}`)
	summary := `<FilingSummary><MyReports><Report><HtmlFileName>R4.htm</HtmlFileName><LongName>2 - Statement - CONSOLIDATED STATEMENTS OF OPERATIONS</LongName><MenuCategory>Statements</MenuCategory></Report></MyReports></FilingSummary>`
	page := func(eps2, eps1 string) string {
		return earningsPage("CONSOLIDATED STATEMENTS OF OPERATIONS - USD ($) shares in Millions", []string{"Sep. 30, 2025", "Sep. 30, 2024"},
			[4]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassAMember", "", "", ""},
			[4]string{"us-gaap_EarningsPerShareDiluted", "", eps2, eps1},
			[4]string{"us-gaap_WeightedAverageNumberOfDilutedSharesOutstanding", "", "1,966", "2,029"})
	}
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1403161/000225000002/FilingSummary.xml").Reply(200).BodyString(summary)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1403161/000225000002/R4.htm").Reply(200).BodyString(page("$ 10.20", "$ 9.73"))
	// 没有 FilingSummary.xml 的年报缓存空结果；读取失败的年报跳过、计入 Failed，且不缓存。
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1403161/000215000001/FilingSummary.xml").Reply(404)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1403161/000224000001/FilingSummary.xml").Reply(500)

	result, err := c.ClassEarnings(context.Background(), 1403161, "2015-01-01")
	require.NoError(t, err)
	require.Equal(t, []ClassEarnings{
		{End: "2024-09-30", Filed: "2025-11-06", Member: "us-gaap_CommonClassAMember", EPS: 9.73, Shares: 2029e6},
		{End: "2025-09-30", Filed: "2025-11-06", Member: "us-gaap_CommonClassAMember", EPS: 10.20, Shares: 1966e6},
	}, result.Earnings)
	require.Equal(t, 1, result.Failed)

	// 第二次只重新读取上次失败的年报，其他读缓存。
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1403161/000224000001/FilingSummary.xml").Reply(200).BodyString(summary)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1403161/000224000001/R4.htm").Reply(200).BodyString(page("$ 9.70", "$ 8.28"))
	result, err = c.ClassEarnings(context.Background(), 1403161, "2015-01-01")
	require.NoError(t, err)
	require.Len(t, result.Earnings, 4)
	require.Zero(t, result.Failed)
	require.True(t, gock.IsDone())

	// 全部失败时返回错误。
	gock.New("https://data.sec.gov").Get("/submissions/CIK0001067983.json").Reply(200).
		BodyString(`{"name":"BERKSHIRE","filings":{"recent":{"accessionNumber":["0001-26-000001"],"filingDate":["2026-02-28"],"form":["10-K"]}}}`)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1067983/000126000001/FilingSummary.xml").Reply(500)
	_, err = c.ClassEarnings(context.Background(), 1067983, "2015-01-01")
	require.ErrorContains(t, err, "利润表")
}
