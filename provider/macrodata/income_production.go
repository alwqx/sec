package macrodata

import (
	"fmt"

	"github.com/alwqx/sec/provider/fred"
)

// RealIncome 直接保留实际税后收入总量，不改为人均或当月发生额。
func RealIncome(result *fred.RealIncomeResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("实际收入结果不能为空")
	}
	if result.Indicator != "real-income" || result.SeriesID != "DSPIC96" || result.Measure != "real-disposable-personal-income" || result.Frequency != "monthly" || result.ReferenceYear != 2017 || !result.SeasonallyAdjusted || !result.LevelAnnualized || result.GrowthAnnualized {
		return nil, fmt.Errorf("实际收入统计口径不符合统一导出契约")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	point := func(v fred.RealIncomeValue) monthlyLevelValue {
		return monthlyLevelValue{v.Period, v.Level, v.RawValue}
	}
	items := make([]monthlyLevelObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		items = append(items, monthlyLevelObservation{point(o.RealIncomeValue), point(o.YoYBase), point(o.MoMBase), o.YoY, o.MoM, o.Note})
	}
	s := Series{ID: "us.real-income", Indicator: "real-income", Measure: "level", Unit: "USD_billion", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "real-chained", ReferenceYear: &result.ReferenceYear, Annualized: true, ValueOrigin: "official", Definition: result.SeriesID, Coverage: "实际可支配个人收入总量（税后、扣除价格影响），非人均"}
	if err := appendMonthlyLevels(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items); err != nil {
		return nil, err
	}
	return d, nil
}

// Production 将生产指数增速与利用率百分点差分开；总量、NAICS制造业各自独立。
func Production(result *fred.ProductionResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("工业结果不能为空")
	}
	ids := map[string]map[string]string{"industrial-production": {"total": "INDPRO", "manufacturing": "IPMAN"}, "capacity-utilization": {"total": "TCU", "manufacturing": "MCUMFN"}}
	id := ids[result.Indicator][result.Sector]
	if id == "" || result.SeriesID != id || result.Frequency != "monthly" || !result.SeasonallyAdjusted || result.GrowthAnnualized || result.Coverage == "" {
		return nil, fmt.Errorf("工业统计口径不符合统一导出契约")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	points := result.Indicator == "capacity-utilization"
	s := Series{ID: "us." + result.Indicator + "." + result.Sector, Indicator: result.Indicator, Measure: "index", Unit: "index", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "volume-index", ValueOrigin: "official", Definition: id, Coverage: result.Coverage, IndexBase: "2017=100", ReferenceYear: result.ReferenceYear}
	if points {
		if result.ChangeUnit != "percentage-points" || result.ReferenceYear != nil {
			return nil, fmt.Errorf("利用率必须使用百分点变化，不设置指数参考年")
		}
		s.Measure, s.Unit, s.PriceBasis, s.IndexBase = "rate", "percent", "not_applicable", ""
	} else if result.ChangeUnit != "percent" || result.ReferenceYear == nil || *result.ReferenceYear != 2017 {
		return nil, fmt.Errorf("生产指数必须使用2017参考年和百分比增速")
	}
	point := func(v fred.ProductionValue) monthlyLevelValue {
		return monthlyLevelValue{v.Period, v.Level, v.RawValue}
	}
	items := make([]monthlyLevelObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		yoy, mom := o.YoY, o.MoM
		if points {
			yoy, mom = o.YoYPercentagePoints, o.MoMPercentagePoints
		}
		items = append(items, monthlyLevelObservation{point(o.ProductionValue), point(o.YoYBase), point(o.MoMBase), yoy, mom, o.Note})
	}
	if err := appendMonthlyChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, points); err != nil {
		return nil, err
	}
	return d, nil
}
