package history

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const klinePath = "/api/qt/stock/kline/get"

func mockSina(rows []map[string]string) {
	body, _ := json.Marshal(rows)
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(200).BodyString(string(body))
}

func row(day, open, high, low, closePrice string) map[string]string {
	return map[string]string{"day": day, "open": open, "high": high, "low": low, "close": closePrice, "volume": "1000"}
}

func unadjusted(begin, end string) *eastmoney.GetQuoteHistoryReq {
	return &eastmoney.GetQuoteHistoryReq{Code: "600036", MarketCode: 1, Begin: begin, End: end}
}

func TestPrimarySourceHasNoNote(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(200).
		BodyString(`{"data":{"code":"600036","market":1,"name":"招商银行","klines":["2026-10-30,1,2,3,0.5,100,1000,0,0,0,0"]}}`)
	result, err := Get(context.Background(), unadjusted("20261001", "20261031"))
	require.NoError(t, err)
	require.Empty(t, result.Note)
	require.False(t, result.Quotes[0].NoTurnover)
	require.False(t, gock.HasUnmatchedRequest())
}

func TestFallbackFiltersRangeAndComputesChange(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(503)
	mockSina([]map[string]string{
		row("2026-10-28", "9", "10", "9", "10"),
		row("2026-10-29", "10", "12", "10", "11"),
		row("2026-10-30", "11", "12", "10", "12"),
		row("2026-11-02", "12", "13", "11", "13"),
	})
	result, err := Get(context.Background(), unadjusted("20261029", "20261030"))
	require.NoError(t, err)
	require.Equal(t, FallbackNote, result.Note)
	require.Len(t, result.Quotes, 2, "只返回请求区间")
	first := result.Quotes[0]
	require.Equal(t, "2026-10-29", first.Date.Format("2006-01-02"))
	require.Equal(t, "600036", first.Code)
	require.Equal(t, eastmoney.MarketTypeSse, first.Market)
	require.Equal(t, int64(1000), first.Volume)
	require.True(t, first.NoTurnover)
	require.False(t, first.NoChange)
	require.Equal(t, 1.0, first.Change, "前一日收盘价来自区间之前的数据")
	require.InDelta(t, 10, first.ChangeRate, 1e-9)
	require.InDelta(t, 20, first.Amplitude, 1e-9)
}

func TestFallbackShenzhenSymbol(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(500)
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").MatchParam("symbol", "sz000001").Reply(200).
		BodyString(`[{"day":"2026-10-30","open":"1","high":"2","low":"1","close":"2","volume":"100"}]`)
	result, err := Get(context.Background(), &eastmoney.GetQuoteHistoryReq{Code: "000001", MarketCode: 0, Begin: "20261001"})
	require.NoError(t, err)
	require.Len(t, result.Quotes, 1)
	require.True(t, result.Quotes[0].NoChange, "第一条没有前一日收盘价")
	require.Equal(t, FallbackNote, result.Note)
}

// 新浪没有数据时不能当成这段时间没有行情，代码无效也是这种情况。
func TestEmptyFallbackIsAnError(t *testing.T) {
	for _, body := range []string{"null", "[]", ""} {
		gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(503)
		gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(200).BodyString(body)
		_, err := Get(context.Background(), unadjusted("20261001", "20261031"))
		require.ErrorContains(t, err, "HTTP 503", body)
		require.ErrorContains(t, err, "没有返回数据", body)
		gock.Off()
	}
}

// 新浪只有不复权数据，复权请求不能拿它顶替。
func TestAdjustedHasNoFallback(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Times(2).Reply(500)
	for _, fqt := range []eastmoney.FuQuanType{eastmoney.QuoteFQTFront, eastmoney.QuoteFQTPost} {
		req := unadjusted("20261001", "20261031")
		req.FQT = fqt
		_, err := Get(context.Background(), req)
		require.ErrorIs(t, err, ErrAdjustedUnavailable)
	}
}

func TestOtherMarketsHaveNoFallback(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(500)
	_, err := Get(context.Background(), &eastmoney.GetQuoteHistoryReq{Code: "00700", MarketCode: int(eastmoney.MarketTypeHK)})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrAdjustedUnavailable)
}

// 取满上限且开始日期更早时，数据被截断，不能悄悄返回一部分。
func TestFallbackRejectsTruncatedRange(t *testing.T) {
	bars := make([]*sina.Bar, sina.MaxBars)
	start := time.Date(2022, 8, 10, 0, 0, 0, 0, time.UTC)
	for i := range bars {
		bars[i] = &sina.Bar{Date: start.AddDate(0, 0, i).Format("2006-01-02"), Close: 10}
	}
	_, err := toQuotes(bars, unadjusted("20200101", ""), "20200101")
	require.ErrorIs(t, err, ErrRangeNotCovered)

	quotes, err := toQuotes(bars, unadjusted("20220810", ""), "20220810")
	require.NoError(t, err)
	require.Len(t, quotes, sina.MaxBars)

	// 预热部分超出范围时，只要可见区间有数据就可以返回。
	quotes, err = toQuotes(bars, unadjusted("20200101", ""), "20230101")
	require.NoError(t, err)
	require.Len(t, quotes, sina.MaxBars)

	// 不满上限说明已经到上市首日，早于上市日的开始日期不算截断。
	quotes, err = toQuotes(bars[:100], unadjusted("20200101", ""), "20200101")
	require.NoError(t, err)
	require.Len(t, quotes, 100)
}

func TestBothSourcesFail(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(500)
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(456)
	_, err := Get(context.Background(), unadjusted("20261001", "20261031"))
	require.ErrorContains(t, err, "东方财富")
	require.ErrorContains(t, err, "HTTP 456")
}

func TestCanceledRequestDoesNotFallBack(t *testing.T) {
	defer gock.Off()
	mockSina(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Get(ctx, unadjusted("20261001", "20261031"))
	require.Error(t, err)
	require.Len(t, gock.Pending(), 1, "取消后不再请求备用数据源")
}

// 周线和月线请求对应周期的数据，东方财富和新浪都一样。
func TestFreqIsPassedToBothSources(t *testing.T) {
	defer gock.Off()
	for _, tc := range []struct {
		freq       eastmoney.Freq
		klt, scale string
	}{{eastmoney.FreqDay, "101", "240"}, {eastmoney.FreqWeek, "102", "1200"}, {eastmoney.FreqMonth, "103", "7200"}, {"", "101", "240"}} {
		gock.New("https://push2his.eastmoney.com").Get(klinePath).MatchParam("klt", tc.klt).Reply(503)
		gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").MatchParam("scale", tc.scale).Reply(200).
			BodyString(`[{"day":"2026-10-30","open":"1","high":"2","low":"1","close":"2","volume":"100"}]`)
		req := unadjusted("20261001", "20261031")
		req.Freq = tc.freq
		result, err := Get(context.Background(), req)
		require.NoError(t, err, string(tc.freq))
		require.Len(t, result.Quotes, 1)
	}
	require.False(t, gock.IsPending())
}

func TestFallbackRoundsChange(t *testing.T) {
	quotes, err := toQuotes([]*sina.Bar{{Date: "2026-10-08", Close: 41.26}, {Date: "2026-10-09", Close: 41.71, High: 41.83, Low: 41}}, unadjusted("", ""), "")
	require.NoError(t, err)
	require.Equal(t, 0.45, quotes[1].Change)
	require.Equal(t, 1.09, quotes[1].ChangeRate)
	require.Equal(t, 2.01, quotes[1].Amplitude)
}
