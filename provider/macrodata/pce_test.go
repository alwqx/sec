package macrodata

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
)

func pceSample() *fred.PCEResult {
	return &fred.PCEResult{Indicator: "pce", SeriesID: "PCEPI", Frequency: "monthly", Measure: "chain-type-price-index", IndexBase: "2017=100", SeasonallyAdjusted: true, Begin: "202601", End: "202601", Source: "https://fred.stlouisfed.org/pce", FetchedAt: time.Date(2026, 2, 27, 12, 0, 0, 0, time.UTC), Observations: []fred.PCEObservation{{PCEValue: fred.PCEValue{Period: "202601", Index: value(120), RawIndex: "120.000"}, YoY: value(20), MoM: value(0), YoYBase: fred.PCEValue{Period: "202501", Index: value(100), RawIndex: "100.000"}, MoMBase: fred.PCEValue{Period: "202512", Index: value(120), RawIndex: "120.000"}}}}
}

func realPCESample() *fred.RealPCEResult {
	return &fred.RealPCEResult{Indicator: "real-pce", Component: "goods", SeriesID: "DGDSRX1", Frequency: "monthly", ReferenceYear: 2017, SeasonallyAdjusted: true, LevelAnnualized: true, Begin: "202601", End: "202601", Observations: []fred.RealPCEObservation{{RealPCEValue: fred.RealPCEValue{Period: "202601", Level: value(120), RawValue: "120.0"}, YoY: value(20), MoM: value(0), YoYBase: fred.RealPCEValue{Period: "202501", Level: value(100), RawValue: "100.0"}, MoMBase: fred.RealPCEValue{Period: "202512", Level: value(120), RawValue: "120.0"}}}}
}

func TestPCEPriceAndRealSpendingRemainDistinct(t *testing.T) {
	r := pceSample()
	d, err := PCE(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 3)
	require.Len(t, d.Observations, 3)
	require.Equal(t, "us.pce.index", d.Series[0].ID)
	require.Equal(t, "index", d.Series[0].Unit)
	require.Equal(t, "price-index", d.Series[0].PriceBasis)
	require.Equal(t, "2017=100", d.Series[0].IndexBase)
	for _, s := range d.Series {
		require.False(t, s.Annualized)
		require.Equal(t, "seasonally-adjusted", s.SeasonalAdjustment)
	}
	require.Equal(t, "202501", d.Observations[1].Base.Period)
	require.Equal(t, "202512", d.Observations[2].Base.Period)
	require.Equal(t, "120.000", *d.Observations[0].RawValue)
	require.Nil(t, d.Observations[1].RawValue)
	require.Equal(t, "official", d.Series[0].ValueOrigin)
	require.Equal(t, "derived", d.Series[1].ValueOrigin)
	require.Equal(t, "available", d.Observations[2].Status)
	require.Equal(t, 0.0, *d.Observations[2].Value)
	require.Equal(t, r.FetchedAt, *d.Observations[2].FetchedAt)
	require.Nil(t, d.Observations[2].PublishedAt)
	require.Nil(t, d.Observations[2].RevisionID)
	var original fred.PCEResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	rp := realPCESample()
	rd, err := RealPCE(rp)
	require.NoError(t, err)
	require.Equal(t, "us.real-pce.goods.level", rd.Series[0].ID)
	require.Equal(t, "USD_billion", rd.Series[0].Unit)
	require.Equal(t, "real-chained", rd.Series[0].PriceBasis)
	require.Equal(t, 2017, *rd.Series[0].ReferenceYear)
	require.Empty(t, rd.Series[0].IndexBase)
	require.True(t, rd.Series[0].Annualized)
	require.False(t, rd.Series[1].Annualized)
	require.False(t, *rd.Series[0].ComponentsAdditive)
	require.Nil(t, rd.Series[1].ComponentsAdditive)
	require.Equal(t, 120.0, *rd.Observations[0].Value, "不除以12或转换成价格指数")
	var realOriginal fred.RealPCEResult
	require.NoError(t, json.Unmarshal(rd.Original, &realOriginal))
	require.Equal(t, *rp, realOriginal)
}

func TestPCEMissingAndZeroValues(t *testing.T) {
	for _, mode := range []string{"current", "base", "zero-base", "zero-current"} {
		r := pceSample()
		r.Observations[0].YoY = nil
		reason := "baseline_missing"
		switch mode {
		case "current":
			r.Observations[0].Index = nil
			reason = "current_value_missing"
		case "base":
			r.Observations[0].YoYBase.Index = nil
		case "zero-base":
			r.Observations[0].YoYBase.Index = value(0)
			reason = "zero_baseline"
		case "zero-current":
			r.Observations[0].Index = value(0)
			r.Observations[0].YoY = value(-100)
		}
		d, err := PCE(r)
		require.NoError(t, err)
		if mode == "zero-current" {
			require.Equal(t, "available", d.Observations[0].Status)
			require.Equal(t, -100.0, *d.Observations[1].Value)
		} else {
			require.Nil(t, d.Observations[1].Value)
			require.Equal(t, reason, *d.Observations[1].MissingReason)
		}
	}
	r := realPCESample()
	r.Observations[0].Level = nil
	r.Observations[0].YoY = nil
	r.Observations[0].MoM = nil
	d, err := RealPCE(r)
	require.NoError(t, err)
	require.Nil(t, d.Observations[0].Value)
	require.Equal(t, "current_value_missing", *d.Observations[1].MissingReason)
	r.Observations = nil
	d, err = RealPCE(r)
	require.NoError(t, err)
	require.NotNil(t, d.Observations)
	require.Empty(t, d.Observations)
}

func TestPCEAdaptersRejectInvalidMetadataAndBaselines(t *testing.T) {
	for _, mutate := range []func(*fred.PCEResult){
		func(r *fred.PCEResult) { r.Indicator = "unknown" }, func(r *fred.PCEResult) { r.SeriesID = "PCEC96" }, func(r *fred.PCEResult) { r.IndexBase = "2012=100" }, func(r *fred.PCEResult) { r.SeasonallyAdjusted = false },
		func(r *fred.PCEResult) { r.Observations[0].MoMBase.Period = "202511" }, func(r *fred.PCEResult) { r.Observations[0].Period = "202602" }, func(r *fred.PCEResult) { r.Observations = append(r.Observations, r.Observations[0]) }, func(r *fred.PCEResult) { r.Observations[0].Index = value(math.NaN()) },
	} {
		r := pceSample()
		mutate(r)
		d, err := PCE(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*fred.RealPCEResult){
		func(r *fred.RealPCEResult) { r.Component = "unknown" }, func(r *fred.RealPCEResult) { r.SeriesID = "PCEPI" }, func(r *fred.RealPCEResult) { r.ReferenceYear = 2012 }, func(r *fred.RealPCEResult) { r.LevelAnnualized = false }, func(r *fred.RealPCEResult) { r.GrowthAnnualized = true }, func(r *fred.RealPCEResult) { r.ComponentsAdditive = true }, func(r *fred.RealPCEResult) { r.Observations[0].YoYBase.Period = "202401" },
	} {
		r := realPCESample()
		mutate(r)
		d, err := RealPCE(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err := PCE(nil)
	require.Error(t, err)
	_, err = RealPCE(nil)
	require.Error(t, err)
}
