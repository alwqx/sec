package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
)

func cpiSample() *bls.CPIResult {
	fetched := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	point := func(period, id string, n float64) *bls.CPIValue {
		return &bls.CPIValue{Period: period, SeriesID: id, Index: value(n), RawValue: "100.000", FetchedAt: fetched}
	}
	return &bls.CPIResult{Indicator: "cpi", Frequency: "monthly", IndexBase: "1982-1984=100", Begin: "202609", End: "202609", Source: "https://api.bls.gov", Series: []bls.CPISeries{{ID: "CUUR0000SA0"}, {ID: "CUSR0000SA0", SeasonallyAdjusted: true}}, Observations: []bls.CPIObservation{{Period: "202609", NSA: point("202609", "CUUR0000SA0", 100), SA: point("202609", "CUSR0000SA0", 200), YoY: value(0), MoM: value(-1), YoYBase: point("202509", "CUUR0000SA0", 100), MoMBase: point("202608", "CUSR0000SA0", 202)}}}
}

func TestCPISeparateSeasonalityAndMissingValues(t *testing.T) {
	r := cpiSample()
	d, err := CPI(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 4)
	require.Equal(t, 100.0, *d.Observations[0].Value)
	require.Equal(t, 200.0, *d.Observations[1].Value)
	require.Equal(t, "CUUR0000SA0", d.Observations[2].SourceSeriesID)
	require.Equal(t, "CUSR0000SA0", d.Observations[3].SourceSeriesID)
	require.Equal(t, "202509", d.Observations[2].Base.Period)
	require.Equal(t, "202608", d.Observations[3].Base.Period)
	require.Equal(t, "available", d.Observations[2].Status)
	require.Equal(t, 0.0, *d.Observations[2].Value)
	require.Equal(t, -1.0, *d.Observations[3].Value)
	require.Nil(t, d.Observations[3].RawValue)
	require.Equal(t, r.Observations[0].SA.FetchedAt, *d.Observations[3].FetchedAt)
	for _, s := range d.Series {
		require.False(t, s.Annualized)
		require.Equal(t, "1982-1984=100", s.IndexBase)
	}
	var original bls.CPIResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	for _, mode := range []string{"current", "base", "zero"} {
		r = cpiSample()
		r.Observations[0].MoM = nil
		reason := "baseline_missing"
		switch mode {
		case "current":
			r.Observations[0].SA = nil
			reason = "current_value_missing"
		case "base":
			r.Observations[0].MoMBase = nil
		case "zero":
			r.Observations[0].MoMBase.Index = value(0)
			reason = "zero_baseline"
		}
		d, err = CPI(r)
		require.NoError(t, err)
		require.Equal(t, reason, *d.Observations[3].MissingReason)
		require.Equal(t, "available", d.Observations[0].Status, "SA缺失不影响NSA水平")
	}
}

func claimsSample() *fred.ClaimsResult {
	week := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	point := func(n int) fred.ClaimsValue {
		return fred.ClaimsValue{WeekEnding: week.AddDate(0, 0, -7*n).Format("2006-01-02"), Count: value(float64(100 + n)), RawValue: "100"}
	}
	return &fred.ClaimsResult{Indicator: "initial-claims", SeriesID: "ICSA", Frequency: "weekly", WeekEndingDay: "Saturday", SeasonallyAdjusted: true, Begin: "20261015", End: "20261018", Observations: []fred.ClaimsObservation{{ClaimsValue: point(0), PreviousWeek: point(1), WeeklyChange: value(-1), FourWeekAverage: value(101.5), FourWeekWindow: []fred.ClaimsValue{point(0), point(1), point(2), point(3)}}}}
}

func TestClaimsWeeklyInputsAndNulls(t *testing.T) {
	r := claimsSample()
	d, err := Claims(r)
	require.NoError(t, err)
	require.Len(t, d.Observations, 3)
	require.Equal(t, "20261017", d.Observations[0].Period)
	require.Equal(t, "claims", d.Series[0].Unit)
	require.Equal(t, "weekly", d.Series[0].Frequency)
	require.Equal(t, -1.0, *d.Observations[1].Value)
	require.Equal(t, "20261010", d.Observations[1].Base.Period)
	require.Len(t, d.Observations[2].Inputs, 4)
	require.Equal(t, "20260926", d.Observations[2].Inputs[3].Period)
	var original fred.ClaimsResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	r.Observations[0].PreviousWeek.Count = nil
	r.Observations[0].WeeklyChange = nil
	r.Observations[0].FourWeekWindow[2].Count = nil
	r.Observations[0].FourWeekAverage = nil
	d, err = Claims(r)
	require.NoError(t, err)
	require.Equal(t, "baseline_missing", *d.Observations[1].MissingReason)
	require.Equal(t, "window_incomplete", *d.Observations[2].MissingReason)
	require.Nil(t, d.Observations[2].Inputs[2].Value)
	r = claimsSample()
	r.Observations[0].Count = value(0)
	r.Observations[0].WeeklyChange = value(0)
	r.Observations[0].FourWeekAverage = value(0)
	d, err = Claims(r)
	require.NoError(t, err)
	for _, o := range d.Observations {
		require.Equal(t, "available", o.Status)
		require.Equal(t, 0.0, *o.Value)
	}
	r = claimsSample()
	r.Observations = nil
	d, err = Claims(r)
	require.NoError(t, err)
	require.NotNil(t, d.Observations)
	require.Empty(t, d.Observations)
}

func TestPricesClaimsRejectBadMetadataAndPeriods(t *testing.T) {
	for _, mutate := range []func(*bls.CPIResult){
		func(r *bls.CPIResult) { r.Indicator = "unknown" }, func(r *bls.CPIResult) { r.IndexBase = "" }, func(r *bls.CPIResult) { r.Series[1].SeasonallyAdjusted = false },
		func(r *bls.CPIResult) { r.Observations[0].NSA.SeriesID = "CUSR0000SA0" }, func(r *bls.CPIResult) { r.Observations[0].MoMBase.Period = "202607" },
		func(r *bls.CPIResult) { r.Observations = append(r.Observations, r.Observations[0]) }, func(r *bls.CPIResult) { r.End = "202608" },
	} {
		r := cpiSample()
		mutate(r)
		d, err := CPI(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*fred.ClaimsResult){
		func(r *fred.ClaimsResult) { r.Indicator = "unknown" }, func(r *fred.ClaimsResult) { r.SeriesID = "CCSA" }, func(r *fred.ClaimsResult) { r.SeasonallyAdjusted = false },
		func(r *fred.ClaimsResult) { r.Observations[0].WeekEnding = "2026-10-18" }, func(r *fred.ClaimsResult) { r.Observations[0].PreviousWeek.WeekEnding = "2026-10-03" },
		func(r *fred.ClaimsResult) { r.Observations[0].FourWeekWindow = r.Observations[0].FourWeekWindow[:3] }, func(r *fred.ClaimsResult) { r.Observations[0].FourWeekWindow[2].WeekEnding = "2026-09-26" },
		func(r *fred.ClaimsResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := claimsSample()
		mutate(r)
		d, err := Claims(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err := CPI(nil)
	require.Error(t, err)
	_, err = Claims(nil)
	require.Error(t, err)
}
