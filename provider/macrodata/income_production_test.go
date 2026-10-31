package macrodata

import (
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
)

func incomeSample() *fred.RealIncomeResult {
	return &fred.RealIncomeResult{Indicator: "real-income", SeriesID: "DSPIC96", Measure: "real-disposable-personal-income", Frequency: "monthly", ReferenceYear: 2017, SeasonallyAdjusted: true, LevelAnnualized: true, Begin: "202601", End: "202601", Observations: []fred.RealIncomeObservation{{RealIncomeValue: fred.RealIncomeValue{Period: "202601", Level: value(120), RawValue: "120.0"}, YoY: value(20), MoM: value(0), YoYBase: fred.RealIncomeValue{Period: "202501", Level: value(100), RawValue: "100.0"}, MoMBase: fred.RealIncomeValue{Period: "202512", Level: value(120), RawValue: "120.0"}}}}
}

func productionSample(points bool) *fred.ProductionResult {
	year := 2017
	r := &fred.ProductionResult{Indicator: "industrial-production", Sector: "total", SeriesID: "INDPRO", Frequency: "monthly", ChangeUnit: "percent", ReferenceYear: &year, SeasonallyAdjusted: true, Coverage: "工业总量", Begin: "202601", End: "202601", Observations: []fred.ProductionObservation{{ProductionValue: fred.ProductionValue{Period: "202601", Level: value(80), RawValue: "80.0"}, YoY: value(0), MoM: value(0), YoYBase: fred.ProductionValue{Period: "202501", Level: value(80), RawValue: "80.0"}, MoMBase: fred.ProductionValue{Period: "202512", Level: value(80), RawValue: "80.0"}}}}
	if points {
		r.Indicator, r.SeriesID, r.ChangeUnit, r.ReferenceYear = "capacity-utilization", "TCU", "percentage-points", nil
		r.Observations[0].YoY, r.Observations[0].MoM = nil, nil
		r.Observations[0].YoYPercentagePoints = value(1)
		r.Observations[0].YoYBase.Level = value(79)
		r.Observations[0].MoMPercentagePoints = value(0)
	}
	return r
}

func TestIncomeKeepsRealSAARAndStrictBases(t *testing.T) {
	r := incomeSample()
	d, err := RealIncome(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 3)
	require.Equal(t, "USD_billion", d.Series[0].Unit)
	require.Equal(t, "real-chained", d.Series[0].PriceBasis)
	require.True(t, d.Series[0].Annualized)
	require.False(t, d.Series[1].Annualized)
	require.Contains(t, d.Series[0].Coverage, "非人均")
	require.Equal(t, 120.0, *d.Observations[0].Value)
	require.Equal(t, "202501", d.Observations[1].Base.Period)
	require.Equal(t, "202512", d.Observations[2].Base.Period)
	require.Equal(t, "120.0", *d.Observations[0].RawValue)
	require.Nil(t, d.Observations[1].RawValue)
	var original fred.RealIncomeResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	r.Observations[0].Level = nil
	r.Observations[0].YoY = nil
	r.Observations[0].MoM = nil
	d, err = RealIncome(r)
	require.NoError(t, err)
	require.Nil(t, d.Observations[0].Value)
	require.Equal(t, "current_value_missing", *d.Observations[1].MissingReason)
}

func TestProductionSeparatesRatiosFromPercentagePoints(t *testing.T) {
	for _, points := range []bool{false, true} {
		r := productionSample(points)
		d, err := Production(r)
		require.NoError(t, err)
		require.False(t, d.Series[0].Annualized)
		require.Equal(t, "202501", d.Observations[1].Base.Period)
		if points {
			require.Equal(t, "rate", d.Series[0].Measure)
			require.Equal(t, "percent", d.Series[0].Unit)
			require.Equal(t, "percentage_points", d.Series[1].Unit)
			require.Equal(t, "yoy_percentage_points", d.Series[1].Measure)
			require.Equal(t, 1.0, *d.Observations[1].Value)
			require.Equal(t, "current_level - base_level", *d.Series[1].Formula)
			require.Empty(t, d.Series[0].IndexBase)
			require.Nil(t, d.Series[0].ReferenceYear)
		} else {
			require.Equal(t, "index", d.Series[0].Unit)
			require.Equal(t, "percent", d.Series[1].Unit)
			require.Equal(t, "2017=100", d.Series[0].IndexBase)
		}
		var original fred.ProductionResult
		require.NoError(t, json.Unmarshal(d.Original, &original))
		require.Equal(t, *r, original)
		// 零基期可用于百分点差，但不能用于百分比增速。
		r.Observations[0].YoYBase.Level = value(0)
		if points {
			r.Observations[0].YoYPercentagePoints = value(80)
		} else {
			r.Observations[0].YoY = nil
		}
		d, err = Production(r)
		require.NoError(t, err)
		if points {
			require.Equal(t, "available", d.Observations[1].Status)
			require.Equal(t, 80.0, *d.Observations[1].Value)
			require.Nil(t, d.Observations[1].MissingReason)
		} else {
			require.Equal(t, "zero_baseline", *d.Observations[1].MissingReason)
		}
		r.Observations[0].MoMBase.Level = nil
		r.Observations[0].MoM = nil
		r.Observations[0].MoMPercentagePoints = nil
		d, err = Production(r)
		require.NoError(t, err)
		require.Equal(t, "baseline_missing", *d.Observations[2].MissingReason)
		r.Observations = nil
		d, err = Production(r)
		require.NoError(t, err)
		require.NotNil(t, d.Observations)
		require.Empty(t, d.Observations)
	}
}

func TestIncomeProductionRejectWrongDefinitions(t *testing.T) {
	for _, mutate := range []func(*fred.RealIncomeResult){
		func(r *fred.RealIncomeResult) { r.SeriesID = "PCEC96" }, func(r *fred.RealIncomeResult) { r.ReferenceYear = 2012 }, func(r *fred.RealIncomeResult) { r.LevelAnnualized = false }, func(r *fred.RealIncomeResult) { r.GrowthAnnualized = true }, func(r *fred.RealIncomeResult) { r.Observations[0].MoMBase.Period = "202511" },
	} {
		r := incomeSample()
		mutate(r)
		d, err := RealIncome(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, points := range []bool{false, true} {
		for _, mutate := range []func(*fred.ProductionResult){
			func(r *fred.ProductionResult) { r.Sector = "unknown" }, func(r *fred.ProductionResult) { r.SeriesID = "wrong" }, func(r *fred.ProductionResult) { r.ChangeUnit = "wrong" }, func(r *fred.ProductionResult) { r.SeasonallyAdjusted = false }, func(r *fred.ProductionResult) { r.Coverage = "" }, func(r *fred.ProductionResult) { r.Observations[0].YoYBase.Period = "202401" }, func(r *fred.ProductionResult) { r.Observations = append(r.Observations, r.Observations[0]) },
		} {
			r := productionSample(points)
			mutate(r)
			d, err := Production(r)
			require.Error(t, err)
			require.Nil(t, d)
		}
	}
	_, err := RealIncome(nil)
	require.Error(t, err)
	_, err = Production(nil)
	require.Error(t, err)
}
