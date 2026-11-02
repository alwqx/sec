//go:build windows || darwin

package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

type strategyResponse struct {
	Params map[string]float64 `json:"params"`
	Levels []float64          `json:"levels"`
	Dates  []string           `json:"dates"`
	Closes []float64          `json:"closes"`
	Series []struct {
		Label  string     `json:"label"`
		Panel  string     `json:"panel"`
		Style  string     `json:"style"`
		Values []*float64 `json:"values"`
	} `json:"series"`
	Signals []struct {
		Date   string  `json:"date"`
		Type   string  `json:"type"`
		Price  float64 `json:"price"`
		Reason string  `json:"reason"`
	} `json:"signals"`
	Required     int  `json:"required"`
	Insufficient bool `json:"insufficient"`
}

// mockStrategyCloses 从 2026-01-01 起每天一条，收盘价取自 closes。
func mockStrategyCloses(closes []float64) {
	lines := make([]string, 0, len(closes))
	for i, value := range closes {
		lines = append(lines, fmt.Sprintf(`"2026-%02d-%02d,%.2f,%.2f,%.2f,%.2f,100,1000,0,0,0,0"`, i/28+1, i%28+1, value, value, value, value))
	}
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(200).BodyString(`{"data":{"code":"600036","market":1,"name":"A","klines":[` + strings.Join(lines, ",") + `]}}`)
}

func strategyRequest(t *testing.T, query string) strategyResponse {
	t.Helper()
	rr := macroRequest(t, "/api/v1/strategy?code=SH600036&end=2026-10-30&"+query)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data strategyResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	return data
}

// 下跌后上涨，快线上穿慢线；信号日期和价格来自查询区间内。
func TestStrategyMA(t *testing.T) {
	defer gock.Off()
	mockStrategyCloses([]float64{10, 9, 8, 7, 6, 7, 8, 9, 10, 11})
	data := strategyRequest(t, "kind=ma&fast=2&slow=3&begin=2026-01-03")
	require.Equal(t, "2026-01-03", data.Dates[0], "预热数据不进入结果")
	require.Len(t, data.Dates, 8)
	require.Len(t, data.Closes, 8)
	require.Len(t, data.Series, 2)
	require.Equal(t, "MA2", data.Series[0].Label)
	require.Equal(t, "price", data.Series[0].Panel)
	require.Equal(t, 8.5, *data.Series[0].Values[0], "窗口使用了查询起点之前的数据")
	require.Len(t, data.Signals, 1)
	require.Equal(t, "buy", data.Signals[0].Type)
	require.Equal(t, "2026-01-07", data.Signals[0].Date)
	require.Equal(t, 8.0, data.Signals[0].Price)
	require.False(t, data.Insufficient)
}

// 查询起点之前触发的信号不返回。
func TestStrategySignalsBeforeRangeAreHidden(t *testing.T) {
	defer gock.Off()
	mockStrategyCloses([]float64{10, 9, 8, 7, 6, 7, 8, 9, 10, 11})
	data := strategyRequest(t, "kind=ma&fast=2&slow=3&begin=2026-01-08")
	require.Empty(t, data.Signals)
}

func TestStrategyWindowTooShortIsNullAndInsufficient(t *testing.T) {
	defer gock.Off()
	mockStrategyCloses([]float64{10, 11, 12})
	data := strategyRequest(t, "kind=ma&fast=2&slow=5&begin=2026-01-01")
	require.Nil(t, data.Series[0].Values[0])
	require.NotNil(t, data.Series[0].Values[1])
	require.Nil(t, data.Series[1].Values[2])
	require.True(t, data.Insufficient)
	require.Equal(t, 5, data.Required)
}

func TestStrategyRSIKeepsZeroAndLevels(t *testing.T) {
	defer gock.Off()
	mockStrategyCloses([]float64{10, 9, 8, 7, 6, 5})
	data := strategyRequest(t, "kind=rsi&period=2&begin=2026-01-01")
	require.Equal(t, []float64{30, 70}, data.Levels)
	require.Equal(t, "indicator", data.Series[0].Panel)
	require.Nil(t, data.Series[0].Values[1])
	require.NotNil(t, data.Series[0].Values[2], "只跌不涨时 RSI 是有效的 0")
	require.Zero(t, *data.Series[0].Values[2])
}

func TestStrategyMACDAndBollSeries(t *testing.T) {
	defer gock.Off()
	closes := make([]float64, 60)
	for i := range closes {
		closes[i] = 10 + math.Sin(float64(i)/4)*2
	}
	mockStrategyCloses(closes)
	macd := strategyRequest(t, "kind=macd&begin=2026-01-01")
	require.Equal(t, []string{"柱", "MACD", "信号线"}, []string{macd.Series[0].Label, macd.Series[1].Label, macd.Series[2].Label})
	require.Equal(t, "bar", macd.Series[0].Style)
	require.Nil(t, macd.Series[1].Values[24])
	require.NotNil(t, macd.Series[1].Values[25])
	require.Nil(t, macd.Series[2].Values[32])
	require.NotNil(t, macd.Series[2].Values[33])
	require.Equal(t, 35, macd.Required)

	mockStrategyCloses(closes)
	boll := strategyRequest(t, "kind=boll&begin=2026-01-01")
	require.Equal(t, "上轨", boll.Series[0].Label)
	require.Nil(t, boll.Series[0].Values[18])
	require.NotNil(t, boll.Series[0].Values[19])
	require.Greater(t, *boll.Series[0].Values[30], *boll.Series[2].Values[30])
}

// 前复权价格可以为负，指标不能被写成 0。
func TestStrategyNegativeAdjustedPrices(t *testing.T) {
	defer gock.Off()
	closes := []float64{-5, -4, -3, -2, -1, -2, -3, -4, -5, -6}
	mockStrategyCloses(closes)
	data := strategyRequest(t, "kind=macd&fast=2&slow=3&signal=2&begin=2026-01-01")
	require.Less(t, *data.Series[1].Values[9], 0.0)
	require.NotEmpty(t, data.Signals)
}

func TestStrategyWarmupIsAtLeastOneYear(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").
		MatchParam("beg", "20250701").Reply(200).BodyString(`{"data":{"code":"600036","market":1,"name":"A","klines":[]}}`)
	data := strategyRequest(t, "kind=rsi&begin=2026-07-01")
	require.Empty(t, data.Dates)
	require.False(t, gock.HasUnmatchedRequest())
}

func TestStrategyUpstreamFailure(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(500)
	rr := macroRequest(t, "/api/v1/strategy?code=SH600036&begin=2026-01-01&end=2026-02-01")
	require.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestStrategyRejectsInvalidParams(t *testing.T) {
	for _, query := range []string{
		"kind=nope", "kind=ma&fast=20&slow=5", "kind=ma&fast=0", "kind=ma&slow=251", "kind=ma&fast=abc",
		"kind=macd&fast=26&slow=12", "kind=macd&signal=0", "kind=rsi&period=0", "kind=rsi&oversold=70&overbought=30",
		"kind=rsi&overbought=101", "kind=boll&k=0", "kind=boll&k=11", "kind=boll&k=NaN", "kind=rsi&overbought=NaN", "kind=rsi&oversold=NaN", "kind=boll&k=Inf", "kind=boll&period=0", "fq=nope", "begin=2026-13-01",
	} {
		rr := macroRequest(t, "/api/v1/strategy?code=SH600036&"+query)
		require.Equal(t, http.StatusBadRequest, rr.Code, query)
	}
	rr := macroRequest(t, "/api/v1/strategy?code=BJ920025")
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func mockSinaBars() {
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(200).BodyString(`[
		{"day":"2026-01-02","open":"10","high":"11","low":"9","close":"10","volume":"1000"},
		{"day":"2026-01-03","open":"10","high":"12","low":"10","close":"11","volume":"2000"}]`)
}

func TestHistoryFallbackIsLabeledAndHasNoTurnover(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(503)
	mockSinaBars()
	rr := macroRequest(t, "/api/v1/history?code=SH600036&begin=2026-01-01&end=2026-01-31")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		SourceNote string           `json:"source_note"`
		Results    []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Contains(t, data.SourceNote, "新浪备用数据")
	require.Len(t, data.Results, 2)
	require.Nil(t, data.Results[0]["turnover"], "没有成交额时是 null，不是 0")
	require.Equal(t, 2000.0, data.Results[1]["volume"])
}

func TestStrategyFallback(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(503)
	mockSinaBars()
	rr := macroRequest(t, "/api/v1/strategy?code=SH600036&kind=ma&fast=1&slow=2&begin=2026-01-01&end=2026-01-31&fq=bfq")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "新浪备用数据")
	require.Contains(t, rr.Body.String(), "2026-01-03")
}

func TestAdjustedHistoryFailureSuggestsUnadjusted(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Times(2).Reply(503)
	for _, path := range []string{"/api/v1/history", "/api/v1/strategy"} {
		rr := macroRequest(t, path+"?code=SH600036&begin=2026-01-01&end=2026-01-31&fq=qfq")
		require.Equal(t, http.StatusBadGateway, rr.Code, path)
		require.Contains(t, rr.Body.String(), "可切换为不复权", path)
	}
}

func fullSinaBars(start time.Time) string {
	rows := make([]string, sina.MaxDailyBars)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"day":"%s","open":"10","high":"11","low":"9","close":"10","volume":"1000"}`, start.AddDate(0, 0, i).Format("2006-01-02"))
	}
	return "[" + strings.Join(rows, ",") + "]"
}

// 预热超出备用数据范围时，可见区间有数据就可以返回。
func TestFallbackWarmupIsBestEffort(t *testing.T) {
	defer gock.Off()
	body := fullSinaBars(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	for _, path := range []string{"/api/v1/strategy?kind=ma&fast=2&slow=3&fq=bfq", "/api/v1/history?ma=5&fq=bfq"} {
		gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(503)
		gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(200).BodyString(body)
		rr := macroRequest(t, path+"&code=SH600036&begin=2024-02-01&end=2026-01-01")
		require.Equal(t, http.StatusOK, rr.Code, path+rr.Body.String())
		require.Contains(t, rr.Body.String(), "新浪备用数据", path)
	}
}

func TestFallbackRangeNotCoveredSuggestsShortening(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(503)
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(200).BodyString(fullSinaBars(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)))
	rr := macroRequest(t, "/api/v1/history?code=SH600036&begin=2021-01-01&end=2026-01-01&fq=bfq")
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "请缩短查询区间")
}
