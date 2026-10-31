package macrodata

import (
	"fmt"

	"github.com/alwqx/sec/provider/fred"
)

// USGDP 分开实际折年水平、官方环比年化与未年化计算增速。
// 发布版本档案保留在Original，不以最新水平回填历史版本。
func USGDP(result *fred.GDPResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("美国GDP结果不能为空")
	}
	if result.Frequency != "quarterly" || result.ReferenceYear != 2017 || !result.SeasonallyAdjusted || !result.LevelAnnualized || result.SeriesID != "GDPC1" || result.GrowthSeriesID != "A191RL1Q225SBEA" {
		return nil, fmt.Errorf("美国GDP统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, "quarterly", result)
	if err != nil {
		return nil, err
	}
	for _, measure := range []string{"level", "qoq_annualized_percent", "qoq_percent", "yoy_percent"} {
		s := Series{ID: "us.gdp." + measure, Indicator: "gdp", Measure: measure, Unit: "percent", Frequency: "quarterly", PeriodBasis: "quarter", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "real-chained", ValueOrigin: "official", ReferenceYear: &result.ReferenceYear, Definition: result.GrowthSeriesID}
		if measure == "level" {
			s.Unit, s.Annualized, s.Definition = "USD_billion", true, result.SeriesID
		}
		if measure == "qoq_annualized_percent" {
			s.Annualized = true
		}
		if measure == "qoq_percent" || measure == "yoy_percent" {
			s.ValueOrigin, s.Definition, s.Formula = "derived", result.SeriesID, text("(current_level / base_level - 1) * 100")
		}
		d.Series = append(d.Series, s)
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if seen[item.Period] {
			return nil, fmt.Errorf("GDP观测所属期重复: %s", item.Period)
		}
		seen[item.Period] = true
		for _, spec := range []struct {
			measure, raw, sourceID string
			value                  *float64
			base                   *fred.GDPValue
		}{
			{"level", item.RawLevel, result.SeriesID, item.Level, nil},
			{"qoq_annualized_percent", item.RawQoQAnnualized, result.GrowthSeriesID, item.QoQAnnualized, nil},
			{"qoq_percent", "", result.SeriesID, item.QoQ, &item.QoQBase},
			{"yoy_percent", "", result.SeriesID, item.YoY, &item.YoYBase},
		} {
			o := Observation{SeriesID: "us.gdp." + spec.measure, Period: item.Period, Value: spec.value, RawValue: text(spec.raw), SourceSeriesID: spec.sourceID, Source: text(result.Source), FetchedAt: timestamp(result.FetchedAt), PublishedAt: result.PublishedAt, RevisionID: result.RevisionID, DataVersion: "current_visible_revision", Note: item.Note}
			reason := "source_value_missing"
			if spec.base != nil {
				if err := validatePeriod(spec.base.Period, "quarterly"); err != nil {
					return nil, err
				}
				o.Base = &Base{Period: spec.base.Period, Value: spec.base.Level, RawValue: text(spec.base.RawLevel)}
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
