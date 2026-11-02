package sina

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestCalculateCoreMetrics(t *testing.T) {
	quote := &SecurityQuote{Current: 10, TradeDate: "2026-10-23", Time: "15:00:00"}
	profile := &sinaPartProfile{Category: "银行", VPS: 5, Cap: 10000, TradeCap: 5000, Profit: 2}
	for _, tc := range []struct {
		code          string
		cap, floatCap float64
	}{
		{"SH600036", 1e9, 5e8}, {"SZ000001", 1e9, 5e8}, {"BJ920000", 1e9, 5e8}, {"HK00700", 100000, 50000},
	} {
		t.Run(tc.code, func(t *testing.T) {
			result := calculateCoreMetrics(tc.code, quote, profile)
			require.Equal(t, "银行", result.Category)
			if tc.code == "HK00700" {
				require.Zero(t, result.PeTTM, "港股接口算不出 TTM")
				require.Zero(t, result.PB, "每股净资产的币种未标明")
			} else {
				require.InDelta(t, 5, result.PeTTM, 1e-9)
				require.InDelta(t, 2, result.PB, 1e-9)
			}
			require.InDelta(t, tc.cap, result.MarketCap, 1e-9)
			require.InDelta(t, tc.floatCap, result.TradedMarketCap, 1e-9)
			require.Equal(t, quote.TradeDate, result.TradeDate)
		})
	}
}

// 第 13 项是近四个季度净利润，第 18 项只是最新报告期累计值，用后者时半年报的 PE 会高一倍。
func TestParseAStockProfitIsTTM(t *testing.T) {
	line := "A,zsyh,5.7000,6.0149,3.0311,45.4000,4346.8609,2521984.5601,2062894.4429,2062894.4429,0,CNY,1501.8100,1516.9600,7.3000,1,6.7100,1781.8100,764.4500,43.16,34.277,0.1,招商银行,X|O|0|0|0,47.23|38.65,20260630|38222500000,671.3800|85.5940,|,,1/1,EQA,,2.34,40.084|41.260|41.770|39.184|41.260,股份制银行Ⅱ,,1,345744000000"
	profile, err := parseInfoPartProfileOfAstock(line)
	require.NoError(t, err)
	require.Equal(t, 1516.96, profile.Profit)
	result := calculateCoreMetrics("SH600036", &SecurityQuote{Current: 43.55}, profile)
	require.InDelta(t, 7.24, result.PeTTM, 0.01)
}

// 开盘前新浪的当前价是 0，指标按昨收计算，不能显示成没有数据。
func TestCalculateCoreMetricsFallsBackToYClose(t *testing.T) {
	profile := &sinaPartProfile{VPS: 5, Cap: 10000, TradeCap: 5000, Profit: 2}
	result := calculateCoreMetrics("SH600036", &SecurityQuote{Current: 0, YClose: 10}, profile)
	require.True(t, result.PriceIsYClose)
	require.Equal(t, 10.0, result.Price)
	require.InDelta(t, 2, result.PB, 1e-9)
	require.InDelta(t, 5, result.PeTTM, 1e-9)
	require.InDelta(t, 1e9, result.MarketCap, 1e-9)

	result = calculateCoreMetrics("SH600036", &SecurityQuote{Current: 11, YClose: 10}, profile)
	require.False(t, result.PriceIsYClose)
	require.Equal(t, 11.0, result.Price)

	result = calculateCoreMetrics("SH600036", &SecurityQuote{}, profile)
	require.False(t, result.PriceIsYClose, "昨收也没有时不伪造")
	require.Zero(t, result.PB)
	require.Zero(t, result.MarketCap)
}

func TestEffectivePrice(t *testing.T) {
	for _, tc := range []struct {
		quote  SecurityQuote
		price  float64
		yclose bool
	}{{SecurityQuote{Current: 11, YClose: 10}, 11, false}, {SecurityQuote{YClose: 10}, 10, true}, {SecurityQuote{}, 0, false}} {
		price, yclose := tc.quote.EffectivePrice()
		require.Equal(t, tc.price, price)
		require.Equal(t, tc.yclose, yclose)
	}
}

func TestQuerySecQuoteUpstreamStatus(t *testing.T) {
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(503).BodyString(`<html>"busy"</html>`)
	_, err := QuerySecQuote(context.Background(), "SH600036")
	require.ErrorContains(t, err, "HTTP 503")
}

func TestProfilePriceText(t *testing.T) {
	require.Equal(t, "10.50", (&CorpProfile{Current: 10.5}).PriceText())
	require.Contains(t, (&CorpProfile{Current: 10.5, PriceIsYClose: true}).PriceText(), "昨收")
}

func TestParseBShareHasNoRatios(t *testing.T) {
	line := "B,ysbg,0.1480,0.1384,0.1062,3.5249,67.5602,136767.3455,29337.0465,107430.299,29337.0465,US,2.0239,1.8928,70.8720,1,3.0220,45.9907,1.4521,0.895,0.562,0.1,云赛B股,X|O|0|0|0,0.752|0.616,20250930|48403049.46,701.0800|89.9590,|,,1/1,EQB,,0.00,0.651|0.651|0.684|0.592|0.598,软件开发,,3,6163454303.75"
	profile, err := parseInfoPartProfileOfAstock(line)
	require.NoError(t, err)
	result := calculateCoreMetrics("SH900901", &SecurityQuote{Current: 0.55}, profile)
	require.Zero(t, result.PeTTM)
	require.Zero(t, result.PB)
	require.Zero(t, result.MarketCap, "外币股价不能乘 A、B 股合计股本")
	require.Zero(t, result.TradedMarketCap)
}

func TestCalculateCoreMetricsUnknownRatios(t *testing.T) {
	result := calculateCoreMetrics("SH600036", &SecurityQuote{Current: 10}, &sinaPartProfile{Profit: -1})
	require.Zero(t, result.PB)
	require.Zero(t, result.PeTTM)
	require.Zero(t, result.MarketCap)
	result = calculateCoreMetrics("SH600036", &SecurityQuote{Current: 10}, &sinaPartProfile{VPS: -5, Profit: 0})
	require.Equal(t, -2.0, result.PB)
	require.Zero(t, result.PeTTM)
}
