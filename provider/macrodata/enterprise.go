package macrodata

import (
	"fmt"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fred"
)

// Enterprise 分开利润流量、月末库存和新订单，不混用频率或季调折年口径。
func Enterprise(result *fred.EnterpriseResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("企业指标结果不能为空")
	}
	specs := map[string]struct{ indicator, id, frequency, unit, basis, measurement string }{
		"profit":     {"corporate-profits", "CPROFIT", "quarterly", "USD_billion", "quarter", "quarterly_flow"},
		"inventory":  {"business-inventories", "BUSINV", "monthly", "USD_million", "month-end", "month_end_stock"},
		"new-orders": {"new-orders", "AMTMNO", "monthly", "USD_million", "month", "monthly_flow"},
	}
	spec, ok := specs[result.Metric]
	if !ok || result.SeriesID != spec.id || result.Frequency != spec.frequency || result.Measurement != spec.measurement || result.PriceBasis != "current_dollars" || !result.SeasonallyAdjusted || result.LevelAnnualized != (result.Metric == "profit") || result.GrowthAnnualized || result.Definition == "" || (result.Metric == "profit" && result.TaxBasis != "before_tax") {
		return nil, fmt.Errorf("企业指标统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, spec.frequency, result)
	if err != nil {
		return nil, err
	}
	previousMeasure := "mom_percent"
	if spec.frequency == "quarterly" {
		previousMeasure = "qoq_percent"
	}
	for _, measure := range []string{"level", "yoy_percent", previousMeasure} {
		s := Series{ID: "us." + spec.indicator + "." + measure, Indicator: spec.indicator, Measure: measure, Unit: spec.unit, Frequency: spec.frequency, PeriodBasis: spec.basis, SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "nominal", Annualized: result.LevelAnnualized, Coverage: result.Definition, Definition: spec.id, TaxBasis: result.TaxBasis, ValueOrigin: "official"}
		if measure != "level" {
			s.Unit, s.Annualized, s.ValueOrigin, s.Formula = "percent", false, "derived", text("(current_level / base_level - 1) * 100")
		}
		d.Series = append(d.Series, s)
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if err := validatePeriod(item.Period, spec.frequency); err != nil {
			return nil, err
		}
		if seen[item.Period] {
			return nil, fmt.Errorf("企业指标所属期重复: %s", item.Period)
		}
		seen[item.Period] = true
		previous, yearAgo := enterpriseBasePeriods(item.Period, spec.frequency)
		for i, field := range []struct {
			value    *float64
			base     *fred.EnterpriseValue
			expected string
		}{{item.Level, nil, ""}, {item.YoY, &item.YoYBase, yearAgo}, {item.PreviousGrowth, &item.PreviousBase, previous}} {
			o := Observation{SeriesID: d.Series[i].ID, Period: item.Period, Value: field.value, AsOfDate: item.AsOfDate, SourceSeriesID: spec.id, Source: text(result.Source), FetchedAt: timestamp(result.FetchedAt), PublishedAt: result.PublishedAt, RevisionID: result.RevisionID, DataVersion: "current_visible_revision", Note: item.Note}
			reason := "source_value_missing"
			if i == 0 {
				o.RawValue = text(item.RawLevel)
			} else {
				if field.base.Period != field.expected {
					return nil, fmt.Errorf("企业指标计算基期不匹配")
				}
				o.Base = &Base{Period: field.base.Period, Value: field.base.Level, RawValue: text(field.base.RawLevel)}
				reason = "calculation_unavailable"
				if item.Level == nil {
					reason = "current_value_missing"
				} else if field.base.Level == nil {
					reason = "baseline_missing"
				} else if *field.base.Level <= 0 {
					reason = "nonpositive_baseline"
				}
			}
			if err := d.append(o, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

func enterpriseBasePeriods(period, frequency string) (string, string) {
	if frequency == "quarterly" {
		year, _ := strconv.Atoi(period[:4])
		q := year*4 + int(period[5]-'1')
		format := func(n int) string { return fmt.Sprintf("%04dQ%d", n/4, n%4+1) }
		return format(q - 1), format(q - 4)
	}
	month, _ := time.Parse("200601", period)
	return month.AddDate(0, -1, 0).Format("200601"), month.AddDate(-1, 0, 0).Format("200601")
}
