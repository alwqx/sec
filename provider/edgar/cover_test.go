package edgar

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// coverPage 按 SEC 封面页的格式生成表格：rows 是 {概念, 第 1 列, 第 2 列}。
func coverPage(title string, dates []string, rows ...[3]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<table class="report"><tr><th class="tl" rowspan="2"><div><strong>%s<br></strong></div></th><th class="th">9 Months Ended</th><th class="th"></th></tr><tr>`, title)
	for _, d := range dates {
		fmt.Fprintf(&b, `<th class="th"><div>%s</div></th>`, d)
	}
	b.WriteString("</tr>")
	for _, r := range rows {
		fmt.Fprintf(&b, `<tr class="re"><td class="pl"><a onclick="Show.showAR( this, 'defref_%s', window );">x</a></td><td class="text">%s<span></span></td><td class="nump">%s<span></span></td></tr>`, r[0], r[1], r[2])
	}
	// 页面末尾的定义说明也带 defref，不能当成数据。
	b.WriteString(`</table><table><tr><td>Name:</td><td>dei_EntityCommonStockSharesOutstanding</td></tr></table>`)
	return b.String()
}

func TestParseCover(t *testing.T) {
	// 伯克希尔：交易代码在股数后面，债券只有交易代码。
	date, classes := parseCover(coverPage("Document and Entity Information - shares", []string{"Jun. 30, 2026", "Jul. 29, 2026"},
		[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassAMember", "&#160;", "&#160;"},
		[3]string{"dei_EntityCommonStockSharesOutstanding", "&#160;", "488,450"},
		[3]string{"dei_TradingSymbol", "BRK.A", "&#160;"},
		[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassBMember", "", ""},
		[3]string{"dei_EntityCommonStockSharesOutstanding", "", "1,408,503,417"},
		[3]string{"dei_TradingSymbol", "BRK.B", ""},
		[3]string{"us-gaap_StatementClassOfStockAxis=brka_Notes2033Member", "", ""},
		[3]string{"dei_TradingSymbol", "BRK33", ""},
	))
	require.Equal(t, "2026-07-29", date)
	require.Equal(t, []ShareClass{{"us-gaap_CommonClassAMember", "BRK.A", 488450}, {"us-gaap_CommonClassBMember", "BRK.B", 1408503417}}, classes)

	// Visa：交易代码在股数前面；表头注明股数单位；其他维度下的股数不计入。
	date, classes = parseCover(coverPage("Cover - shares in Thousands", []string{"Sep. 30, 2016", "Nov 09, 2016"},
		[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassAMember", "", ""},
		[3]string{"dei_TradingSymbol", "V", ""},
		[3]string{"dei_EntityCommonStockSharesOutstanding", "", "1,867,580"},
		[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassBMember", "", ""},
		[3]string{"dei_EntityCommonStockSharesOutstanding", "", "245,513"},
		[3]string{"dei_LegalEntityAxis=v_SubsidiaryMember", "", ""},
		[3]string{"dei_EntityCommonStockSharesOutstanding", "", "100"},
	))
	require.Equal(t, "2016-11-09", date)
	require.Equal(t, []ShareClass{{"us-gaap_CommonClassAMember", "V", 1867580e3}, {"us-gaap_CommonClassBMember", "", 245513e3}}, classes)

	date, classes = parseCover("<html>not a cover</html>")
	require.Empty(t, date)
	require.Empty(t, classes)
}

func TestCoverShares(t *testing.T) {
	defer gock.Off()
	c := client(t)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0001067983.json").Reply(200).
		BodyString(`{"name":"BERKSHIRE","filings":{"recent":{"accessionNumber":["0001-26-000002","0001-26-000001","0001-26-000003","0001-15-000001"],` +
			`"filingDate":["2026-08-10","2026-05-04","2026-03-02","2015-08-07"],"form":["10-Q","10-Q","8-K","10-Q"]}}}`)
	page := func(a, b string) string {
		return coverPage("Document and Entity Information - shares", []string{"Jun. 30, 2026", "Jul. 29, 2026"},
			[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassAMember", "", ""},
			[3]string{"dei_EntityCommonStockSharesOutstanding", "", a},
			[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassBMember", "", ""},
			[3]string{"dei_EntityCommonStockSharesOutstanding", "", b})
	}
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1067983/000126000002/R1.htm").Reply(200).BodyString(page("488,450", "1,408,503,417"))
	// 没有封面页的申报缓存空结果，读取失败的申报跳过且不缓存。
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1067983/000126000001/R1.htm").Reply(404)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1067983/000115000001/R1.htm").Reply(500)

	result, err := c.CoverShares(context.Background(), 1067983, "2015-01-01")
	require.NoError(t, err)
	require.Equal(t, []ClassShares{{Date: "2026-07-29", Filed: "2026-08-10", Classes: []ShareClass{
		{Member: "us-gaap_CommonClassAMember", Shares: 488450}, {Member: "us-gaap_CommonClassBMember", Shares: 1408503417}}}}, result.Points)
	require.Equal(t, 1, result.Failed, "读取失败的申报计入 Failed")

	// 第二次只重新读取上次失败的申报；同一个 Client 的申报列表不再下载。
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1067983/000115000001/R1.htm").Reply(200).BodyString(page("1,000", "2,000"))
	result, err = c.CoverShares(context.Background(), 1067983, "2015-01-01")
	require.NoError(t, err)
	require.Len(t, result.Points, 2)
	require.Zero(t, result.Failed)
	require.Equal(t, "2015-08-07", result.Points[0].Filed)
	require.True(t, gock.IsDone())

	// 全部失败时返回错误。
	gock.New("https://data.sec.gov").Get("/submissions/CIK0001403161.json").Reply(200).
		BodyString(`{"name":"VISA","filings":{"recent":{"accessionNumber":["0002-26-000001"],"filingDate":["2026-07-29"],"form":["10-Q"]}}}`)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1403161/000226000001/R1.htm").Reply(500)
	_, err = c.CoverShares(context.Background(), 1403161, "2015-01-01")
	require.ErrorContains(t, err, "读取 SEC 申报封面失败")
}

// 刚提交的申报读不到封面（R 页面还没生成）时不缓存，下次重新读。
func TestCoverSharesRecentNotFound(t *testing.T) {
	defer gock.Off()
	c := client(t)
	filed := time.Now().AddDate(0, 0, -2).Format(time.DateOnly)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0001067983.json").Reply(200).
		BodyString(`{"name":"BERKSHIRE","filings":{"recent":{"accessionNumber":["0001-26-000009"],"filingDate":["` + filed + `"],"form":["10-Q"]}}}`)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1067983/000126000009/R1.htm").Reply(404)
	result, err := c.CoverShares(context.Background(), 1067983, "2015-01-01")
	require.NoError(t, err)
	require.Empty(t, result.Points)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1067983/000126000009/R1.htm").Reply(200).
		BodyString(coverPage("Document and Entity Information - shares", []string{"Sep. 30, 2026", "Oct. 29, 2026"},
			[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassBMember", "", ""},
			[3]string{"dei_EntityCommonStockSharesOutstanding", "", "1,400,000,000"}))
	result, err = c.CoverShares(context.Background(), 1067983, "2015-01-01")
	require.NoError(t, err)
	require.Len(t, result.Points, 1, "没有缓存 404，第二次读到了")
	require.True(t, gock.IsDone())
}

// 交易代码不在第一列数值时也能识别。
func TestParseCoverSymbolColumn(t *testing.T) {
	_, classes := parseCover(coverPage("Document and Entity Information - shares", []string{"Jun. 30, 2026", "Jul. 29, 2026"},
		[3]string{"us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassBMember", "", ""},
		[3]string{"dei_TradingSymbol", "", "BRK.B"},
		[3]string{"dei_EntityCommonStockSharesOutstanding", "", "1,408,503,417"}))
	require.Len(t, classes, 1)
	require.Equal(t, "BRK.B", classes[0].Symbol)
}

// 表头第一行就写了某一列的日期（rowspan=2）时，按单元格位置对应列，不能按出现顺序。
func TestParseCoverRowspanDate(t *testing.T) {
	page := `<table>
<tr><th rowspan="2">Cover - shares</th><th colspan="1">6 Months Ended</th><th rowspan="2">Jul. 26, 2026</th></tr>
<tr><th>Jun. 30, 2026</th></tr>
<tr><td><a onclick="top.Show.showAR( this, 'defref_us-gaap_StatementClassOfStockAxis=us-gaap_CommonClassBMember', window );">Class B</a></td><td></td><td></td></tr>
<tr><td><a onclick="top.Show.showAR( this, 'defref_dei_EntityCommonStockSharesOutstanding', window );">Shares</a></td><td></td><td>1,400</td></tr>
</table>`
	date, classes := parseCover(page)
	require.Equal(t, "2026-07-26", date, "股数在第 2 个数值列，对应第一行跨两行的日期")
	require.Equal(t, 1400.0, classes[0].Shares)
}
