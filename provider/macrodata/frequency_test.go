package macrodata

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/nyfed"
	"github.com/stretchr/testify/require"
)

func gdpSample() *fred.GDPResult {
	return &fred.GDPResult{Region: "US", Frequency: "quarterly", SeriesID: "GDPC1", GrowthSeriesID: "A191RL1Q225SBEA", ReferenceYear: 2017, SeasonallyAdjusted: true, LevelAnnualized: true, Begin: "2026Q1", End: "2026Q1", Source: "https://fred.stlouisfed.org/graph/fredgraph.csv", Observations: []fred.GDPObservation{{GDPValue: fred.GDPValue{Period: "2026Q1", Level: value(110), RawLevel: "110.000"}, QoQAnnualized: value(2.2), RawQoQAnnualized: "2.2", QoQ: value(10), YoY: value(5), QoQBase: fred.GDPValue{Period: "2025Q4", Level: value(100), RawLevel: "100"}, YoYBase: fred.GDPValue{Period: "2025Q1", Level: value(104), RawLevel: "104"}, Vintages: []fred.GDPVintage{{Estimate: "Advance", ReleaseDate: "2026-04-30", QoQAnnualized: value(1.5), NominalLevel: value(200)}}}}}
}

func TestGDPFrequencyAndAnnualization(t *testing.T) {
	r := gdpSample()
	d, err := USGDP(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 4)
	for _, s := range d.Series {
		require.Equal(t, "quarterly", s.Frequency)
		require.Equal(t, 2017, *s.ReferenceYear)
		require.Equal(t, "real-chained", s.PriceBasis)
	}
	require.Equal(t, "USD_billion", d.Series[0].Unit)
	require.True(t, d.Series[0].Annualized)
	require.True(t, d.Series[1].Annualized)
	require.Equal(t, "official", d.Series[1].ValueOrigin)
	require.Equal(t, 2.2, *d.Observations[1].Value, "不从当前水平重算或用初值覆盖官方当前年化增速")
	require.Equal(t, "2.2", *d.Observations[1].RawValue)
	require.Equal(t, "A191RL1Q225SBEA", d.Observations[1].SourceSeriesID)
	require.False(t, d.Series[2].Annualized)
	require.False(t, d.Series[3].Annualized)
	require.Equal(t, "derived", d.Series[2].ValueOrigin)
	require.Equal(t, "2025Q4", d.Observations[2].Base.Period)
	require.Equal(t, "2025Q1", d.Observations[3].Base.Period)
	require.Nil(t, d.Observations[2].RawValue)
	require.Nil(t, d.Observations[0].PublishedAt)
	require.Nil(t, d.Observations[0].RevisionID)
	var original fred.GDPResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	require.Nil(t, original.Observations[0].Vintages[0].RealLevel)
	// 官方年化增速可独立存在，不能因水平缺失而丢弃。
	r.Observations[0].Level, r.Observations[0].QoQ, r.Observations[0].YoY = nil, nil, nil
	d, err = USGDP(r)
	require.NoError(t, err)
	require.Equal(t, "missing", d.Observations[0].Status)
	require.Equal(t, "available", d.Observations[1].Status)
	require.Equal(t, "current_value_missing", *d.Observations[2].MissingReason)
	for _, mode := range []string{"missing", "zero"} {
		r = gdpSample()
		r.Observations[0].YoY = nil
		reason := "baseline_missing"
		if mode == "missing" {
			r.Observations[0].YoYBase.Level = nil
		} else {
			r.Observations[0].YoYBase.Level = value(0)
			reason = "zero_baseline"
		}
		d, err = USGDP(r)
		require.NoError(t, err)
		require.Equal(t, reason, *d.Observations[3].MissingReason)
	}
}

func effrSample() *nyfed.EFFRResult {
	return &nyfed.EFFRResult{Region: "US", Frequency: "daily", SeriesID: "EFFR", Unit: "%", Begin: "20160229", End: "20160304", SourceURL: "https://markets.newyorkfed.org/api/rates/unsecured/effr/search.json", Observations: []nyfed.EFFRObservation{
		{EffectiveDate: "2016-02-29", RatePercent: 0, PreviousDate: "2016-02-26", ChangeBasisPoints: value(-37), Methodology: "旧口径", Raw: json.RawMessage(`{"percentRate":0}`)},
		{EffectiveDate: "2016-03-01", RatePercent: 0.36, PreviousDate: "2016-02-29", RevisionIndicator: "Y", Methodology: "新口径"},
		{EffectiveDate: "2016-03-04", RatePercent: 0.36, PreviousDate: "2016-03-01", ChangeBasisPoints: value(0), Methodology: "新口径"},
	}}
}

func TestEFFRMethodsDatesAndMissingBaselines(t *testing.T) {
	r := effrSample()
	d, err := EFFR(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 6, "不插入缺失的交易日或周末")
	require.Equal(t, "20160229", d.Observations[0].Period)
	require.Equal(t, 0.0, *d.Observations[0].Value)
	require.Equal(t, "available", d.Observations[0].Status)
	require.Equal(t, "basis_points", d.Series[1].Unit)
	require.Equal(t, "derived", d.Series[1].ValueOrigin)
	require.Equal(t, "20160226", d.Observations[1].Base.Period)
	require.Nil(t, d.Observations[1].Base.Value, "不反推窗口外基期数值")
	require.NotEmpty(t, d.Observations[1].Note)
	require.NotEqual(t, d.Observations[0].SeriesID, d.Observations[2].SeriesID)
	require.Equal(t, "Y", d.Observations[2].RevisionIndicator)
	require.Nil(t, d.Observations[2].RevisionID, "修订标记不等同于版本编号")
	require.Equal(t, "methodology_break", *d.Observations[3].MissingReason)
	require.Equal(t, 0.0, *d.Observations[3].Base.Value)
	require.Equal(t, 0.0, *d.Observations[5].Value)
	require.Equal(t, "20160301", d.Observations[5].Base.Period)
	require.Equal(t, 0.36, *d.Observations[5].Base.Value)
	var original nyfed.EFFRResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	expected, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(d.Original))
	r.Observations = r.Observations[:1]
	r.Observations[0].PreviousDate = ""
	r.Observations[0].ChangeBasisPoints = nil
	d, err = EFFR(r)
	require.NoError(t, err)
	require.Equal(t, "previous_observation_missing", *d.Observations[1].MissingReason)
	require.Nil(t, d.Observations[1].Base)
	r.Observations = nil
	d, err = EFFR(r)
	require.NoError(t, err)
	require.NotNil(t, d.Observations)
	require.Empty(t, d.Observations)
}

func TestPeriodValidationAndAdapterErrors(t *testing.T) {
	for frequency, periods := range map[string][]string{"quarterly": {"2026Q0", "2026Q5", "0000Q1", "202601", "20a6Q1", "-001Q1"}, "daily": {"20250229", "2026-01-01", "202601", "00000101"}, "monthly": {"2026Q1", "20260101", "202613"}} {
		for _, p := range periods {
			require.Error(t, validatePeriod(p, frequency), frequency+"/"+p)
		}
	}
	require.NoError(t, validatePeriod("20240229", "daily"))
	require.NoError(t, validatePeriod("2026Q4", "quarterly"))
	require.Error(t, validatePeriod("202601", "unknown"))
	for _, mutate := range []func(*fred.GDPResult){
		func(r *fred.GDPResult) { r.End = "2026Q0" }, func(r *fred.GDPResult) { r.End = "2025Q4" },
		func(r *fred.GDPResult) { r.ReferenceYear = 2012 }, func(r *fred.GDPResult) { r.LevelAnnualized = false },
		func(r *fred.GDPResult) { r.Observations[0].Period = "2026Q2" },
		func(r *fred.GDPResult) { r.Observations[0].QoQBase.Period = "202613" },
		func(r *fred.GDPResult) { r.Observations = append(r.Observations, r.Observations[0]) },
		func(r *fred.GDPResult) { r.Observations[0].Level = value(math.Inf(1)) },
	} {
		r := gdpSample()
		mutate(r)
		d, err := USGDP(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*nyfed.EFFRResult){
		func(r *nyfed.EFFRResult) { r.Begin = "20160230" }, func(r *nyfed.EFFRResult) { r.Unit = "basis_points" },
		func(r *nyfed.EFFRResult) { r.Observations[0].EffectiveDate = "2016-02-30" },
		func(r *nyfed.EFFRResult) { r.Observations[0].PreviousDate = "2016-03-01" },
		func(r *nyfed.EFFRResult) { r.Observations[0].RatePercent = math.NaN() },
		func(r *nyfed.EFFRResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := effrSample()
		mutate(r)
		d, err := EFFR(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err := EFFR(nil)
	require.Error(t, err)
	_, err = USGDP(nil)
	require.Error(t, err)
}
