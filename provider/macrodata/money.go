// Package macrodata 将已采集的数据适配为统一的宏观序列契约。
package macrodata

import (
	"fmt"
	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/pbc"
)

// CNMoney 区分2025年前后的M1序列，不由余额重新计算官方同比。
func CNMoney(items []pbc.MoneySupplyObservation, begin, end string) (*Dataset, error) {
	if items == nil {
		items = []pbc.MoneySupplyObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	seen, periods := map[string]bool{}, map[string]bool{}
	for _, item := range items {
		if periods[item.Period] {
			return nil, fmt.Errorf("货币观测月份重复: %s", item.Period)
		}
		periods[item.Period] = true
		definition := "original"
		if item.Period >= "202501" {
			definition = "revised-2025"
		}
		if item.M1Definition != definition {
			return nil, fmt.Errorf("M1统计口径与月份不一致: %s", item.Period)
		}
		for _, spec := range []struct {
			indicator, measure, unit, definition string
			value                                *float64
		}{
			{"m1", "level", "CNY_100_million", definition, item.M1Balance},
			{"m1", "yoy_percent", "percent", definition, item.M1YoY},
			{"m2", "level", "CNY_100_million", "pbc-m2", item.M2Balance},
			{"m2", "yoy_percent", "percent", "pbc-m2", item.M2YoY},
		} {
			id := "cn." + spec.indicator + "." + spec.definition + "." + spec.measure
			if !seen[id] {
				d.Series = append(d.Series, Series{ID: id, Indicator: spec.indicator, Measure: spec.measure, Unit: spec.unit, Frequency: "monthly", PeriodBasis: "month-end", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Definition: spec.definition})
				seen[id] = true
			}
			reason := "source_value_missing"
			if item.Source == "" {
				reason = "report_not_found"
			}
			if err := d.append(Observation{SeriesID: id, Period: item.Period, Value: spec.value, Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), PublishedDate: text(item.PublishedDate), DataVersion: "current_visible_revision", Note: item.Note}, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// USMoney 保留FRED严格基期和原始字符串，计算增速与官方水平分别标记。
func USMoney(result *fred.MoneyResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("美国M2结果不能为空")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	for _, measure := range []string{"level", "yoy_percent", "mom_percent"} {
		s := Series{ID: "us.m2." + measure, Indicator: "m2", Measure: measure, Unit: "USD_billion", Frequency: result.Frequency, PeriodBasis: result.PeriodBasis, SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Definition: result.SeriesID, Annualized: result.LevelAnnualized}
		if result.SeasonallyAdjusted {
			s.SeasonalAdjustment = "seasonally-adjusted"
		}
		if measure != "level" {
			s.Unit, s.ValueOrigin, s.Annualized, s.Formula = "percent", "derived", result.GrowthAnnualized, text("(current_level / base_level - 1) * 100")
		}
		d.Series = append(d.Series, s)
	}
	periods := map[string]bool{}
	for _, item := range result.Observations {
		if periods[item.Period] {
			return nil, fmt.Errorf("货币观测月份重复: %s", item.Period)
		}
		periods[item.Period] = true
		for _, spec := range []struct {
			measure string
			value   *float64
			base    *fred.MoneyValue
		}{
			{"level", item.Level, nil}, {"yoy_percent", item.YoY, &item.YoYBase}, {"mom_percent", item.MoM, &item.MoMBase},
		} {
			o := Observation{SeriesID: "us.m2." + spec.measure, Period: item.Period, Value: spec.value, Source: text(result.Source), FetchedAt: timestamp(result.FetchedAt), PublishedAt: result.PublishedAt, RevisionID: result.RevisionID, DataVersion: "current_visible_revision", Note: item.Note}
			reason := "source_value_missing"
			if spec.base == nil {
				o.RawValue = text(item.RawValue)
			} else {
				o.Base = &Base{Period: spec.base.Period, Value: spec.base.Level, RawValue: text(spec.base.RawValue)}
				reason = "calculation_unavailable"
				if item.Level == nil {
					reason = "current_value_missing"
				} else if spec.base.Level == nil {
					reason = "baseline_missing"
				} else if *spec.base.Level == 0 {
					reason = "zero_baseline"
				}
			}
			if err := d.append(o, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
