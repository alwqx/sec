package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

var pbcFetched = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

func financingItem() pbc.FinancingObservation {
	return pbc.FinancingObservation{Period: "202609", YTDPeriod: "202601-202609", Stock: value(4681700), StockYoY: value(7.1), Flow: value(35476), YTDFlow: value(274589), Unit: "亿元", YTDMethod: "sum-of-published-monthly-flows", FlowSource: "flow", StockSource: "stock", FlowFetchedAt: pbcFetched, StockFetchedAt: pbcFetched.Add(time.Hour), Components: []pbc.FinancingComponent{{Key: "corporate-bonds", Name: "企业债券", Stock: value(100), StockYoY: value(1), Flow: value(-393), YTDFlow: nil}}}
}

func TestFinancingSeparatesStockFlowAndDerived(t *testing.T) {
	d, err := Financing([]pbc.FinancingObservation{financingItem()}, "202609", "202609")
	require.NoError(t, err)
	require.Len(t, d.Series, 8)
	require.Len(t, d.Observations, 8)
	series := seriesByID(d)
	require.Equal(t, "month-end", series["cn.financing.total.since-202301.level"].PeriodBasis)
	require.Equal(t, "CNY_100_million", series["cn.financing.total.since-202301.level"].Unit)
	require.Equal(t, "official", series["cn.financing.total.since-202301.yoy_percent"].ValueOrigin)
	require.Equal(t, "month", series["cn.financing.total.since-202301.monthly_flow"].PeriodBasis)
	ytd := series["cn.financing.total.since-202301.ytd_flow"]
	require.Equal(t, "derived", ytd.ValueOrigin, "累计是计算值")
	require.Equal(t, "sum-of-published-monthly-flows", ytd.Methodology)
	require.Equal(t, "year-to-date", ytd.PeriodBasis)
	o := observationOf(t, d, ytd.ID)
	require.Equal(t, 274589.0, *o.Value)
	require.Equal(t, "202601", o.PeriodBegin)
	require.Equal(t, "flow", *o.Source)
	require.Equal(t, "stock", *observationOf(t, d, "cn.financing.total.since-202301.level").Source, "存量和增量来源分开")
	require.Equal(t, -393.0, *observationOf(t, d, "cn.financing.corporate-bonds.since-201909.monthly_flow").Value, "负增量保留")
	missing := observationOf(t, d, "cn.financing.corporate-bonds.since-201909.ytd_flow")
	require.Equal(t, "window_incomplete", *missing.MissingReason)
	require.Contains(t, missing.Note, "累计值不计算")
	require.Empty(t, observationOf(t, d, "cn.financing.corporate-bonds.since-201909.level").Note)
	require.False(t, *series["cn.financing.total.since-202301.level"].ComponentsAdditive)
	require.Nil(t, series["cn.financing.total.since-202301.yoy_percent"].ComponentsAdditive)

	item := financingItem()
	item.Components[0].Flow = nil
	d, err = Financing([]pbc.FinancingObservation{item}, "202609", "202609")
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *observationOf(t, d, "cn.financing.corporate-bonds.since-201909.ytd_flow").MissingReason, "当月缺失不算窗口不完整")

	for _, test := range []struct{ key, period, version string }{
		{"total", "201806", "before-201807"}, {"total", "201807", "since-201807"}, {"total", "201808", "since-201807"}, {"total", "201809", "since-201809"},
		{"total", "201909", "since-201909"}, {"total", "201912", "since-201912"}, {"total", "202212", "since-201912"}, {"total", "202301", "since-202301"},
		{"corporate-bonds", "201908", "before-201909"}, {"corporate-bonds", "202609", "since-201909"},
		{"rmb-loans", "202212", "before-202301"}, {"rmb-loans", "202301", "since-202301"}, {"loan-writeoffs", "202301", "since-202301"},
	} {
		require.Equal(t, test.version, pbcScopeVersion(financingScopeBreaks[test.key], test.period), test.key+test.period)
	}
	require.Nil(t, financingScopeBreaks["trust-loans"], "范围未变的分项不带版本")
	stable := financingItem()
	stable.Components[0].Key = "trust-loans"
	d, err = Financing([]pbc.FinancingObservation{stable}, "202609", "202609")
	require.NoError(t, err)
	require.Contains(t, seriesByID(d), "cn.financing.trust-loans.level")

	empty, err := Financing(nil, "202609", "202609")
	require.NoError(t, err)
	require.Empty(t, empty.Series)
	for _, mutate := range []func(*pbc.FinancingObservation){
		func(i *pbc.FinancingObservation) { i.Unit = "万亿元" },
		func(i *pbc.FinancingObservation) { i.YTDMethod = "official" },
		func(i *pbc.FinancingObservation) { i.FlowSource = "" },
		func(i *pbc.FinancingObservation) { i.StockFetchedAt = time.Time{} },
		func(i *pbc.FinancingObservation) { i.Components[0].Key = "" },
		func(i *pbc.FinancingObservation) { i.Components[0].Key = "total" },
		func(i *pbc.FinancingObservation) { i.YTDPeriod = "202601-202608" },
		func(i *pbc.FinancingObservation) { i.Period = "2026-09" },
	} {
		item := financingItem()
		mutate(&item)
		d, err := Financing([]pbc.FinancingObservation{item}, "202609", "202609")
		require.Error(t, err)
		require.Nil(t, d)
	}
}

func loanItem() pbc.LoanObservation {
	fetched := pbcFetched
	return pbc.LoanObservation{Period: "202609", YTDPeriod: "202601-202609", Balance: value(2834357.83), ReportedBalance: value(2834400), YoY: value(4.8), MonthlyFlow: value(10800), YTDFlow: value(115200), MonthlyFlowMethod: "difference-of-rounded-ytd-releases", MonthlyFlowApproximate: true, PreviousYTDFlow: value(104400), Unit: "亿元", BalanceSource: "table", BalanceFetchedAt: pbcFetched, ReportSource: "report", ReportPublishedDate: "2026-10-14", ReportFetchedAt: &fetched, PreviousReportSource: "previous", Components: []pbc.LoanComponent{{Key: "households", Name: "住户贷款", Balance: value(100), MonthlyFlow: value(5), MonthlyFlowMethod: "published-monthly"}, {Key: "enterprises", Name: "企业贷款", Balance: value(0), MonthlyFlowMethod: "unavailable"}}}
}

func TestLoansSeparateBalancesAndApproximateFlows(t *testing.T) {
	d, err := Loans([]pbc.LoanObservation{loanItem()}, "202609", "202609")
	require.NoError(t, err)
	series := seriesByID(d)
	require.Equal(t, 2834357.83, *observationOf(t, d, "cn.loans.total.since-202301.level").Value, "精确余额不被舍入值覆盖")
	require.Equal(t, 2834400.0, *observationOf(t, d, "cn.loans.total.since-202301.reported_level").Value)
	require.Equal(t, "table", *observationOf(t, d, "cn.loans.total.since-202301.level").Source)
	require.Nil(t, observationOf(t, d, "cn.loans.total.since-202301.level").PublishedDate)
	require.Equal(t, "2026-10-14", *observationOf(t, d, "cn.loans.total.since-202301.yoy_percent").PublishedDate)
	require.NotContains(t, series, "cn.loans.total.since-202301.monthly_flow", "相减值不进入官方序列")
	derived := series["cn.loans.total.since-202301.monthly_flow_derived"]
	require.Equal(t, "derived", derived.ValueOrigin)
	o := observationOf(t, d, derived.ID)
	require.Equal(t, 10800.0, *o.Value)
	require.Contains(t, o.Note, "近似值")
	require.Contains(t, o.Note, "previous")
	require.Equal(t, "202608", o.Inputs[0].Period)
	require.Equal(t, 104400.0, *o.Inputs[0].Value, "保留上期累计，可核对相减")
	require.Equal(t, 115200.0, *o.Inputs[1].Value)
	require.False(t, *series["cn.loans.total.since-202301.level"].ComponentsAdditive)
	require.False(t, *series["cn.loans.households.since-202301.ytd_flow"].ComponentsAdditive)
	require.Nil(t, series["cn.loans.total.since-202301.yoy_percent"].ComponentsAdditive)

	early := loanItem()
	early.Period, early.YTDPeriod = "202212", "202201-202212"
	d2, err := Loans([]pbc.LoanObservation{early, loanItem()}, "202212", "202609")
	require.NoError(t, err)
	require.Contains(t, seriesByID(d2), "cn.loans.total.before-202301.level", "2023年纳入新机构前后不共用序列")
	require.Contains(t, seriesByID(d2), "cn.loans.total.since-202301.level")
	require.Equal(t, 115200.0, *observationOf(t, d, "cn.loans.total.since-202301.ytd_flow").Value)
	require.Equal(t, "official", series["cn.loans.households.since-202301.monthly_flow"].ValueOrigin)
	require.Equal(t, 5.0, *observationOf(t, d, "cn.loans.households.since-202301.monthly_flow").Value)
	unavailable := observationOf(t, d, "cn.loans.enterprises.since-202301.monthly_flow")
	require.Equal(t, "calculation_unavailable", *unavailable.MissingReason)
	require.Contains(t, unavailable.Note, "不以余额差额替代")
	require.Equal(t, 0.0, *observationOf(t, d, "cn.loans.enterprises.since-202301.level").Value)
	require.NotContains(t, series, "cn.loans.households.since-202301.yoy_percent")

	item := loanItem()
	item.ReportSource, item.ReportPublishedDate, item.ReportFetchedAt, item.ReportedBalance, item.YoY, item.MonthlyFlow, item.YTDFlow = "", "", nil, nil, nil, nil, nil
	item.MonthlyFlowMethod, item.MonthlyFlowApproximate, item.Components = "unavailable", false, nil
	d, err = Loans([]pbc.LoanObservation{item}, "202609", "202609")
	require.NoError(t, err)
	require.Equal(t, 2834357.83, *observationOf(t, d, "cn.loans.total.since-202301.level").Value, "报告缺失时余额仍可用")
	for _, id := range []string{"reported_level", "yoy_percent", "monthly_flow", "ytd_flow"} {
		o := observationOf(t, d, "cn.loans.total.since-202301."+id)
		require.Equal(t, "report_not_found", *o.MissingReason, id)
		require.Contains(t, o.Note, "未检索到")
		require.Nil(t, o.Source)
	}

	for _, mutate := range []func(*pbc.LoanObservation){
		func(i *pbc.LoanObservation) { i.Unit = "元" },
		func(i *pbc.LoanObservation) { i.BalanceSource = "" },
		func(i *pbc.LoanObservation) { i.BalanceFetchedAt = time.Time{} },
		func(i *pbc.LoanObservation) { i.ReportFetchedAt = nil },
		func(i *pbc.LoanObservation) { i.MonthlyFlowMethod = "unknown" },
		func(i *pbc.LoanObservation) { i.MonthlyFlowApproximate = false },
		func(i *pbc.LoanObservation) { i.MonthlyFlow = nil },
		func(i *pbc.LoanObservation) { i.PreviousYTDFlow = nil },
		func(i *pbc.LoanObservation) { i.PreviousYTDFlow = value(1) },
		func(i *pbc.LoanObservation) { i.YTDFlow = nil },
		func(i *pbc.LoanObservation) { i.Components[1].MonthlyFlow = value(1) },
		func(i *pbc.LoanObservation) { i.Components[0].Key = "total" },
		func(i *pbc.LoanObservation) { i.Components[0].Name = "" },
	} {
		item := loanItem()
		mutate(&item)
		d, err := Loans([]pbc.LoanObservation{item}, "202609", "202609")
		require.Error(t, err)
		require.Nil(t, d)
	}
}

func depositSample() *pbc.DepositResult {
	fetched := pbcFetched
	balance := func(period string, n float64) pbc.DepositBalance {
		return pbc.DepositBalance{Period: period, Balance: value(n), Demand: value(n / 2), TimeAndOther: value(n / 2), RawValues: map[string]string{"balance": "raw-balance", "demand": "raw-demand", "time-and-other": "raw-time"}, Source: "table", FetchedAt: &fetched}
	}
	return &pbc.DepositResult{Frequency: "monthly", Unit: "亿元", Currency: "CNY", Measurement: "month-end-stock", Population: "全国金融机构住户人民币存款", Begin: "202609", End: "202609", Observations: []pbc.DepositObservation{{DepositBalance: balance("202609", 1752979), YearAgoBase: balance("202509", 1639847), ComputedYoY: value(6.9), MonthlyFlow: value(24000), YTDFlow: value(93900), MonthlyFlowMethod: "difference-of-rounded-ytd-releases", MonthlyFlowApproximate: true, PreviousYTDFlow: value(69900), ReportSource: "report", ReportPublishedDate: "2026-10-14", ReportFetchedAt: &fetched, PreviousReportSource: "previous"}}}
}

func TestDepositsSeparateComputedAndOfficial(t *testing.T) {
	d, err := Deposits(depositSample())
	require.NoError(t, err)
	series := seriesByID(d)
	level := observationOf(t, d, "cn.deposits.households.since-202301.level")
	require.Equal(t, 1752979.0, *level.Value)
	require.Equal(t, "raw-balance", *level.RawValue)
	require.Equal(t, "raw-demand", *observationOf(t, d, "cn.deposits.demand.since-202301.level").RawValue)
	require.Equal(t, "raw-time", *observationOf(t, d, "cn.deposits.time-and-other.since-202301.level").RawValue)
	yoy := series["cn.deposits.households.since-202301.yoy_percent"]
	require.Equal(t, "derived", yoy.ValueOrigin)
	require.Equal(t, "level_ratio_not_comparable_scope", yoy.ChangeMethod)
	o := observationOf(t, d, yoy.ID)
	require.Equal(t, "202509", o.Base.Period)
	require.Equal(t, "raw-balance", *o.Base.RawValue)
	require.Nil(t, o.RawValue)
	reported := observationOf(t, d, "cn.deposits.households.since-202301.reported_yoy_percent")
	require.Equal(t, "source_value_missing", *reported.MissingReason)
	require.Contains(t, reported.Note, "不套用")
	require.Equal(t, 24000.0, *observationOf(t, d, "cn.deposits.households.since-202301.monthly_flow_derived").Value)
	ytd := observationOf(t, d, "cn.deposits.households.since-202301.ytd_flow")
	require.Equal(t, 93900.0, *ytd.Value)
	require.Equal(t, "202601", ytd.PeriodBegin)

	for mutate, reason := range map[*func(*pbc.DepositObservation)]string{
		ptrDeposit(func(i *pbc.DepositObservation) { i.Balance, i.ComputedYoY = nil, nil }):                  "current_value_missing",
		ptrDeposit(func(i *pbc.DepositObservation) { i.YearAgoBase.Balance, i.ComputedYoY = nil, nil }):      "baseline_missing",
		ptrDeposit(func(i *pbc.DepositObservation) { i.YearAgoBase.Balance, i.ComputedYoY = value(0), nil }): "zero_baseline",
	} {
		r := depositSample()
		(*mutate)(&r.Observations[0])
		d, err := Deposits(r)
		require.NoError(t, err)
		require.Equal(t, reason, *observationOf(t, d, "cn.deposits.households.since-202301.yoy_percent").MissingReason)
	}

	cross := depositSample()
	cross.Begin, cross.End = "202306", "202306"
	cross.Observations[0].Period, cross.Observations[0].YearAgoBase.Period = "202306", "202206"
	cross.Observations[0].MonthlyFlow, cross.Observations[0].YTDFlow, cross.Observations[0].PreviousYTDFlow, cross.Observations[0].MonthlyFlowMethod, cross.Observations[0].MonthlyFlowApproximate = nil, nil, nil, "unavailable", false
	d, err = Deposits(cross)
	require.NoError(t, err)
	crossed := observationOf(t, d, "cn.deposits.households.since-202301.yoy_percent")
	require.Nil(t, crossed.Value, "基期在2023年前不跨统计范围")
	require.Equal(t, "methodology_break", *crossed.MissingReason)
	require.Equal(t, "202206", crossed.Base.Period)
	var original pbc.DepositResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, 6.9, *original.Observations[0].ComputedYoY, "provider计算值保留在original")

	before := depositSample()
	before.Begin, before.End = "202212", "202212"
	before.Observations[0].Period, before.Observations[0].YearAgoBase.Period = "202212", "202112"
	before.Observations[0].MonthlyFlow, before.Observations[0].YTDFlow, before.Observations[0].PreviousYTDFlow, before.Observations[0].MonthlyFlowMethod, before.Observations[0].MonthlyFlowApproximate = nil, nil, nil, "unavailable", false
	d, err = Deposits(before)
	require.NoError(t, err)
	require.Equal(t, 6.9, *observationOf(t, d, "cn.deposits.households.before-202301.yoy_percent").Value, "同一口径内保留")

	r := depositSample()
	item := &r.Observations[0]
	item.ReportSource, item.ReportPublishedDate, item.ReportFetchedAt, item.MonthlyFlow, item.YTDFlow, item.MonthlyFlowMethod, item.MonthlyFlowApproximate = "", "", nil, nil, nil, "unavailable", false
	item.PreviousYTDFlow = nil
	d, err = Deposits(r)
	require.NoError(t, err)
	require.Equal(t, "report_not_found", *observationOf(t, d, "cn.deposits.households.since-202301.monthly_flow").MissingReason)
	require.Equal(t, "report_not_found", *observationOf(t, d, "cn.deposits.households.since-202301.reported_yoy_percent").MissingReason)
	require.Equal(t, "report_not_found", *observationOf(t, d, "cn.deposits.households.since-202301.ytd_flow").MissingReason)

	r = depositSample()
	r.Observations[0].DepositBalance = pbc.DepositBalance{Period: "202609", RawValues: map[string]string{}}
	r.Observations[0].ComputedYoY = nil
	d, err = Deposits(r)
	require.NoError(t, err)
	missing := observationOf(t, d, "cn.deposits.households.since-202301.level")
	require.Nil(t, missing.Source)
	require.Nil(t, missing.FetchedAt)

	for _, mutate := range []func(*pbc.DepositResult){
		func(r *pbc.DepositResult) { r.Frequency = "daily" },
		func(r *pbc.DepositResult) { r.Unit = "元" },
		func(r *pbc.DepositResult) { r.Currency = "USD" },
		func(r *pbc.DepositResult) { r.Measurement = "flow" },
		func(r *pbc.DepositResult) { r.Population = "" },
		func(r *pbc.DepositResult) { r.SeasonallyAdjusted = true },
		func(r *pbc.DepositResult) { r.Annualized = true },
		func(r *pbc.DepositResult) { r.Observations[0].Period = "2026-09" },
		func(r *pbc.DepositResult) { r.Observations[0].YearAgoBase.Period = "202508" },
		func(r *pbc.DepositResult) { r.Observations[0].Balance = nil },
		func(r *pbc.DepositResult) { r.Observations[0].YearAgoBase.Balance = value(0) },
		func(r *pbc.DepositResult) { r.Observations[0].MonthlyFlowMethod = "unknown" },
		func(r *pbc.DepositResult) { r.Observations[0].ReportFetchedAt = nil },
		func(r *pbc.DepositResult) { r.Observations[0].ReportSource = "" },
		func(r *pbc.DepositResult) { r.Observations[0].Source = "" },
		func(r *pbc.DepositResult) { r.Observations[0].FetchedAt = nil },
		func(r *pbc.DepositResult) { r.Observations[0].PreviousYTDFlow = nil },
		func(r *pbc.DepositResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := depositSample()
		mutate(r)
		d, err := Deposits(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = Deposits(nil)
	require.Error(t, err)
}

func ptrDeposit(f func(*pbc.DepositObservation)) *func(*pbc.DepositObservation) { return &f }
