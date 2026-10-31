package macrodata

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/bls"
)

type laborPoint struct {
	period, id, raw string
	value           *float64
	fetchedAt       time.Time
	preliminary     *bool
}

type laborMeasure struct {
	measure, unit, formula string
	value                  *float64
	base                   *laborPoint
	ratio                  bool
}

// LaborForce 保留CPS官方比率及分子分母，变化为严格基期百分点差。
func LaborForce(result *bls.LaborForceResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("劳动力比率结果不能为空")
	}
	id := map[string]string{"unemployment": "LNS14000000", "participation": "LNS11300000"}[result.Indicator]
	if id == "" || result.SeriesID != id || result.Frequency != "monthly" || !result.SeasonallyAdjusted || result.Unit != "%" || result.ChangeUnit != "百分点" || result.Survey == "" || result.Population == "" || result.Numerator == "" || result.Denominator == "" {
		return nil, fmt.Errorf("劳动力比率统计口径不符合统一导出契约")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	s := Series{ID: "us." + result.Indicator, Indicator: result.Indicator, Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "not_applicable", Definition: id, Coverage: result.Population, Survey: result.Survey, Numerator: result.Numerator, Denominator: result.Denominator}
	point := func(v bls.LaborForceValue) laborPoint {
		return laborPoint{period: v.Period, id: v.SeriesID, raw: v.RawValue, value: v.Rate, fetchedAt: v.FetchedAt}
	}
	seen := map[string]bool{}
	for _, o := range result.Observations {
		if seen[o.Period] {
			return nil, fmt.Errorf("劳动力比率月份重复: %s", o.Period)
		}
		seen[o.Period] = true
		yoy, mom := point(o.YoYBase), point(o.MoMBase)
		specs := []laborMeasure{{"rate", "percent", "", o.Rate, nil, false}, {"yoy_percentage_points", "percentage_points", "current_level - base_level", o.YoYChange, &yoy, false}, {"mom_percentage_points", "percentage_points", "current_level - base_level", o.MoMChange, &mom, false}}
		if err := appendLaborRow(d, s, point(o.LaborForceValue), specs, result.Source, result.PublishedAt, o.Note); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// EarningsHours 保留CES私人非农全部雇员范围，不把平均时薪或工时当作收入总量。
func EarningsHours(result *bls.EarningsHoursResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("时薪工时结果不能为空")
	}
	id, unit, price := "CES0500000003", "USD_per_hour", "nominal"
	if result.Indicator == "weekly-hours" {
		id, unit, price = "CES0500000002", "hours_per_week", "not_applicable"
	} else if result.Indicator != "earnings" {
		return nil, fmt.Errorf("未知时薪工时指标: %s", result.Indicator)
	}
	if result.SeriesID != id || result.Frequency != "monthly" || !result.SeasonallyAdjusted || result.Population == "" || (result.Indicator == "earnings" && result.PriceBasis != "current-dollars") {
		return nil, fmt.Errorf("时薪工时统计口径不符合统一导出契约")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	s := Series{ID: "us." + result.Indicator, Indicator: result.Indicator, Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: price, Definition: id, Coverage: result.Population, Survey: "CES establishment survey"}
	point := func(v bls.EarningsHoursValue) laborPoint {
		p := laborPoint{period: v.Period, id: v.SeriesID, raw: v.RawValue, value: v.Value, fetchedAt: v.FetchedAt}
		if v.Value != nil || v.RawValue != "" || !v.FetchedAt.IsZero() {
			p.preliminary = &v.Preliminary
		}
		return p
	}
	seen := map[string]bool{}
	for _, o := range result.Observations {
		if seen[o.Period] {
			return nil, fmt.Errorf("时薪工时月份重复: %s", o.Period)
		}
		seen[o.Period] = true
		yoy, mom := point(o.YoYBase), point(o.MoMBase)
		specs := []laborMeasure{{"level", unit, "", o.Value, nil, false}, {"monthly_change", unit, "current_level - base_level", o.Change, &mom, false}, {"yoy_percent", "percent", "(current_level / base_level - 1) * 100", o.YoY, &yoy, true}, {"mom_percent", "percent", "(current_level / base_level - 1) * 100", o.MoM, &mom, true}}
		if err := appendLaborRow(d, s, point(o.EarningsHoursValue), specs, result.Source, result.PublishedAt, o.Note); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// appendLaborRow 使用当前点的实际抓取时间；基期值来自provider同批结果，不跨批重建。
func appendLaborRow(d *Dataset, prefix Series, current laborPoint, specs []laborMeasure, source string, publishedAt *time.Time, note string) error {
	if err := validatePeriod(current.period, "monthly"); err != nil {
		return err
	}
	if current.id != prefix.Definition {
		return fmt.Errorf("劳动力当期来源序列不匹配")
	}
	month, _ := time.Parse("200601", current.period)
	for _, spec := range specs {
		s := prefix
		s.ID += "." + spec.measure
		s.Measure, s.Unit, s.ValueOrigin = spec.measure, spec.unit, "official"
		o := Observation{SeriesID: s.ID, Period: current.period, Value: spec.value, SourceSeriesID: current.id, Source: text(source), FetchedAt: timestamp(current.fetchedAt), PublishedAt: publishedAt, DataVersion: "current_visible_revision", Note: note}
		reason := "source_value_missing"
		if spec.base == nil {
			o.RawValue, o.Preliminary = text(current.raw), current.preliminary
		} else {
			s.ValueOrigin, s.Formula = "derived", text(spec.formula)
			expected := month.AddDate(0, -1, 0).Format("200601")
			if spec.measure == "yoy_percent" || spec.measure == "yoy_percentage_points" {
				expected = month.AddDate(-1, 0, 0).Format("200601")
			}
			if spec.base.period != expected || spec.base.id != current.id {
				return fmt.Errorf("劳动力计算基期或来源序列不匹配")
			}
			o.Base = &Base{Period: spec.base.period, Value: spec.base.value, RawValue: text(spec.base.raw)}
			reason = "calculation_unavailable"
			if current.value == nil {
				reason = "current_value_missing"
			} else if spec.base.value == nil {
				reason = "baseline_missing"
			} else if spec.ratio && *spec.base.value == 0 {
				reason = "zero_baseline"
			}
		}
		found := false
		for _, existing := range d.Series {
			if existing.ID == s.ID {
				found = true
				break
			}
		}
		if !found {
			d.Series = append(d.Series, s)
		}
		if err := d.append(o, reason); err != nil {
			return err
		}
	}
	return nil
}
