package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/stretchr/testify/require"
)

func laborForceSample() *bls.LaborForceResult {
	p := func(period string, n float64) bls.LaborForceValue {
		return bls.LaborForceValue{Period: period, SeriesID: "LNS14000000", Rate: value(n), RawValue: "4.0", FetchedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	}
	return &bls.LaborForceResult{Indicator: "unemployment", SeriesID: "LNS14000000", Survey: "CPS household survey", Population: "age 16 and older", Numerator: "unemployed persons", Denominator: "civilian labor force", Frequency: "monthly", Unit: "%", ChangeUnit: "百分点", SeasonallyAdjusted: true, Begin: "202601", End: "202601", Observations: []bls.LaborForceObservation{{LaborForceValue: p("202601", 4), YoYChange: value(1), MoMChange: value(0), YoYBase: p("202501", 3), MoMBase: p("202512", 4)}}}
}

func earningsSample() *bls.EarningsHoursResult {
	p := func(period string, n float64) bls.EarningsHoursValue {
		return bls.EarningsHoursValue{Period: period, SeriesID: "CES0500000003", Value: value(n), RawValue: "40.0", FetchedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}
	}
	return &bls.EarningsHoursResult{Indicator: "earnings", SeriesID: "CES0500000003", Population: "private nonfarm all employees", Frequency: "monthly", Unit: "美元/小时", PriceBasis: "current-dollars", SeasonallyAdjusted: true, Begin: "202601", End: "202601", Observations: []bls.EarningsHoursObservation{{EarningsHoursValue: p("202601", 40), Change: value(0), YoY: value(25), MoM: value(0), YoYBase: p("202501", 32), MoMBase: p("202512", 40)}}}
}

func TestLaborForceRatiosAndScope(t *testing.T) {
	r := laborForceSample()
	d, err := LaborForce(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 3)
	require.Len(t, d.Observations, 3)
	require.Equal(t, "CPS household survey", d.Series[0].Survey)
	require.Equal(t, r.Numerator, d.Series[0].Numerator)
	require.Equal(t, r.Denominator, d.Series[0].Denominator)
	require.Equal(t, "percentage_points", d.Series[1].Unit)
	require.Equal(t, 1.0, *d.Observations[1].Value)
	require.Equal(t, "202501", d.Observations[1].Base.Period)
	require.Equal(t, "202512", d.Observations[2].Base.Period)
	require.Equal(t, r.Observations[0].FetchedAt, *d.Observations[0].FetchedAt)
	require.Nil(t, d.Observations[0].Preliminary)
	var original bls.LaborForceResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	r.Observations[0].YoYBase.Rate = value(0)
	r.Observations[0].YoYChange = value(4)
	d, err = LaborForce(r)
	require.NoError(t, err)
	require.Equal(t, "available", d.Observations[1].Status)
	require.Equal(t, 4.0, *d.Observations[1].Value)
	r.Observations[0].MoMBase.Rate = nil
	r.Observations[0].MoMChange = nil
	d, err = LaborForce(r)
	require.NoError(t, err)
	require.Equal(t, "baseline_missing", *d.Observations[2].MissingReason)
}

func TestEarningsChangesFlagsAndPerPointFetchTime(t *testing.T) {
	r := earningsSample()
	r.End = "202602"
	second := r.Observations[0]
	second.Period = "202602"
	second.MoMBase.Period = "202601"
	second.YoYBase.Period = "202502"
	second.Preliminary = true
	second.Footnotes = []bls.Footnote{{Code: "P", Text: "初步"}}
	second.FetchedAt = second.FetchedAt.Add(time.Hour)
	r.Observations = append(r.Observations, second)
	d, err := EarningsHours(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 8)
	require.False(t, *d.Observations[0].Preliminary)
	require.True(t, *d.Observations[4].Preliminary)
	require.Nil(t, d.Observations[5].Preliminary)
	require.Equal(t, r.Observations[0].FetchedAt, *d.Observations[0].FetchedAt)
	require.Equal(t, second.FetchedAt, *d.Observations[7].FetchedAt)
	require.Equal(t, "USD_per_hour", d.Series[1].Unit)
	require.Equal(t, "percent", d.Series[2].Unit)
	require.Equal(t, 25.0, *d.Observations[2].Value)
	require.Equal(t, "current_level - base_level", *d.Series[1].Formula)
	require.Nil(t, d.Observations[0].PublishedAt)
	require.Nil(t, d.Observations[0].RevisionID)
	var original bls.EarningsHoursResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
	// 同一个零基期能计算绝对变化，不能计算百分比增速。
	r = earningsSample()
	r.Observations[0].MoMBase.Value = value(0)
	r.Observations[0].Change = value(40)
	r.Observations[0].MoM = nil
	d, err = EarningsHours(r)
	require.NoError(t, err)
	require.Equal(t, "available", d.Observations[1].Status)
	require.Equal(t, 40.0, *d.Observations[1].Value)
	require.Equal(t, "zero_baseline", *d.Observations[3].MissingReason)
	r.Observations[0].Value = nil
	r.Observations[0].RawValue = ""
	r.Observations[0].FetchedAt = time.Time{}
	r.Observations[0].YoY = nil
	r.Observations[0].Change = nil
	d, err = EarningsHours(r)
	require.NoError(t, err)
	require.Nil(t, d.Observations[0].Preliminary)
	require.Nil(t, d.Observations[0].FetchedAt)
	require.Equal(t, "current_value_missing", *d.Observations[2].MissingReason)
}

func TestLaborAdaptersRejectBadInputs(t *testing.T) {
	for _, mutate := range []func(*bls.LaborForceResult){
		func(r *bls.LaborForceResult) { r.SeriesID = "CES0500000003" }, func(r *bls.LaborForceResult) { r.Denominator = "" }, func(r *bls.LaborForceResult) { r.SeasonallyAdjusted = false }, func(r *bls.LaborForceResult) { r.Observations[0].YoYBase.SeriesID = "wrong" }, func(r *bls.LaborForceResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := laborForceSample()
		mutate(r)
		d, err := LaborForce(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*bls.EarningsHoursResult){
		func(r *bls.EarningsHoursResult) { r.Indicator = "unknown" }, func(r *bls.EarningsHoursResult) { r.SeriesID = "LNS14000000" }, func(r *bls.EarningsHoursResult) { r.PriceBasis = "real" }, func(r *bls.EarningsHoursResult) { r.Observations[0].MoMBase.Period = "202511" }, func(r *bls.EarningsHoursResult) { r.Observations[0].SeriesID = "wrong" }, func(r *bls.EarningsHoursResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := earningsSample()
		mutate(r)
		d, err := EarningsHours(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err := LaborForce(nil)
	require.Error(t, err)
	_, err = EarningsHours(nil)
	require.Error(t, err)
	r := earningsSample()
	r.Observations = nil
	d, err := EarningsHours(r)
	require.NoError(t, err)
	require.NotNil(t, d.Observations)
	require.Empty(t, d.Observations)
}
