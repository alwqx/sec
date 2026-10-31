package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/mof"
	"github.com/alwqx/sec/provider/mofcom"
	"github.com/stretchr/testify/require"
)

func cnFiscalSample() *mof.FiscalResult {
	fetched := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	return &mof.FiscalResult{Frequency: "monthly", Measurement: "year-to-date-flow", Unit: "亿元", GrowthUnit: "%", PriceBasis: "current-prices", Begin: "202201", End: "202202", Budget: "general", Observations: []mof.FiscalObservation{
		{Period: "202201", YTDPeriod: "202201-202201", Status: "missing-release", Note: "年初通常发布1—2月合并数", Metrics: []mof.FiscalMetric{{Key: "general-revenue", Name: "一般公共预算收入", Budget: "general"}}},
		{Period: "202202", YTDPeriod: "202201-202202", Status: "complete", Source: "url", PublishedDate: "2022-03-20", FetchedAt: &fetched, Metrics: []mof.FiscalMetric{{Key: "general-revenue", Name: "一般公共预算收入", Budget: "general", YTDAmount: value(100), YTDYoY: value(10.5), ComparableYTDYoY: value(9), TaxRefundAdjustedYTDYoY: value(8)}}},
	}}
}

func TestCNFiscalKeepsCumulativeAndAlternateGrowth(t *testing.T) {
	d, err := CNFiscal(cnFiscalSample())
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 6)
	series := seriesByID(d)
	level := series["cn.fiscal.general-revenue.ytd_level"]
	require.Equal(t, "CNY_100_million", level.Unit)
	require.Equal(t, "year-to-date", level.PeriodBasis)
	require.Equal(t, "nominal", level.PriceBasis)
	require.Equal(t, "一般公共预算收入", level.Definition)
	missing := d.Observations[0]
	require.Equal(t, "report_not_found", *missing.MissingReason)
	require.Contains(t, missing.Note, "合并")
	require.Nil(t, missing.Source)
	require.Nil(t, missing.FetchedAt)
	available := d.Observations[2]
	require.Equal(t, 100.0, *available.Value)
	require.Equal(t, "202201", available.PeriodBegin)
	require.Equal(t, "2022-03-20", *available.PublishedDate)
	require.Equal(t, "url", *available.Source)
	require.Equal(t, 9.0, *observationOf(t, d, "cn.fiscal.general-revenue.ytd_comparable_yoy_percent").Value)
	require.Equal(t, 8.0, *observationOf(t, d, "cn.fiscal.general-revenue.ytd_tax_refund_adjusted_yoy_percent").Value)

	r := cnFiscalSample()
	metric := &r.Observations[1].Metrics[0]
	metric.YTDYoY, metric.ComparableYTDYoY, metric.TaxRefundAdjustedYTDYoY, metric.Note = nil, nil, nil, "官方仅文字表述持平"
	r.Observations[1].Status = "missing-values"
	d, err = CNFiscal(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 2, "官方没提到的另列同比不算缺口")
	flat := d.Observations[3]
	require.Equal(t, "source_value_missing", *flat.MissingReason)
	require.Contains(t, flat.Note, "持平")
	require.Empty(t, d.Observations[2].Note)

	for _, mutate := range []func(*mof.FiscalResult){
		func(r *mof.FiscalResult) { r.Frequency = "annual" },
		func(r *mof.FiscalResult) { r.Measurement = "flow" },
		func(r *mof.FiscalResult) { r.Unit = "元" },
		func(r *mof.FiscalResult) { r.PriceBasis = "real" },
		func(r *mof.FiscalResult) { r.SeasonallyAdjusted = true },
		func(r *mof.FiscalResult) { r.Annualized = true },
		func(r *mof.FiscalResult) { r.Observations[1].Source = "" },
		func(r *mof.FiscalResult) { r.Observations[1].FetchedAt = nil },
		func(r *mof.FiscalResult) { r.Observations[0].Source = "url" },
		func(r *mof.FiscalResult) { r.Observations[0].Metrics[0].YTDAmount = value(1) },
		func(r *mof.FiscalResult) { r.Observations[1].Metrics[0].Key = "" },
		func(r *mof.FiscalResult) { r.Observations[1].Metrics[0].Budget = "other" },
		func(r *mof.FiscalResult) { r.Observations[1].YTDPeriod = "202201-202201" },
		func(r *mof.FiscalResult) { r.Observations = append(r.Observations, r.Observations[1]) },
	} {
		r := cnFiscalSample()
		mutate(r)
		d, err := CNFiscal(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = CNFiscal(nil)
	require.Error(t, err)
}

func cnTradeSample() *mofcom.TradeResult {
	fetched := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	amount := func(n float64) mofcom.TradeAmount {
		return mofcom.TradeAmount{Monthly: value(n), YearToDate: value(n * 8), MonthlyYoY: value(1), YearToDateYoY: value(2)}
	}
	cny := &mofcom.TradeCurrencyValues{Unit: "亿元人民币", Total: amount(300), Exports: amount(200), Imports: amount(100), Balance: amount(-5), Source: "fdi", FetchedAt: fetched, RawValues: map[string]string{"exports_monthly": "24,099.4", "exports_ytd": "null"}}
	cny.Exports.MonthlyMoM = value(8.6)
	usd := &mofcom.TradeCurrencyValues{Unit: "亿美元", Total: amount(30), Exports: amount(20), Imports: amount(10), Balance: amount(10), Source: "usd", FetchedAt: fetched, RawValues: map[string]string{"export_value": "3356.27", "imexgap_per": "null"}}
	return &mofcom.TradeResult{Measurement: "customs-goods-trade", Frequency: "monthly", Begin: "202608", End: "202608", PartnerCurrency: "CNY", Observations: []mofcom.TradeObservation{{Period: "202608", CNY: cny, USD: usd}}, Partners: []mofcom.TradePartner{
		{Name: "总值", Period: "202608", Currency: "CNY", Unit: "亿元人民币", TotalMonthly: value(300), TotalYTD: value(2400), TotalYTDYoY: value(2), PublishedDate: "2026-09-18", Source: "fdi", FetchedAt: fetched, RawValues: map[string]string{"total_monthly": "300"}},
		{Name: "欧洲联盟", Period: "202608", Currency: "CNY", Unit: "亿元人民币", ExportsMonthly: value(50), Source: "fdi", FetchedAt: fetched, RawValues: map[string]string{}},
	}}
}

func TestCNTradeSeparatesCurrenciesAndPartners(t *testing.T) {
	d, err := CNTrade(cnTradeSample())
	require.NoError(t, err)
	series := seriesByID(d)
	exports := series["cn.goods-trade.cny.exports.level"]
	require.Equal(t, "CNY_100_million", exports.Unit)
	require.Equal(t, "goods-trade.exports", exports.Indicator)
	require.Equal(t, "USD_100_million", series["cn.goods-trade.usd.exports.level"].Unit)
	require.Equal(t, "year-to-date", series["cn.goods-trade.cny.exports.ytd_level"].PeriodBasis)
	require.Contains(t, series, "cn.goods-trade.cny.exports.mom_percent")
	require.NotContains(t, series, "cn.goods-trade.usd.exports.mom_percent", "美元来源没有环比")
	o := observationOf(t, d, exports.ID)
	require.Equal(t, 200.0, *o.Value)
	require.Equal(t, "24,099.4", *o.RawValue)
	require.Equal(t, "fdi", *o.Source)
	require.Nil(t, observationOf(t, d, "cn.goods-trade.cny.exports.ytd_level").RawValue, "null标记不写成字符串")
	require.Equal(t, "3356.27", *observationOf(t, d, "cn.goods-trade.usd.exports.level").RawValue)
	require.Equal(t, "usd", *observationOf(t, d, "cn.goods-trade.usd.exports.level").Source)
	require.Equal(t, -5.0, *observationOf(t, d, "cn.goods-trade.cny.balance.level").Value)
	require.Equal(t, 8.6, *observationOf(t, d, "cn.goods-trade.cny.exports.mom_percent").Value)

	partner := series["cn.goods-trade.partner.cny.总值.total.ytd_level"]
	require.Equal(t, "goods-trade.partner.total", partner.Indicator)
	require.False(t, *partner.ComponentsAdditive)
	require.Equal(t, "总值", partner.Definition)
	require.Nil(t, series["cn.goods-trade.partner.cny.总值.total.ytd_yoy_percent"].ComponentsAdditive)
	require.Equal(t, "2026-09-18", *observationOf(t, d, partner.ID).PublishedDate)
	require.Equal(t, 300.0, *observationOf(t, d, "cn.goods-trade.partner.cny.总值.total.level").Value)
	require.Equal(t, 50.0, *observationOf(t, d, "cn.goods-trade.partner.cny.欧洲联盟.exports.level").Value)

	r := cnTradeSample()
	r.Observations[0].USD = nil
	r.Partners = []mofcom.TradePartner{{Name: "美国", Period: "202608", Currency: "USD", Unit: "亿美元", TotalYTD: value(10), Source: "partners", FetchedAt: nbsFetched, RawValues: map[string]string{"total_lj_value": "10"}}}
	d, err = CNTrade(r)
	require.NoError(t, err)
	missing := observationOf(t, d, "cn.goods-trade.usd.total.level")
	require.Equal(t, "report_not_found", *missing.MissingReason)
	require.Nil(t, missing.Source)
	require.Equal(t, 200.0, *observationOf(t, d, "cn.goods-trade.cny.exports.level").Value, "另一币种不受影响")
	series = seriesByID(d)
	require.Equal(t, "10", *observationOf(t, d, "cn.goods-trade.partner.usd.美国.total.ytd_level").RawValue)
	require.NotContains(t, series, "cn.goods-trade.partner.usd.美国.total.level", "美元伙伴只有累计，不差分")

	for _, mutate := range []func(*mofcom.TradeResult){
		func(r *mofcom.TradeResult) { r.Measurement = "services" },
		func(r *mofcom.TradeResult) { r.Frequency = "annual" },
		func(r *mofcom.TradeResult) { r.Observations[0].CNY.Unit = "亿美元" },
		func(r *mofcom.TradeResult) { r.Observations[0].USD.Source = "" },
		func(r *mofcom.TradeResult) { r.Observations[0].USD.FetchedAt = time.Time{} },
		func(r *mofcom.TradeResult) { r.Observations = append(r.Observations, r.Observations[0]) },
		func(r *mofcom.TradeResult) { r.Partners[0].Currency = "EUR" },
		func(r *mofcom.TradeResult) { r.Partners[0].Name = " " },
		func(r *mofcom.TradeResult) { r.Partners[0].Source = "" },
		func(r *mofcom.TradeResult) { r.Partners[0].Period = "202607" },
		func(r *mofcom.TradeResult) { r.Partners = append(r.Partners, r.Partners[0]) },
	} {
		r := cnTradeSample()
		mutate(r)
		d, err := CNTrade(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = CNTrade(nil)
	require.Error(t, err)
}

func cnServicesSample() *mofcom.ServicesResult {
	fetched := nbsFetched
	return &mofcom.ServicesResult{Indicator: "services_trade", Frequency: "monthly", PeriodBasis: "monthly", PriceBasis: "nominal", Begin: "202601", End: "202608", Observations: []mofcom.ServicesObservation{
		{Period: "202601"},
		{Period: "202608", Source: "url", FetchedAt: &fetched,
			CNY:       &mofcom.ServicesValues{Currency: "CNY", Unit: "亿元人民币", Total: value(100), Exports: value(40), Imports: value(61), Balance: value(-20)},
			USD:       &mofcom.ServicesValues{Currency: "USD", Unit: "亿美元", Total: nil, Exports: value(361.4), Imports: value(579.8), Balance: value(-218.5)},
			RawValues: map[string]json.RawMessage{"exportUSD": json.RawMessage(`361.4`), "importAndExportUSD": json.RawMessage(`null`), "differenceRMB": json.RawMessage(`"-20"`)}},
	}}
}

func TestCNServicesKeepsOfficialFields(t *testing.T) {
	d, err := CNServices(cnServicesSample())
	require.NoError(t, err)
	require.Len(t, d.Series, 8)
	require.Len(t, d.Observations, 16)
	series := seriesByID(d)
	require.Equal(t, "USD_100_million", series["cn.services-trade.usd.balance.level"].Unit)
	require.Equal(t, "CNY_100_million", series["cn.services-trade.cny.total.level"].Unit)
	require.Equal(t, "month", series["cn.services-trade.cny.total.level"].PeriodBasis)
	gap := d.Observations[0]
	require.Equal(t, "report_not_found", *gap.MissingReason)
	require.Contains(t, gap.Note, "不拆分")
	require.Nil(t, gap.FetchedAt)
	august := func(id string) Observation {
		for _, o := range d.Observations {
			if o.SeriesID == id && o.Period == "202608" {
				return o
			}
		}
		require.FailNow(t, id)
		return Observation{}
	}
	require.Equal(t, -218.5, *august("cn.services-trade.usd.balance.level").Value, "差额用官方值，不用出口减进口")
	require.Equal(t, "361.4", *august("cn.services-trade.usd.exports.level").RawValue)
	require.Equal(t, "-20", *august("cn.services-trade.cny.balance.level").RawValue)
	total := august("cn.services-trade.usd.total.level")
	require.Nil(t, total.RawValue)
	require.Equal(t, "source_value_missing", *total.MissingReason)

	for _, mutate := range []func(*mofcom.ServicesResult){
		func(r *mofcom.ServicesResult) { r.Indicator = "goods" },
		func(r *mofcom.ServicesResult) { r.Frequency = "annual" },
		func(r *mofcom.ServicesResult) { r.PeriodBasis = "ytd" },
		func(r *mofcom.ServicesResult) { r.PriceBasis = "real" },
		func(r *mofcom.ServicesResult) { r.SeasonallyAdjusted = true },
		func(r *mofcom.ServicesResult) { r.LevelAnnualized = true },
		func(r *mofcom.ServicesResult) { r.Observations[1].Source = "" },
		func(r *mofcom.ServicesResult) { r.Observations[1].FetchedAt = nil },
		func(r *mofcom.ServicesResult) { r.Observations[1].CNY.Unit = "亿美元" },
		func(r *mofcom.ServicesResult) { r.Observations[1].USD.Currency = "CNY" },
		func(r *mofcom.ServicesResult) { r.Observations = append(r.Observations, r.Observations[1]) },
	} {
		r := cnServicesSample()
		mutate(r)
		d, err := CNServices(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = CNServices(nil)
	require.Error(t, err)
}
