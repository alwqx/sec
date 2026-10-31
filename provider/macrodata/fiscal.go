package macrodata

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/fred"
)

// Fiscal 保留官方月度余额和符号转换值；财年累计是provider计算结果，不冒充官方公布值。
func Fiscal(result *fred.FiscalResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("财政结果不能为空")
	}
	if result.Frequency != "monthly" || result.Measurement != "federal-unified-budget-monthly-flow" || result.PriceBasis != "current-dollars" || result.Unit != "百万美元" || result.SeasonallyAdjusted || result.Annualized {
		return nil, fmt.Errorf("财政统计口径不符合统一导出契约")
	}
	d, err := newDataset("US", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	keys := []string{"receipts", "outlays", "surplus_or_deficit", "deficit_positive"}
	ids := []string{"MTSR133FMS", "MTSO133FMS", "MTSDS133FMS", "MTSDS133FMS"}
	for _, cumulative := range []bool{false, true} {
		for i, key := range keys {
			measure, basis, origin := key, "month", "official"
			var formula *string
			if i == 3 {
				origin, formula = "derived", text("-surplus_or_deficit")
			}
			if cumulative {
				measure, basis, origin, formula = "fytd_"+key, "fiscal-year-to-date", "derived", text("sum_of_current_snapshot_months")
				if i == 3 {
					formula = text("-fytd_surplus_or_deficit")
				}
			}
			d.Series = append(d.Series, Series{ID: "us.fiscal." + measure, Indicator: "fiscal", Measure: measure, Unit: "USD_million", Frequency: "monthly", PeriodBasis: basis, SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", Coverage: "US federal unified budget", Definition: ids[i], ValueOrigin: origin, Formula: formula})
		}
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if err := validatePeriod(item.Period, "monthly"); err != nil {
			return nil, err
		}
		if seen[item.Period] {
			return nil, fmt.Errorf("财政月份重复: %s", item.Period)
		}
		seen[item.Period] = true
		month, _ := time.Parse("200601", item.Period)
		year := month.Year()
		if month.Month() >= time.October {
			year++
		}
		start := fmt.Sprintf("%04d10", year-1)
		if item.FiscalYear != year || item.FiscalMonth != (int(month.Month())+2)%12+1 || item.FYTD.Begin != start || item.FYTD.End != item.Period || item.FYTD.Method != "sum_of_current_snapshot_months" {
			return nil, fmt.Errorf("财政年度或累计所属期不匹配")
		}
		values := []*float64{item.Receipts, item.Outlays, item.SurplusDeficit, item.Deficit, item.FYTD.Receipts, item.FYTD.Outlays, item.FYTD.SurplusDeficit, item.FYTD.Deficit}
		for i, v := range values {
			o := Observation{SeriesID: d.Series[i].ID, Period: item.Period, Value: v, FiscalYear: year, PeriodBegin: item.Period, PeriodEnd: item.Period, SourceSeriesID: ids[i%4], Source: text(result.Source), FetchedAt: timestamp(result.FetchedAt), PublishedAt: result.PublishedAt, RevisionID: result.RevisionID, DataVersion: "current_visible_revision", Note: item.Note}
			reason := "source_value_missing"
			if i < 3 {
				o.RawValue = text(item.RawValues[ids[i]])
			}
			if i == 3 {
				o.Inputs = []Base{{Period: item.Period, Value: item.SurplusDeficit, RawValue: text(item.RawValues[ids[i]])}}
			}
			if i >= 4 {
				o.PeriodBegin = start
				reason = "window_incomplete"
				o.Note += " 财年累计来自provider对当前快照的严格月度求和；完整窗口输入未随结果返回，不重建或当作独立官方累计。"
			}
			if err := d.append(o, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
