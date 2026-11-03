package sina

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const futuresQuoteBody = `var hq_str_nf_SC0="上海原油连续,150000,717.600,719.700,677.300,698.200,698.200,698.500,698.200,694.900,745.200,1,5,24109.000,177041,沪,上海原油,2026-11-03,1,,,,,,,,,694.957,698.100,3";
var hq_str_hf_CL="98.456,,98.420,98.440,98.690,96.120,19:28:50,96.460,96.300,0,7,4,2026-11-03,纽约原油,0";
`

func TestParseCommodityQuotes(t *testing.T) {
	sc, _ := FindCommodity("SC")
	cl, _ := FindCommodity("cl")
	quotes, failed := parseCommodityQuotes(futuresQuoteBody, []Commodity{sc, cl})
	require.Empty(t, failed)
	require.Len(t, quotes, 2)

	require.Equal(t, "原油", quotes[0].Name)
	require.Equal(t, 698.2, quotes[0].Price)
	require.Equal(t, 717.6, quotes[0].Open)
	require.Equal(t, 677.3, quotes[0].Low)
	require.Equal(t, 745.2, *quotes[0].PrevSettle, "国内涨跌相对昨结算")
	require.InDelta(t, -47, *quotes[0].Change, 1e-9)
	require.Equal(t, "2026-11-03", quotes[0].Date)
	require.Equal(t, "15:00:00", quotes[0].Time)

	require.Equal(t, 98.456, quotes[1].Price)
	require.Equal(t, 96.3, quotes[1].Open)
	require.Equal(t, 98.69, quotes[1].High)
	require.Equal(t, 96.46, *quotes[1].PrevSettle)
	require.Equal(t, "19:28:50", quotes[1].Time)
	require.Equal(t, "美元/桶", quotes[1].Unit)

	// 个别品种缺行情或格式无效时，其余品种照常返回。
	hg, _ := FindCommodity("hg")
	quotes, failed = parseCommodityQuotes(futuresQuoteBody, []Commodity{sc, hg})
	require.Len(t, quotes, 1)
	require.Equal(t, []string{"COMEX 铜：新浪没有返回行情"}, failed)
	_, failed = parseCommodityQuotes(`var hq_str_nf_SC0="上海原油连续,150000";`, []Commodity{sc})
	require.Contains(t, failed[0], "字段不足")
}

func TestFindCommodity(t *testing.T) {
	item, err := FindCommodity(" Brent ")
	require.NoError(t, err)
	require.Equal(t, "布伦特原油", item.Name)
	_, err = FindCommodity("au")
	require.ErrorContains(t, err, "不支持的品种 au")
	require.ErrorContains(t, err, "hg（COMEX 铜）")
}

// 国内用前一天的结算价算涨跌，国际没有结算价，用前一天的收盘价；无效行跳过，日期乱序也能排好。
func TestParseCommodityHistory(t *testing.T) {
	sc, _ := FindCommodity("sc")
	bars, err := parseCommodityHistory(sc, `/*<script>location.href='//sina.com';</script>*/
var t=([{"d":"2026-11-03","o":"717.600","h":"719.700","l":"677.300","c":"698.200","v":"177041","p":"24109","s":"694.900"},{"d":"2026-11-02","o":"750.900","h":"763.900","l":"719.400","c":"729.500","v":"156040","p":"26207","s":"745.200"},{"d":"2026-11-01","o":"0","h":"0","l":"0","c":"0","v":"0","p":"0","s":"0"}]);`)
	require.NoError(t, err)
	require.Len(t, bars, 2)
	require.Equal(t, "2026-11-02", bars[0].Date)
	require.Nil(t, bars[0].Change)
	require.Equal(t, 745.2, *bars[1].PrevClose)
	require.Equal(t, "settle", bars[1].Basis)
	require.InDelta(t, 698.2-745.2, *bars[1].Change, 1e-9)
	require.Equal(t, 694.9, *bars[1].Settle)

	cl, _ := FindCommodity("cl")
	bars, err = parseCommodityHistory(cl, `var t=([{"date":"2026-11-02","open":"102.770","high":"102.750","low":"95.150","close":"96.250","volume":"0","position":"0","s":"0.000","settlement":"0"},{"date":"2026-11-03","open":"96.250","high":"98.690","low":"96.120","close":"98.430","volume":"0","position":"0","s":"0.000","settlement":"0"}]);`)
	require.NoError(t, err)
	require.Nil(t, bars[1].Settle)
	require.Equal(t, 96.25, *bars[1].PrevClose, "国际用昨收")
	require.Equal(t, "close", bars[1].Basis)

	_, err = parseCommodityHistory(cl, `<html>`)
	require.ErrorContains(t, err, "格式无效")
}

func TestQueryCommodityHistoryRange(t *testing.T) {
	defer gock.Off()
	gock.New("https://stock2.finance.sina.com.cn").Get("/futures/api/jsonp.php/var.t=/InnerFuturesNewService.getDailyKLine").MatchParam("symbol", "CU0").
		Reply(200).BodyString(`var t=([{"d":"2026-10-30","o":"1","h":"1","l":"1","c":"100","v":"1","s":"100"},{"d":"2026-11-02","o":"1","h":"1","l":"1","c":"102","v":"1","s":"101"},{"d":"2026-11-03","o":"1","h":"1","l":"1","c":"99","v":"1","s":"100"}]);`)
	cu, _ := FindCommodity("cu")
	bars, err := QueryCommodityHistory(context.Background(), cu, "2026-11-02", "2026-11-03")
	require.NoError(t, err)
	require.Len(t, bars, 2)
	require.InDelta(t, 2, *bars[0].Change, 1e-9, "区间第一天的涨跌用区间之前的结算价")
	require.True(t, gock.IsDone())
}

// 国内品种前一天没有结算价时，涨跌相对收盘价，并用 Basis 标出。
func TestParseCommodityHistoryMissingSettle(t *testing.T) {
	cu, _ := FindCommodity("cu")
	bars, err := parseCommodityHistory(cu, `var t=([{"d":"2005-01-04","o":"1","h":"1","l":"1","c":"100","v":"1","s":"0"},{"d":"2005-01-05","o":"1","h":"1","l":"1","c":"103","v":"1","s":"101"},{"d":"2005-01-06","o":"1","h":"1","l":"1","c":"102","v":"1","s":"0"}]);`)
	require.NoError(t, err)
	require.Equal(t, "close", bars[1].Basis)
	require.InDelta(t, 3, *bars[1].Change, 1e-9)
	require.Equal(t, "settle", bars[2].Basis)
	require.InDelta(t, 1, *bars[2].Change, 1e-9)
}
