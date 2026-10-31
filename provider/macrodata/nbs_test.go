package macrodata

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func retailSample(period string) nbs.RetailObservation {
	return nbs.RetailObservation{Region: "CN", Period: period, YTDPeriod: period[:4] + "01-" + period, Amount: value(10), YoY: value(2), YTDAmount: value(100), YTDGrowth: value(5), AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", Source: "https://data.stats.gov.cn/retail", AmountSeriesID: "monthly-level", YoYSeriesID: "monthly-growth", YTDAmountSeriesID: "ytd-level", YTDGrowthSeriesID: "ytd-growth", FetchedAt: time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC)}
}

func propertySample(period, component, scope string) nbs.PropertyObservation {
	unit, price := "万平方米", "not_applicable"
	if component == "sales-value" {
		unit, price = "亿元", "nominal"
	}
	return nbs.PropertyObservation{Region: "CN", Component: component, Period: period, YTDPeriod: period[:4] + "01-" + period, YTDAmount: value(100), YTDGrowth: value(-5), AmountUnit: unit, GrowthUnit: "%", PriceBasis: price, ScopeVersion: scope, Source: "https://data.stats.gov.cn/property", AmountSeriesID: "level-id", GrowthSeriesID: "growth-id", Definition: "官方定义", Annotation: "官方注释"}
}

func TestRetailPeriodsAndNoDerivedMonthlyValues(t *testing.T) {
	jan, feb, mar := retailSample("202601"), retailSample("202602"), retailSample("202603")
	jan.Amount, jan.YoY, jan.YTDAmount, jan.YTDGrowth = nil, nil, nil, nil
	feb.Amount, feb.YoY, feb.YTDAmount, feb.YTDGrowth = nil, nil, value(80), value(7)
	mar.Amount, mar.YoY, mar.YTDAmount, mar.YTDGrowth = value(0), value(0), value(100), value(5)
	items := []nbs.RetailObservation{jan, feb, mar}
	d, err := Retail(items, "202601", "202603")
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 12)
	require.Equal(t, "month", d.Series[0].PeriodBasis)
	require.Equal(t, "year-to-date", d.Series[2].PeriodBasis)
	require.Equal(t, "202601", d.Observations[6].PeriodBegin)
	require.Equal(t, "202602", d.Observations[6].PeriodEnd)
	require.Equal(t, 80.0, *d.Observations[6].Value)
	require.Nil(t, d.Observations[4].Value, "不把1—2月累计视为2月单月")
	require.Nil(t, d.Observations[0].Value, "不分摊给1月")
	require.Equal(t, 0.0, *d.Observations[8].Value, "不把累计差额20覆盖官方单月零值")
	require.Equal(t, "available", d.Observations[8].Status)
	require.Equal(t, 5.0, *d.Observations[11].Value, "直接保留官方累计同比，不作5-7差分")
	require.Equal(t, "monthly-level", d.Observations[8].SourceSeriesID)
	for _, s := range d.Series {
		require.Equal(t, "official", s.ValueOrigin)
		require.Nil(t, s.Formula)
		require.False(t, s.Annualized)
	}
	for _, o := range d.Observations {
		require.Nil(t, o.Base)
		require.Nil(t, o.RawValue)
		require.Nil(t, o.PublishedAt)
		require.Equal(t, jan.FetchedAt, *o.FetchedAt)
	}
	var original []nbs.RetailObservation
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, items, original)
	// 仅查询2月时也不为累计区间额外插入1月观测。
	d, err = Retail([]nbs.RetailObservation{feb}, "202602", "202602")
	require.NoError(t, err)
	require.Len(t, d.Observations, 4)
	require.Equal(t, "202601", d.Observations[2].PeriodBegin)
}

func TestPropertyUnitsAndHistoricalScope(t *testing.T) {
	items := []nbs.PropertyObservation{
		propertySample("200507", "sales-value", "before_200508_actual_sales"),
		propertySample("200508", "sales-value", "200508_transition_unspecified"),
		propertySample("200509", "sales-value", "after_200508_presales_and_completed_sales"),
		propertySample("200509", "sales-area", "after_200508_presales_and_completed_sales"),
		propertySample("200509", "starts-area", "real_estate_developers"),
	}
	items[4].YTDAmount = value(0)
	items[0].PublishedAt = timestamp(time.Date(2005, 8, 20, 1, 0, 0, 0, time.UTC))
	r := &nbs.PropertyResult{Observations: items, Notes: []string{"调查范围说明"}}
	d, err := Property(r, "200507", "200509")
	require.NoError(t, err)
	require.Len(t, d.Series, 10)
	require.NotEqual(t, d.Observations[0].SeriesID, d.Observations[2].SeriesID)
	require.NotEqual(t, d.Observations[2].SeriesID, d.Observations[4].SeriesID)
	require.Equal(t, "CNY_100_million", d.Series[0].Unit)
	require.Equal(t, "square_meters_10000", d.Series[6].Unit)
	require.Equal(t, "not_applicable", d.Series[6].PriceBasis)
	require.Equal(t, "percent", d.Series[7].Unit)
	require.Equal(t, "nominal", d.Series[0].PriceBasis)
	require.Equal(t, "growth-id", d.Observations[1].SourceSeriesID)
	require.Equal(t, -5.0, *d.Observations[1].Value)
	require.Equal(t, "200501", d.Observations[0].PeriodBegin)
	require.Equal(t, items[0].PublishedAt, d.Observations[0].PublishedAt)
	require.Nil(t, d.Observations[0].FetchedAt)
	require.Equal(t, "available", d.Observations[8].Status)
	var original nbs.PropertyResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, *r, original)
}

func TestPropertyYearResetAndMissingJanuary(t *testing.T) {
	dec := propertySample("202512", "sales-value", "after_200508_presales_and_completed_sales")
	jan := propertySample("202601", "sales-value", dec.ScopeVersion)
	feb := propertySample("202602", "sales-value", dec.ScopeVersion)
	jan.YTDAmount, jan.YTDGrowth = nil, nil
	feb.YTDAmount, feb.YTDGrowth = value(20), value(-10)
	d, err := Property(&nbs.PropertyResult{Observations: []nbs.PropertyObservation{dec, jan, feb}}, "202512", "202602")
	require.NoError(t, err)
	require.Len(t, d.Series, 2)
	require.Len(t, d.Observations, 6)
	require.Equal(t, "202501", d.Observations[0].PeriodBegin)
	require.Equal(t, "202601", d.Observations[4].PeriodBegin)
	require.Equal(t, "202602", d.Observations[4].PeriodEnd)
	require.Equal(t, 20.0, *d.Observations[4].Value)
	require.Equal(t, "missing", d.Observations[2].Status)
	require.Equal(t, "source_value_missing", *d.Observations[2].MissingReason)
	require.Nil(t, d.Observations[2].Value)
	require.Nil(t, d.Observations[2].Base)
}

func TestNBSAdaptersRejectInvalidData(t *testing.T) {
	for _, mutate := range []func(*nbs.RetailObservation){
		func(o *nbs.RetailObservation) { o.YTDPeriod = "202602-202603" },
		func(o *nbs.RetailObservation) { o.Period = "202613" },
		func(o *nbs.RetailObservation) { o.AmountUnit = "万元" },
		func(o *nbs.RetailObservation) { o.PriceBasis = "real" },
		func(o *nbs.RetailObservation) { o.SeasonallyAdjusted = true },
		func(o *nbs.RetailObservation) { o.Amount = value(math.Inf(1)) },
	} {
		o := retailSample("202603")
		mutate(&o)
		d, err := Retail([]nbs.RetailObservation{o}, "202601", "202603")
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*nbs.PropertyObservation){
		func(o *nbs.PropertyObservation) { o.Component = "unknown" },
		func(o *nbs.PropertyObservation) { o.ScopeVersion = "" },
		func(o *nbs.PropertyObservation) { o.YTDPeriod = "202501-202603" },
		func(o *nbs.PropertyObservation) { o.AmountUnit = "亿元" },
		func(o *nbs.PropertyObservation) { o.SeasonallyAdjusted = true },
		func(o *nbs.PropertyObservation) { o.YTDGrowth = value(math.NaN()) },
	} {
		o := propertySample("202603", "sales-area", "after_200508_presales_and_completed_sales")
		mutate(&o)
		d, err := Property(&nbs.PropertyResult{Observations: []nbs.PropertyObservation{o}}, "202601", "202603")
		require.Error(t, err)
		require.Nil(t, d)
	}
	o := retailSample("202603")
	_, err := Retail([]nbs.RetailObservation{o, o}, "202601", "202603")
	require.ErrorContains(t, err, "重复")
	p := propertySample("202603", "sales-area", "scope")
	_, err = Property(&nbs.PropertyResult{Observations: []nbs.PropertyObservation{p, p}}, "202601", "202603")
	require.ErrorContains(t, err, "重复")
	_, err = Retail([]nbs.RetailObservation{o}, "202601", "202602")
	require.ErrorContains(t, err, "范围")
	_, err = Property(nil, "202601", "202603")
	require.Error(t, err)
	d, err := Retail(nil, "202601", "202603")
	require.NoError(t, err)
	require.Empty(t, d.Observations)
	require.JSONEq(t, "[]", string(d.Original))
	d, err = Property(&nbs.PropertyResult{}, "202601", "202603")
	require.NoError(t, err)
	require.NotNil(t, d.Observations)
	require.NotNil(t, d.Series)
}
