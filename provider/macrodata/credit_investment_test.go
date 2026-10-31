package macrodata

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
)

func creditSample(metric string) *fred.CreditResult {
	r := &fred.CreditResult{Metric: metric, Frequency: "quarterly", Unit: "%", PriceBasis: "not_applicable", ChangeUnit: "percentage_points", ChangeMethod: "difference", Definition: "统计总体", Begin: "2026Q1", End: "2026Q1", Source: "source", FetchedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	o := fred.CreditObservation{CreditValue: fred.CreditValue{Period: "2026Q1", Level: value(-5), RawLevel: "-5.00"}, PreviousBase: fred.CreditValue{Period: "2025Q4", Level: value(0), RawLevel: "0.0"}, YearAgoBase: fred.CreditValue{Period: "2025Q1", Level: value(-10), RawLevel: "-10"}, PreviousChangePP: value(-5), YearAgoChangePP: value(5)}
	switch metric {
	case "bank":
		r.SeriesID, r.Frequency, r.Unit, r.Measurement, r.PeriodBasis, r.PriceBasis, r.ChangeUnit, r.ChangeMethod, r.Methodology, r.SeasonallyAdjusted = "LOANINV", "monthly", "十亿美元", "monthly_average_stock", "pro_rata_average_of_weekly_levels", "current_dollars", "percent", "level_ratio_not_break_adjusted", "current_h8_monthly_levels", true
		r.Begin, r.End, o.Period = "202601", "202601", "202601"
		o.Level, o.RawLevel = value(120), "120.00"
		o.PreviousBase = fred.CreditValue{Period: "202512", Level: value(100), RawLevel: "100"}
		o.YearAgoBase = fred.CreditValue{Period: "202501", Level: value(80), RawLevel: "80"}
		o.PreviousChangePP, o.YearAgoChangePP, o.PreviousChangePercent, o.YearAgoChangePercent = nil, nil, value(20), value(50)
	case "standards":
		r.SeriesID, r.Measurement, r.PeriodBasis, r.Methodology = "DRTSCILM", "net_tightening_share", "survey_quarter", "sloos_domestic_banks_large_middle_market_ci"
	case "debt-service":
		r.SeriesID, r.Measurement, r.PeriodBasis, r.Methodology, r.SeasonallyAdjusted = "TDSP", "required_payments_to_disposable_income", "calendar_quarter", "credit_bureau_based_dsr", true
		o.Level, o.RawLevel, o.YearAgoBase.Level, o.YearAgoBase.RawLevel = value(10), "10", value(9), "9"
		o.PreviousChangePP, o.YearAgoChangePP = value(10), value(1)
	}
	r.Observations = []fred.CreditObservation{o}
	return r
}

func investmentSample(component string) *fred.InvestmentResult {
	r := &fred.InvestmentResult{Indicator: "investment", Component: component, SeriesID: "HOUST", Frequency: "monthly", Measure: "housing_units", PriceBasis: "not_applicable", Unit: "千套（季调折年）", SeasonallyAdjusted: true, LevelAnnualized: true, Begin: "202601", End: "202601", Source: "source", FetchedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	o := fred.InvestmentObservation{InvestmentValue: fred.InvestmentValue{Period: "202601", Level: value(120), RawLevel: "120.00"}, PreviousBase: fred.InvestmentValue{Period: "202512", Level: value(100), RawLevel: "100"}, YoYBase: fred.InvestmentValue{Period: "202501", Level: value(80), RawLevel: "80"}, PreviousGrowth: value(20), YoY: value(50)}
	if component == "building-permits" {
		r.SeriesID = "PERMIT"
	}
	if component == "real" || component == "nominal" {
		r.Indicator, r.Component, r.Frequency, r.Measure, r.PriceBasis, r.Begin, r.End = "equipment_investment", "equipment", "quarterly", "investment_flow", component, "2026Q1", "2026Q1"
		r.SeriesID, r.BEAAccountCode, r.Unit = "Y033RC1Q027SBEA", "Y033RC", "十亿美元（当期美元，季调折年）"
		if component == "real" {
			year := 2017
			r.SeriesID, r.BEAAccountCode, r.Unit, r.ReferenceYear = "Y033RX1Q020SBEA", "Y033RX", "十亿美元（2017年链式美元，季调折年）", &year
		}
		o.Period, o.PreviousBase.Period, o.YoYBase.Period = "2026Q1", "2025Q4", "2025Q1"
	}
	r.Observations = []fred.InvestmentObservation{o}
	return r
}

func TestCreditDefinitionsAndStrictChanges(t *testing.T) {
	for _, metric := range []string{"bank", "standards", "debt-service"} {
		t.Run(metric, func(t *testing.T) {
			r := creditSample(metric)
			d, err := Credit(r)
			require.NoError(t, err)
			require.Len(t, d.Series, 3)
			require.Len(t, d.Observations, 3)
			require.Equal(t, r.Methodology, d.Series[0].Methodology)
			require.Equal(t, r.ChangeMethod, d.Series[2].ChangeMethod)
			require.Empty(t, d.Series[0].ChangeMethod)
			require.Equal(t, *r.Observations[0].Level, *d.Observations[0].Value)
			require.Equal(t, r.Observations[0].RawLevel, *d.Observations[0].RawValue)
			require.Equal(t, r.FetchedAt, *d.Observations[0].FetchedAt)
			require.Nil(t, d.Observations[0].PublishedAt)
			require.Nil(t, d.Observations[0].RevisionID)
			require.Equal(t, r.Observations[0].PreviousBase.Period, d.Observations[2].Base.Period)
			require.Nil(t, d.Observations[1].RawValue)
			require.Equal(t, "derived", d.Series[1].ValueOrigin)
			require.False(t, d.Series[0].Annualized)
			if metric == "bank" {
				require.Equal(t, "pro_rata_average_of_weekly_levels", d.Series[0].PeriodBasis)
				require.Equal(t, "nominal", d.Series[0].PriceBasis)
				require.Equal(t, "percent", d.Series[1].Unit)
				require.Equal(t, 20.0, *d.Observations[2].Value)
			} else {
				require.Equal(t, "percentage_points", d.Series[1].Unit)
				require.Equal(t, "current_level - base_level", *d.Series[1].Formula)
				require.Equal(t, "qoq_percentage_points", d.Series[2].Measure)
				require.Equal(t, "available", d.Observations[2].Status, "零基数仍可计算百分点差")
				if metric == "standards" {
					require.Equal(t, -5.0, *d.Observations[0].Value)
					require.Equal(t, 5.0, *d.Observations[1].Value, "负基数使用差值而非百分比")
					require.Equal(t, "2026Q1", d.Observations[0].Period, "不把调查季度回移")
					require.Equal(t, "SLOOS", d.Series[0].Survey)
					require.Equal(t, "not-seasonally-adjusted", d.Series[0].SeasonalAdjustment)
				}
			}
			var original fred.CreditResult
			require.NoError(t, json.Unmarshal(d.Original, &original))
			require.Equal(t, *r, original)
			r.Observations[0].YearAgoBase.Level = nil
			r.Observations[0].YearAgoChangePercent, r.Observations[0].YearAgoChangePP = nil, nil
			d, err = Credit(r)
			require.NoError(t, err)
			require.Equal(t, "baseline_missing", *d.Observations[1].MissingReason)
			require.Equal(t, "available", d.Observations[0].Status)
			require.Equal(t, "available", d.Observations[2].Status)
		})
	}
}

func TestInvestmentUnitsAndIndependentPriceSeries(t *testing.T) {
	ids := map[string]bool{}
	for _, component := range []string{"housing-starts", "building-permits", "real", "nominal"} {
		r := investmentSample(component)
		d, err := Investment(r)
		require.NoError(t, err)
		require.False(t, ids[d.Series[0].ID], "不同分项／价格口径不得重用序列ID")
		ids[d.Series[0].ID] = true
		require.Len(t, d.Series, 3)
		require.True(t, d.Series[0].Annualized)
		require.False(t, d.Series[1].Annualized)
		require.Equal(t, 120.0, *d.Observations[0].Value, "不将折年水平除以12或4")
		require.Equal(t, 20.0, *d.Observations[2].Value, "不把provider未年化增速转成年化增速")
		require.Equal(t, r.Observations[0].PreviousBase.Period, d.Observations[2].Base.Period)
		require.Equal(t, "100", *d.Observations[2].Base.RawValue)
		require.Equal(t, "percent", d.Series[1].Unit)
		if component == "real" {
			require.Equal(t, "real-chained", d.Series[0].PriceBasis)
			require.Equal(t, 2017, *d.Series[0].ReferenceYear)
			require.False(t, *d.Series[0].ComponentsAdditive)
			require.Nil(t, d.Series[1].ComponentsAdditive)
		} else if component == "nominal" {
			require.Equal(t, "nominal", d.Series[0].PriceBasis)
			require.Nil(t, d.Series[0].ReferenceYear)
		} else {
			require.Equal(t, component, d.Series[0].Indicator, "住宅开工与建筑许可不共用指标")
			require.Equal(t, component, d.Series[2].Indicator)
			require.Equal(t, "housing_units_thousand", d.Series[0].Unit)
			require.Equal(t, "not_applicable", d.Series[0].PriceBasis)
		}
		var original fred.InvestmentResult
		require.NoError(t, json.Unmarshal(d.Original, &original))
		require.Equal(t, *r, original)
		r.Observations[0].PreviousBase.Level, r.Observations[0].PreviousGrowth = value(0), nil
		r.Observations[0].Level, r.Observations[0].YoY = value(0), value(-100)
		d, err = Investment(r)
		require.NoError(t, err)
		require.Equal(t, "zero_baseline", *d.Observations[2].MissingReason)
		require.Equal(t, "available", d.Observations[0].Status, "零水平不是缺失")
		require.Equal(t, -100.0, *d.Observations[1].Value)
		r.Observations[0].Level, r.Observations[0].YoY = nil, nil
		d, err = Investment(r)
		require.NoError(t, err)
		require.Equal(t, "source_value_missing", *d.Observations[0].MissingReason)
		require.Equal(t, "current_value_missing", *d.Observations[1].MissingReason)
		r.Observations = nil
		d, err = Investment(r)
		require.NoError(t, err)
		require.NotNil(t, d.Observations)
		require.Empty(t, d.Observations)
	}
	r := creditSample("bank")
	r.Observations[0].PreviousBase.Level, r.Observations[0].PreviousChangePercent = value(0), nil
	d, err := Credit(r)
	require.NoError(t, err)
	require.Equal(t, "zero_baseline", *d.Observations[2].MissingReason)
}

func TestAppendPeriodicChangesNamesPreviousMeasureByFrequency(t *testing.T) {
	for _, test := range []struct {
		frequency, period, begin, previous, yearAgo, measure string
	}{
		{"monthly", "202601", "202601", "202512", "202501", "mom_percentage_points"},
		{"quarterly", "2026Q1", "2026Q1", "2025Q4", "2025Q1", "qoq_percentage_points"},
	} {
		d, err := newDatasetFrequency("US", test.begin, test.period, test.frequency, map[string]string{})
		require.NoError(t, err)
		item := periodicObservation{current: periodicValue{test.period, value(2), "2"}, yearAgo: periodicValue{test.yearAgo, value(1), "1"}, previous: periodicValue{test.previous, value(0), "0"}, yoy: value(1), change: value(2)}
		level := Series{ID: "us.test", Measure: "rate", Unit: "percent", Frequency: test.frequency}
		require.NoError(t, appendPeriodicChanges(d, level, monthlyProvenance{}, []periodicObservation{item}, periodicRule{method: "difference", kind: pointsChange}))
		require.Equal(t, test.measure, d.Series[2].Measure)
		require.Equal(t, "us.test."+test.measure, d.Series[2].ID)
	}
}

func TestCreditInvestmentRejectInconsistentResults(t *testing.T) {
	for _, mutate := range []func(*fred.CreditResult){
		func(r *fred.CreditResult) { r.Metric = "unknown" },
		func(r *fred.CreditResult) { r.SeriesID = "TDSP" },
		func(r *fred.CreditResult) { r.Frequency = "monthly" },
		func(r *fred.CreditResult) { r.PeriodBasis = "calendar_quarter" },
		func(r *fred.CreditResult) { r.ChangeUnit = "percent" },
		func(r *fred.CreditResult) { r.ChangeMethod = "level_ratio" },
		func(r *fred.CreditResult) { r.SeasonallyAdjusted = true },
		func(r *fred.CreditResult) { r.Methodology = "old_method" },
		func(r *fred.CreditResult) { r.Unit = "十亿美元" },
		func(r *fred.CreditResult) { r.LevelAnnualized = true },
		func(r *fred.CreditResult) { r.GrowthAnnualized = true },
		func(r *fred.CreditResult) { r.Observations[0].PreviousBase.Period = "2025Q3" },
		func(r *fred.CreditResult) { r.Observations[0].Period = "2026Q5" },
		func(r *fred.CreditResult) { r.Begin = "2026Q2"; r.End = "2026Q2" },
		func(r *fred.CreditResult) { r.Observations[0].Level = value(math.Inf(1)) },
		func(r *fred.CreditResult) { r.Observations = append(r.Observations, r.Observations[0]) },
		func(r *fred.CreditResult) { r.Observations[0].PreviousChangePercent = value(1) },
		func(r *fred.CreditResult) { r.Observations[0].YearAgoChangePercent = value(1) },
		func(r *fred.CreditResult) { r.Observations[0].YearAgoBase.Level = nil },
		func(r *fred.CreditResult) { r.Observations[0].Level = nil },
	} {
		r := creditSample("standards")
		mutate(r)
		d, err := Credit(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*fred.InvestmentResult){
		func(r *fred.InvestmentResult) { r.SeriesID = "Y033RC1Q027SBEA" },
		func(r *fred.InvestmentResult) { r.Component = "housing-starts" },
		func(r *fred.InvestmentResult) { r.PriceBasis = "nominal" },
		func(r *fred.InvestmentResult) { r.Unit = "美元" },
		func(r *fred.InvestmentResult) { r.ReferenceYear = nil },
		func(r *fred.InvestmentResult) { year := 2012; r.ReferenceYear = &year },
		func(r *fred.InvestmentResult) { r.BEAAccountCode = "Y033RC" },
		func(r *fred.InvestmentResult) { r.SeasonallyAdjusted = false },
		func(r *fred.InvestmentResult) { r.LevelAnnualized = false },
		func(r *fred.InvestmentResult) { r.GrowthAnnualized = true },
		func(r *fred.InvestmentResult) { r.Observations[0].YoYBase.Period = "2025Q2" },
		func(r *fred.InvestmentResult) { r.Observations[0].PreviousBase.Level = value(math.NaN()) },
		func(r *fred.InvestmentResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := investmentSample("real")
		mutate(r)
		d, err := Investment(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, mutate := range []func(*fred.CreditResult){
		func(r *fred.CreditResult) { r.Observations[0].PreviousChangePP = value(1) },
		func(r *fred.CreditResult) { r.Observations[0].YearAgoChangePP = value(1) },
		func(r *fred.CreditResult) { r.Observations[0].PreviousBase.Level = value(0) },
	} {
		r := creditSample("bank")
		mutate(r)
		d, err := Credit(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, component := range []string{"housing-starts", "building-permits"} {
		r := investmentSample(component)
		r.BEAAccountCode = "Y033RC"
		d, err := Investment(r)
		require.Error(t, err, "住宅序列不得携带BEA账户代码")
		require.Nil(t, d)
	}
	_, err := Credit(nil)
	require.Error(t, err)
	_, err = Investment(nil)
	require.Error(t, err)
}
