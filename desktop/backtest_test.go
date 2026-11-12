package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

type backtestResponse struct {
	Backtest *struct {
		Begin  string `json:"begin"`
		Trades []struct {
			Date       string `json:"date"`
			SignalDate string `json:"signal_date"`
			Side       string `json:"side"`
		} `json:"trades"`
		Curve  []struct{ Date string } `json:"curve"`
		Events []struct {
			Reason string `json:"reason"`
		} `json:"events"`
		Config struct {
			Cash       float64 `json:"cash"`
			Commission float64 `json:"commission"`
		} `json:"config"`
	} `json:"backtest"`
	BacktestNote string `json:"backtest_note"`
}

func backtestRequest(t *testing.T, query string) backtestResponse {
	t.Helper()
	rr := macroRequest(t, "/api/v1/strategy?end=2026-10-30&kind=ma&fast=2&slow=3&begin=2026-01-03&"+query)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data backtestResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	return data
}

func TestStrategyBacktest(t *testing.T) {
	defer gock.Off()
	mockStrategyCloses([]float64{10, 9.5, 9.2, 9, 8.8, 9, 9.3, 9.6, 9.9, 10.1})
	data := backtestRequest(t, "code=SH600036&fq=qfq&cash=20000&commission=0.1")
	require.NotNil(t, data.Backtest, data.BacktestNote)
	require.Equal(t, "2026-01-03", data.Backtest.Begin, "只回测查询区间，不含预热数据")
	require.Len(t, data.Backtest.Curve, 8)
	require.Equal(t, 20000.0, data.Backtest.Config.Cash)
	require.InDelta(t, 0.001, data.Backtest.Config.Commission, 1e-12, "费率以 % 为单位传入")
	require.Len(t, data.Backtest.Trades, 1)
	require.Equal(t, "2026-01-07", data.Backtest.Trades[0].SignalDate)
	require.Equal(t, "2026-01-08", data.Backtest.Trades[0].Date, "下一交易日开盘成交")
}

// 每天涨 10% 以上，信号次日开盘涨停，一直买不进。
func TestStrategyBacktestLimitUp(t *testing.T) {
	defer gock.Off()
	mockStrategyCloses([]float64{10, 9, 8, 7, 6, 7, 8, 9, 10, 11})
	data := backtestRequest(t, "code=SH600036&fq=qfq")
	require.NotNil(t, data.Backtest)
	require.Empty(t, data.Backtest.Trades)
	require.Contains(t, data.Backtest.Events[0].Reason, "开盘涨停")
}

func TestStrategyBacktestNotes(t *testing.T) {
	for _, tc := range []struct{ query, note string }{
		{"code=SH600036&fq=bfq", "复权"},
		{"code=SH000001&fq=qfq", "A 股个股"},
		{"code=SH600036&fq=qfq&freq=week", "日线"},
		{"code=SH600036&fq=qfq&cash=10", "初始资金"},
		{"code=SH600036&fq=qfq&slippage=abc", "滑点须为数字"},
	} {
		mockStrategyCloses([]float64{10, 9, 8, 7, 6, 7, 8, 9, 10, 11})
		data := backtestRequest(t, tc.query)
		require.Nil(t, data.Backtest, tc.query)
		require.Contains(t, data.BacktestNote, tc.note, tc.query)
		gock.Off()
	}
}
