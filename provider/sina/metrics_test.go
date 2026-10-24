package sina

import (
	"testing"

	"github.com/stretchr/testify/require"
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
			require.InDelta(t, 2, result.PB, 1e-9)
			require.InDelta(t, 5, result.PeTTM, 1e-9)
			require.InDelta(t, tc.cap, result.MarketCap, 1e-9)
			require.InDelta(t, tc.floatCap, result.TradedMarketCap, 1e-9)
			require.Equal(t, quote.TradeDate, result.TradeDate)
		})
	}
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
