package macrodata

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/fred"
)

type monthlyLevelValue struct {
	period string
	value  *float64
	raw    string
}

type monthlyLevelObservation struct {
	current, yoyBase, momBase monthlyLevelValue
	yoy, mom                  *float64
	note                      string
}

type monthlyProvenance struct {
	source      string
	fetchedAt   time.Time
	publishedAt *time.Time
	revisionID  *string
}

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
	point := func(v fred.PCEValue) monthlyLevelValue { return monthlyLevelValue{v.Period, v.Index, v.RawIndex} }
	items := make([]monthlyLevelObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		items = append(items, monthlyLevelObservation{point(o.PCEValue), point(o.YoYBase), point(o.MoMBase), o.YoY, o.MoM, o.Note})
	}
	s := Series{ID: "us." + result.Indicator, Indicator: result.Indicator, Measure: "index", Unit: "index", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "price-index", IndexBase: result.IndexBase, Definition: id, ValueOrigin: "official"}
	err = appendMonthlyLevels(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items)
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
	point := func(v fred.RealPCEValue) monthlyLevelValue { return monthlyLevelValue{v.Period, v.Level, v.RawValue} }
	items := make([]monthlyLevelObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		items = append(items, monthlyLevelObservation{point(o.RealPCEValue), point(o.YoYBase), point(o.MoMBase), o.YoY, o.MoM, o.Note})
	}
	s := Series{ID: "us.real-pce." + result.Component, Indicator: "real-pce", Measure: "level", Unit: "USD_billion", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "real-chained", ReferenceYear: &result.ReferenceYear, Annualized: true, ComponentsAdditive: &result.ComponentsAdditive, Definition: id, ValueOrigin: "official"}
	err = appendMonthlyLevels(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// appendMonthlyLevels 复用月度官方水平与两种未年化增速的溯源逻辑，不重算provider值。
func appendMonthlyLevels(d *Dataset, level Series, provenance monthlyProvenance, items []monthlyLevelObservation) error {
	prefix := level.ID
	level.ID += "." + level.Measure
	d.Series = append(d.Series, level)
	for _, measure := range []string{"yoy_percent", "mom_percent"} {
		s := level
		s.ID, s.Measure, s.Unit, s.Annualized, s.ComponentsAdditive = prefix+"."+measure, measure, "percent", false, nil
		s.ValueOrigin, s.Formula = "derived", text("(current_level / base_level - 1) * 100")
		d.Series = append(d.Series, s)
	}
	seen := map[string]bool{}
	for _, item := range items {
		if err := validatePeriod(item.current.period, "monthly"); err != nil {
			return err
		}
		if seen[item.current.period] {
			return fmt.Errorf("月度水平所属期重复: %s", item.current.period)
		}
		seen[item.current.period] = true
		month, _ := time.Parse("200601", item.current.period)
		for i, spec := range []struct {
			value      *float64
			base       *monthlyLevelValue
			basePeriod string
		}{
			{item.current.value, nil, ""}, {item.yoy, &item.yoyBase, month.AddDate(-1, 0, 0).Format("200601")}, {item.mom, &item.momBase, month.AddDate(0, -1, 0).Format("200601")},
		} {
			o := Observation{SeriesID: d.Series[i].ID, Period: item.current.period, Value: spec.value, SourceSeriesID: level.Definition, Source: text(provenance.source), FetchedAt: timestamp(provenance.fetchedAt), PublishedAt: provenance.publishedAt, RevisionID: provenance.revisionID, DataVersion: "current_visible_revision", Note: item.note}
			reason := "source_value_missing"
			if i == 0 {
				o.RawValue = text(item.current.raw)
			} else {
				if spec.base.period != spec.basePeriod {
					return fmt.Errorf("月度计算基期不匹配: %s，预期%s", spec.base.period, spec.basePeriod)
				}
				o.Base = &Base{Period: spec.base.period, Value: spec.base.value, RawValue: text(spec.base.raw)}
				reason = "calculation_unavailable"
				if item.current.value == nil {
					reason = "current_value_missing"
				} else if spec.base.value == nil {
					reason = "baseline_missing"
				} else if *spec.base.value == 0 {
					reason = "zero_baseline"
				}
			}
			if err := d.append(o, reason); err != nil {
				return err
			}
		}
	}
	return nil
}
