package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/chinabond"
	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

var rateFetched = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)

func lprItem(period, quote string, one, five, oneChange, fiveChange *float64, status string) pbc.LPRObservation {
	fetched := rateFetched
	return pbc.LPRObservation{Period: period, QuoteDate: quote, OneYearRate: one, FiveYearPlusRate: five, OneYearChangeBP: oneChange, FiveYearPlusChangeBP: fiveChange, ChangeStatus: status, Unit: "%", Definition: "lpr-reform-2019", PublishedAt: "2025-05-20T09:00:00+08:00", PublishedPrecision: "second", Source: "url", FetchedAt: &fetched}
}

func TestLPRKeepsTenorsAndAdjacentChanges(t *testing.T) {
	d, err := LPR([]pbc.LPRObservation{
		lprItem("202504", "20250421", value(3.1), value(3.6), value(0), value(0), "unchanged"),
		lprItem("202505", "20250520", value(3), value(3.5), value(-10), value(-10), "changed"),
		{Period: "202506", ChangeStatus: "missing-release", Unit: "%", Definition: "lpr-reform-2019", Note: "未检索到公告"},
		lprItem("202507", "20250721", value(3), value(3.5), nil, nil, "unknown"),
	}, "202504", "202507")
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 16)
	series := seriesByID(d)
	require.Equal(t, "percent", series["cn.lpr.1y.rate"].Unit)
	require.Equal(t, "official", series["cn.lpr.1y.rate"].ValueOrigin)
	require.Equal(t, "not_applicable", series["cn.lpr.1y.rate"].PriceBasis)
	require.Equal(t, "basis_points", series["cn.lpr.5y-plus.change"].Unit)
	require.Equal(t, "derived", series["cn.lpr.5y-plus.change"].ValueOrigin)

	rate := d.Observations[4]
	require.Equal(t, "cn.lpr.1y.rate", rate.SeriesID)
	require.Equal(t, 3.0, *rate.Value)
	require.Equal(t, "20250520", *rate.AsOfDate, "报价日不固定为20日")
	require.Equal(t, "2025-05-20", *rate.PublishedDate)
	require.NotNil(t, rate.PublishedAt)
	change := d.Observations[5]
	require.Equal(t, -10.0, *change.Value)
	require.Equal(t, "202504", change.Base.Period)
	require.Equal(t, 3.1, *change.Base.Value)
	require.Equal(t, 0.0, *d.Observations[1].Value, "持平是有效的零")
	require.Nil(t, d.Observations[1].Base.Value, "窗口外基期不反推")
	require.Contains(t, d.Observations[1].Note, "窗口外")

	gap := d.Observations[8]
	require.Equal(t, "report_not_found", *gap.MissingReason)
	require.Contains(t, gap.Note, "未检索到")
	require.Nil(t, gap.AsOfDate)
	require.Nil(t, gap.Source)
	afterGap := d.Observations[13]
	require.Equal(t, "cn.lpr.1y.change", afterGap.SeriesID)
	require.Equal(t, "baseline_missing", *afterGap.MissingReason, "缺月后不跨缺口比较")
	require.Nil(t, afterGap.Base.Value)

	day := lprItem("202506", "20250620", value(3), value(3.5), nil, nil, "unknown")
	day.PublishedAt, day.PublishedPrecision = "2025-06-20", "day"
	d, err = LPR([]pbc.LPRObservation{day}, "202506", "202506")
	require.NoError(t, err)
	require.Equal(t, "2025-06-20", *d.Observations[0].PublishedDate)
	require.Nil(t, d.Observations[0].PublishedAt, "日期精度不补时分秒")

	start := lprItem("201908", "20190820", value(4.25), value(4.85), nil, nil, "unknown")
	d, err = LPR([]pbc.LPRObservation{start}, "201908", "201908")
	require.NoError(t, err)
	require.Equal(t, "methodology_break", *d.Observations[1].MissingReason)

	for _, mutate := range []func(*pbc.LPRObservation){
		func(i *pbc.LPRObservation) { i.Unit = "bp" },
		func(i *pbc.LPRObservation) { i.Definition = "old" },
		func(i *pbc.LPRObservation) { i.Source = "" },
		func(i *pbc.LPRObservation) { i.FetchedAt = nil },
		func(i *pbc.LPRObservation) { i.QuoteDate = "" },
		func(i *pbc.LPRObservation) { i.PublishedPrecision = "minute" },
		func(i *pbc.LPRObservation) { i.PublishedAt = "bad" },
		func(i *pbc.LPRObservation) { i.Period = "2025-05" },
		func(i *pbc.LPRObservation) { i.OneYearRate = nil },
		func(i *pbc.LPRObservation) { i.OneYearChangeBP = value(5) },
	} {
		item := lprItem("202505", "20250520", value(3), value(3.5), value(-10), value(-10), "changed")
		mutate(&item)
		d, err := LPR([]pbc.LPRObservation{lprItem("202504", "20250421", value(3.1), value(3.6), value(0), value(0), "unchanged"), item}, "202504", "202505")
		require.Error(t, err)
		require.Nil(t, d)
	}
	empty, err := LPR(nil, "202505", "202505")
	require.NoError(t, err)
	require.Empty(t, empty.Series)
}

func reverseRepoEvent(date string, before, after, change float64) pbc.ReverseRepoEvent {
	return pbc.ReverseRepoEvent{EffectiveDate: date, PublishedAt: "2025-05-07T09:25:30+08:00", PublishedPrecision: "second", PreviousRate: before, Rate: after, ChangeBasisPoints: change, Source: "url", FetchedAt: rateFetched}
}

func TestReverseRepoEventsAndPrevious(t *testing.T) {
	previous := reverseRepoEvent("20240927", 1.7, 1.5, -20)
	r := &pbc.ReverseRepoResult{Indicator: "pbc-7-day-reverse-repo-policy-rate", TenorDays: 7, Unit: "%", Begin: "20250101", End: "20251231", PreviousEvent: &previous, Events: []pbc.ReverseRepoEvent{reverseRepoEvent("20250508", 1.5, 1.4, -10)}}
	d, err := ReverseRepo(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 2)
	require.Len(t, d.Observations, 2)
	require.Equal(t, "event", d.Series[0].Frequency)
	require.Equal(t, "effective-date", d.Series[0].PeriodBasis)
	rate, change := d.Observations[0], d.Observations[1]
	require.Equal(t, "20250508", rate.Period)
	require.Equal(t, 1.4, *rate.Value)
	require.Equal(t, "2025-05-07", *rate.PublishedDate, "公布日和生效日分开")
	require.Equal(t, -10.0, *change.Value)
	require.Equal(t, "20240927", change.Base.Period)
	require.Equal(t, 1.5, *change.Base.Value)
	require.Equal(t, "derived", d.Series[1].ValueOrigin)

	first := *r
	first.PreviousEvent, first.Begin, first.Events = nil, "20240722", []pbc.ReverseRepoEvent{reverseRepoEvent("20240722", 1.8, 1.7, -10)}
	d, err = ReverseRepo(&first)
	require.NoError(t, err)
	require.Equal(t, "", d.Observations[1].Base.Period, "覆盖前的前次生效日未知")
	require.Equal(t, 1.8, *d.Observations[1].Base.Value)

	none := *r
	none.Events = []pbc.ReverseRepoEvent{}
	d, err = ReverseRepo(&none)
	require.NoError(t, err)
	require.Len(t, d.Series, 2)
	require.Empty(t, d.Observations)
	var original pbc.ReverseRepoResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, "20240927", original.PreviousEvent.EffectiveDate)

	for _, mutate := range []func(*pbc.ReverseRepoResult){
		func(r *pbc.ReverseRepoResult) { r.Indicator = "x" },
		func(r *pbc.ReverseRepoResult) { r.TenorDays = 14 },
		func(r *pbc.ReverseRepoResult) { r.Unit = "bp" },
		func(r *pbc.ReverseRepoResult) { r.Events[0].EffectiveDate = "2025-05-08" },
		func(r *pbc.ReverseRepoResult) { r.Events[0].EffectiveDate = "20260101" },
		func(r *pbc.ReverseRepoResult) { r.Events[0].Source = "" },
		func(r *pbc.ReverseRepoResult) { r.Events[0].FetchedAt = time.Time{} },
		func(r *pbc.ReverseRepoResult) { r.Events[0].ChangeBasisPoints = -20 },
		func(r *pbc.ReverseRepoResult) { r.Events[0].PreviousRate = 1.6; r.Events[0].ChangeBasisPoints = -20 },
		func(r *pbc.ReverseRepoResult) { r.Events[0].PublishedAt = "bad" },
		func(r *pbc.ReverseRepoResult) { r.Events = append(r.Events, r.Events[0]) },
	} {
		p := reverseRepoEvent("20240927", 1.7, 1.5, -20)
		r := &pbc.ReverseRepoResult{Indicator: "pbc-7-day-reverse-repo-policy-rate", TenorDays: 7, Unit: "%", Begin: "20250101", End: "20251231", PreviousEvent: &p, Events: []pbc.ReverseRepoEvent{reverseRepoEvent("20250508", 1.5, 1.4, -10)}}
		mutate(r)
		d, err := ReverseRepo(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = ReverseRepo(nil)
	require.Error(t, err)
}

func TestReverseRepoHistorySeparatesSameDayOperations(t *testing.T) {
	event := func(url, raw string, rate float64, operation string) pbc.ReverseRepoHistoryEvent {
		return pbc.ReverseRepoHistoryEvent{PublicationDate: "2016-06-13", PublishedAt: "2016-06-13 10:30", OperationDate: operation, Term: "7天", AwardedRateRaw: raw, RatePercent: rate, SourceURL: url}
	}
	r := &pbc.ReverseRepoHistoryResult{Indicator: "7-day-reverse-repo-awarded-rate-history", Unit: "%", Currency: "CNY", DateBasis: "publication-date", Begin: "20160101", End: "20161231", Events: []pbc.ReverseRepoHistoryEvent{event("a", "2.25%", 2.25, "2016-06-13"), event("b", "2.30%", 2.3, "")}}
	d, err := ReverseRepoHistory(r)
	require.NoError(t, err)
	require.Len(t, d.Observations, 2)
	require.Equal(t, d.Observations[0].Period, d.Observations[1].Period)
	require.NotEqual(t, d.Observations[0].EventID, d.Observations[1].EventID, "同日多次操作靠event_id区分")
	require.Equal(t, "publication-date", d.Series[0].PeriodBasis)
	require.Equal(t, "2.25%", *d.Observations[0].RawValue)
	require.Equal(t, "2016-06-13", *d.Observations[0].AsOfDate)
	require.Nil(t, d.Observations[1].AsOfDate, "正文没有操作日时不推断")
	require.Nil(t, d.Observations[0].FetchedAt)

	for _, mutate := range []func(*pbc.ReverseRepoHistoryResult){
		func(r *pbc.ReverseRepoHistoryResult) { r.Indicator = "x" },
		func(r *pbc.ReverseRepoHistoryResult) { r.Unit = "bp" },
		func(r *pbc.ReverseRepoHistoryResult) { r.Currency = "USD" },
		func(r *pbc.ReverseRepoHistoryResult) { r.DateBasis = "operation-date" },
		func(r *pbc.ReverseRepoHistoryResult) { r.Events[0].PublicationDate = "bad" },
		func(r *pbc.ReverseRepoHistoryResult) { r.Events[0].SourceURL = "" },
		func(r *pbc.ReverseRepoHistoryResult) { r.Events[0].Term = "14天" },
		func(r *pbc.ReverseRepoHistoryResult) { r.Events = append(r.Events, r.Events[0]) },
	} {
		r := &pbc.ReverseRepoHistoryResult{Indicator: "7-day-reverse-repo-awarded-rate-history", Unit: "%", Currency: "CNY", DateBasis: "publication-date", Begin: "20160101", End: "20161231", Events: []pbc.ReverseRepoHistoryEvent{event("a", "2.25%", 2.25, "2016-06-13")}}
		mutate(r)
		d, err := ReverseRepoHistory(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = ReverseRepoHistory(nil)
	require.Error(t, err)
}

func rrrEvent(scope string, additional bool, lower, upper float64) pbc.RRREvent {
	return pbc.RRREvent{EffectiveDate: "20200415", EffectiveDateBasis: "explicit-date", ScopeType: "targeted", Scope: scope, Additional: additional, ChangePPLower: lower, ChangePPUpper: upper, TrancheIndex: 1, TrancheCount: 2, PublishedAt: "2020-04-03T17:00:00+08:00", PublishedPrecision: "second", Title: "决定", Source: "url", FetchedAt: rateFetched}
}

func TestRRRKeepsRangesAndScopes(t *testing.T) {
	r := &pbc.RRRResult{Indicator: "rmb-reserve-requirement-ratio-adjustments", Currency: "CNY", Unit: "百分点", Begin: "20200101", End: "20201231", Events: []pbc.RRREvent{rrrEvent("中小银行", false, -1, -0.5), rrrEvent("城商行", true, -0.25, -0.25)}}
	d, err := RRR(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 2)
	require.Len(t, d.Observations, 4)
	require.Len(t, d.PolicyEvents, 2)
	require.Equal(t, "percentage_points", d.Series[0].Unit)
	require.Equal(t, "event", d.Series[0].Frequency)
	require.Equal(t, -1.0, *d.Observations[0].Value, "区间不取均值")
	require.Equal(t, -0.5, *d.Observations[1].Value)
	require.Equal(t, d.Observations[0].EventID, d.Observations[1].EventID)
	require.NotEqual(t, d.Observations[0].EventID, d.Observations[2].EventID, "同日不同机构分开")
	require.Equal(t, d.Observations[0].EventID, d.PolicyEvents[0].ID)
	require.Equal(t, "中小银行", d.PolicyEvents[0].Scope)
	require.True(t, d.PolicyEvents[1].Additional)
	require.Equal(t, 2, d.PolicyEvents[0].TrancheCount)
	require.Equal(t, "2020-04-03", *d.Observations[0].PublishedDate)
	require.Empty(t, d.Events)
	again, err := RRR(r)
	require.NoError(t, err)
	require.Equal(t, d.Observations[0].EventID, again.Observations[0].EventID, "标识稳定")

	encoded, err := json.Marshal(mustPMIDataset(t))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "policy_events", "其他导出不增加该字段")

	for _, mutate := range []func(*pbc.RRRResult){
		func(r *pbc.RRRResult) { r.Indicator = "x" },
		func(r *pbc.RRRResult) { r.Currency = "USD" },
		func(r *pbc.RRRResult) { r.Unit = "%" },
		func(r *pbc.RRRResult) { r.Events[0].EffectiveDate = "2020-04-15" },
		func(r *pbc.RRRResult) { r.Events[0].Source = "" },
		func(r *pbc.RRRResult) { r.Events[0].FetchedAt = time.Time{} },
		func(r *pbc.RRRResult) { r.Events[0].Scope = "" },
		func(r *pbc.RRRResult) { r.Events[0].ScopeType = "" },
		func(r *pbc.RRRResult) { r.Events[0].ChangePPLower = 0 },
		func(r *pbc.RRRResult) { r.Events[0].TrancheIndex = 3 },
		func(r *pbc.RRRResult) { r.Events[0].PublishedPrecision = "minute" },
		func(r *pbc.RRRResult) { r.Events = append(r.Events, r.Events[0]) },
	} {
		r := &pbc.RRRResult{Indicator: "rmb-reserve-requirement-ratio-adjustments", Currency: "CNY", Unit: "百分点", Begin: "20200101", End: "20201231", Events: []pbc.RRREvent{rrrEvent("中小银行", false, -1, -0.5)}}
		mutate(r)
		d, err := RRR(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = RRR(nil)
	require.Error(t, err)
}

func mustPMIDataset(t *testing.T) *Dataset {
	t.Helper()
	d, err := PMI(nil, "manufacturing", "202609", "202609")
	require.NoError(t, err)
	return d
}

func TestCNTreasuryKeepsAllTenors(t *testing.T) {
	r := &chinabond.GetChinaBondResp{Flag: "0", HeList: []*chinabond.ChinaBondItem{
		{Date: "2026-10-20", OneYear: "1.40", TenYear: "0"},
		{Date: "2026-10-19", ThreeMonth: "1.18", ThirtyYear: "2.10"},
	}}
	d, err := CNTreasury(r, "url", "20261019", "20261020")
	require.NoError(t, err)
	require.Len(t, d.Series, 11)
	require.Len(t, d.Observations, 22)
	require.Equal(t, "20261019", d.Observations[0].Period, "按日期升序")
	require.Equal(t, "cn.treasury.3m.yield", d.Observations[0].SeriesID)
	require.Equal(t, 1.18, *d.Observations[0].Value)
	require.Equal(t, "1.18", *d.Observations[0].RawValue)
	require.Equal(t, "url", *d.Observations[0].Source)
	require.Nil(t, d.Observations[0].FetchedAt, "接口不提供抓取时间")
	series := seriesByID(d)
	require.Equal(t, "daily", series["cn.treasury.10y.yield"].Frequency)
	require.Equal(t, "percent", series["cn.treasury.10y.yield"].Unit)
	require.Equal(t, 2.10, *observationOf(t, d, "cn.treasury.30y.yield").Value)
	missing := observationOf(t, d, "cn.treasury.2y.yield")
	require.Equal(t, "source_value_missing", *missing.MissingReason)
	require.Nil(t, missing.RawValue)
	for _, o := range d.Observations {
		if o.SeriesID == "cn.treasury.10y.yield" && o.Period == "20261020" {
			require.Equal(t, 0.0, *o.Value, "零值不是缺失")
		}
	}
	var original chinabond.GetChinaBondResp
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, "2026-10-20", original.HeList[0].Date, "原结果顺序不变")

	for _, mutate := range []func(*chinabond.GetChinaBondResp){
		func(r *chinabond.GetChinaBondResp) { r.Flag = "1" },
		func(r *chinabond.GetChinaBondResp) { r.HeList = nil },
		func(r *chinabond.GetChinaBondResp) { r.HeList[0] = nil },
		func(r *chinabond.GetChinaBondResp) { r.HeList[0].Date = "20261020" },
		func(r *chinabond.GetChinaBondResp) { r.HeList[0].Date = "2026-11-01" },
		func(r *chinabond.GetChinaBondResp) { r.HeList[0].OneYear = "abc" },
		func(r *chinabond.GetChinaBondResp) { r.HeList = append(r.HeList, r.HeList[0]) },
	} {
		r := &chinabond.GetChinaBondResp{Flag: "0", HeList: []*chinabond.ChinaBondItem{{Date: "2026-10-20", OneYear: "1.40"}}}
		mutate(r)
		d, err := CNTreasury(r, "url", "20261019", "20261020")
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = CNTreasury(&chinabond.GetChinaBondResp{Flag: "0", HeList: []*chinabond.ChinaBondItem{}}, "", "20261019", "20261020")
	require.Error(t, err)
	_, err = CNTreasury(nil, "url", "20261019", "20261020")
	require.Error(t, err)
}
