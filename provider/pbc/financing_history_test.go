package pbc

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

func parseHTML(t *testing.T, html string, year int, kind string) (financingTable, error) {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return parseFinancing(doc, year, kind)
}

// 2017 年存量表：第一个分项写作“其中:人民币贷款”。
const stock2017 = `<table>
<tr><td colspan="5">社会融资规模存量统计表</td></tr>
<tr><td colspan="5">单位：万亿元人民币</td></tr>
<tr><td></td><td>2017.1</td><td>2017.1</td><td>2017.2</td><td>2017.2</td></tr>
<tr><td>项目Items</td><td>存量</td><td>增速（%）</td><td>存量</td><td>增速（%）</td></tr>
<tr><td>社会融资规模存量AFRE（stock)</td><td>159.61</td><td>12.7</td><td>160.69</td><td>12.8</td></tr>
<tr><td>其中:人民币贷款Ofwhich:RMBloans</td><td>107.50</td><td>12.8</td><td>108.53</td><td>12.9</td></tr>
<tr><td colspan="243">注：1.社会融资规模存量是指一定时期末实体经济从金融体系获得的资金余额。</td></tr>
</table>`

// 2017 年增量表：月份行 2017.01 不能被当成编号注释。
const flow2017 = `<table>
<tr><td colspan="3">社会融资规模增量统计表</td></tr>
<tr><td colspan="3">单位：亿元人民币</td></tr>
<tr><td>项目Items月份Month</td><td>社会融资规模增量</td><td>人民币贷款</td></tr>
<tr><td>2017.01</td><td>36970</td><td>23133</td></tr>
<tr><td>2017.02</td><td>10908</td><td>10317</td></tr>
<tr><td colspan="3">注：1.社会融资规模增量是指一定时期内实体经济从金融体系获得的资金额。</td></tr>
<tr><td colspan="3">2.数据来源于中国人民银行。</td></tr>
</table>`

func TestFinancingStockWithOfWhichPrefix(t *testing.T) {
	table, err := parseHTML(t, stock2017, 2017, "stock")
	require.NoError(t, err, "注释行横跨两百多列也能解析")
	require.Equal(t, 107.50, *table.Values["201701"]["rmb-loans"].Amount, "其中:人民币贷款归入人民币贷款")
	require.Equal(t, "人民币贷款", table.Names["rmb-loans"])
	for key := range table.Names {
		require.False(t, strings.HasPrefix(key, "other:"), key)
	}
}

func TestFinancingNotesExcludeMonthRows(t *testing.T) {
	table, err := parseHTML(t, flow2017, 2017, "flow")
	require.NoError(t, err)
	require.Len(t, table.Notes, 2)
	for _, note := range table.Notes {
		require.False(t, strings.HasPrefix(note, "2017."), note)
	}
	require.Equal(t, 36970.0, *table.Values["201701"]["total"].Amount)
}

// 2019 年增量表：主表后附带口径调整后的历史表，标题在第二列，其中又有本年月份。
func TestFinancingIgnoresAppendixTables(t *testing.T) {
	html := `<table>
<tr><td colspan="3">社会融资规模增量统计表</td></tr>
<tr><td colspan="3">单位：亿元人民币</td></tr>
<tr><td>项目Items月份Month</td><td>社会融资规模增量</td><td>人民币贷款</td></tr>
<tr><td>2019.01</td><td>46791</td><td>35668</td></tr>
<tr><td colspan="3">注：1.社会融资规模增量是指一定时期内实体经济从金融体系获得的资金额。</td></tr>
<tr><td></td><td colspan="2">表1：2017年以来各月完善后的社会融资规模增量数据</td></tr>
<tr><td>月份Month</td><td>社会融资规模当月增量AFRE(flow)</td><td>人民币贷款RMBloans</td></tr>
<tr><td>2019.01</td><td>47000</td><td>35668</td></tr>
</table>`
	table, err := parseHTML(t, html, 2019, "flow")
	require.NoError(t, err, "附表里的月份不能算重复")
	require.Equal(t, 46791.0, *table.Values["201901"]["total"].Amount, "用当时公布的主表")
	require.Contains(t, table.Notes, "页面附带的口径调整历史表未采用，数值为当时公布的主表")

	// 存量表的附表有表头和数据行，不能把附表标题当成数据。真实页面里注释在附表之前，要保留。
	stock := strings.Replace(stock2017, `</table>`, `<tr><td></td><td colspan="4">表1：2017年以来各月完善后的社会融资规模存量余额</td></tr><tr><td>月份/Months</td><td>2017.1</td><td>2017.1</td><td>2017.2</td><td>2017.2</td></tr><tr><td>存量（亿元）</td><td>1648295</td><td>1648295</td><td>1659291</td><td>1659291</td></tr></table>`, 1)
	table, err = parseHTML(t, stock, 2017, "stock")
	require.NoError(t, err)
	require.Equal(t, 160.69, *table.Values["201702"]["total"].Amount)
	require.Contains(t, strings.Join(table.Notes, "\n"), "社会融资规模存量是指", "附表之前的官方注释保留")
}

func TestFinancingTitleStartingWithTableNumber(t *testing.T) {
	html := strings.Replace(flow2017, `社会融资规模增量统计表</td>`, `表1：社会融资规模增量统计表</td>`, 1)
	table, err := parseHTML(t, html, 2017, "flow")
	require.NoError(t, err, "表头之前的“表1：”是主表标题，不能截断")
	require.Equal(t, 36970.0, *table.Values["201701"]["total"].Amount)
}

func TestFinancingSpanLimits(t *testing.T) {
	_, err := parseHTML(t, strings.Replace(stock2017, `colspan="243"`, `colspan="301"`, 1), 2017, "stock")
	require.ErrorContains(t, err, "合并单元格无效")
	_, err = parseHTML(t, strings.Replace(stock2017, `<td>项目Items</td>`, `<td rowspan="101">项目Items</td>`, 1), 2017, "stock")
	require.ErrorContains(t, err, "合并单元格无效")
}

func TestFirstText(t *testing.T) {
	require.Equal(t, "表1：x", firstText([]string{"", "", "表1：x"}))
	require.Empty(t, firstText([]string{"", ""}))
}

func TestFinancingGridCellLimit(t *testing.T) {
	var rows strings.Builder
	rows.WriteString(`<table><tr><td>社会融资规模存量统计表 单位：万亿元人民币</td></tr>`)
	for i := 0; i < 8; i++ {
		rows.WriteString(`<tr><td rowspan="100" colspan="300">x</td></tr>`)
	}
	rows.WriteString(`</table>`)
	_, err := parseHTML(t, rows.String(), 2017, "stock")
	require.ErrorContains(t, err, "单元格超过限制")
}
