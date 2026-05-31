package strategy

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
)

func makeQuotes(prices []float64) []*eastmoney.Quote {
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	quotes := make([]*eastmoney.Quote, len(prices))
	for i, p := range prices {
		quotes[i] = &eastmoney.Quote{
			Date:  base.AddDate(0, 0, i),
			Close: p,
		}
	}
	return quotes
}

func TestSMA(t *testing.T) {
	prices := []float64{10, 12, 14, 16, 18}
	result := sma(prices, 3)
	// MA3: [0, 0, 12, 14, 16]
	require.Equal(t, 0.0, result[0])
	require.Equal(t, 0.0, result[1])
	require.InDelta(t, 12.0, result[2], 0.01) // (10+12+14)/3
	require.InDelta(t, 14.0, result[3], 0.01) // (12+14+16)/3
	require.InDelta(t, 16.0, result[4], 0.01) // (14+16+18)/3
}

func TestSMAInsufficientData(t *testing.T) {
	result := sma([]float64{10, 20}, 5)
	for _, v := range result {
		require.Equal(t, 0.0, v)
	}
}

func TestEMA(t *testing.T) {
	prices := make([]float64, 30)
	for i := range prices {
		prices[i] = 10.0
	}
	result := ema(prices, 10)
	require.InDelta(t, 10.0, result[9], 0.01)  // SMA seed
	require.InDelta(t, 10.0, result[29], 0.01) // stays flat with constant price
}

func TestComputeMA(t *testing.T) {
	// Flat then spike: MA5 crosses above MA20 when prices surge
	prices := []float64{}
	// 30 days flat at 10
	for i := 0; i < 30; i++ {
		prices = append(prices, 10.0)
	}
	// 20 days spike to 50
	for i := 0; i < 20; i++ {
		prices = append(prices, 10.0+float64(i+1)*2.0)
	}
	quotes := makeQuotes(prices)
	_, _, signals := ComputeMA(quotes, 5, 20)
	buyCount := 0
	for _, s := range signals {
		if s.Type == "buy" {
			buyCount++
		}
	}
	require.True(t, buyCount > 0, "expected at least one golden cross when prices spike up")
}

func TestComputeMADecline(t *testing.T) {
	// Flat then crash: MA5 crosses below MA20 when prices crash
	prices := []float64{}
	for i := 0; i < 30; i++ {
		prices = append(prices, 50.0)
	}
	for i := 0; i < 20; i++ {
		prices = append(prices, 50.0-float64(i+1)*2.0)
	}
	quotes := makeQuotes(prices)
	_, _, signals := ComputeMA(quotes, 5, 20)
	sellCount := 0
	for _, s := range signals {
		if s.Type == "sell" {
			sellCount++
		}
	}
	require.True(t, sellCount > 0, "expected at least one dead cross when prices crash")
}

func TestComputeMAEmpty(t *testing.T) {
	h, d, s := ComputeMA(nil, 5, 20)
	require.Nil(t, h)
	require.Nil(t, d)
	require.Nil(t, s)
}

func TestComputeMACD(t *testing.T) {
	prices := make([]float64, 50)
	for i := range prices {
		prices[i] = 10.0 + float64(i)*0.1
	}
	quotes := makeQuotes(prices)
	headers, data, signals := ComputeMACD(quotes, 12, 26, 9)
	require.NotNil(t, headers)
	require.Equal(t, 50, len(data))
	// Should have MACD, Signal, Histogram columns
	require.Contains(t, headers[2], "MACD")
	require.Contains(t, headers[3], "信号线")
	_ = signals // signals may or may not exist depending on trend
}

func TestComputeMACDEmpty(t *testing.T) {
	h, d, s := ComputeMACD(nil, 12, 26, 9)
	require.Nil(t, h)
	require.Nil(t, d)
	require.Nil(t, s)
}

func TestComputeRSI(t *testing.T) {
	// Create oscillating prices to produce varied RSI
	prices := []float64{10, 12, 11, 13, 12, 14, 13, 15, 14, 16, 15, 17, 16, 18, 17, 19, 18, 20}
	quotes := makeQuotes(prices)
	headers, data, signals := ComputeRSI(quotes, 14, 70, 30)
	require.NotNil(t, headers)
	require.True(t, len(data) > 0)
	// RSI should be calculated for period+ onward
	rsiCol := 2
	require.Contains(t, headers[rsiCol], "RSI")
	// RSI should be valid (between 0-100) for later entries
	lastRSI := data[len(data)-1][rsiCol]
	require.NotEqual(t, "-", lastRSI)
	_ = signals
}

func TestComputeRSIEmpty(t *testing.T) {
	h, d, s := ComputeRSI(nil, 14, 70, 30)
	require.Nil(t, h)
	require.Nil(t, d)
	require.Nil(t, s)
}

func TestComputeBollinger(t *testing.T) {
	prices := make([]float64, 50)
	for i := range prices {
		prices[i] = 50.0 + math.Sin(float64(i)*0.3)*20.0
	}
	quotes := makeQuotes(prices)
	headers, data, signals := ComputeBollinger(quotes, 20, 2.0)
	require.NotNil(t, headers)
	// Verify bands are ordered: lower < middle < upper
	for i, row := range data {
		if i < 19 {
			continue // insufficient data for bands
		}
		lo := parseOrZero(row[2])
		mid := parseOrZero(row[3])
		hi := parseOrZero(row[4])
		if lo > 0 && mid > 0 && hi > 0 {
			require.True(t, lo <= mid, "lower band should be <= middle at row %d", i)
			require.True(t, mid <= hi, "middle should be <= upper at row %d", i)
		}
	}
	_ = signals
}

func TestComputeBollingerEmpty(t *testing.T) {
	h, d, s := ComputeBollinger(nil, 20, 2.0)
	require.Nil(t, h)
	require.Nil(t, d)
	require.Nil(t, s)
}

func parseOrZero(s string) float64 {
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}
