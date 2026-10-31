package macrodata

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/fred"
)

// Claims 保留各自所属周，四周均值保留全部连续日历周输入。
func Claims(result *fred.ClaimsResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("失业金结果不能为空")
	}
	id, unit := "ICSA", "claims"
	if result.Indicator == "continued-claims" {
		id, unit = "CCSA", "persons"
	} else if result.Indicator != "initial-claims" {
		return nil, fmt.Errorf("未知失业金指标: %s", result.Indicator)
	}
	if result.SeriesID != id || result.Frequency != "weekly" || result.WeekEndingDay != "Saturday" || !result.SeasonallyAdjusted {
		return nil, fmt.Errorf("失业金周度统计口径不匹配")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, "weekly", result)
	if err != nil {
		return nil, err
	}
	for _, measure := range []string{"count", "weekly_change", "four_week_average"} {
		s := Series{ID: "us." + result.Indicator + "." + measure, Indicator: result.Indicator, Measure: measure, Unit: unit, Frequency: "weekly", PeriodBasis: "week-ending-saturday", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "not_applicable", ValueOrigin: "official", Definition: id}
		if measure == "weekly_change" {
			s.ValueOrigin, s.Formula = "derived", text("current_count - previous_week_count")
		}
		if measure == "four_week_average" {
			s.ValueOrigin, s.Formula = "derived", text("sum(current_and_previous_three_calendar_weeks) / 4")
		}
		d.Series = append(d.Series, s)
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		week, err := time.Parse("2006-01-02", item.WeekEnding)
		if err != nil || week.Format("2006-01-02") != item.WeekEnding || week.Weekday() != time.Saturday {
			return nil, fmt.Errorf("无效失业金所属周: %s", item.WeekEnding)
		}
		if seen[item.WeekEnding] {
			return nil, fmt.Errorf("失业金所属周重复: %s", item.WeekEnding)
		}
		seen[item.WeekEnding] = true
		if item.PreviousWeek.WeekEnding != week.AddDate(0, 0, -7).Format("2006-01-02") || len(item.FourWeekWindow) != 4 {
			return nil, fmt.Errorf("失业金严格前周或四周窗口不完整")
		}
		for i, p := range item.FourWeekWindow {
			if p.WeekEnding != week.AddDate(0, 0, -7*i).Format("2006-01-02") {
				return nil, fmt.Errorf("失业金四周窗口日期不连续")
			}
		}
		for i, spec := range []struct {
			value *float64
			raw   string
		}{{item.Count, item.RawValue}, {item.WeeklyChange, ""}, {item.FourWeekAverage, ""}} {
			o := Observation{SeriesID: d.Series[i].ID, Period: week.Format("20060102"), Value: spec.value, RawValue: text(spec.raw), SourceSeriesID: result.SeriesID, Source: text(result.Source), FetchedAt: timestamp(result.FetchedAt), PublishedAt: result.PublishedAt, RevisionID: result.RevisionID, DataVersion: "current_visible_revision", Note: item.Note}
			reason := "source_value_missing"
			if i == 1 {
				o.Base = &Base{Period: week.AddDate(0, 0, -7).Format("20060102"), Value: item.PreviousWeek.Count, RawValue: text(item.PreviousWeek.RawValue)}
				reason = "calculation_unavailable"
				if item.Count == nil {
					reason = "current_value_missing"
				} else if item.PreviousWeek.Count == nil {
					reason = "baseline_missing"
				}
			}
			if i == 2 {
				reason = "calculation_unavailable"
				for j, p := range item.FourWeekWindow {
					o.Inputs = append(o.Inputs, Base{Period: week.AddDate(0, 0, -7*j).Format("20060102"), Value: p.Count, RawValue: text(p.RawValue)})
					if p.Count == nil {
						reason = "window_incomplete"
					}
				}
			}
			if err := d.append(o, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
