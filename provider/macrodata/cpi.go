package macrodata

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/bls"
)

// CPI 保留NSA同比与SA月环比各自的水平、来源序列和严格基期。
func CPI(result *bls.CPIResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("CPI结果不能为空")
	}
	nsa, sa := "CUUR0000SA0", "CUSR0000SA0"
	if result.Indicator == "core-cpi" {
		nsa += "L1E"
		sa += "L1E"
	} else if result.Indicator != "cpi" {
		return nil, fmt.Errorf("未知CPI指标: %s", result.Indicator)
	}
	if result.Frequency != "monthly" || result.IndexBase == "" || len(result.Series) != 2 {
		return nil, fmt.Errorf("CPI统计口径不完整")
	}
	ids := map[string]bool{}
	for _, s := range result.Series {
		if ids[s.ID] || (s.ID != nsa && s.ID != sa) || s.SeasonallyAdjusted != (s.ID == sa) {
			return nil, fmt.Errorf("CPI季调序列不匹配")
		}
		ids[s.ID] = true
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	specs := []struct {
		key, measure, id, adjustment string
		derived                      bool
	}{
		{"nsa.index", "index", nsa, "not-seasonally-adjusted", false},
		{"sa.index", "index", sa, "seasonally-adjusted", false},
		{"nsa.yoy_percent", "yoy_percent", nsa, "not-seasonally-adjusted", true},
		{"sa.mom_percent", "mom_percent", sa, "seasonally-adjusted", true},
	}
	for _, spec := range specs {
		s := Series{ID: "us." + result.Indicator + "." + spec.key, Indicator: result.Indicator, Measure: spec.measure, Unit: "index", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: spec.adjustment, PriceBasis: "price-index", IndexBase: result.IndexBase, Definition: spec.id, ValueOrigin: "official"}
		if spec.derived {
			s.Unit, s.ValueOrigin, s.Formula = "percent", "derived", text("(current_index / base_index - 1) * 100")
		}
		d.Series = append(d.Series, s)
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if err := validatePeriod(item.Period, "monthly"); err != nil {
			return nil, err
		}
		if seen[item.Period] {
			return nil, fmt.Errorf("CPI观测所属期重复: %s", item.Period)
		}
		seen[item.Period] = true
		month, _ := time.Parse("200601", item.Period)
		for i, spec := range specs {
			current, base, basePeriod := item.NSA, item.YoYBase, month.AddDate(-1, 0, 0).Format("200601")
			if spec.id == sa {
				current, base, basePeriod = item.SA, item.MoMBase, month.AddDate(0, -1, 0).Format("200601")
			}
			o := Observation{SeriesID: d.Series[i].ID, Period: item.Period, SourceSeriesID: spec.id, Source: text(result.Source), PublishedAt: result.PublishedAt, DataVersion: "current_visible_revision", Note: item.Note}
			if current != nil {
				if current.Period != item.Period || current.SeriesID != spec.id {
					return nil, fmt.Errorf("CPI当期指数所属期或系列不匹配")
				}
				o.Value, o.RawValue, o.FetchedAt = current.Index, text(current.RawValue), timestamp(current.FetchedAt)
			}
			reason := "source_value_missing"
			if spec.derived {
				o.RawValue = nil
				o.Value = item.YoY
				if spec.id == sa {
					o.Value = item.MoM
				}
				o.Base = &Base{Period: basePeriod}
				if base != nil {
					if base.Period != basePeriod || base.SeriesID != spec.id {
						return nil, fmt.Errorf("CPI计算基期或系列不匹配")
					}
					o.Base.Value, o.Base.RawValue = base.Index, text(base.RawValue)
				}
				reason = "calculation_unavailable"
				if current == nil || current.Index == nil {
					reason = "current_value_missing"
				} else if base == nil || base.Index == nil {
					reason = "baseline_missing"
				} else if *base.Index == 0 {
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
