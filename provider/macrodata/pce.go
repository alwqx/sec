package macrodata

import (
	"fmt"

	"github.com/alwqx/sec/provider/fred"
)

// PCE 只适配价格指数，不将指数误当作支出金额。
func PCE(result *fred.PCEResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("PCE价格结果不能为空")
	}
	id := "PCEPI"
	if result.Indicator == "core-pce" {
		id = "PCEPILFE"
	} else if result.Indicator != "pce" {
		return nil, fmt.Errorf("未知PCE价格指标: %s", result.Indicator)
	}
	if result.SeriesID != id || result.Frequency != "monthly" || result.Measure != "chain-type-price-index" || result.IndexBase != "2017=100" || !result.SeasonallyAdjusted {
		return nil, fmt.Errorf("PCE价格统计口径不符合统一导出契约")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	point := func(v fred.PCEValue) periodicValue { return periodicValue{v.Period, v.Index, v.RawIndex} }
	items := make([]periodicObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		items = append(items, periodicObservation{current: point(o.PCEValue), yearAgo: point(o.YoYBase), previous: point(o.MoMBase), yoy: o.YoY, change: o.MoM, note: o.Note})
	}
	s := Series{ID: "us." + result.Indicator, Indicator: result.Indicator, Measure: "index", Unit: "index", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "price-index", IndexBase: result.IndexBase, Definition: id, ValueOrigin: "official"}
	err = appendPeriodicChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, periodicRule{})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// RealPCE 保留官方总量／分项，不相加链式金额或按比例生成贡献度。
func RealPCE(result *fred.RealPCEResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("实际消费结果不能为空")
	}
	id := map[string]string{"total": "PCEC96", "goods": "DGDSRX1", "durable-goods": "PCEDGC96", "nondurable-goods": "PCENDC96", "services": "PCESC96"}[result.Component]
	if id == "" || result.SeriesID != id || result.Indicator != "real-pce" || result.Frequency != "monthly" || result.ReferenceYear != 2017 || !result.SeasonallyAdjusted || !result.LevelAnnualized || result.GrowthAnnualized || result.ComponentsAdditive {
		return nil, fmt.Errorf("实际消费统计口径不符合统一导出契约")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	point := func(v fred.RealPCEValue) periodicValue { return periodicValue{v.Period, v.Level, v.RawValue} }
	items := make([]periodicObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		items = append(items, periodicObservation{current: point(o.RealPCEValue), yearAgo: point(o.YoYBase), previous: point(o.MoMBase), yoy: o.YoY, change: o.MoM, note: o.Note})
	}
	s := Series{ID: "us.real-pce." + result.Component, Indicator: "real-pce", Measure: "level", Unit: "USD_billion", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "real-chained", ReferenceYear: &result.ReferenceYear, Annualized: true, ComponentsAdditive: &result.ComponentsAdditive, Definition: id, ValueOrigin: "official"}
	err = appendPeriodicChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, periodicRule{})
	if err != nil {
		return nil, err
	}
	return d, nil
}
