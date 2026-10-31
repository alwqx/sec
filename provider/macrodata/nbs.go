package macrodata

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/nbs"
)

// Retail 保留单月与年初累计两种所属期，不从累计金额或增速推算单月值。
func Retail(items []nbs.RetailObservation, begin, end string) (*Dataset, error) {
	if items == nil {
		items = []nbs.RetailObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.AmountUnit != "亿元" || item.PriceBasis != "nominal" || item.SeasonallyAdjusted {
			return nil, fmt.Errorf("社零统计口径不符合统一导出契约")
		}
		for _, spec := range []struct {
			measure, sourceID string
			value             *float64
			cumulative        bool
		}{
			{"level", item.AmountSeriesID, item.Amount, false}, {"yoy_percent", item.YoYSeriesID, item.YoY, false},
			{"ytd_level", item.YTDAmountSeriesID, item.YTDAmount, true}, {"ytd_yoy_percent", item.YTDGrowthSeriesID, item.YTDGrowth, true},
		} {
			unit := "CNY_100_million"
			if spec.measure == "yoy_percent" || spec.measure == "ytd_yoy_percent" {
				unit = "percent"
			}
			s := Series{ID: "cn.retail." + spec.measure, Indicator: "retail", Measure: spec.measure, Unit: unit, Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Definition: "national-total"}
			o := Observation{Period: item.Period, Value: spec.value, SourceSeriesID: spec.sourceID, Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), DataVersion: "current_visible_revision", Note: item.Note}
			if err := d.appendNBS(s, o, spec.cumulative, item.YTDPeriod); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// Property 使用provider已核验的历史范围版本划分序列，金额和面积不混用单位。
func Property(result *nbs.PropertyResult, begin, end string) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("房地产结果不能为空")
	}
	d, err := newDataset("CN", begin, end, result)
	if err != nil {
		return nil, err
	}
	for _, item := range result.Observations {
		unit, price := "square_meters_10000", "not_applicable"
		switch item.Component {
		case "sales-value":
			unit, price = "CNY_100_million", "nominal"
		case "sales-area", "starts-area":
		default:
			return nil, fmt.Errorf("未知房地产指标: %s", item.Component)
		}
		expectedUnit := "万平方米"
		if item.Component == "sales-value" {
			expectedUnit = "亿元"
		}
		if item.AmountUnit != expectedUnit || item.PriceBasis != price || item.SeasonallyAdjusted || item.ScopeVersion == "" {
			return nil, fmt.Errorf("房地产统计口径不符合统一导出契约")
		}
		for _, spec := range []struct {
			measure, sourceID string
			value             *float64
		}{
			{"ytd_level", item.AmountSeriesID, item.YTDAmount}, {"ytd_yoy_percent", item.GrowthSeriesID, item.YTDGrowth},
		} {
			measureUnit := unit
			if spec.measure == "ytd_yoy_percent" {
				measureUnit = "percent"
			}
			s := Series{ID: "cn.property." + item.Component + "." + item.ScopeVersion + "." + spec.measure, Indicator: "property." + item.Component, Measure: spec.measure, Unit: measureUnit, Frequency: "monthly", PeriodBasis: "year-to-date", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: price, ValueOrigin: "official", Definition: item.ScopeVersion}
			o := Observation{Period: item.Period, Value: spec.value, SourceSeriesID: spec.sourceID, Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), PublishedAt: item.PublishedAt, DataVersion: "current_visible_revision", Note: item.Note}
			if err := d.appendNBS(s, o, true, item.YTDPeriod); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

func (d *Dataset) appendNBS(s Series, o Observation, cumulative bool, ytd string) error {
	return d.appendNBSReason(s, o, cumulative, ytd, "source_value_missing")
}

func (d *Dataset) appendNBSReason(s Series, o Observation, cumulative bool, ytd, reason string) error {
	month, err := time.Parse("200601", o.Period)
	if err != nil || month.Format("200601") != o.Period {
		return fmt.Errorf("无效所属月份: %s", o.Period)
	}
	o.SeriesID, o.PeriodBegin, o.PeriodEnd = s.ID, o.Period, o.Period
	if cumulative {
		s.PeriodBasis = "year-to-date"
		o.PeriodBegin = month.Format("2006") + "01"
		if ytd != o.PeriodBegin+"-"+o.PeriodEnd {
			return fmt.Errorf("累计所属期与月份不一致: %s", ytd)
		}
	}
	// 不生成provider没有返回的月份，不把2月累计分摊给1月。
	for _, existing := range d.Observations {
		if existing.SeriesID == o.SeriesID && existing.Period == o.Period {
			return fmt.Errorf("序列所属期重复: %s %s", o.SeriesID, o.Period)
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
	return d.append(o, reason)
}
