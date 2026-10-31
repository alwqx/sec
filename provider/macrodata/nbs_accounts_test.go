package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func cnGDPSample() *nbs.GDPResult {
	series := []nbs.GDPSeries{}
	for _, key := range []string{"real-value", "real-ytd-value", "yoy-index", "ytd-yoy-index", "qoq"} {
		series = append(series, nbs.GDPSeries{Key: key, SeriesID: "id-" + key, SeriesName: key, Annotation: "分段基期", SeasonallyAdjusted: key == "qoq", Source: "url", FetchedAt: nbsFetched})
	}
	return &nbs.GDPResult{Frequency: "quarterly", PriceBasis: "constant-prices", Begin: "2026Q3", End: "2026Q3", ValueUnit: "亿元", GrowthUnit: "%", Series: series, Observations: []nbs.GDPObservation{{Period: "2026Q3", RealValue: value(300), RealYTDValue: value(900), YoYIndex: value(104.8), YoY: value(4.8), YTDYoYIndex: value(105), YTDYoY: value(5), QoQ: value(-0.5)}}}
}

func TestCNGDPSeparatesLevelsIndexesAndGrowth(t *testing.T) {
	d, err := CNGDP(cnGDPSample())
	require.NoError(t, err)
	require.Len(t, d.Series, 7)
	require.Len(t, d.Observations, 7)
	series := seriesByID(d)
	require.Equal(t, "CNY_100_million", series["cn.gdp.level"].Unit)
	require.Equal(t, "constant-prices", series["cn.gdp.level"].PriceBasis)
	require.Nil(t, series["cn.gdp.level"].ReferenceYear, "分段基期不标统一基年")
	require.Equal(t, "分段基期", series["cn.gdp.level"].Coverage)
	require.Equal(t, "year-to-date", series["cn.gdp.ytd_level"].PeriodBasis)
	require.Equal(t, "official", series["cn.gdp.yoy_index"].ValueOrigin)
	require.Equal(t, "上年同期=100", series["cn.gdp.yoy_index"].IndexBase)
	require.Equal(t, "derived", series["cn.gdp.yoy_percent"].ValueOrigin)
	require.Equal(t, "official_index - 100", *series["cn.gdp.yoy_percent"].Formula)
	require.Equal(t, "seasonally-adjusted", series["cn.gdp.qoq_percent"].SeasonalAdjustment)
	require.False(t, series["cn.gdp.qoq_percent"].Annualized, "环比未年化")
	require.Equal(t, "not-seasonally-adjusted", series["cn.gdp.yoy_percent"].SeasonalAdjustment)
	ytd := observationOf(t, d, "cn.gdp.ytd_yoy_percent")
	require.Equal(t, "2026Q1", ytd.PeriodBegin)
	require.Equal(t, "2026Q3", ytd.PeriodEnd)
	require.Equal(t, 105.0, *ytd.Inputs[0].Value)
	require.Equal(t, "id-ytd-yoy-index", ytd.SourceSeriesID)
	require.Equal(t, -0.5, *observationOf(t, d, "cn.gdp.qoq_percent").Value)

	r := cnGDPSample()
	r.Observations[0].YoYIndex, r.Observations[0].YoY = nil, nil
	r.Observations[0].RealValue = nil
	d, err = CNGDP(r)
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *observationOf(t, d, "cn.gdp.level").MissingReason)
	require.Equal(t, "current_value_missing", *observationOf(t, d, "cn.gdp.yoy_percent").MissingReason)

	for _, mutate := range []func(*nbs.GDPResult){
		func(r *nbs.GDPResult) { r.Frequency = "monthly" },
		func(r *nbs.GDPResult) { r.PriceBasis = "current" },
		func(r *nbs.GDPResult) { r.ValueUnit = "元" },
		func(r *nbs.GDPResult) { year := 2020; r.RealBaseYear = &year },
		func(r *nbs.GDPResult) { r.Series = r.Series[:4] },
		func(r *nbs.GDPResult) { r.Series[0].Source = "" },
		func(r *nbs.GDPResult) { r.Series[0].SeasonallyAdjusted = true },
		func(r *nbs.GDPResult) { r.Observations[0].YoY = value(1) },
		func(r *nbs.GDPResult) { r.Observations[0].YTDYoY = nil },
		func(r *nbs.GDPResult) { r.Observations = append(r.Observations, r.Observations[0]) },
		func(r *nbs.GDPResult) { r.Observations[0].Period = "2026Q4" },
		func(r *nbs.GDPResult) { r.Observations[0].Period = "" },
		func(r *nbs.GDPResult) { r.Observations[0].Period = "202" },
	} {
		r := cnGDPSample()
		mutate(r)
		d, err := CNGDP(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = CNGDP(nil)
	require.Error(t, err)
}

func householdSample(period string) *nbs.HouseholdResult {
	series := []nbs.HouseholdSeries{}
	for key, basis := range map[string]string{"income-ytd": "current-prices", "income-real-yoy": "real-growth", "consumption-ytd": "current-prices", "consumption-real-yoy": "real-growth"} {
		series = append(series, nbs.HouseholdSeries{Key: key, SeriesID: "id-" + key, SeriesName: key, PriceBasis: basis, Source: "url", FetchedAt: nbsFetched})
	}
	return &nbs.HouseholdResult{Frequency: "quarterly", Measurement: "year-to-date-per-capita-flow", Population: "全国居民", SurveyMethod: "住户调查", ValueUnit: "元/人", GrowthUnit: "%", Begin: period, End: period, Series: series, Observations: []nbs.HouseholdObservation{{Period: period, IncomeYTD: value(100), IncomeRealYTDYoY: value(5), ConsumptionYTD: value(60), ConsumptionRealYTDYoY: value(4), RawValues: map[string]json.RawMessage{"income-ytd": json.RawMessage(`"100"`)}, IncludesOwnProduction: period[5] == '4'}}}
}

func TestHouseholdKeepsCumulativeScope(t *testing.T) {
	d, err := Household(householdSample("2026Q3"))
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	series := seriesByID(d)
	income := series["cn.household.income.ytd_level"]
	require.Equal(t, "CNY_per_person", income.Unit)
	require.Equal(t, "nominal", income.PriceBasis)
	require.Equal(t, "year-to-date", income.PeriodBasis)
	require.False(t, income.Annualized)
	require.Equal(t, "real", series["cn.household.income.ytd_real_yoy_percent"].PriceBasis)
	o := observationOf(t, d, income.ID)
	require.Equal(t, "2026Q1", o.PeriodBegin)
	require.Equal(t, "100", *o.RawValue)
	require.Empty(t, o.Note)
	require.Nil(t, observationOf(t, d, "cn.household.consumption.ytd_level").RawValue)

	r := householdSample("2026Q3")
	r.Observations[0].IncomeYTD = nil
	r.Observations[0].RawValues["income-ytd"] = json.RawMessage(`null`)
	r.Observations[0].RawValues["consumption-real-yoy"] = json.RawMessage(`""`)
	r.Observations[0].RawValues["consumption-ytd"] = json.RawMessage(`60.5`)
	r.Observations[0].RawValues["income-real-yoy"] = json.RawMessage(`"a\"b"`)
	d, err = Household(r)
	require.NoError(t, err)
	require.Nil(t, observationOf(t, d, income.ID).RawValue, "JSON null不是字符串null")
	require.Nil(t, observationOf(t, d, "cn.household.consumption.ytd_real_yoy_percent").RawValue, "空字符串也是缺失")
	require.Equal(t, "60.5", *observationOf(t, d, "cn.household.consumption.ytd_level").RawValue)
	require.Equal(t, `a"b`, *observationOf(t, d, "cn.household.income.ytd_real_yoy_percent").RawValue, "按JSON解码")

	d, err = Household(householdSample("2026Q4"))
	require.NoError(t, err)
	require.Contains(t, d.Observations[0].Note, "自产自用", "全年口径不同")

	r = householdSample("2026Q3")
	r.Observations[0].IncomeRealYTDYoY = nil
	d, err = Household(r)
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *observationOf(t, d, "cn.household.income.ytd_real_yoy_percent").MissingReason)

	for _, mutate := range []func(*nbs.HouseholdResult){
		func(r *nbs.HouseholdResult) { r.Frequency = "annual" },
		func(r *nbs.HouseholdResult) { r.Measurement = "quarter" },
		func(r *nbs.HouseholdResult) { r.ValueUnit = "元" },
		func(r *nbs.HouseholdResult) { r.SeasonallyAdjusted = true },
		func(r *nbs.HouseholdResult) { r.Annualized = true },
		func(r *nbs.HouseholdResult) { r.Series = r.Series[:3] },
		func(r *nbs.HouseholdResult) { r.Series[0].Source = "" },
		func(r *nbs.HouseholdResult) { r.Series[0].PriceBasis = "unknown" },
		func(r *nbs.HouseholdResult) { r.Observations[0].IncludesOwnProduction = true },
		func(r *nbs.HouseholdResult) { r.Observations[0].Period = "2026" },
		func(r *nbs.HouseholdResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := householdSample("2026Q3")
		mutate(r)
		d, err := Household(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = Household(nil)
	require.Error(t, err)
}

func investmentItem(sector, scope, period string, amount, growth *float64) nbs.InvestmentObservation {
	fetched := nbsFetched
	published := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	return nbs.InvestmentObservation{Sector: sector, Period: period, YTDPeriod: period[:4] + "01-" + period, YTDAmount: amount, YTDGrowth: growth, AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", InfrastructureScope: scope, BasisNote: "可比口径说明", Status: "published", Source: "url", PublishedAt: &published, FetchedAt: &fetched, Note: "整行备注；官方月报未公布金额"}
}

func TestCNInvestmentKeepsScopesAndGaps(t *testing.T) {
	r := &nbs.InvestmentResult{Sector: "infrastructure", Observations: []nbs.InvestmentObservation{
		{Sector: "infrastructure", Period: "202601", YTDPeriod: "202601-202601", AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", Status: "january_exempt", Note: "1月份免报"},
		investmentItem("infrastructure", "including_electricity_heat_gas_water", "202602", nil, value(11.4)),
		investmentItem("infrastructure", "excluding_electricity_heat_gas_water", "202603", nil, value(-1)),
		{Sector: "infrastructure", Period: "202604", YTDPeriod: "202601-202604", AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", Status: "not_published", Note: "尚未检索到"},
	}}
	d, err := CNInvestment(r, "202601", "202604")
	require.NoError(t, err)
	require.Len(t, d.Observations, 8)
	series := seriesByID(d)
	including := "cn.investment.infrastructure.including_electricity_heat_gas_water.ytd_yoy_percent"
	excluding := "cn.investment.infrastructure.excluding_electricity_heat_gas_water.ytd_yoy_percent"
	require.Contains(t, series, including)
	require.Contains(t, series, excluding, "含电力与不含电力不共用序列")
	require.Equal(t, series[including].Indicator, series[excluding].Indicator)
	require.Contains(t, series[including].Coverage, "含电力")
	require.Equal(t, "january_exempt", *d.Observations[0].MissingReason)
	require.Equal(t, "1月份免报", d.Observations[0].Note)
	require.Nil(t, d.Observations[0].Source)
	require.Nil(t, d.Observations[0].FetchedAt)
	require.Equal(t, "report_not_found", *d.Observations[6].MissingReason)
	amount := observationOf(t, d, "cn.investment.infrastructure.including_electricity_heat_gas_water.ytd_level")
	require.Equal(t, "source_value_missing", *amount.MissingReason)
	require.Contains(t, amount.Note, "不反推")
	growth := observationOf(t, d, including)
	require.Equal(t, 11.4, *growth.Value)
	require.Equal(t, "可比口径说明", growth.Note, "金额缺失说明不写到增速上")
	require.Equal(t, "202601", growth.PeriodBegin)
	require.NotNil(t, growth.PublishedAt)

	r = &nbs.InvestmentResult{Sector: "total", Observations: []nbs.InvestmentObservation{investmentItem("total", "", "202609", value(334115), value(-7.2))}}
	d, err = CNInvestment(r, "202609", "202609")
	require.NoError(t, err)
	require.Equal(t, 334115.0, *d.Observations[0].Value)
	require.Empty(t, d.Observations[0].Note)
	require.Equal(t, "CNY_100_million", d.Series[0].Unit)
	require.Equal(t, "nominal", d.Series[0].PriceBasis)

	for _, mutate := range []func(*nbs.InvestmentObservation){
		func(i *nbs.InvestmentObservation) { i.Sector = "other" },
		func(i *nbs.InvestmentObservation) { i.AmountUnit = "元" },
		func(i *nbs.InvestmentObservation) { i.GrowthUnit = "点" },
		func(i *nbs.InvestmentObservation) { i.PriceBasis = "real" },
		func(i *nbs.InvestmentObservation) { i.SeasonallyAdjusted = true },
		func(i *nbs.InvestmentObservation) { i.Status = "unknown" },
		func(i *nbs.InvestmentObservation) { i.Source = "" },
		func(i *nbs.InvestmentObservation) { i.FetchedAt = nil },
		func(i *nbs.InvestmentObservation) { i.YTDPeriod = "202601-202608" },
		func(i *nbs.InvestmentObservation) { i.Status = "not_published" },
	} {
		item := investmentItem("total", "", "202609", value(1), value(1))
		mutate(&item)
		d, err := CNInvestment(&nbs.InvestmentResult{Sector: "total", Observations: []nbs.InvestmentObservation{item}}, "202609", "202609")
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = CNInvestment(&nbs.InvestmentResult{Sector: "infrastructure", Observations: []nbs.InvestmentObservation{investmentItem("infrastructure", "", "202609", nil, value(1))}}, "202609", "202609")
	require.Error(t, err, "已发布的基建必须有统计范围")
	_, err = CNInvestment(nil, "202609", "202609")
	require.Error(t, err)
}

func cnEnterpriseSample() *nbs.EnterpriseResult {
	ytd := "202601-202602"
	asOf := "2026-02-28"
	metric := func(name, measurement string) nbs.EnterpriseMetric {
		return nbs.EnterpriseMetric{Metric: name, Measurement: measurement, ValueSeries: nbs.EnterpriseSeries{ID: name + "-value", Name: name + "累计值"}, GrowthSeries: nbs.EnterpriseSeries{ID: name + "-growth", Name: name + "增长"}}
	}
	return &nbs.EnterpriseResult{AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", Source: "url", FetchedAt: nbsFetched,
		Metrics: []nbs.EnterpriseMetric{metric("profit", "year_to_date_flow"), metric("receivables", "month_end_stock")},
		Observations: []nbs.EnterpriseObservation{
			{Metric: "profit", Period: "202602", Measurement: "year_to_date_flow", YTDPeriod: &ytd, Value: value(-5), YoY: value(-120), ScopeStandard: "scope20m", SurveyScope: "2000万元及以上", Status: "available", Note: "1—2月合并累计利润"},
			{Metric: "receivables", Period: "202602", Measurement: "month_end_stock", AsOfDate: &asOf, Value: value(0), YoY: nil, ScopeStandard: "scope20m", SurveyScope: "2000万元及以上", AccountingScope: "separate_accounts_receivable_since_2020", Status: "partial", Note: "部分单元格未提供数值"},
		}}
}

func TestCNEnterpriseSplitsScopesAndFlows(t *testing.T) {
	d, err := CNEnterprise(cnEnterpriseSample(), "202601", "202602")
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	series := seriesByID(d)
	profit := series["cn.enterprise.profit.scope20m.ytd_level"]
	require.Equal(t, "year-to-date", profit.PeriodBasis)
	require.Equal(t, "CNY_100_million", profit.Unit)
	require.Equal(t, "profit累计值", profit.Definition)
	require.Equal(t, "2000万元及以上", profit.Coverage)
	require.Equal(t, "ytd_yoy_percent", series["cn.enterprise.profit.scope20m.ytd_yoy_percent"].Measure)
	first := observationOf(t, d, profit.ID)
	require.Equal(t, -5.0, *first.Value, "亏损保留")
	require.Equal(t, "202601", first.PeriodBegin)
	require.Empty(t, first.Note, "有值的观测不带整行备注")
	require.Equal(t, -120.0, *observationOf(t, d, "cn.enterprise.profit.scope20m.ytd_yoy_percent").Value)
	stockID := "cn.enterprise.receivables.scope20m.separate_accounts_receivable_since_2020.level"
	require.Equal(t, "month-end", series[stockID].PeriodBasis)
	stock := observationOf(t, d, stockID)
	require.Equal(t, 0.0, *stock.Value)
	require.Equal(t, "2026-02-28", *stock.AsOfDate)
	require.Equal(t, "202602", stock.PeriodBegin, "月末余额不是累计")
	missing := observationOf(t, d, "cn.enterprise.receivables.scope20m.separate_accounts_receivable_since_2020.yoy_percent")
	require.Equal(t, "source_value_missing", *missing.MissingReason)
	require.Equal(t, "部分单元格未提供数值", missing.Note)

	for status, reason := range map[string]string{"january_exempt": "january_exempt", "not_surveyed": "not_surveyed", "definition_suspended": "definition_suspended", "no_value": "source_value_missing"} {
		r := cnEnterpriseSample()
		r.Observations = r.Observations[1:]
		r.Observations[0].Value, r.Observations[0].Status = nil, status
		d, err := CNEnterprise(r, "202601", "202602")
		require.NoError(t, err)
		require.Equal(t, reason, *d.Observations[0].MissingReason)
	}

	r := cnEnterpriseSample()
	r.Observations[0].YoY, r.Observations[0].Note = nil, nbs.EnterpriseYoYNotComputableNote+"；1—2月合并累计利润"
	d, err = CNEnterprise(r, "202601", "202602")
	require.NoError(t, err)
	computed := observationOf(t, d, "cn.enterprise.profit.scope20m.ytd_yoy_percent")
	require.Equal(t, "official_not_computable", *computed.MissingReason)
	require.Contains(t, computed.Note, "不按零处理")

	r = cnEnterpriseSample()
	r.Observations[0].Value, r.Observations[0].YoY = nil, nil
	r.Observations[0].Note = nbs.EnterpriseYoYNotComputableNote + "；部分单元格未提供数值"
	d, err = CNEnterprise(r, "202601", "202602")
	require.NoError(t, err)
	levelMissing := observationOf(t, d, "cn.enterprise.profit.scope20m.ytd_level")
	require.Equal(t, "source_value_missing", *levelMissing.MissingReason)
	require.Equal(t, "部分单元格未提供数值", levelMissing.Note, "金额不带同比不可计算的说明")

	r = cnEnterpriseSample()
	other := r.Observations[1]
	other.Period, other.ScopeStandard, other.AccountingScope = "202601", "scope5m", "accounts_receivable_before_2019"
	r.Observations = append(r.Observations, other)
	d, err = CNEnterprise(r, "202601", "202602")
	require.NoError(t, err)
	require.Len(t, d.Series, 6, "调查范围和列报方式不同不共用序列")

	for _, mutate := range []func(*nbs.EnterpriseResult){
		func(r *nbs.EnterpriseResult) { r.AmountUnit = "元" },
		func(r *nbs.EnterpriseResult) { r.GrowthUnit = "点" },
		func(r *nbs.EnterpriseResult) { r.PriceBasis = "real" },
		func(r *nbs.EnterpriseResult) { r.SeasonallyAdjusted = true },
		func(r *nbs.EnterpriseResult) { r.Source = "" },
		func(r *nbs.EnterpriseResult) { r.Metrics[0].ValueSeries.ID = "" },
		func(r *nbs.EnterpriseResult) { r.Observations[0].Metric = "unknown" },
		func(r *nbs.EnterpriseResult) { r.Observations[0].Measurement = "month_end_stock" },
		func(r *nbs.EnterpriseResult) { r.Observations[0].ScopeStandard = "" },
		func(r *nbs.EnterpriseResult) { r.Observations[0].YTDPeriod = nil },
		func(r *nbs.EnterpriseResult) { r.Observations[1].AsOfDate = nil },
		func(r *nbs.EnterpriseResult) { bad := "202601-202601"; r.Observations[0].YTDPeriod = &bad },
		func(r *nbs.EnterpriseResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := cnEnterpriseSample()
		mutate(r)
		d, err := CNEnterprise(r, "202601", "202602")
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = CNEnterprise(nil, "202601", "202602")
	require.Error(t, err)
}
