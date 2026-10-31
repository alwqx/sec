package macrodata

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/alwqx/sec/provider/nbs"
)

// quarterYTDBegin 返回季度累计的起始季度，如2026Q3从2026Q1开始。
func quarterYTDBegin(period string) string { return period[:4] + "Q1" }

// CNGDP 分开不变价金额、官方同比指数、指数减100的计算同比和官方季调环比；分段基期不统一标记。
func CNGDP(result *nbs.GDPResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("GDP结果不能为空")
	}
	if result.Frequency != "quarterly" || result.PriceBasis != "constant-prices" || result.ValueUnit != "亿元" || result.GrowthUnit != "%" || result.RealBaseYear != nil {
		return nil, fmt.Errorf("GDP统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("CN", result.Begin, result.End, "quarterly", result)
	if err != nil {
		return nil, err
	}
	sources := map[string]nbs.GDPSeries{}
	for _, s := range result.Series {
		if s.SeriesID == "" || s.Source == "" || s.FetchedAt.IsZero() || s.SeasonallyAdjusted != (s.Key == "qoq") {
			return nil, fmt.Errorf("GDP序列元数据不符合统一导出契约: %s", s.Key)
		}
		sources[s.Key] = s
	}
	specs := []struct {
		measure, key, unit, price, formula string
		cumulative                         bool
		value                              func(nbs.GDPObservation) *float64
		input                              func(nbs.GDPObservation) *float64
	}{
		{"level", "real-value", "CNY_100_million", "constant-prices", "", false, func(o nbs.GDPObservation) *float64 { return o.RealValue }, nil},
		{"ytd_level", "real-ytd-value", "CNY_100_million", "constant-prices", "", true, func(o nbs.GDPObservation) *float64 { return o.RealYTDValue }, nil},
		{"yoy_index", "yoy-index", "index", "real", "", false, func(o nbs.GDPObservation) *float64 { return o.YoYIndex }, nil},
		{"ytd_yoy_index", "ytd-yoy-index", "index", "real", "", true, func(o nbs.GDPObservation) *float64 { return o.YTDYoYIndex }, nil},
		{"yoy_percent", "yoy-index", "percent", "real", "official_index - 100", false, func(o nbs.GDPObservation) *float64 { return o.YoY }, func(o nbs.GDPObservation) *float64 { return o.YoYIndex }},
		{"ytd_yoy_percent", "ytd-yoy-index", "percent", "real", "official_index - 100", true, func(o nbs.GDPObservation) *float64 { return o.YTDYoY }, func(o nbs.GDPObservation) *float64 { return o.YTDYoYIndex }},
		{"qoq_percent", "qoq", "percent", "real", "", false, func(o nbs.GDPObservation) *float64 { return o.QoQ }, nil},
	}
	for _, spec := range specs {
		source, ok := sources[spec.key]
		if !ok {
			return nil, fmt.Errorf("GDP缺少序列: %s", spec.key)
		}
		s := Series{ID: "cn.gdp." + spec.measure, Indicator: "gdp", Measure: spec.measure, Unit: spec.unit, Frequency: "quarterly", PeriodBasis: "quarter", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: spec.price, ValueOrigin: "official", Definition: strings.TrimSpace(source.SeriesName), Coverage: strings.TrimSpace(source.Annotation)}
		if spec.cumulative {
			s.PeriodBasis = "year-to-date"
		}
		if source.SeasonallyAdjusted {
			s.SeasonalAdjustment = "seasonally-adjusted"
		}
		if strings.HasSuffix(spec.measure, "_index") {
			s.IndexBase = "上年同期=100"
		}
		if spec.formula != "" {
			s.ValueOrigin, s.Formula = "derived", text(spec.formula)
		}
		d.Series = append(d.Series, s)
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if err := validatePeriod(item.Period, "quarterly"); err != nil {
			return nil, err
		}
		if seen[item.Period] {
			return nil, fmt.Errorf("GDP季度重复: %s", item.Period)
		}
		seen[item.Period] = true
		for i, spec := range specs {
			source := sources[spec.key]
			o := Observation{SeriesID: d.Series[i].ID, Period: item.Period, Value: spec.value(item), SourceSeriesID: source.SeriesID, Source: text(source.Source), FetchedAt: timestamp(source.FetchedAt), PublishedAt: result.PublishedAt, RevisionID: result.RevisionID, DataVersion: "current_visible_revision"}
			if spec.cumulative {
				o.PeriodBegin, o.PeriodEnd = quarterYTDBegin(item.Period), item.Period
			}
			reason := "source_value_missing"
			if spec.input != nil {
				index := spec.input(item)
				if (o.Value == nil) != (index == nil) || (index != nil && math.Abs(*o.Value-(*index-100)) > 1e-6) {
					return nil, fmt.Errorf("GDP同比与官方指数不一致: %s", item.Period)
				}
				o.Inputs = []Base{{Period: item.Period, Value: index}}
				reason = "current_value_missing"
			}
			if err := d.append(o, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// Household 现价累计金额和实际累计同比分开；全年含自产自用，与前三季度累计不可比，也不拆单季。
func Household(result *nbs.HouseholdResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("居民收支结果不能为空")
	}
	if result.Frequency != "quarterly" || result.Measurement != "year-to-date-per-capita-flow" || result.ValueUnit != "元/人" || result.GrowthUnit != "%" || result.SeasonallyAdjusted || result.Annualized {
		return nil, fmt.Errorf("居民收支统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("CN", result.Begin, result.End, "quarterly", result)
	if err != nil {
		return nil, err
	}
	sources := map[string]nbs.HouseholdSeries{}
	for _, s := range result.Series {
		if s.SeriesID == "" || s.Source == "" || s.FetchedAt.IsZero() {
			return nil, fmt.Errorf("居民收支序列元数据不符合统一导出契约: %s", s.Key)
		}
		sources[s.Key] = s
	}
	specs := []struct {
		id, indicator, measure, key, unit, price, sourceBasis string
		value                                                 func(nbs.HouseholdObservation) *float64
	}{
		{"cn.household.income.ytd_level", "household.income", "ytd_level", "income-ytd", "CNY_per_person", "nominal", "current-prices", func(o nbs.HouseholdObservation) *float64 { return o.IncomeYTD }},
		{"cn.household.income.ytd_real_yoy_percent", "household.income", "ytd_real_yoy_percent", "income-real-yoy", "percent", "real", "real-growth", func(o nbs.HouseholdObservation) *float64 { return o.IncomeRealYTDYoY }},
		{"cn.household.consumption.ytd_level", "household.consumption", "ytd_level", "consumption-ytd", "CNY_per_person", "nominal", "current-prices", func(o nbs.HouseholdObservation) *float64 { return o.ConsumptionYTD }},
		{"cn.household.consumption.ytd_real_yoy_percent", "household.consumption", "ytd_real_yoy_percent", "consumption-real-yoy", "percent", "real", "real-growth", func(o nbs.HouseholdObservation) *float64 { return o.ConsumptionRealYTDYoY }},
	}
	for _, spec := range specs {
		source, ok := sources[spec.key]
		if !ok || source.PriceBasis != spec.sourceBasis {
			return nil, fmt.Errorf("居民收支序列口径不符合统一导出契约: %s", spec.key)
		}
		d.Series = append(d.Series, Series{ID: spec.id, Indicator: spec.indicator, Measure: spec.measure, Unit: spec.unit, Frequency: "quarterly", PeriodBasis: "year-to-date", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: spec.price, ValueOrigin: "official", Definition: strings.TrimSpace(source.SeriesName), Coverage: result.Population + "人均平均数；" + result.SurveyMethod + "；不是中位数或部门总额", Survey: result.SurveyMethod})
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if err := validatePeriod(item.Period, "quarterly"); err != nil {
			return nil, err
		}
		if seen[item.Period] {
			return nil, fmt.Errorf("居民收支季度重复: %s", item.Period)
		}
		seen[item.Period] = true
		if item.IncludesOwnProduction != strings.HasSuffix(item.Period, "Q4") {
			return nil, fmt.Errorf("居民收支自产自用范围与季度不符: %s", item.Period)
		}
		for i, spec := range specs {
			source := sources[spec.key]
			o := Observation{SeriesID: d.Series[i].ID, Period: item.Period, PeriodBegin: quarterYTDBegin(item.Period), PeriodEnd: item.Period, Value: spec.value(item), RawValue: householdRaw(item.RawValues[spec.key]), SourceSeriesID: source.SeriesID, Source: text(source.Source), FetchedAt: timestamp(source.FetchedAt), PublishedAt: result.PublishedAt, RevisionID: result.RevisionID, DataVersion: "current_visible_revision"}
			if item.IncludesOwnProduction {
				o.Note = "全年累计含自产自用部分，季度累计不含，不能相减得到第四季度"
			}
			if err := d.append(o, "source_value_missing"); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// householdRaw 解码源字符串；JSON null和空字符串都是缺失，不写成字符串。
func householdRaw(raw json.RawMessage) *string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return text(value)
	}
	return text(string(raw))
}

// CNInvestment 只保留官方累计值，基建按统计范围分序列；未公布金额不反推，1月不拆分。
func CNInvestment(result *nbs.InvestmentResult, begin, end string) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("固定资产投资结果不能为空")
	}
	d, err := newDataset("CN", begin, end, result)
	if err != nil {
		return nil, err
	}
	coverage := map[string]string{
		"total":          "全国固定资产投资（不含农户）累计；统计范围按当期月报",
		"manufacturing":  "全国制造业固定资产投资累计；行业分类按当期月报",
		"infrastructure": "基础设施投资累计；统计范围未能从该期确定",
		"infrastructure.excluding_electricity_heat_gas_water": "基础设施投资累计，不含电力、热力、燃气及水生产和供应业",
		"infrastructure.including_electricity_heat_gas_water": "基础设施投资累计，含电力、热力、燃气及水生产和供应业",
		"real-estate": "全国房地产开发投资累计；与按行业分的房地产业固定资产投资不同",
	}
	for _, item := range result.Observations {
		key := item.Sector
		if item.InfrastructureScope != "" {
			key += "." + item.InfrastructureScope
		}
		if coverage[key] == "" || item.Sector != result.Sector || item.AmountUnit != "亿元" || item.GrowthUnit != "%" || item.PriceBasis != "nominal" || item.SeasonallyAdjusted {
			return nil, fmt.Errorf("固定资产投资统计口径不符合统一导出契约: %s", item.Period)
		}
		// 金额和增速的说明分开：未公布金额不写到增速上，1月免报等整月状态两者相同。
		reason, levelNote, growthNote := "source_value_missing", "官方月报未公布该分项累计金额，不反推", item.BasisNote
		if item.YTDAmount != nil {
			levelNote = ""
		}
		switch item.Status {
		case "published":
			if item.Source == "" || item.FetchedAt == nil || (item.Sector == "infrastructure" && item.InfrastructureScope == "") {
				return nil, fmt.Errorf("固定资产投资发布稿缺少来源或统计范围: %s", item.Period)
			}
		case "january_exempt", "not_published":
			if item.YTDAmount != nil || item.YTDGrowth != nil {
				return nil, fmt.Errorf("未发布月份不应有投资数值: %s", item.Period)
			}
			reason, levelNote, growthNote = item.Status, item.Note, item.Note
			if item.Status == "not_published" {
				reason = "report_not_found"
			}
		default:
			return nil, fmt.Errorf("未知固定资产投资状态: %s", item.Status)
		}
		for _, spec := range []struct {
			measure, unit, note string
			value               *float64
		}{{"ytd_level", "CNY_100_million", levelNote, item.YTDAmount}, {"ytd_yoy_percent", "percent", growthNote, item.YTDGrowth}} {
			s := Series{ID: "cn.investment." + key + "." + spec.measure, Indicator: "investment." + item.Sector, Measure: spec.measure, Unit: spec.unit, Frequency: "monthly", PeriodBasis: "year-to-date", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Definition: "official-monthly-release", Coverage: coverage[key]}
			o := Observation{Period: item.Period, Value: spec.value, Source: text(item.Source), PublishedAt: item.PublishedAt, RevisionID: item.HistoricalVersion, DataVersion: "current_visible_revision", Note: spec.note}
			if item.FetchedAt != nil {
				o.FetchedAt = timestamp(*item.FetchedAt)
			}
			if err := d.appendNBSReason(s, o, true, item.YTDPeriod, reason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// CNEnterprise 按指标、调查范围和应收账款列报方式分序列，不跨断点连接；利润是累计流量，其余是月末余额。
func CNEnterprise(result *nbs.EnterpriseResult, begin, end string) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("工业企业结果不能为空")
	}
	if result.AmountUnit != "亿元" || result.GrowthUnit != "%" || result.PriceBasis != "nominal" || result.SeasonallyAdjusted || result.Source == "" {
		return nil, fmt.Errorf("工业企业统计口径不符合统一导出契约")
	}
	d, err := newDataset("CN", begin, end, result)
	if err != nil {
		return nil, err
	}
	metrics := map[string]nbs.EnterpriseMetric{}
	for _, metric := range result.Metrics {
		if metric.ValueSeries.ID == "" || metric.GrowthSeries.ID == "" {
			return nil, fmt.Errorf("工业企业指标缺少系列标识: %s", metric.Metric)
		}
		metrics[metric.Metric] = metric
	}
	reasons := map[string]string{"january_exempt": "january_exempt", "not_surveyed": "not_surveyed", "definition_suspended": "definition_suspended"}
	for _, item := range result.Observations {
		metric, ok := metrics[item.Metric]
		if !ok || item.Measurement != metric.Measurement || item.ScopeStandard == "" || item.SurveyScope == "" {
			return nil, fmt.Errorf("工业企业观测与指标不匹配: %s %s", item.Metric, item.Period)
		}
		flow := item.Measurement == "year_to_date_flow"
		if !flow && item.Measurement != "month_end_stock" {
			return nil, fmt.Errorf("未知工业企业计量方式: %s", item.Measurement)
		}
		if flow == (item.YTDPeriod == nil) || flow == (item.AsOfDate != nil) {
			return nil, fmt.Errorf("工业企业累计范围或月末日期不符: %s %s", item.Metric, item.Period)
		}
		prefix := "cn.enterprise." + item.Metric + "." + item.ScopeStandard
		if item.AccountingScope != "" {
			prefix += "." + item.AccountingScope
		}
		level, change, basis, ytd := "level", "yoy_percent", "month-end", ""
		if flow {
			level, change, basis, ytd = "ytd_level", "ytd_yoy_percent", "year-to-date", *item.YTDPeriod
		}
		reason := "source_value_missing"
		if mapped := reasons[item.Status]; mapped != "" {
			reason = mapped
		}
		for _, spec := range []struct {
			measure, unit, sourceID, name string
			value                         *float64
		}{{level, "CNY_100_million", metric.ValueSeries.ID, metric.ValueSeries.Name, item.Value}, {change, "percent", metric.GrowthSeries.ID, metric.GrowthSeries.Name, item.YoY}} {
			s := Series{ID: prefix + "." + spec.measure, Indicator: "enterprise." + item.Metric, Measure: spec.measure, Unit: spec.unit, Frequency: "monthly", PeriodBasis: basis, SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Definition: strings.TrimSpace(spec.name), Coverage: item.SurveyScope}
			o := Observation{Period: item.Period, Value: spec.value, AsOfDate: item.AsOfDate, SourceSeriesID: spec.sourceID, Source: text(result.Source), FetchedAt: timestamp(result.FetchedAt), PublishedAt: item.PublishedAt, RevisionID: result.HistoricalVersion, DataVersion: "current_visible_revision"}
			missingReason := reason
			// provider的整行备注说明缺失原因，有值的观测不带；同比不可计算只属于同比。
			if spec.value == nil {
				o.Note = item.Note
				if strings.Contains(item.Note, nbs.EnterpriseYoYNotComputableNote) {
					if spec.measure == change {
						missingReason = "official_not_computable"
					} else {
						o.Note = strings.Trim(strings.ReplaceAll(item.Note, nbs.EnterpriseYoYNotComputableNote, ""), "；")
					}
				}
			}
			if err := d.appendNBSReason(s, o, flow, ytd, missingReason); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
