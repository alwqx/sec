package macrodata

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/federalreserve"
)

// targetRateSpecs 同时定义序列和观测取值；change为nil时是官方区间上下限。
var targetRateSpecs = []struct {
	measure, unit string
	bound         func(*federalreserve.TargetRateEvent) float64
	change        func(*federalreserve.TargetRateEvent) *float64
}{
	{measure: "lower_bound", unit: "percent", bound: targetLower},
	{measure: "upper_bound", unit: "percent", bound: targetUpper},
	{"lower_change", "basis_points", targetLower, func(e *federalreserve.TargetRateEvent) *float64 { return e.LowerChangeBasisPoints }},
	{"upper_change", "basis_points", targetUpper, func(e *federalreserve.TargetRateEvent) *float64 { return e.UpperChangeBasisPoints }},
}

func targetLower(e *federalreserve.TargetRateEvent) float64 { return e.LowerPercent }
func targetUpper(e *federalreserve.TargetRateEvent) float64 { return e.UpperPercent }

// TargetRate 只输出窗口内实际调整事件，不补每日或未调整的会议；区间前基准和截至日有效区间保留在original。
func TargetRate(result *federalreserve.TargetRateResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("目标利率结果不能为空")
	}
	if result.Indicator != "target-rate" || result.Frequency != "event" || result.Unit != "%" || result.Source == "" {
		return nil, fmt.Errorf("目标利率统计口径不符合统一导出契约")
	}
	exported := result.Events
	if result.Baseline != nil {
		exported = append([]*federalreserve.TargetRateEvent{result.Baseline}, exported...)
	}
	for _, event := range exported {
		if err := validateTargetEvent(event); err != nil {
			return nil, err
		}
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, "event", result)
	if err != nil {
		return nil, err
	}
	for _, spec := range targetRateSpecs {
		s := Series{ID: "us.target-rate." + spec.measure, Indicator: "target-rate", Measure: spec.measure, Unit: spec.unit, Frequency: "event", PeriodBasis: "effective-date", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "not_applicable", Definition: "fomc-target-federal-funds-range", Coverage: "FOMC联邦基金目标区间；每条观测是一次实际调整，不是有效联邦基金利率EFFR", ValueOrigin: "official"}
		if spec.change != nil {
			s.ValueOrigin, s.Formula, s.ChangeMethod = "derived", text("(current_bound - previous_bound) * 100"), "difference_checked_against_official_adjustment"
		}
		d.Series = append(d.Series, s)
	}
	previous := result.Baseline
	for _, event := range result.Events {
		firstRange := event.EffectiveDate == federalreserve.RangeStartDate
		if firstRange && (previous != nil || event.PreviousEffectiveDate != "") {
			return nil, fmt.Errorf("区间机制起点不应有同口径前值")
		}
		if !firstRange && (previous == nil || previous.EffectiveDate != event.PreviousEffectiveDate || previous.EffectiveDate >= event.EffectiveDate) {
			return nil, fmt.Errorf("目标利率前次调整不匹配: %s", event.EffectiveDate)
		}
		for _, spec := range targetRateSpecs {
			o := Observation{SeriesID: "us.target-rate." + spec.measure, Period: strings.ReplaceAll(event.EffectiveDate, "-", ""), Source: text(event.SourceURL), FetchedAt: timestamp(event.FetchedAt), PublishedDate: event.AnnouncedDate, PublishedAt: event.PublishedAt, DataVersion: "current_visible_revision"}
			if event.ReleaseStatus != "available" {
				o.Note = event.ReleaseNote
			}
			if spec.change == nil {
				bound := spec.bound(event)
				o.Value, o.RawValue = &bound, text(event.Raw.Level)
			} else if firstRange {
				if spec.change(event) != nil {
					return nil, fmt.Errorf("区间机制起点不应有同口径前值")
				}
				o.Note = "由单一目标切换为区间，无同口径前值；官方调整幅度原文保留在original。" + o.Note
			} else {
				before := spec.bound(previous)
				o.Value = spec.change(event)
				if o.Value == nil || math.Abs(*o.Value-(spec.bound(event)-before)*100) >= 0.001 {
					return nil, fmt.Errorf("目标利率变化与前次区间不一致: %s", event.EffectiveDate)
				}
				o.Base = &Base{Period: strings.ReplaceAll(previous.EffectiveDate, "-", ""), Value: &before, RawValue: text(previous.Raw.Level)}
			}
			// 校验已保证只有区间机制起点的变化值为空。
			if err := d.append(o, "methodology_break"); err != nil {
				return nil, err
			}
		}
		previous = event
	}
	return d, nil
}

// validateTargetEvent 同时用于窗口内事件和区间前基准，后者也会进入base和original。
func validateTargetEvent(event *federalreserve.TargetRateEvent) error {
	if event == nil {
		return fmt.Errorf("目标利率事件不能为空")
	}
	// 下载失败不输出部分结果，否则同一查询会因网络状况得到不同的公布信息。
	if event.ReleaseFetchFailed {
		return fmt.Errorf("目标利率公布信息下载失败（%s）: %s", event.EffectiveDate, event.ReleaseNote)
	}
	effective, err := time.Parse("2006-01-02", event.EffectiveDate)
	if err != nil || effective.Format("2006-01-02") != event.EffectiveDate {
		return fmt.Errorf("无效目标利率生效日期: %s", event.EffectiveDate)
	}
	if event.LowerPercent < 0 || event.LowerPercent >= event.UpperPercent {
		return fmt.Errorf("目标区间上下限无效: %s", event.EffectiveDate)
	}
	if event.SourceURL == "" || event.FetchedAt.IsZero() {
		return fmt.Errorf("目标利率事件缺少来源或抓取时间: %s", event.EffectiveDate)
	}
	return validateTargetRelease(event)
}

// validateTargetRelease 确保公布状态与字段一致，不把缺失或矛盾的公布时间伪装成已核验。
func validateTargetRelease(event *federalreserve.TargetRateEvent) error {
	valid := false
	switch event.ReleaseStatus {
	case "available":
		valid = event.AnnouncedDate != nil && event.PublishedAt != nil && event.PublishedAt.Format("2006-01-02") == *event.AnnouncedDate
	case "date-only":
		valid = event.AnnouncedDate != nil && event.PublishedAt == nil
	case "unavailable":
		valid = event.AnnouncedDate == nil && event.PublishedAt == nil
	}
	if !valid {
		return fmt.Errorf("目标利率公布信息状态不一致: %s", event.EffectiveDate)
	}
	if event.AnnouncedDate != nil {
		announced, err := time.Parse("2006-01-02", *event.AnnouncedDate)
		if err != nil || announced.Format("2006-01-02") != *event.AnnouncedDate || *event.AnnouncedDate > event.EffectiveDate {
			return fmt.Errorf("目标利率公布日期无效: %s", *event.AnnouncedDate)
		}
	}
	return nil
}
