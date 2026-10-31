package macrodata

import (
	"fmt"

	"github.com/alwqx/sec/provider/fred"
)

// Enterprise 分开利润流量、月末库存和新订单，不混用频率或季调折年口径。
func Enterprise(result *fred.EnterpriseResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("企业指标结果不能为空")
	}
	specs := map[string]struct{ indicator, id, frequency, unit, basis, measurement string }{
		"profit":     {"corporate-profits", "CPROFIT", "quarterly", "USD_billion", "quarter", "quarterly_flow"},
		"inventory":  {"business-inventories", "BUSINV", "monthly", "USD_million", "month-end", "month_end_stock"},
		"new-orders": {"new-orders", "AMTMNO", "monthly", "USD_million", "month", "monthly_flow"},
	}
	spec, ok := specs[result.Metric]
	if !ok || result.SeriesID != spec.id || result.Frequency != spec.frequency || result.Measurement != spec.measurement || result.PriceBasis != "current_dollars" || !result.SeasonallyAdjusted || result.LevelAnnualized != (result.Metric == "profit") || result.GrowthAnnualized || result.Definition == "" || (result.Metric == "profit" && result.TaxBasis != "before_tax") {
		return nil, fmt.Errorf("企业指标统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, spec.frequency, result)
	if err != nil {
		return nil, err
	}
	s := Series{ID: "us." + spec.indicator, Indicator: spec.indicator, Measure: "level", Unit: spec.unit, Frequency: spec.frequency, PeriodBasis: spec.basis, SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "nominal", Annualized: result.LevelAnnualized, Coverage: result.Definition, Definition: spec.id, TaxBasis: result.TaxBasis, ValueOrigin: "official"}
	point := func(v fred.EnterpriseValue) periodicValue { return periodicValue{v.Period, v.Level, v.RawLevel} }
	items := make([]periodicObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		items = append(items, periodicObservation{current: point(o.EnterpriseValue), yearAgo: point(o.YoYBase), previous: point(o.PreviousBase), yoy: o.YoY, change: o.PreviousGrowth, note: o.Note, asOfDate: o.AsOfDate})
	}
	if err := appendPeriodicChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, periodicRule{positiveBase: true}); err != nil {
		return nil, err
	}
	return d, nil
}
