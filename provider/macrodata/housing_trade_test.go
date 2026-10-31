package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
)

func payrollSample() *bls.PayrollResult {
	fetched := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return &bls.PayrollResult{SeriesID: "CES0000000001", Survey: "CES establishment survey", Frequency: "monthly", Unit: "千个岗位", SeasonallyAdjusted: true, Begin: "202601", End: "202601", Source: "source", Observations: []bls.PayrollObservation{{
		PayrollValue: bls.PayrollValue{Period: "202601", Employment: value(100), RawValue: "100", Preliminary: true, FetchedAt: fetched},
		Previous:     bls.PayrollValue{Period: "202512", Employment: value(100), RawValue: "100", FetchedAt: fetched},
		Change:       value(0),
	}}}
}

func housingSample(component string) *fred.HousingResult {
	r := &fred.HousingResult{Indicator: "housing_sales", Frequency: "monthly", Component: component, Begin: "202601", End: "202601", Source: "source", FetchedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	add := func(component, id, unit, indexBase, period, previous, yearAgo string, adjusted bool) {
		r.Series = append(r.Series, fred.HousingSeries{Component: component, SeriesID: id, Unit: unit, IndexBase: indexBase, SeasonallyAdjusted: adjusted, LevelAnnualized: adjusted, Definition: "定义" + component})
		r.Observations = append(r.Observations, fred.HousingObservation{HousingBase: fred.HousingBase{Period: period, Value: value(120), Raw: "120"}, Component: component, SeriesID: id, Unit: unit, YoY: value(50), PreviousPeriodGrowth: value(20), PreviousBase: fred.HousingBase{Period: previous, Value: value(100), Raw: "100"}, YoYBase: fred.HousingBase{Period: yearAgo, Value: value(80), Raw: "80"}})
	}
	if component == "house-price" {
		r.Indicator, r.Frequency, r.Begin, r.End = "house_price", "quarterly", "2026Q1", "2026Q1"
		add(component, "USSTHPI", "指数", "1980Q1=100", "2026Q1", "2025Q4", "2025Q1", false)
		return r
	}
	if component != "existing-sales" {
		add("new-sales", "HSN1F", "千套", "", "202601", "202512", "202501", true)
	}
	if component != "new-sales" {
		add("existing-sales", "EXHOSLUSM495S", "套", "", "202601", "202512", "202501", true)
	}
	return r
}

func tradeSample(real bool, component string) *fred.TradeResult {
	r := &fred.TradeResult{Indicator: "trade", Component: component, Frequency: "monthly", PriceBasis: "nominal", AccountingBasis: "BOP", Unit: "百万美元（当期美元）", SeasonallyAdjusted: true, Begin: "202601", End: "202601", Source: "source", FetchedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	period, previous, yearAgo := "202601", "202512", "202501"
	ids := map[string][3]string{"total": {"BOPTEXP", "BOPTIMP", "BOPGSTB"}, "goods": {"BOPGEXP", "BOPGIMP", "BOPGTB"}, "services": {"BOPSEXP", "BOPSIMP", "BOPSTB"}}[component]
	if real {
		year := 2017
		r.Indicator, r.Frequency, r.PriceBasis, r.AccountingBasis, r.Unit, r.LevelAnnualized, r.ReferenceYear, r.Begin, r.End = "real_trade", "quarterly", "real", "NIPA", "十亿美元（2017年链式美元）", true, &year, "2026Q1", "2026Q1"
		period, previous, yearAgo = "2026Q1", "2025Q4", "2025Q1"
		ids = map[string][3]string{"total": {"EXPGSC1", "IMPGSC1", "NETEXC"}, "goods": {"A253RX1Q020SBEA", "A255RX1Q020SBEA", ""}, "services": {"A646RX1Q020SBEA", "B656RX1Q020SBEA", ""}}[component]
	}
	for i, measure := range []string{"exports", "imports", "balance"} {
		if ids[i] != "" {
			r.Series = append(r.Series, fred.TradeSeries{Measure: measure, SeriesID: ids[i]})
		}
	}
	point := func(p string, exports, imports float64) fred.TradeValue {
		v := fred.TradeValue{Period: p, Exports: value(exports), Imports: value(imports), RawExports: "e", RawImports: "i"}
		if ids[2] != "" {
			v.Balance, v.RawBalance = value(exports-imports), "b"
		}
		return v
	}
	o := fred.TradeObservation{TradeValue: point(period, 120, 150), PreviousBase: point(previous, 100, 100), YoYBase: point(yearAgo, 80, 100), ExportsYoY: value(50), ImportsYoY: value(50), ExportsPreviousGrowth: value(20), ImportsPreviousGrowth: value(50)}
	if ids[2] != "" {
		o.BalanceYoYChange, o.BalancePreviousChange = value(-10), value(-30)
	}
	r.Observations = []fred.TradeObservation{o}
	return r
}

func TestPayrollsKeepsLevelAndNativeChange(t *testing.T) {
	r := payrollSample()
	d, err := Payrolls(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 2)
	require.Equal(t, "us.payrolls.level", d.Series[0].ID)
	require.Equal(t, "jobs_thousand", d.Series[0].Unit)
	require.Equal(t, "CES establishment survey", d.Series[0].Survey)
	require.Equal(t, "official", d.Series[0].ValueOrigin)
	require.Equal(t, "us.payrolls.monthly_change", d.Series[1].ID)
	require.Equal(t, "jobs_thousand", d.Series[1].Unit)
	require.Equal(t, "derived", d.Series[1].ValueOrigin)
	require.True(t, *d.Observations[0].Preliminary)
	require.Nil(t, d.Observations[1].Preliminary, "计算差值不设置初步标记")
	require.Equal(t, 0.0, *d.Observations[1].Value, "零新增不是缺失")
	require.Equal(t, "available", d.Observations[1].Status)
	require.Equal(t, "202512", d.Observations[1].Base.Period)
	require.Nil(t, d.Observations[0].PublishedAt)
	require.Nil(t, d.Observations[0].RevisionID)
	var original bls.PayrollResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, r.SeriesID, original.SeriesID)

	r.Observations[0].Previous.Employment, r.Observations[0].Change = nil, nil
	r.Observations[0].Note = "上月岗位水平缺失，不计算新增"
	d, err = Payrolls(r)
	require.NoError(t, err)
	require.Equal(t, "baseline_missing", *d.Observations[1].MissingReason)
	require.Empty(t, d.Observations[0].Note)
	require.Equal(t, r.Observations[0].Note, d.Observations[1].Note)

	batchTime := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	r = payrollSample()
	r.Batches = []bls.CPIBatch{{StartYear: 2016, EndYear: 2025, FetchedAt: batchTime.Add(-time.Hour)}, {StartYear: 2026, EndYear: 2026, FetchedAt: batchTime}}
	r.Observations[0].PayrollValue = bls.PayrollValue{Period: "202601"}
	r.Observations[0].Change = nil
	d, err = Payrolls(r)
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *d.Observations[0].MissingReason)
	require.Equal(t, batchTime, *d.Observations[0].FetchedAt, "请求成功时缺失行保留批次抓取时间")
	require.Nil(t, d.Observations[0].Preliminary, "未返回原点时不写初步标记")

	for _, mutate := range []func(*bls.PayrollResult){
		func(r *bls.PayrollResult) { r.SeriesID = "CES0500000003" },
		func(r *bls.PayrollResult) { r.Unit = "人" },
		func(r *bls.PayrollResult) { r.SeasonallyAdjusted = false },
		func(r *bls.PayrollResult) { r.Survey = "" },
		func(r *bls.PayrollResult) { r.Observations[0].Previous.Period = "202511" },
		func(r *bls.PayrollResult) { r.Observations[0].Previous.Employment = nil },
		func(r *bls.PayrollResult) { r.Observations[0].Employment = nil },
		func(r *bls.PayrollResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := payrollSample()
		mutate(r)
		d, err := Payrolls(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = Payrolls(nil)
	require.Error(t, err)
}

func TestHousingSeparatesUnitsAndScopes(t *testing.T) {
	d, err := Housing(housingSample("all"))
	require.NoError(t, err)
	require.Len(t, d.Series, 6)
	require.Len(t, d.Observations, 6)
	require.Equal(t, "us.housing.new-sales.level", d.Series[0].ID)
	require.Equal(t, "housing_units_thousand", d.Series[0].Unit)
	require.Equal(t, "us.housing.existing-sales.level", d.Series[3].ID)
	require.Equal(t, "housing_units", d.Series[3].Unit)
	require.Equal(t, "existing-sales", d.Series[3].Indicator)
	require.True(t, d.Series[3].Annualized)
	require.False(t, d.Series[4].Annualized)
	for i, o := range d.Observations {
		require.Equal(t, d.Series[i].ID, o.SeriesID, "第二条序列的观测不得指向第一条序列")
	}
	require.Equal(t, "EXHOSLUSM495S", d.Observations[3].SourceSeriesID)
	require.Equal(t, 120.0, *d.Observations[3].Value, "不做单位换算或除以12")

	for _, component := range []string{"new-sales", "existing-sales"} {
		d, err := Housing(housingSample(component))
		require.NoError(t, err)
		require.Len(t, d.Series, 3)
		require.Equal(t, component, d.Series[0].Indicator)
		require.Equal(t, "mom_percent", d.Series[2].Measure)
	}

	d, err = Housing(housingSample("house-price"))
	require.NoError(t, err)
	require.Equal(t, "us.house-price.index", d.Series[0].ID)
	require.Equal(t, "index", d.Series[0].Unit)
	require.Equal(t, "1980Q1=100", d.Series[0].IndexBase)
	require.Equal(t, "not-seasonally-adjusted", d.Series[0].SeasonalAdjustment)
	require.Equal(t, "price-index", d.Series[0].PriceBasis)
	require.False(t, d.Series[0].Annualized)
	require.Equal(t, "qoq_percent", d.Series[2].Measure)

	r := housingSample("house-price")
	r.Observations[0].Note = "严格上年同期基数缺失或为零；季环比未季调"
	r.Observations[0].YoYBase.Value, r.Observations[0].YoY = nil, nil
	d, err = Housing(r)
	require.NoError(t, err)
	require.Equal(t, "严格基期缺失", d.Observations[1].Note)
	require.Empty(t, d.Observations[2].Note)
	r.Observations[0].Value, r.Observations[0].Raw, r.Observations[0].PreviousPeriodGrowth = nil, "", nil
	r.Observations[0].Note = "当前快照未提供数值；季环比未季调"
	d, err = Housing(r)
	require.NoError(t, err)
	require.Equal(t, "当前快照未提供数值", d.Observations[0].Note)

	r = housingSample("new-sales")
	r.Observations[0].PreviousBase.Value, r.Observations[0].PreviousPeriodGrowth = value(0), nil
	d, err = Housing(r)
	require.NoError(t, err)
	require.Equal(t, "zero_baseline", *d.Observations[2].MissingReason)

	r = housingSample("existing-sales")
	r.Observations[0].Value, r.Observations[0].Raw, r.Observations[0].YoY, r.Observations[0].PreviousPeriodGrowth = nil, "", nil, nil
	r.Observations[0].Note = "超出当前成屋公开快照有效覆盖"
	d, err = Housing(r)
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *d.Observations[0].MissingReason)
	require.Contains(t, d.Observations[0].Note, "超出当前成屋")
	require.Contains(t, d.Observations[1].Note, "超出当前成屋", "当期缺失导致的增速缺失沿用覆盖说明")

	for _, mutate := range []func(*fred.HousingResult){
		func(r *fred.HousingResult) { r.Component = "unknown" },
		func(r *fred.HousingResult) { r.Frequency = "quarterly" },
		func(r *fred.HousingResult) { r.GrowthAnnualized = true },
		func(r *fred.HousingResult) { r.Series = r.Series[:1] },
		func(r *fred.HousingResult) { r.Series[0].SeriesID = "USSTHPI" },
		func(r *fred.HousingResult) { r.Series[1].Unit = "千套" },
		func(r *fred.HousingResult) { r.Series[0].LevelAnnualized = false },
		func(r *fred.HousingResult) { r.Series[0].Definition = "" },
		func(r *fred.HousingResult) { r.Observations[0].SeriesID = "EXHOSLUSM495S" },
		func(r *fred.HousingResult) { r.Observations[0].Component = "other" },
		func(r *fred.HousingResult) { r.Observations[0].YoYBase.Period = "202412" },
		func(r *fred.HousingResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := housingSample("all")
		mutate(r)
		d, err := Housing(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = Housing(nil)
	require.Error(t, err)
}

func TestTradeSeparatesMeasuresAndBases(t *testing.T) {
	d, err := Trade(tradeSample(false, "goods"))
	require.NoError(t, err)
	require.Len(t, d.Series, 9)
	require.Len(t, d.Observations, 9)
	for i, o := range d.Observations {
		require.Equal(t, d.Series[i].ID, o.SeriesID)
	}
	require.Equal(t, "us.exports.goods.level", d.Series[0].ID)
	require.Equal(t, "USD_million", d.Series[0].Unit)
	require.Equal(t, "nominal", d.Series[0].PriceBasis)
	require.False(t, d.Series[0].Annualized)
	require.Equal(t, "mom_percent", d.Series[2].Measure)
	require.Equal(t, "level_ratio", d.Series[2].ChangeMethod)
	require.Equal(t, "BOPGEXP", d.Observations[0].SourceSeriesID)
	require.Equal(t, "us.trade-balance.goods.level", d.Series[6].ID)
	require.Equal(t, "BOPGTB", d.Observations[6].SourceSeriesID)
	require.Equal(t, -30.0, *d.Observations[6].Value, "负差额是有效官方值")
	require.Equal(t, "yoy_change", d.Series[7].Measure)
	require.Equal(t, "mom_change", d.Series[8].Measure)
	require.Equal(t, "USD_million", d.Series[8].Unit, "差额变化保留原单位，不计算百分比")
	require.Equal(t, "current_level - base_level", *d.Series[8].Formula)
	require.Equal(t, "difference", d.Series[8].ChangeMethod)

	d, err = Trade(tradeSample(true, "total"))
	require.NoError(t, err)
	require.Len(t, d.Series, 9)
	require.Equal(t, "us.real-net-exports.total.level", d.Series[6].ID)
	require.Equal(t, "real-chained", d.Series[6].PriceBasis)
	require.Equal(t, 2017, *d.Series[6].ReferenceYear)
	require.False(t, *d.Series[6].ComponentsAdditive)
	require.Nil(t, d.Series[7].ComponentsAdditive)
	require.True(t, d.Series[0].Annualized)
	require.False(t, d.Series[1].Annualized)
	require.Equal(t, "qoq_change", d.Series[8].Measure)
	require.True(t, d.Series[7].Annualized, "折年水平之差仍是折年口径")
	require.True(t, d.Series[8].Annualized)

	for _, component := range []string{"goods", "services"} {
		d, err := Trade(tradeSample(true, component))
		require.NoError(t, err)
		require.Len(t, d.Series, 6, "未采集的实际分项净额不生成序列")
		for _, s := range d.Series {
			require.NotEqual(t, "real-net-exports", s.Indicator)
		}
	}

	r := tradeSample(false, "total")
	r.Observations[0].PreviousBase.Balance, r.Observations[0].BalancePreviousChange = value(0), value(-30)
	d, err = Trade(r)
	require.NoError(t, err)
	require.Equal(t, "available", d.Observations[8].Status, "差额零基期仍可计算")

	r = tradeSample(false, "total")
	r.Observations[0].PreviousBase.Balance, r.Observations[0].BalancePreviousChange = nil, nil
	d, err = Trade(r)
	require.NoError(t, err)
	require.Equal(t, "baseline_missing", *d.Observations[8].MissingReason)
	require.Equal(t, "严格基期缺失", d.Observations[8].Note)
	require.Equal(t, "available", d.Observations[2].Status)

	r = tradeSample(true, "goods")
	r.Observations[0].Note = "未采集该实际分类官方净额；上年同期基数缺失或为零"
	r.Observations[0].YoYBase.Exports, r.Observations[0].ExportsYoY = nil, nil
	d, err = Trade(r)
	require.NoError(t, err)
	require.Equal(t, "严格基期缺失", d.Observations[1].Note, "不把未输出序列的说明写到出口同比上")
	require.Empty(t, d.Observations[0].Note)
	require.Empty(t, d.Observations[4].Note, "进口同比可用，不带出口的基期说明")

	var original fred.TradeResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Equal(t, r.Component, original.Component)

	for _, mutate := range []func(*fred.TradeResult){
		func(r *fred.TradeResult) { r.Component = "unknown" },
		func(r *fred.TradeResult) { r.Indicator = "unknown" },
		func(r *fred.TradeResult) { r.Frequency = "quarterly" },
		func(r *fred.TradeResult) { r.Unit = "十亿美元" },
		func(r *fred.TradeResult) { r.AccountingBasis = "NIPA" },
		func(r *fred.TradeResult) { r.PriceBasis = "real" },
		func(r *fred.TradeResult) { r.SeasonallyAdjusted = false },
		func(r *fred.TradeResult) { r.LevelAnnualized = true },
		func(r *fred.TradeResult) { r.GrowthAnnualized = true },
		func(r *fred.TradeResult) { year := 2017; r.ReferenceYear = &year },
		func(r *fred.TradeResult) { r.Series = r.Series[:2] },
		func(r *fred.TradeResult) { r.Series[0].SeriesID = "BOPGEXP" },
		func(r *fred.TradeResult) { r.Observations[0].YoYBase.Period = "202412" },
		func(r *fred.TradeResult) {
			r.Observations[0].ExportsPreviousGrowth = nil
			r.Observations[0].Exports = nil
			r.Observations[0].ExportsYoY = value(1)
		},
		func(r *fred.TradeResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := tradeSample(false, "total")
		mutate(r)
		d, err := Trade(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*fred.TradeResult){
		func(r *fred.TradeResult) { r.ReferenceYear = nil },
		func(r *fred.TradeResult) { year := 2012; r.ReferenceYear = &year },
		func(r *fred.TradeResult) { r.Observations[0].Balance = value(1) },
		func(r *fred.TradeResult) {
			r.Series = append(r.Series, fred.TradeSeries{Measure: "balance", SeriesID: "NETEXC"})
		},
	} {
		r := tradeSample(true, "goods")
		mutate(r)
		d, err := Trade(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = Trade(nil)
	require.Error(t, err)
}

func TestPeriodicRuleNativeDifference(t *testing.T) {
	d, err := newDatasetFrequency("US", "2026Q1", "2026Q1", "quarterly", map[string]string{})
	require.NoError(t, err)
	item := periodicObservation{current: periodicValue{"2026Q1", value(-2), "-2"}, yearAgo: periodicValue{"2025Q1", value(-1), "-1"}, previous: periodicValue{"2025Q4", value(0), "0"}, yoy: value(-1), change: value(-2)}
	require.NoError(t, appendPeriodicChanges(d, Series{ID: "us.test", Measure: "level", Unit: "USD_billion", Frequency: "quarterly"}, monthlyProvenance{}, []periodicObservation{item}, periodicRule{kind: nativeChange}))
	require.Equal(t, "qoq_change", d.Series[2].Measure)
	require.Equal(t, "USD_billion", d.Series[2].Unit)
	require.False(t, d.Series[2].Annualized)
	require.Equal(t, "available", d.Observations[2].Status)
}
