package macrodata

import (
	"fmt"

	"github.com/alwqx/sec/provider/fred"
)

// Housing 新房、成屋和房价指数各自成序列，不加总、不统一单位，也不把折年水平换算为当期量。
func Housing(result *fred.HousingResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("住宅结果不能为空")
	}
	specs := map[string]struct {
		id, seriesPrefix, unit, standardUnit, measure, basis, price, indexBase, adjustment string
		annualized                                                                         bool
	}{
		"new-sales":      {"HSN1F", "us.housing.new-sales", "千套", "housing_units_thousand", "level", "month", "not_applicable", "", "seasonally-adjusted", true},
		"existing-sales": {"EXHOSLUSM495S", "us.housing.existing-sales", "套", "housing_units", "level", "month", "not_applicable", "", "seasonally-adjusted", true},
		"house-price":    {"USSTHPI", "us.house-price", "指数", "index", "index", "quarter", "price-index", "1980Q1=100", "not-seasonally-adjusted", false},
	}
	frequency, components := "monthly", []string{result.Component}
	switch {
	case result.Indicator == "house_price" && result.Component == "house-price":
		frequency = "quarterly"
	case result.Indicator == "housing_sales" && result.Component == "all":
		components = []string{"new-sales", "existing-sales"}
	case result.Indicator == "housing_sales" && (result.Component == "new-sales" || result.Component == "existing-sales"):
	default:
		return nil, fmt.Errorf("住宅统计口径不符合统一导出契约")
	}
	if result.Frequency != frequency || result.GrowthAnnualized || len(result.Series) != len(components) {
		return nil, fmt.Errorf("住宅统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, frequency, result)
	if err != nil {
		return nil, err
	}
	point := func(v fred.HousingBase) periodicValue { return periodicValue{v.Period, v.Value, v.Raw} }
	matched := 0
	for i, component := range components {
		spec, series := specs[component], result.Series[i]
		if series.Component != component || series.SeriesID != spec.id || series.Unit != spec.unit || series.IndexBase != spec.indexBase || series.SeasonallyAdjusted != (spec.adjustment == "seasonally-adjusted") || series.LevelAnnualized != spec.annualized || series.Definition == "" {
			return nil, fmt.Errorf("住宅序列口径不符合统一导出契约: %s", component)
		}
		items := []periodicObservation{}
		for _, o := range result.Observations {
			if o.Component == component {
				if o.SeriesID != spec.id || o.Unit != spec.unit {
					return nil, fmt.Errorf("住宅观测与序列不匹配: %s", o.Period)
				}
				// 只保留当期缺失说明（含成屋覆盖范围）；房价的固定未季调说明已由序列口径表明。
				note := ""
				if o.Value == nil && component != "house-price" {
					note = o.Note
				}
				items = append(items, periodicObservation{current: point(o.HousingBase), yearAgo: point(o.YoYBase), previous: point(o.PreviousBase), yoy: o.YoY, change: o.PreviousPeriodGrowth, note: note})
			}
		}
		matched += len(items)
		s := Series{ID: spec.seriesPrefix, Indicator: component, Measure: spec.measure, Unit: spec.standardUnit, Frequency: frequency, PeriodBasis: spec.basis, SeasonalAdjustment: spec.adjustment, PriceBasis: spec.price, IndexBase: spec.indexBase, Annualized: spec.annualized, Definition: spec.id, Coverage: series.Definition, ValueOrigin: "official"}
		if err := appendPeriodicChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, periodicRule{method: "level_ratio", reasonNotes: true}); err != nil {
			return nil, err
		}
	}
	if matched != len(result.Observations) {
		return nil, fmt.Errorf("住宅观测包含未声明的分类")
	}
	return d, nil
}

// Trade 出口、进口和官方差额分别成序列；差额只用原单位差值，实际分项净额未采集时不输出。
// provider的整行备注混合三个指标，这里按各值的缺失原因生成备注。
func Trade(result *fred.TradeResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("贸易结果不能为空")
	}
	ids := map[string][3]string{"total": {"BOPTEXP", "BOPTIMP", "BOPGSTB"}, "goods": {"BOPGEXP", "BOPGIMP", "BOPGTB"}, "services": {"BOPSEXP", "BOPSIMP", "BOPSTB"}}
	indicators := [3]string{"exports", "imports", "trade-balance"}
	frequency, unit, standardUnit, basis, accounting, methodology, providerPrice, price, period := "monthly", "百万美元（当期美元）", "USD_million", "BOP", "国际收支口径", "bop", "nominal", "nominal", "month"
	real := result.Indicator == "real_trade"
	if real {
		ids = map[string][3]string{"total": {"EXPGSC1", "IMPGSC1", "NETEXC"}, "goods": {"A253RX1Q020SBEA", "A255RX1Q020SBEA", ""}, "services": {"A646RX1Q020SBEA", "B656RX1Q020SBEA", ""}}
		indicators = [3]string{"real-exports", "real-imports", "real-net-exports"}
		frequency, unit, standardUnit, basis, accounting, methodology, providerPrice, price, period = "quarterly", "十亿美元（2017年链式美元）", "USD_billion", "NIPA", "NIPA口径", "nipa", "real", "real-chained", "quarter"
	}
	selected, ok := ids[result.Component]
	scope := map[string]string{"total": "商品与服务", "goods": "商品", "services": "服务"}[result.Component]
	if !ok || (!real && result.Indicator != "trade") || result.Frequency != frequency || result.Unit != unit || result.AccountingBasis != basis || result.PriceBasis != providerPrice || !result.SeasonallyAdjusted || result.LevelAnnualized != real || result.GrowthAnnualized || (real != (result.ReferenceYear != nil)) || (real && *result.ReferenceYear != 2017) {
		return nil, fmt.Errorf("贸易统计口径不符合统一导出契约")
	}
	expected := 3
	if selected[2] == "" {
		expected = 2
	}
	if len(result.Series) != expected {
		return nil, fmt.Errorf("贸易序列不符合统一导出契约")
	}
	for i, measure := range []string{"exports", "imports", "balance"}[:expected] {
		if result.Series[i].Measure != measure || result.Series[i].SeriesID != selected[i] {
			return nil, fmt.Errorf("贸易序列不符合统一导出契约: %s", measure)
		}
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, frequency, result)
	if err != nil {
		return nil, err
	}
	fields := []struct {
		name   string
		point  func(fred.TradeValue) periodicValue
		yoy    func(fred.TradeObservation) *float64
		change func(fred.TradeObservation) *float64
		rule   periodicRule
	}{
		{"出口", func(v fred.TradeValue) periodicValue { return periodicValue{v.Period, v.Exports, v.RawExports} }, func(o fred.TradeObservation) *float64 { return o.ExportsYoY }, func(o fred.TradeObservation) *float64 { return o.ExportsPreviousGrowth }, periodicRule{method: "level_ratio", reasonNotes: true}},
		{"进口", func(v fred.TradeValue) periodicValue { return periodicValue{v.Period, v.Imports, v.RawImports} }, func(o fred.TradeObservation) *float64 { return o.ImportsYoY }, func(o fred.TradeObservation) *float64 { return o.ImportsPreviousGrowth }, periodicRule{method: "level_ratio", reasonNotes: true}},
		{"官方差额", func(v fred.TradeValue) periodicValue { return periodicValue{v.Period, v.Balance, v.RawBalance} }, func(o fred.TradeObservation) *float64 { return o.BalanceYoYChange }, func(o fred.TradeObservation) *float64 { return o.BalancePreviousChange }, periodicRule{method: "difference", kind: nativeChange, reasonNotes: true}},
	}
	for _, o := range result.Observations {
		if expected == 2 && (o.Balance != nil || o.PreviousBase.Balance != nil || o.YoYBase.Balance != nil || o.BalanceYoYChange != nil || o.BalancePreviousChange != nil) {
			return nil, fmt.Errorf("未采集实际分项净额却返回了数值: %s", o.Period)
		}
	}
	for i := 0; i < expected; i++ {
		field := fields[i]
		s := Series{ID: "us." + indicators[i] + "." + result.Component, Indicator: indicators[i], Measure: "level", Unit: standardUnit, Frequency: frequency, PeriodBasis: period, SeasonalAdjustment: "seasonally-adjusted", PriceBasis: price, ReferenceYear: result.ReferenceYear, Annualized: real, Definition: selected[i], Coverage: accounting + scope + field.name, Methodology: methodology, ValueOrigin: "official"}
		if real {
			additive := false
			s.ComponentsAdditive = &additive
		}
		items := make([]periodicObservation, 0, len(result.Observations))
		for _, o := range result.Observations {
			items = append(items, periodicObservation{current: field.point(o.TradeValue), yearAgo: field.point(o.YoYBase), previous: field.point(o.PreviousBase), yoy: field.yoy(o), change: field.change(o)})
		}
		if err := appendPeriodicChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, field.rule); err != nil {
			return nil, err
		}
	}
	return d, nil
}
