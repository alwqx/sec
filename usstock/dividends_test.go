package usstock

import (
	"testing"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
)

// bars 生成不复权日线，每个 {日期, 收盘价} 一根。
func bars(points ...[2]any) []*sina.Bar {
	list := []*sina.Bar{}
	for _, p := range points {
		list = append(list, &sina.Bar{Date: p[0].(string), Close: p[1].(float64)})
	}
	return list
}

// 苹果式的拆股（2020-08-31 1 拆 4）：2018 财年的每股股息只出现在拆股前的年报里，按 1 拆 4 调整；2019 财年被拆股后的年报重述，不再调整。
// 只有利润的财年不列出；分红率 = 支付股息 ÷ 归母净利润；同比只和相邻财年比。
const dividendFacts = `{"entityName":"Apple Inc.","facts":{"us-gaap":{
"CommonStockDividendsPerShareDeclared":{"units":{"USD/shares":[
 {"start":"2017-10-01","end":"2018-09-29","val":2.72,"form":"10-K","filed":"2018-11-05"},
 {"start":"2018-09-30","end":"2019-09-28","val":3.00,"form":"10-K","filed":"2019-10-31"},
 {"start":"2018-09-30","end":"2019-09-28","val":0.75,"form":"10-K","filed":"2020-10-30"},
 {"start":"2019-09-29","end":"2020-09-26","val":0.795,"form":"10-K","filed":"2020-10-30"},
 {"start":"2014-09-28","end":"2015-09-26","val":1.98,"form":"10-K","filed":"2015-10-28"}]}},
"NetIncomeLoss":{"units":{"USD":[
 {"start":"2019-09-29","end":"2020-09-26","val":57411000000,"form":"10-K","filed":"2020-10-30"},
 {"start":"2020-09-27","end":"2021-09-25","val":94680000000,"form":"10-K","filed":"2021-10-29"}]}},
"PaymentsOfDividends":{"units":{"USD":[
 {"start":"2019-09-29","end":"2020-09-26","val":14081000000,"form":"10-K","filed":"2020-10-30"}]}},
"PaymentsForRepurchaseOfCommonStock":{"units":{"USD":[
 {"start":"2019-09-29","end":"2020-09-26","val":72358000000,"form":"10-K","filed":"2020-10-30"}]}}},
"dei":{"EntityCommonStockSharesOutstanding":{"units":{"shares":[
 {"end":"2018-10-19","val":4745398000,"form":"10-K","filed":"2018-11-05"},
 {"end":"2019-10-18","val":4443236000,"form":"10-K","filed":"2019-10-31"},
 {"end":"2020-10-16","val":17001802000,"form":"10-K","filed":"2020-10-30"}]}}}}}`

func TestBuildDividends(t *testing.T) {
	facts, err := edgar.ParseFacts(320193, []byte(dividendFacts))
	require.NoError(t, err)
	daily := bars([2]any{"2020-08-28", 499.23}, [2]any{"2020-08-31", 129.04})
	d := BuildDividends(edgar.Company{CIK: 320193, Ticker: "AAPL"}, facts, daily, 0)
	require.Equal(t, "AAPL", d.Ticker)
	require.Equal(t, "Apple Inc.", d.Name)
	require.Equal(t, "美元", d.CurrencyName)
	require.Equal(t, []Split{{Date: "2020-08-31", Factor: 4}}, d.Splits)

	years := map[int]DividendYear{}
	for _, y := range d.Years {
		years[y.FiscalYear] = y
	}
	require.NotContains(t, years, 2021, "只有利润，没有分红和回购")
	require.Len(t, d.Years, 4)
	require.Equal(t, 2020, d.Years[0].FiscalYear, "从新到旧")

	require.InDelta(t, 0.795, *years[2020].PerShare, 1e-9)
	require.InDelta(t, 6.0, *years[2020].Growth, 1e-9)
	require.InDelta(t, 14081.0/57411*100, *years[2020].Payout, 1e-9)
	require.Equal(t, 72358000000.0, *years[2020].Buyback)
	require.InDelta(t, 0.75, *years[2019].PerShare, 1e-9, "已被拆股后的年报重述")
	require.InDelta(t, 0.68, *years[2018].PerShare, 1e-9, "拆股前的年报，按 1 拆 4 调整")
	require.InDelta(t, (0.75/0.68-1)*100, *years[2019].Growth, 1e-9)
	require.Nil(t, years[2019].Payout, "没有支付股息和净利润")
	require.InDelta(t, 1.98/4, *years[2015].PerShare, 1e-9)
	require.Nil(t, years[2018].Growth, "2015 和 2018 不相邻，不跨年比较")
}

// 没有日线时不识别拆股，每股股息保持申报的原值。
func TestBuildDividendsWithoutBars(t *testing.T) {
	facts, err := edgar.ParseFacts(320193, []byte(dividendFacts))
	require.NoError(t, err)
	d := BuildDividends(edgar.Company{CIK: 320193, Ticker: "AAPL"}, facts, nil, 0)
	require.Empty(t, d.Splits)
	require.InDelta(t, 2.72, *d.Years[2].PerShare, 1e-9)
}

// 只有现金流量数据（回购）的财年也列出。
func TestBuildDividendsCashFlowOnlyYear(t *testing.T) {
	facts, err := edgar.ParseFacts(1, []byte(`{"facts":{"us-gaap":{
"NetIncomeLoss":{"units":{"USD":[{"start":"2024-01-01","end":"2024-12-31","val":100,"form":"10-K","filed":"2025-02-01"}]}},
"PaymentsForRepurchaseOfCommonStock":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":30,"form":"10-K","filed":"2026-02-01"}]}}}}}`))
	require.NoError(t, err)
	d := BuildDividends(edgar.Company{CIK: 1}, facts, nil, 0)
	require.Len(t, d.Years, 1)
	require.Equal(t, 2025, d.Years[0].FiscalYear)
	require.Equal(t, 30.0, *d.Years[0].Buyback)
}

// 价格跳变且前后股数对得上才算拆股：1 拆 1.5 也能识别；只有股数翻倍（换股并购、封面类别变化）或只有价格暴跌都不算。
func TestDetectSplits(t *testing.T) {
	shares := [][]SharePoint{{{Since: "2025-01-20", Shares: 100}, {Since: "2025-07-20", Shares: 150}}}
	require.Equal(t, []Split{{Date: "2025-06-02", Factor: 1.5}}, DetectSplits(bars([2]any{"2025-05-30", 30.0}, [2]any{"2025-06-02", 20.0}), shares, 0))
	require.Empty(t, DetectSplits(bars([2]any{"2025-05-30", 30.0}, [2]any{"2025-06-02", 29.0}), [][]SharePoint{{{Since: "2025-01-20", Shares: 100}, {Since: "2025-07-20", Shares: 200}}}, 0), "股数翻倍但价格没跳")
	require.Empty(t, DetectSplits(bars([2]any{"2025-05-30", 30.0}, [2]any{"2025-06-02", 15.0}), [][]SharePoint{{{Since: "2025-01-20", Shares: 100}, {Since: "2025-07-20", Shares: 100}}}, 0), "暴跌一半但股数没变")
	require.Equal(t, []Split{{Date: "2025-06-02", Factor: 2}}, DetectSplits(bars([2]any{"2025-05-30", 30.0}, [2]any{"2025-06-02", 15.0}), [][]SharePoint{{{Since: "2025-01-20", Shares: 100}}}, 200), "拆股后还没有申报时用当前股数核对")
}
