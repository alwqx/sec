package macrodata

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/nyfed"
)

// EFFR 仅转换官方实际返回的交易日；方法切换前后分开序列。
func EFFR(result *nyfed.EFFRResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("EFFR结果不能为空")
	}
	if result.Frequency != "daily" || result.SeriesID != "EFFR" || result.Unit != "%" {
		return nil, fmt.Errorf("EFFR统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, "daily", result)
	if err != nil {
		return nil, err
	}
	rates, seen := map[string]float64{}, map[string]bool{}
	for _, item := range result.Observations {
		if seen[item.EffectiveDate] {
			return nil, fmt.Errorf("EFFR所属日期重复: %s", item.EffectiveDate)
		}
		seen[item.EffectiveDate] = true
		rates[item.EffectiveDate] = item.RatePercent
	}
	for _, item := range result.Observations {
		date, err := time.Parse("2006-01-02", item.EffectiveDate)
		if err != nil || date.Format("2006-01-02") != item.EffectiveDate {
			return nil, fmt.Errorf("无效EFFR所属日期: %s", item.EffectiveDate)
		}
		method := "fr2420-volume-weighted-median"
		if item.EffectiveDate < "2016-03-01" {
			method = "broker-volume-weighted-mean"
		}
		for _, spec := range []struct {
			measure, unit string
			value         *float64
		}{
			{"rate", "percent", &item.RatePercent}, {"previous_observation_change", "basis_points", item.ChangeBasisPoints},
		} {
			id := "us.effr." + method + "." + spec.measure
			s := Series{ID: id, Indicator: "effr", Measure: spec.measure, Unit: spec.unit, Frequency: "daily", PeriodBasis: "overnight-transaction-date", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "not_applicable", ValueOrigin: "official", Definition: item.Methodology}
			o := Observation{SeriesID: id, Period: date.Format("20060102"), Value: spec.value, SourceSeriesID: result.SeriesID, Source: text(result.SourceURL), FetchedAt: timestamp(item.FetchedAt), PublishedAt: item.PublishedAt, RevisionIndicator: item.RevisionIndicator, DataVersion: "current_visible_revision"}
			reason := "previous_observation_missing"
			if spec.measure == "previous_observation_change" {
				s.ValueOrigin, s.Formula = "derived", text("(current_rate_percent - previous_rate_percent) * 100")
				if item.PreviousDate != "" {
					previous, err := time.Parse("2006-01-02", item.PreviousDate)
					if err != nil || previous.Format("2006-01-02") != item.PreviousDate || !previous.Before(date) {
						return nil, fmt.Errorf("无效EFFR前次所属日期: %s", item.PreviousDate)
					}
					o.Base = &Base{Period: previous.Format("20060102")}
					if rate, ok := rates[item.PreviousDate]; ok {
						o.Base.Value = &rate
					} else {
						o.Note = "前值位于当前结果之外，基期水平未由provider返回；不从变化值反推。"
					}
					reason = "calculation_unavailable"
					if item.PreviousDate < "2016-03-01" && item.EffectiveDate >= "2016-03-01" {
						o.Value = nil
						reason = "methodology_break"
					}
				}
			}
			found := false
			for _, existing := range d.Series {
				if existing.ID == id {
					found = true
					break
				}
			}
			if !found {
				d.Series = append(d.Series, s)
			}
			if err := d.append(o, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
