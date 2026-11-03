package backtest

import (
	"fmt"
	"testing"
	"time"

	"github.com/alwqx/sec/indicator"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
)

// bars 生成每天开盘价和收盘价相同的交易日，日期从 2026-01-01 起每天加一天。
func bars(prices ...float64) []Bar {
	result := make([]Bar, len(prices))
	prev := 0.0
	for i, price := range prices {
		result[i] = Bar{Date: fmt.Sprintf("2026-01-%02d", i+1), Open: price, Close: price, PrevClose: prev, Volume: 100, Limit: 0.1}
		prev = price
	}
	return result
}

func noCost() Config { return Config{Cash: 10000} }

func sig(index int, side, reason string) indicator.Signal {
	return indicator.Signal{Index: index, Type: side, Reason: reason}
}

func TestBuyNextOpenAndSell(t *testing.T) {
	data := bars(10, 10, 11, 12, 12)
	data[1].Open, data[3].Open = 10.5, 11.5 // 成交价用下一天的开盘价
	result, err := Run(data, []indicator.Signal{sig(0, indicator.Buy, "金叉"), sig(2, indicator.Sell, "死叉")}, noCost())
	require.NoError(t, err)
	require.Len(t, result.Trades, 2)
	buy, sell := result.Trades[0], result.Trades[1]
	require.Equal(t, "2026-01-02", buy.Date, "信号当天收盘产生，下一天开盘成交")
	require.Equal(t, "2026-01-01", buy.SignalDate)
	require.Equal(t, 10.5, buy.Price)
	require.Equal(t, int64(900), buy.Shares, "10000 元按 10.5 元只能买 9 手")
	require.Equal(t, "2026-01-04", sell.Date)
	require.InDelta(t, 900*(11.5-10.5), *sell.Profit, 1e-9)
	require.Equal(t, 1, result.RoundTrips)
	require.Equal(t, 100.0, *result.WinRate)
	require.InDelta(t, 10900, result.Strategy.FinalEquity, 1e-9)
	require.InDelta(t, 9, result.Strategy.TotalReturn, 1e-9)
	require.False(t, result.OpenPosition)
}

func TestFees(t *testing.T) {
	cfg := Config{Cash: 100000, Commission: 0.00025, MinCommission: 5, StampTax: 0.0005, Slippage: 0.001}
	data := bars(10, 10, 10)
	result, err := Run(data, []indicator.Signal{sig(0, indicator.Buy, ""), sig(1, indicator.Sell, "")}, cfg)
	require.NoError(t, err)
	buy, sell := result.Trades[0], result.Trades[1]
	require.InDelta(t, 10.01, buy.Price, 1e-9, "买入价上浮滑点")
	require.InDelta(t, 9.99, sell.Price, 1e-9, "卖出价下浮滑点")
	require.Equal(t, int64(9900), buy.Shares)
	require.InDelta(t, round(9900*10.01*0.00025), buy.Fee, 1e-9)
	require.InDelta(t, round(9900*9.99*(0.00025+0.0005)), sell.Fee, 1e-9, "卖出另收印花税")
	require.InDelta(t, buy.Fee+sell.Fee, result.TotalFees, 1e-9)
	require.Less(t, *sell.Profit, 0.0, "价格不变时成本导致亏损")

	small := Config{Cash: 2000, Commission: 0.00025, MinCommission: 5}
	result, err = Run(bars(10, 10, 10), []indicator.Signal{sig(0, indicator.Buy, "")}, small)
	require.NoError(t, err)
	require.Equal(t, 5.0, result.Trades[0].Fee, "不足 5 元按最低佣金")
	require.Equal(t, int64(100), result.Trades[0].Shares, "加上佣金后买不起 2 手")
}

func TestLimitAndSuspension(t *testing.T) {
	data := bars(10, 10, 11, 11, 11)
	data[1].Open = 11   // 开盘涨停
	data[2].Volume = 0  // 停牌
	data[3].Open = 11.2 // 可以买
	result, err := Run(data, []indicator.Signal{sig(0, indicator.Buy, "")}, noCost())
	require.NoError(t, err)
	require.Len(t, result.Trades, 1)
	require.Equal(t, "2026-01-04", result.Trades[0].Date, "涨停和停牌顺延到能成交的一天")
	require.Equal(t, []Event{{"2026-01-02", indicator.Buy, "开盘涨停，买不进，顺延"}, {"2026-01-03", indicator.Buy, "停牌，顺延"}}, result.Events)

	down := bars(10, 10, 10)
	down[2].Open = 9
	result, err = Run(down, []indicator.Signal{sig(0, indicator.Buy, ""), sig(1, indicator.Sell, "")}, noCost())
	require.NoError(t, err)
	require.Len(t, result.Trades, 1, "开盘跌停卖不出")
	require.True(t, result.OpenPosition)
	require.Contains(t, result.Events[len(result.Events)-1].Reason, "到区间结束仍未成交")
}

func TestOppositeSignalCancelsPendingOrder(t *testing.T) {
	data := bars(10, 10, 10, 10)
	data[1].Open = 11 // 涨停，买单顺延
	result, err := Run(data, []indicator.Signal{sig(0, indicator.Buy, ""), sig(1, indicator.Sell, "")}, noCost())
	require.NoError(t, err)
	require.Empty(t, result.Trades)
	require.Equal(t, "出现反向信号，未成交的委托作废", result.Events[1].Reason)
}

func TestSignalsThatDoNothing(t *testing.T) {
	result, err := Run(bars(10, 10, 10, 10), []indicator.Signal{sig(0, indicator.Sell, ""), sig(1, indicator.Buy, ""), sig(2, indicator.Buy, ""), sig(3, indicator.Sell, "")}, noCost())
	require.NoError(t, err)
	require.Len(t, result.Trades, 1, "空仓时卖出、持仓时再买入都不操作")
	require.Equal(t, "区间最后一天的信号，没有下一个交易日成交", result.Events[0].Reason)
	require.Nil(t, result.WinRate, "没有完成的轮次")
	require.True(t, result.OpenPosition)
}

func TestNotEnoughCash(t *testing.T) {
	result, err := Run(bars(100, 100, 100), []indicator.Signal{sig(0, indicator.Buy, "")}, Config{Cash: 5000})
	require.NoError(t, err)
	require.Empty(t, result.Trades)
	require.Equal(t, "资金不足一手，放弃", result.Events[0].Reason)
	require.Equal(t, 5000.0, result.Strategy.FinalEquity)
	require.Equal(t, 5000.0, result.Benchmark.FinalEquity, "基准也买不起")
}

func TestBenchmarkAndDrawdown(t *testing.T) {
	data := bars(10, 12, 9, 15)
	result, err := Run(data, nil, noCost())
	require.NoError(t, err)
	require.Equal(t, 0.0, result.Strategy.TotalReturn, "没有信号就一直空仓")
	require.Equal(t, 0.0, result.Exposure)
	require.InDelta(t, 50, result.Benchmark.TotalReturn, 1e-9, "第一天开盘买入持有")
	require.InDelta(t, 25, result.Benchmark.MaxDrawdown, 1e-9, "从 12 跌到 9")
	require.Equal(t, "2026-01-02", result.Benchmark.DrawdownStart)
	require.Equal(t, "2026-01-03", result.Benchmark.DrawdownEnd)
	require.Nil(t, result.Benchmark.Annualized, "不足 30 天不算年化")
}

func TestAnnualized(t *testing.T) {
	curve := []Point{{Date: "2025-01-01", Equity: 100}, {Date: "2026-01-01", Equity: 110}}
	p := performance(curve, 100, func(p Point) float64 { return p.Equity })
	require.InDelta(t, 10, *p.Annualized, 1e-9)
}

func TestValidateAndHelpers(t *testing.T) {
	require.NoError(t, DefaultConfig().Validate())
	_, err := Run(bars(10), nil, Config{Cash: 1})
	require.ErrorContains(t, err, "初始资金")
	_, err = Run(nil, nil, DefaultConfig())
	require.ErrorIs(t, err, ErrNoBars)
	require.Equal(t, 0.3, PriceLimit("BJ920045", "2026-01-01"))
	require.Equal(t, 0.2, PriceLimit("SH688047", "2026-01-01"))
	require.Equal(t, 0.1, PriceLimit("SZ300750", "2020-08-21"), "创业板注册制改革前是 10%")
	require.Equal(t, 0.2, PriceLimit("SZ300750", "2020-08-24"))
	require.Equal(t, 0.1, PriceLimit("SH600036", "2026-01-01"))
	require.NoError(t, Supported("sh600036"))
	require.Error(t, Supported("HK00700"))
}

func TestSupportedOnlyStocks(t *testing.T) {
	for _, code := range []string{"SH600036", "SH688981", "SZ000001", "SZ300750", "BJ920045", "BJ430047"} {
		require.NoError(t, Supported(code), code)
	}
	for _, code := range []string{"SH000001", "SZ399001", "SH510300", "SZ159915", "HK00700", "sh60003"} {
		require.Error(t, Supported(code), code)
	}
}

func TestNonPositivePrice(t *testing.T) {
	data := bars(10, 11, 12)
	data[1].Open, data[1].Close = -0.5, -0.4
	_, err := Run(data, nil, noCost())
	require.ErrorContains(t, err, "2026-01-02 的复权价格不是正数")
	data[1].Open, data[1].Close, data[1].Volume = 0, 11, 0
	_, err = Run(data, nil, noCost())
	require.NoError(t, err, "停牌日开盘价为 0 不算错")
}

// RunQuotes 只回测 quotes[first:]，前收盘价取自区间之前，区间之前的信号忽略。
func TestRunQuotes(t *testing.T) {
	quotes := []*eastmoney.Quote{}
	for i, price := range []float64{10, 10, 10, 10.5, 11} {
		quotes = append(quotes, &eastmoney.Quote{Date: time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC), Open: price, Close: price, Volume: 100})
	}
	result, err := RunQuotes("SH600036", quotes, 2, []indicator.Signal{sig(0, indicator.Buy, "早"), sig(2, indicator.Buy, "金叉")}, noCost())
	require.NoError(t, err)
	require.Equal(t, "2026-01-03", result.Begin)
	require.Len(t, result.Curve, 3)
	require.Len(t, result.Trades, 1)
	require.Equal(t, "2026-01-03", result.Trades[0].SignalDate)
	require.Equal(t, "2026-01-04", result.Trades[0].Date)
	_, err = RunQuotes("SH000001", quotes, 0, nil, noCost())
	require.ErrorContains(t, err, "A 股个股")
}
