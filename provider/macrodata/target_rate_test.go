package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/federalreserve"
	"github.com/stretchr/testify/require"
)

func targetEvent(date, previous string, lower, upper float64, change *float64, raw string) *federalreserve.TargetRateEvent {
	return &federalreserve.TargetRateEvent{EffectiveDate: date, PreviousEffectiveDate: previous, LowerPercent: lower, UpperPercent: upper, LowerChangeBasisPoints: change, UpperChangeBasisPoints: change, ReleaseStatus: "unavailable", ReleaseNote: "未找到声明", SourceURL: "table", FetchedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Raw: federalreserve.RateTableRaw{Level: raw}}
}

func targetRateSample() *federalreserve.TargetRateResult {
	baseline := targetEvent("2019-10-31", "2019-09-19", 1.5, 1.75, value(-25), "1.50-1.75")
	first := targetEvent("2020-03-04", "2019-10-31", 1, 1.25, value(-50), "1.00-1.25")
	announced := "2020-03-03"
	published := time.Date(2020, 3, 3, 15, 0, 0, 0, time.UTC)
	first.AnnouncedDate, first.PublishedAt, first.ReleaseStatus, first.ReleaseNote = &announced, &published, "available", "来自官方声明"
	second := targetEvent("2020-03-16", "2020-03-04", 0, .25, value(-100), "0.00-0.25")
	day := "2020-03-15"
	second.AnnouncedDate, second.ReleaseStatus, second.ReleaseNote = &day, "date-only", "声明未提供具体时间"
	return &federalreserve.TargetRateResult{Indicator: "target-rate", Frequency: "event", Begin: "20200301", End: "20200316", Unit: "%", Source: "Federal Reserve Board", Baseline: baseline, Current: second, Events: []*federalreserve.TargetRateEvent{first, second}}
}

func TestTargetRateEventsAndBaseline(t *testing.T) {
	r := targetRateSample()
	d, err := TargetRate(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 8)
	require.Equal(t, "us.target-rate.lower_bound", d.Series[0].ID)
	require.Equal(t, "percent", d.Series[0].Unit)
	require.Equal(t, "official", d.Series[1].ValueOrigin)
	require.Equal(t, "event", d.Series[0].Frequency)
	require.Equal(t, "effective-date", d.Series[0].PeriodBasis)
	require.Equal(t, "basis_points", d.Series[2].Unit)
	require.Equal(t, "derived", d.Series[3].ValueOrigin)
	require.Equal(t, "difference_checked_against_official_adjustment", d.Series[2].ChangeMethod)
	require.Empty(t, d.Series[0].ChangeMethod)
	for i, o := range d.Observations {
		require.Equal(t, d.Series[i%4].ID, o.SeriesID, "观测与序列按同一规格对应")
	}
	require.Equal(t, 1.25, *d.Observations[1].Value)

	lower, change := d.Observations[0], d.Observations[2]
	require.Equal(t, "20200304", lower.Period, "所属期是生效日，不是公布日")
	require.Equal(t, "2020-03-03", *lower.PublishedDate)
	require.NotNil(t, lower.PublishedAt)
	require.Equal(t, "1.00-1.25", *lower.RawValue)
	require.Nil(t, lower.Base)
	require.Empty(t, lower.Note)
	require.Equal(t, "current_visible_revision", lower.DataVersion)
	require.Equal(t, -50.0, *change.Value)
	require.Nil(t, change.RawValue)
	require.Equal(t, "20191031", change.Base.Period, "窗口外前值来自区间前基准")
	require.Equal(t, 1.5, *change.Base.Value)
	require.Equal(t, "1.50-1.75", *change.Base.RawValue)
	require.Equal(t, 1.75, *d.Observations[3].Base.Value, "上下限基期分别保留")

	second := d.Observations[4]
	require.Equal(t, 0.0, *second.Value, "零下限是有效值")
	require.Equal(t, "available", second.Status)
	require.Nil(t, second.PublishedAt)
	require.Equal(t, "2020-03-15", *second.PublishedDate)
	require.Equal(t, "声明未提供具体时间", second.Note)
	require.Equal(t, "20200304", d.Observations[6].Base.Period, "窗口内前值来自上一事件")

	var original federalreserve.TargetRateResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, "2019-10-31", original.Baseline.EffectiveDate)
	require.Equal(t, "2020-03-16", original.Current.EffectiveDate)
}

func TestTargetRateNoEventsAndRegimeStart(t *testing.T) {
	r := targetRateSample()
	r.Begin, r.End, r.Baseline, r.Events = "20200320", "20200322", r.Current, []*federalreserve.TargetRateEvent{}
	d, err := TargetRate(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.NotNil(t, d.Observations)
	require.Empty(t, d.Observations, "无调整时不补行")
	var original federalreserve.TargetRateResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, "2020-03-16", original.Current.EffectiveDate, "截至日有效区间保留在original")

	first := targetEvent("2008-12-16", "", 0, .25, nil, "0-0.25")
	first.Raw.Decrease = "75-100"
	r = &federalreserve.TargetRateResult{Indicator: "target-rate", Frequency: "event", Begin: "20081216", End: "20081216", Unit: "%", Source: "s", Current: first, Events: []*federalreserve.TargetRateEvent{first}}
	d, err = TargetRate(r)
	require.NoError(t, err)
	require.Equal(t, "available", d.Observations[0].Status)
	require.Equal(t, "未找到声明", d.Observations[0].Note)
	change := d.Observations[2]
	require.Nil(t, change.Value)
	require.Equal(t, "methodology_break", *change.MissingReason)
	require.Nil(t, change.Base)
	require.Contains(t, change.Note, "单一目标")
	require.Contains(t, change.Note, "未找到声明")
}

func TestTargetRateRejectsInconsistentResults(t *testing.T) {
	for _, mutate := range []func(*federalreserve.TargetRateResult){
		func(r *federalreserve.TargetRateResult) { r.Indicator = "effr" },
		func(r *federalreserve.TargetRateResult) { r.Frequency = "daily" },
		func(r *federalreserve.TargetRateResult) { r.Unit = "bp" },
		func(r *federalreserve.TargetRateResult) { r.Source = "" },
		func(r *federalreserve.TargetRateResult) { r.Begin = "202003" },
		func(r *federalreserve.TargetRateResult) { r.Begin = "20200305" },
		func(r *federalreserve.TargetRateResult) { r.Events[0] = nil },
		func(r *federalreserve.TargetRateResult) { r.Events[0].EffectiveDate = "20200304" },
		func(r *federalreserve.TargetRateResult) { r.Events[0].LowerPercent = 2 },
		func(r *federalreserve.TargetRateResult) { r.Events[1].UpperPercent = 0 },
		func(r *federalreserve.TargetRateResult) { r.Events[1].LowerPercent = -.25 },
		func(r *federalreserve.TargetRateResult) { r.Baseline = nil },
		func(r *federalreserve.TargetRateResult) { r.Events[0].PreviousEffectiveDate = "2019-09-19" },
		func(r *federalreserve.TargetRateResult) { r.Events[0].LowerChangeBasisPoints = value(-25) },
		func(r *federalreserve.TargetRateResult) { r.Events[0].LowerChangeBasisPoints = nil },
		func(r *federalreserve.TargetRateResult) { r.Events[1].UpperChangeBasisPoints = nil },
		func(r *federalreserve.TargetRateResult) { r.Events[1].ReleaseFetchFailed = true },
		func(r *federalreserve.TargetRateResult) { r.Baseline.ReleaseFetchFailed = true },
		func(r *federalreserve.TargetRateResult) {
			r.Baseline.EffectiveDate = "2019/10/31"
			r.Events[0].PreviousEffectiveDate = "2019/10/31"
		},
		func(r *federalreserve.TargetRateResult) { r.Baseline.LowerPercent = 2 },
		func(r *federalreserve.TargetRateResult) { r.Baseline.LowerPercent = -1 },
		func(r *federalreserve.TargetRateResult) { r.Baseline.ReleaseStatus = "unknown" },
		func(r *federalreserve.TargetRateResult) { r.Baseline.SourceURL = "" },
		func(r *federalreserve.TargetRateResult) { r.Events[0].SourceURL = "" },
		func(r *federalreserve.TargetRateResult) { r.Events[1].FetchedAt = time.Time{} },
		func(r *federalreserve.TargetRateResult) { bad := "03/03/2020"; r.Events[1].AnnouncedDate = &bad },
		func(r *federalreserve.TargetRateResult) { late := "2020-03-17"; r.Events[1].AnnouncedDate = &late },
		func(r *federalreserve.TargetRateResult) {
			other := time.Date(2020, 3, 2, 15, 0, 0, 0, time.UTC)
			r.Events[0].PublishedAt = &other
		},
		func(r *federalreserve.TargetRateResult) { r.Events[1].UpperChangeBasisPoints = value(-25) },
		func(r *federalreserve.TargetRateResult) { r.Events[1], r.Events[0] = r.Events[0], r.Events[1] },
		func(r *federalreserve.TargetRateResult) { r.Events[0].ReleaseStatus = "unknown" },
		func(r *federalreserve.TargetRateResult) { r.Events[0].PublishedAt = nil },
		func(r *federalreserve.TargetRateResult) { r.Events[1].ReleaseStatus = "available" },
		func(r *federalreserve.TargetRateResult) { r.Events[1].ReleaseStatus = "unavailable" },
	} {
		r := targetRateSample()
		mutate(r)
		d, err := TargetRate(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	first := targetEvent("2008-12-16", "", 0, .25, value(-75), "0-0.25")
	d, err := TargetRate(&federalreserve.TargetRateResult{Indicator: "target-rate", Frequency: "event", Begin: "20081216", End: "20081216", Unit: "%", Source: "s", Events: []*federalreserve.TargetRateEvent{first}})
	require.Error(t, err, "机制起点不得有跨口径变化")
	require.Nil(t, d)
	_, err = TargetRate(nil)
	require.Error(t, err)
}
