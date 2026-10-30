package forex

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func leg(pair, date string, value float64, stamp string) Observation {
	item := NewObservation(pair, date, "https://example.com/official", time.Date(2024, 1, 2, 8, 1, 0, 0, time.UTC))
	item.Value = &value
	item.ObservedAt = stamp
	item.RawValues["original"] = "source value"
	return item
}
func dailyInput() *Result {
	return &Result{Provider: "Federal Reserve via FRED", PriceType: "noon-buying-rate", Frequency: "daily", Timezone: "America/New_York", ObservationTime: "12:00", DateBasis: "observation-date", Begin: "20240102", End: "20240103", Observations: []Observation{leg("USD/HKD", "2024-01-02", 7.8, ""), leg("USD/CNY", "2024-01-02", 7.2, "")}}
}
func quoteInput() *Result {
	return &Result{Provider: "Sina", PriceType: "indicative-quote", Frequency: "snapshot", Timezone: "Asia/Shanghai", DateBasis: "provider-quote-time", Observations: []Observation{leg("USD/CNY", "2024-01-02", 7.2, "2024-01-02T16:00:00+08:00"), leg("USD/CNH", "2024-01-02", 7.21, "2024-01-02T16:00:00+08:00")}}
}
func strictPolicy() QuotePolicy {
	return QuotePolicy{Now: time.Date(2024, 1, 2, 8, 1, 0, 0, time.UTC), MaxAge: 15 * time.Minute}
}

func TestCrossRatesDirectionsAndInputProvenance(t *testing.T) {
	input := dailyInput()
	result, err := CrossRates(input, []string{"HKD/CNY", "CNY/HKD", "CNY/USD", "USD/CNY"}, QuotePolicy{})
	require.NoError(t, err)
	require.Len(t, result.Observations, 4)
	for i, want := range []float64{7.2 / 7.8, 7.8 / 7.2, 1 / 7.2, 7.2} {
		item := result.Observations[i]
		require.InDelta(t, want, *item.Value, 1e-12)
		require.True(t, item.IsDerived)
		require.NotEmpty(t, item.Formula)
		require.NotEmpty(t, item.Inputs)
		require.Equal(t, "same-fixed-time", item.Alignment.Status)
		require.NotEmpty(t, item.Inputs[0].RawValues)
	}
	require.Equal(t, "USD/CNY / USD/HKD", result.Observations[0].Formula)
	require.Equal(t, "America/New_York", result.Timezone)
	require.Equal(t, "noon-buying-rate", result.Derivation.InputPriceType)
	input.Observations[0], input.Observations[1] = input.Observations[1], input.Observations[0]
	again, err := CrossRates(input, []string{"HKD/CNY", "CNY/HKD", "CNY/USD", "USD/CNY"}, QuotePolicy{})
	require.NoError(t, err)
	require.Equal(t, result, again)
}

func TestCrossCFETSFixedTimeAndMissingDates(t *testing.T) {
	input := &Result{Provider: "CFETS", PriceType: "onshore-reference", Frequency: "daily", Timezone: "Asia/Shanghai", DateBasis: "observation-date", ObservationTime: "16:00", Observations: []Observation{leg("USD/CNY", "2024-01-02", 7.2, "2024-01-02T16:00:00+08:00"), leg("HKD/CNY", "2024-01-02", 0.9, "2024-01-02T16:00:00+08:00"), leg("USD/CNY", "2024-01-03", 7.2, "2024-01-03T16:00:00+08:00")}}
	result, err := CrossRates(input, []string{"USD/HKD", "HKD/USD"}, QuotePolicy{})
	require.NoError(t, err)
	require.Len(t, result.Observations, 4)
	require.InDelta(t, 8, *result.Observations[0].Value, 1e-12)
	require.InDelta(t, 0.125, *result.Observations[1].Value, 1e-12)
	require.Nil(t, result.Observations[2].Value)
	require.Equal(t, "missing-input", result.Observations[2].Alignment.Status)
	input.Observations[1].ObservedAt = "2024-01-02T16:00:30+08:00"
	result, err = CrossRates(input, []string{"USD/HKD"}, QuotePolicy{})
	require.NoError(t, err)
	require.Nil(t, result.Observations[0].Value)
	require.Equal(t, "timestamp-mismatch", result.Observations[0].Alignment.Status)
}

func TestCrossRejectsInvalidInputsAndUnsupportedDefinitions(t *testing.T) {
	for _, change := range []func(*Result){
		func(r *Result) { r.PriceType = "provider-daily-bar" }, func(r *Result) { r.Provider = "HKMA" }, func(r *Result) { r.ObservationTime = "16:30" },
		func(r *Result) { r.Observations = append(r.Observations, r.Observations[0]) },
		func(r *Result) { r.Observations[0].QuoteCurrency = "CNY" }, func(r *Result) { r.Observations[0].IsDerived = true },
		func(r *Result) { v := math.NaN(); r.Observations[0].Value = &v },
	} {
		input := dailyInput()
		change(input)
		_, err := CrossRates(input, []string{"HKD/CNY"}, QuotePolicy{})
		require.Error(t, err)
	}
	_, err := CrossRates(nil, []string{"HKD/CNY"}, QuotePolicy{})
	require.Error(t, err)
	_, err = CrossRates(dailyInput(), []string{"USD/CNH"}, QuotePolicy{})
	require.Error(t, err)
	input := dailyInput()
	input.Observations[0].Value = nil
	result, err := CrossRates(input, []string{"HKD/CNY"}, QuotePolicy{})
	require.NoError(t, err)
	require.Nil(t, result.Observations[0].Value)
	require.Equal(t, "missing-value", result.Observations[0].Alignment.Status)
}

func TestQuoteSpreadSignUnitsAndRatio(t *testing.T) {
	for _, cnh := range []float64{7.21, 7.2, 7.19} {
		input := quoteInput()
		input.Observations[1].Value = &cnh
		result, err := QuoteSpread(input, strictPolicy())
		require.NoError(t, err)
		require.InDelta(t, cnh-7.2, *result.Difference, 1e-12)
		require.InDelta(t, (cnh-7.2)*10000, *result.Pips, 1e-8)
		require.InDelta(t, (cnh-7.2)/7.2*100, *result.RelativePercent, 1e-10)
		require.InDelta(t, (cnh-7.2)/7.2*10000, *result.RelativeBPS, 1e-8)
		require.InDelta(t, 7.2/cnh, *result.ImpliedCNHCNY, 1e-12)
		require.Equal(t, "synchronized", result.Alignment.Status)
		require.True(t, result.IsDerived)
		require.Len(t, result.Inputs, 2)
	}
}

func TestQuoteAlignmentStrictToleranceAgeAndTimestamps(t *testing.T) {
	for _, tc := range []struct {
		name, stamp, status string
		skew                time.Duration
	}{
		{"strict", "2024-01-02T16:00:30+08:00", "timestamp-mismatch", 0},
		{"explicit tolerance", "2024-01-02T16:00:30+08:00", "within-tolerance", 30 * time.Second},
		{"outside tolerance", "2024-01-02T16:00:31+08:00", "timestamp-mismatch", 30 * time.Second},
		{"stale", "2024-01-02T15:00:00+08:00", "stale-input", time.Minute},
		{"future", "2024-01-02T17:00:00+08:00", "future-timestamp", time.Minute},
		{"missing timestamp", "", "missing-timestamp", 0},
		{"date mismatch", "2024-01-01T16:00:00+08:00", "missing-timestamp", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := quoteInput()
			input.Observations[1].ObservedAt = tc.stamp
			policy := strictPolicy()
			policy.MaxSkew = tc.skew
			result, err := QuoteSpread(input, policy)
			require.NoError(t, err)
			require.Equal(t, tc.status, result.Alignment.Status)
			if tc.status == "within-tolerance" {
				require.NotNil(t, result.Difference)
				require.Equal(t, 30.0, *result.Alignment.SkewSeconds)
				require.NotEmpty(t, result.Alignment.Note)
			} else {
				require.Nil(t, result.Difference)
				require.Nil(t, result.RelativePercent)
				require.Nil(t, result.Pips)
				require.Nil(t, result.ImpliedCNHCNY)
			}
		})
	}
	input := quoteInput()
	input.Observations = input.Observations[:1]
	result, err := QuoteSpread(input, strictPolicy())
	require.NoError(t, err)
	require.Equal(t, "missing-input", result.Alignment.Status)
	input = quoteInput()
	input.Observations[0].Value = nil
	result, err = QuoteSpread(input, strictPolicy())
	require.NoError(t, err)
	require.Equal(t, "missing-value", result.Alignment.Status)
	for _, policy := range []QuotePolicy{{}, {Now: strictPolicy().Now, MaxSkew: -time.Second, MaxAge: time.Minute}, {Now: strictPolicy().Now, MaxSkew: 61 * time.Second, MaxAge: time.Minute}, {Now: strictPolicy().Now, MaxAge: 25 * time.Hour}} {
		_, err := QuoteSpread(quoteInput(), policy)
		require.Error(t, err)
	}
}

func TestCrossQuoteAlignmentAndMidnight(t *testing.T) {
	input := quoteInput()
	input.Observations = append(input.Observations, leg("USD/HKD", "2024-01-03", 7.8, "2024-01-03T16:00:00+08:00"))
	result, err := CrossRates(input, []string{"CNH/CNY"}, strictPolicy())
	require.NoError(t, err)
	require.InDelta(t, 7.2/7.21, *result.Observations[0].Value, 1e-12)
	require.Equal(t, "2024-01-02", result.Observations[0].Date)
	require.Equal(t, "onshore-offshore", result.Observations[0].Market)
	input = quoteInput()
	input.Observations[0].ObservedAt = "2024-01-02T23:59:50+08:00"
	input.Observations[1].ObservedAt = "2024-01-03T00:00:10+08:00"
	input.Observations[1].Date = "2024-01-03"
	policy := QuotePolicy{Now: time.Date(2024, 1, 2, 16, 0, 20, 0, time.UTC), MaxAge: time.Minute, MaxSkew: 20 * time.Second}
	result, err = CrossRates(input, []string{"CNH/CNY"}, policy)
	require.NoError(t, err)
	require.NotNil(t, result.Observations[0].Value)
	require.Equal(t, "within-tolerance", result.Observations[0].Alignment.Status)
	require.Equal(t, "2024-01-03", result.Observations[0].Date)
	require.Empty(t, result.Observations[0].ObservedAt, "近似值不得伪造同一观测时间")
	result, err = CrossRates(quoteInput(), []string{"HKD/CNH"}, strictPolicy())
	require.NoError(t, err)
	require.Nil(t, result.Observations[0].Value)
	require.Equal(t, "missing-input", result.Observations[0].Alignment.Status)
}
