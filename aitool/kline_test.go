package aitool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func callKlineJSON(t *testing.T, input string) (*Result, *klineData, error) {
	t.Helper()
	result, err := KlineSummaryTool().Call(context.Background(), json.RawMessage(input))
	if err != nil {
		return nil, nil, err
	}
	data, ok := result.Data.(*klineData)
	require.True(t, ok)
	return result, data, nil
}

func TestSummarizeKline(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	quotes := []*eastmoney.Quote{}
	for i, close := range []float64{10, 12, 9, 11, 15, 12} {
		quotes = append(quotes, &eastmoney.Quote{
			Date: day.AddDate(0, 0, i), Open: close, Close: close, High: close + 1, Low: close - 1, Volume: 100 * int64(i+1), TurnOver: 1000,
		})
	}

	summary := summarizeKline(quotes)
	require.Equal(t, 6, summary.Bars)
	require.Equal(t, "2026-10-01", summary.FirstDate)
	require.Equal(t, "2026-10-06", summary.LastDate)
	require.Equal(t, 20.0, *summary.ChangePct, "12 / 10 - 1")
	require.Equal(t, pricePoint{16, "2026-10-05"}, summary.High)
	require.Equal(t, pricePoint{8, "2026-10-03"}, summary.Low)
	require.Equal(t, drawdown{Pct: -25, PeakDate: "2026-10-02", TroughDate: "2026-10-03"}, summary.MaxDrawdown, "12 到 9 比 15 到 12 跌得多")
	require.Equal(t, 11.8, *summary.MA.MA5, "(12+9+11+15+12) / 5")
	require.Nil(t, summary.MA.MA20, "根数不够")
	require.Nil(t, summary.MA.MA60)
	require.Equal(t, 350.0, summary.AvgVolume)
	require.Equal(t, 1000.0, *summary.AvgAmount)

	// 有一根没有成交额时，平均成交额是 null 而不是按 0 算。
	quotes[2].NoTurnover = true
	require.Nil(t, summarizeKline(quotes).AvgAmount)

	// 前复权的早期价格 ≤ 0：涨跌幅是 null，回撤不计这些 K 线。
	adjusted := summarizeKline([]*eastmoney.Quote{
		{Date: day, Close: -1}, {Date: day.AddDate(0, 0, 1), Close: 10}, {Date: day.AddDate(0, 0, 2), Close: -2}, {Date: day.AddDate(0, 0, 3), Close: 8},
	})
	require.Nil(t, adjusted.ChangePct)
	require.Equal(t, drawdown{Pct: -20, PeakDate: "2026-10-02", TroughDate: "2026-10-04"}, adjusted.MaxDrawdown)

	// 一路上涨没有回撤。
	rising := summarizeKline(quotes[:2])
	require.Equal(t, drawdown{Pct: 0, PeakDate: "2026-10-01", TroughDate: "2026-10-01"}, rising.MaxDrawdown)
}

// klineLines 生成 n 根东方财富日线，收盘价从 10 开始每天涨 0.1。
func klineLines(n int) string {
	lines := make([]string, 0, n)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		close := 10 + float64(i)/10
		lines = append(lines, fmt.Sprintf(`"%s,%.2f,%.2f,%.2f,%.2f,1000,%.2f,1.00,1.00,0.10,0.50"`,
			day.AddDate(0, 0, i).Format(time.DateOnly), close, close, close+0.5, close-0.5, close*100000))
	}
	return strings.Join(lines, ",")
}

func TestKlineSummary(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").
		MatchParam("secid", `^1\.600036$`).MatchParam("fqt", "^1$").MatchParam("klt", "^101$").
		MatchParam("beg", "^20260901$").MatchParam("end", "^20261031$").
		Reply(200).BodyString(`{"data":{"code":"600036","market":1,"name":"招商银行","klines":[` + klineLines(25) + `]}}`)

	result, data, err := callKlineJSON(t, `{"code":"600036","begin":"2026-09-01","end":"2026-10-31"}`)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, "get_kline_summary", result.Tool)
	require.Equal(t, "东方财富 K 线", result.Source)
	require.Equal(t, "2026-09-25", result.AsOf, "最后一根 K 线的日期")
	require.Equal(t, "SH600036", data.ExCode)
	require.Equal(t, "day", data.Freq)
	require.Equal(t, "qfq", data.FQ, "默认前复权")
	require.Equal(t, "2026-09-01", data.Begin)

	require.Equal(t, 25, data.Summary.Bars)
	require.Equal(t, 10.0, data.Summary.FirstClose)
	require.Equal(t, 12.4, data.Summary.LastClose)
	require.Equal(t, 24.0, *data.Summary.ChangePct)
	require.Equal(t, 12.2, *data.Summary.MA.MA5)
	require.Equal(t, 100000.0, data.Summary.AvgVolume, "A 股成交量从手换成股")

	require.Len(t, data.Recent, recentBars)
	require.Equal(t, "2026-09-06", data.Recent[0].Date, "只留最近 20 根，按日期升序")
	require.Equal(t, 1.0, *data.Recent[0].ChangePct)
	require.NotNil(t, data.Recent[0].Amount)

	notes := strings.Join(result.Notes, "\n")
	require.Contains(t, notes, "前复权")
	require.Contains(t, notes, "区间共 25 根 K 线，recent 只列最近 20 根")
	require.NotContains(t, notes, "没有成交额")
}

func TestKlineSummaryFallback(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Persist().Reply(200).BodyString(cmbSuggest)
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Persist().Reply(503)

	// 前复权没有备用数据，是上游问题。
	_, _, err := callKlineJSON(t, `{"code":"600036","begin":"2026-10-01","end":"2026-10-31"}`)
	require.False(t, IsInputError(err))
	require.ErrorContains(t, err, "K 线获取失败")
	require.ErrorContains(t, err, "可切换为不复权")

	// 不复权改用新浪日线，没有成交额。
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(200).BodyString(
		`[{"day":"2026-10-29","open":"10","high":"11","low":"9","close":"10","volume":"100"},` +
			`{"day":"2026-10-30","open":"10","high":"12","low":"10","close":"11","volume":"300"}]`)
	result, data, err := callKlineJSON(t, `{"code":"600036","begin":"2026-10-01","end":"2026-10-31","fq":"bfq"}`)
	require.NoError(t, err)
	require.Equal(t, "新浪财经 K 线（备用）", result.Source)
	require.Equal(t, "bfq", data.FQ)
	require.Nil(t, data.Summary.AvgAmount)
	require.Nil(t, data.Recent[0].ChangePct, "第一根没有前收盘")
	require.Equal(t, 10.0, *data.Recent[1].ChangePct)
	notes := strings.Join(result.Notes, "\n")
	require.Contains(t, notes, "新浪备用数据")
	require.Contains(t, notes, "没有成交额")
	require.NotContains(t, notes, "复权价")
}

func TestKlineSummaryErrors(t *testing.T) {
	for input, want := range map[string]string{
		`{}`:                              "code 不能为空",
		`{"code":"x","freq":"y"}`:         "freq",
		`{"code":"x","fq":"y"}`:           "fq",
		`{"code":"x","begin":"20260101"}`: "YYYY-MM-DD",
		`{"code":"x","end":"2026/01/31"}`: "YYYY-MM-DD",
		`{"code":"x","begin":"2026-02-01","end":"2026-01-01"}`: "不能晚于",
	} {
		_, _, err := callKlineJSON(t, input)
		require.True(t, IsInputError(err), input)
		require.ErrorContains(t, err, want, input)
	}

	defer gock.Off()
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	_, _, err := callKlineJSON(t, `{"code":"nosuch"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "没有找到证券 nosuch")

	suggestKey("920000").Reply(200).BodyString(`var suggestvalue="安徽凤凰,11,920000,bj920000,安徽凤凰,,安徽凤凰,99,1,,,,";`)
	_, _, err = callKlineJSON(t, `{"code":"920000"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "BJ920000（安徽凤凰）暂不支持 K 线")

	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").
		Reply(200).BodyString(`{"data":{"code":"600036","market":1,"name":"招商银行","klines":[]}}`)
	_, _, err = callKlineJSON(t, `{"code":"600036","begin":"2026-10-01","end":"2026-10-07"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "没有 K 线")
}

func TestKlineDefaultRange(t *testing.T) {
	end := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	for freq, want := range map[eastmoney.Freq]string{eastmoney.FreqDay: "2025-10-31", eastmoney.FreqWeek: "2023-10-31", eastmoney.FreqMonth: "2016-10-31"} {
		begin, gotEnd, err := klineRange("", "2026-10-31", freq)
		require.NoError(t, err)
		require.Equal(t, end, gotEnd)
		require.Equal(t, want, begin.Format(time.DateOnly), freq)
	}

	// 没给 end 时是上海时区的今天。
	begin, gotEnd, err := klineRange("", "", eastmoney.FreqDay)
	require.NoError(t, err)
	require.Equal(t, utils.ShanghaiToday(), gotEnd)
	require.Equal(t, gotEnd.AddDate(-1, 0, 0), begin)
}

func TestKlineSummaryUpstreamInputErrors(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Persist().Reply(200).BodyString(cmbSuggest)

	// 东方财富没有这只证券：前复权也不提示切换为不复权。
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(200).BodyString(`{"data":null}`)
	_, _, err := callKlineJSON(t, `{"code":"600036","begin":"2026-10-01","end":"2026-10-31","fq":"QFQ"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "东方财富没有 SH600036（招商银行）的 K 线数据")
	require.NotContains(t, err.Error(), "不复权")

	// 新浪备用数据取满上限仍晚于请求的开始日期：要缩短区间。
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(503)
	rows := make([]string, 0, sina.MaxBars)
	day := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range sina.MaxBars {
		rows = append(rows, fmt.Sprintf(`{"day":"%s","open":"1","high":"1","low":"1","close":"1","volume":"1"}`, day.AddDate(0, 0, i).Format(time.DateOnly)))
	}
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(200).BodyString("[" + strings.Join(rows, ",") + "]")
	_, _, err = callKlineJSON(t, `{"code":"600036","begin":"2010-01-01","end":"2026-10-31","fq":"bfq"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "请缩短区间")
}
