package macrodata

import (
	"fmt"

	"github.com/alwqx/sec/provider/fred"
)

// Credit 保留H.8月平均余额、SLOOS调查净比例和当前征信方法的偿债比率。
func Credit(result *fred.CreditResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("信用指标结果不能为空")
	}
	specs := map[string]struct {
		indicator, id, frequency, unit, measurement, basis, price, changeUnit, method, methodology string
		adjusted                                                                                   bool
	}{
		"bank":         {"bank-credit", "LOANINV", "monthly", "十亿美元", "monthly_average_stock", "pro_rata_average_of_weekly_levels", "current_dollars", "percent", "level_ratio_not_break_adjusted", "current_h8_monthly_levels", true},
		"standards":    {"lending-standards", "DRTSCILM", "quarterly", "%", "net_tightening_share", "survey_quarter", "not_applicable", "percentage_points", "difference", "sloos_domestic_banks_large_middle_market_ci", false},
		"debt-service": {"debt-service", "TDSP", "quarterly", "%", "required_payments_to_disposable_income", "calendar_quarter", "not_applicable", "percentage_points", "difference", "credit_bureau_based_dsr", true},
	}
	spec, ok := specs[result.Metric]
	if !ok || result.SeriesID != spec.id || result.Frequency != spec.frequency || result.Unit != spec.unit || result.Measurement != spec.measurement || result.PeriodBasis != spec.basis || result.PriceBasis != spec.price || result.ChangeUnit != spec.changeUnit || result.ChangeMethod != spec.method || result.Methodology != spec.methodology || result.SeasonallyAdjusted != spec.adjusted || result.LevelAnnualized || result.GrowthAnnualized || result.Definition == "" {
		return nil, fmt.Errorf("信用指标统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, spec.frequency, result)
	if err != nil {
		return nil, err
	}
	s := Series{ID: "us." + spec.indicator, Indicator: spec.indicator, Measure: "rate", Unit: "percent", Frequency: spec.frequency, PeriodBasis: spec.basis, SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "not_applicable", Definition: spec.id, Coverage: result.Definition, Methodology: result.Methodology, ValueOrigin: "official"}
	if spec.adjusted {
		s.SeasonalAdjustment = "seasonally-adjusted"
	}
	if result.Metric == "bank" {
		s.Measure, s.Unit, s.PriceBasis = "level", "USD_billion", "nominal"
	}
	if result.Metric == "standards" {
		s.Survey = "SLOOS"
	}
	items := make([]periodicObservation, 0, len(result.Observations))
	point := func(v fred.CreditValue) periodicValue { return periodicValue{v.Period, v.Level, v.RawLevel} }
	for _, o := range result.Observations {
		yoy, previous, unexpectedYoY, unexpectedPrevious := o.YearAgoChangePercent, o.PreviousChangePercent, o.YearAgoChangePP, o.PreviousChangePP
		if result.Metric != "bank" {
			yoy, previous, unexpectedYoY, unexpectedPrevious = o.YearAgoChangePP, o.PreviousChangePP, o.YearAgoChangePercent, o.PreviousChangePercent
		}
		if unexpectedYoY != nil || unexpectedPrevious != nil {
			return nil, fmt.Errorf("信用指标变化单位不符合统一导出契约: %s", o.Period)
		}
		items = append(items, periodicObservation{current: point(o.CreditValue), yearAgo: point(o.YearAgoBase), previous: point(o.PreviousBase), yoy: yoy, change: previous, note: o.Note})
	}
	if err := appendPeriodicChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, periodicRule{method: result.ChangeMethod, kind: percentOrPoints(result.Metric != "bank")}); err != nil {
		return nil, err
	}
	return d, nil
}

// Investment 不转换SAAR水平为当期量，不将住房单元数与投资金额拼接。
func Investment(result *fred.InvestmentResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("投资结果不能为空")
	}
	s := Series{ID: "us.investment." + result.Component, Indicator: result.Component, Measure: "level", Unit: "housing_units_thousand", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "not_applicable", Annualized: true, ValueOrigin: "official", Definition: result.SeriesID}
	valid := result.Indicator == "investment" && result.Frequency == "monthly" && result.Measure == "housing_units" && result.PriceBasis == "not_applicable" && result.Unit == "千套（季调折年）" && result.ReferenceYear == nil && result.BEAAccountCode == ""
	switch result.Component {
	case "housing-starts":
		valid = valid && result.SeriesID == "HOUST"
		s.Coverage = "私人住宅新开工住房单元，含单户及多户住宅；非建筑物数量或投资金额"
	case "building-permits":
		valid = valid && result.SeriesID == "PERMIT"
		s.Coverage = "签发许可地区获准新建的私人住房单元；非许可证张数，不含无需许可地区"
	case "equipment":
		valid = result.Indicator == "equipment_investment" && result.Frequency == "quarterly" && result.Measure == "investment_flow"
		s.ID, s.Indicator, s.Frequency, s.PeriodBasis, s.Unit = "us.equipment-investment."+result.PriceBasis, "equipment-investment", "quarterly", "quarter", "USD_billion"
		s.Coverage = "NIPA私人非住宅固定投资中的设备；不含建筑、知识产权产品或家庭耐用品"
		s.PriceBasis = "nominal"
		if result.PriceBasis == "real" {
			valid = valid && result.SeriesID == "Y033RX1Q020SBEA" && result.BEAAccountCode == "Y033RX" && result.Unit == "十亿美元（2017年链式美元，季调折年）" && result.ReferenceYear != nil && *result.ReferenceYear == 2017
			additive := false
			s.PriceBasis, s.ReferenceYear, s.ComponentsAdditive = "real-chained", result.ReferenceYear, &additive
		} else {
			valid = valid && result.PriceBasis == "nominal" && result.SeriesID == "Y033RC1Q027SBEA" && result.BEAAccountCode == "Y033RC" && result.Unit == "十亿美元（当期美元，季调折年）" && result.ReferenceYear == nil
		}
	default:
		valid = false
	}
	if !valid || !result.SeasonallyAdjusted || !result.LevelAnnualized || result.GrowthAnnualized {
		return nil, fmt.Errorf("投资统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, s.Frequency, result)
	if err != nil {
		return nil, err
	}
	point := func(v fred.InvestmentValue) periodicValue { return periodicValue{v.Period, v.Level, v.RawLevel} }
	items := make([]periodicObservation, 0, len(result.Observations))
	for _, o := range result.Observations {
		items = append(items, periodicObservation{current: point(o.InvestmentValue), yearAgo: point(o.YoYBase), previous: point(o.PreviousBase), yoy: o.YoY, change: o.PreviousGrowth, note: o.Note})
	}
	if err := appendPeriodicChanges(d, s, monthlyProvenance{result.Source, result.FetchedAt, result.PublishedAt, result.RevisionID}, items, periodicRule{method: "level_ratio"}); err != nil {
		return nil, err
	}
	return d, nil
}
