package macrodata

import (
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
)

func enterpriseSample() *fred.EnterpriseResult {
	return &fred.EnterpriseResult{Metric: "profit", SeriesID: "CPROFIT", Frequency: "quarterly", Unit: "十亿美元", Measurement: "quarterly_flow", PriceBasis: "current_dollars", SeasonallyAdjusted: true, LevelAnnualized: true, TaxBasis: "before_tax", Definition: "含IVA及CCAdj的企业税前利润", Begin: "2026Q1", End: "2026Q1", Observations: []fred.EnterpriseObservation{{EnterpriseValue: fred.EnterpriseValue{Period: "2026Q1", Level: value(120), RawLevel: "120.0"}, YoY: value(20), PreviousGrowth: value(0), YoYBase: fred.EnterpriseValue{Period: "2025Q1", Level: value(100), RawLevel: "100.0"}, PreviousBase: fred.EnterpriseValue{Period: "2025Q4", Level: value(120), RawLevel: "120.0"}}}}
}

func fiscalSample() *fred.FiscalResult {
	return &fred.FiscalResult{Frequency: "monthly", Measurement: "federal-unified-budget-monthly-flow", PriceBasis: "current-dollars", Unit: "百万美元", Begin: "202609", End: "202610", Observations: []fred.FiscalObservation{
		{FiscalValue: fred.FiscalValue{Period: "202609", Receipts: value(10), Outlays: value(12), SurplusDeficit: value(-1), Deficit: value(1), RawValues: map[string]string{"MTSR133FMS": "10", "MTSO133FMS": "12", "MTSDS133FMS": "-1"}}, FiscalYear: 2026, FiscalMonth: 12, FYTD: fred.FiscalYTD{Begin: "202510", End: "202609", Receipts: value(100), Outlays: value(120), SurplusDeficit: value(-19), Deficit: value(19), Method: "sum_of_current_snapshot_months"}},
		{FiscalValue: fred.FiscalValue{Period: "202610", Receipts: value(0), Outlays: value(0), SurplusDeficit: value(0), Deficit: value(0)}, FiscalYear: 2027, FiscalMonth: 1, FYTD: fred.FiscalYTD{Begin: "202610", End: "202610", Receipts: value(0), Outlays: value(0), SurplusDeficit: value(0), Deficit: value(0), Method: "sum_of_current_snapshot_months"}},
	}}
}

func TestFiscalSignsYearResetAndDerivedCumulativeValues(t *testing.T) {
	r := fiscalSample()
	d, err := Fiscal(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 8)
	require.Len(t, d.Observations, 16)
	require.Equal(t, -1.0, *d.Observations[2].Value, "不以收入减支出-2覆盖官方余额")
	require.Equal(t, 1.0, *d.Observations[3].Value)
	require.Equal(t, -1.0, *d.Observations[3].Inputs[0].Value)
	require.Nil(t, d.Observations[3].RawValue)
	require.Equal(t, "-1", *d.Observations[2].RawValue)
	require.Equal(t, "official", d.Series[2].ValueOrigin)
	require.Equal(t, "derived", d.Series[3].ValueOrigin)
	require.Equal(t, "derived", d.Series[4].ValueOrigin)
	require.Equal(t, "202510", d.Observations[4].PeriodBegin)
	require.Equal(t, "202610", d.Observations[12].PeriodBegin)
	require.Equal(t, 2027, d.Observations[12].FiscalYear)
	require.Equal(t, 0.0, *d.Observations[12].Value)
	require.Equal(t, "available", d.Observations[12].Status)
	var original fred.FiscalResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	// 当月值仍可用，不因其他月份造成累计缺口而丢弃；指标之间不连带补零。
	r.Observations[0].FYTD.Receipts = nil
	d, err = Fiscal(r)
	require.NoError(t, err)
	require.Equal(t, "available", d.Observations[0].Status)
	require.Equal(t, "window_incomplete", *d.Observations[4].MissingReason)
	require.Equal(t, "available", d.Observations[5].Status)
	r.Observations[0].SurplusDeficit = value(5)
	r.Observations[0].Deficit = value(-5)
	d, err = Fiscal(r)
	require.NoError(t, err)
	require.Equal(t, -5.0, *d.Observations[3].Value, "盈余对应负的正赤字表示")
}

func TestEnterpriseNominalFlowsAndNonpositiveBases(t *testing.T) {
	r := enterpriseSample()
	d, err := Enterprise(r)
	require.NoError(t, err)
	require.Equal(t, "nominal", d.Series[0].PriceBasis)
	require.True(t, d.Series[0].Annualized)
	require.False(t, d.Series[1].Annualized)
	require.Equal(t, "before_tax", d.Series[0].TaxBasis)
	require.Equal(t, "2025Q4", d.Observations[2].Base.Period)
	require.Equal(t, "2025Q1", d.Observations[1].Base.Period)
	var original fred.EnterpriseResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	for _, n := range []float64{0, -10} {
		r = enterpriseSample()
		r.Observations[0].YoYBase.Level = value(n)
		r.Observations[0].YoY = nil
		d, err = Enterprise(r)
		require.NoError(t, err)
		require.Equal(t, "nonpositive_baseline", *d.Observations[1].MissingReason)
	}
	r = enterpriseSample()
	r.Observations[0].Level = value(-10)
	r.Observations[0].YoY = value(-110)
	d, err = Enterprise(r)
	require.NoError(t, err)
	require.Equal(t, -10.0, *d.Observations[0].Value)
	require.Equal(t, "available", d.Observations[0].Status)
	r.Observations = nil
	d, err = Enterprise(r)
	require.NoError(t, err)
	require.NotNil(t, d.Observations)
	require.Empty(t, d.Observations)
}

func TestFiscalEnterpriseRejectBadDefinitions(t *testing.T) {
	for _, mutate := range []func(*fred.FiscalResult){
		func(r *fred.FiscalResult) { r.SeasonallyAdjusted = true }, func(r *fred.FiscalResult) { r.Annualized = true }, func(r *fred.FiscalResult) { r.Observations[1].FiscalYear = 2026 }, func(r *fred.FiscalResult) { r.Observations[0].FYTD.Begin = "202601" }, func(r *fred.FiscalResult) { r.Observations[0].FYTD.Method = "official" }, func(r *fred.FiscalResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := fiscalSample()
		mutate(r)
		d, err := Fiscal(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*fred.EnterpriseResult){
		func(r *fred.EnterpriseResult) { r.SeriesID = "wrong" }, func(r *fred.EnterpriseResult) { r.Frequency = "monthly" }, func(r *fred.EnterpriseResult) { r.TaxBasis = "after_tax" }, func(r *fred.EnterpriseResult) { r.GrowthAnnualized = true }, func(r *fred.EnterpriseResult) { r.Observations[0].PreviousBase.Period = "2025Q3" }, func(r *fred.EnterpriseResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := enterpriseSample()
		mutate(r)
		d, err := Enterprise(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err := Fiscal(nil)
	require.Error(t, err)
	_, err = Enterprise(nil)
	require.Error(t, err)
}
