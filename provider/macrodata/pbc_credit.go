package macrodata

import (
	"fmt"
	"math"
	"time"

	"github.com/alwqx/sec/provider/pbc"
)

const pbcReportMissingNote = "未检索到该月官方金融统计报告"

// pbcInstitutionBreak 是消费金融、理财和金融资产投资公司纳入金融统计范围的所属期。
const pbcInstitutionBreak = "202301"

// financingScopeBreaks 来自社融表注和金融统计报告注释：只列统计范围确实变化的分项。
var financingScopeBreaks = map[string][]string{
	"total":           {"201807", "201809", "201909", "201912", pbcInstitutionBreak},
	"corporate-bonds": {"201909"},
	"rmb-loans":       {pbcInstitutionBreak},
	"loan-writeoffs":  {pbcInstitutionBreak},
}

// pbcScopeVersion 按所属期给出口径版本；序列跨官方统计范围变化时不共用ID。
func pbcScopeVersion(breaks []string, period string) string {
	version := "before-" + breaks[0]
	for _, boundary := range breaks {
		if period >= boundary {
			version = "since-" + boundary
		}
	}
	return version
}

// pbcAmountSeries 标记分项不可相加重建总额，增速序列不适用。
func pbcAmountSeries(s Series) Series {
	additive := false
	s.ComponentsAdditive = &additive
	return s
}

func pbcSeries(id, indicator, measure, unit, basis, definition, coverage string) Series {
	return Series{ID: id, Indicator: indicator, Measure: measure, Unit: unit, Frequency: "monthly", PeriodBasis: basis, SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Definition: definition, Coverage: coverage}
}

// pbcFlow 是报告中的新增额及其来源；月度值的方法决定它是官方值还是相减的近似值。
type pbcFlow struct {
	monthly, ytd, previousYTD       *float64
	method                          string
	approximate                     bool
	period, ytdPeriod               string
	source, publishedDate, previous string
	fetchedAt                       *time.Time
}

// appendPBCFlows 官方月度新增和相邻累计相减的近似值分序列，不以计算值冒充官方值，也不用余额差额替代。
func appendPBCFlows(d *Dataset, prefix, indicator, definition, coverage string, amount func(Series) Series, f pbcFlow) error {
	official := f.method == "published-monthly" || f.method == "january-published-ytd"
	derived := f.method == "difference-of-rounded-ytd-releases"
	if !official && !derived && f.method != "unavailable" {
		return fmt.Errorf("未知月度新增方法: %s", f.method)
	}
	if f.approximate != derived || (f.method == "unavailable") != (f.monthly == nil) {
		return fmt.Errorf("月度新增方法与数值不一致: %s %s", prefix, f.period)
	}
	if derived && (f.ytd == nil || f.previousYTD == nil || math.Abs(*f.monthly-(*f.ytd-*f.previousYTD)) > 1e-6) {
		return fmt.Errorf("近似月度新增缺少累计输入或数值不一致: %s %s", prefix, f.period)
	}
	if f.source == "" && (f.monthly != nil || f.ytd != nil) {
		return fmt.Errorf("新增额缺少报告来源: %s %s", prefix, f.period)
	}
	observation := func(value *float64) Observation {
		o := Observation{Period: f.period, Value: value, Source: text(f.source), PublishedDate: text(f.publishedDate), DataVersion: "current_visible_revision"}
		if f.fetchedAt != nil {
			o.FetchedAt = timestamp(*f.fetchedAt)
		}
		return o
	}
	missing := "source_value_missing"
	if f.source == "" {
		missing = "report_not_found"
	}
	s := amount(pbcSeries(prefix+".monthly_flow", indicator, "monthly_flow", "CNY_100_million", "month", definition, coverage))
	o := observation(f.monthly)
	reason := missing
	if derived {
		s.ID, s.Measure, s.ValueOrigin, s.Formula = prefix+".monthly_flow_derived", "monthly_flow_derived", "derived", text("ytd_flow - previous_month_ytd_flow")
		o.Note = "由相邻报告已四舍五入的累计额相减，是近似值；上期报告：" + f.previous
		month, err := time.Parse("200601", f.period)
		if err != nil {
			return fmt.Errorf("无效所属月份: %s", f.period)
		}
		o.Inputs = []Base{{Period: month.AddDate(0, -1, 0).Format("200601"), Value: f.previousYTD}, {Period: f.period, Value: f.ytd}}
	} else if f.monthly == nil {
		o.Note = pbcReportMissingNote
		if f.source != "" {
			reason, o.Note = "calculation_unavailable", "报告未提供月度值或相邻累计基数，不以余额差额替代"
		}
	}
	if err := d.appendNBSReason(s, o, false, "", reason); err != nil {
		return err
	}
	o = observation(f.ytd)
	if f.source == "" {
		o.Note = pbcReportMissingNote
	}
	return d.appendNBSReason(amount(pbcSeries(prefix+".ytd_flow", indicator, "ytd_flow", "CNY_100_million", "year-to-date", definition, coverage)), o, true, f.ytdPeriod, missing)
}

// Financing 分开存量、官方存量同比、月度增量和计算累计，分项不假定相加等于总量。
func Financing(items []pbc.FinancingObservation, begin, end string) (*Dataset, error) {
	if items == nil {
		items = []pbc.FinancingObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Unit != "亿元" || item.YTDMethod != "sum-of-published-monthly-flows" {
			return nil, fmt.Errorf("社融统计口径不符合统一导出契约: %s", item.Period)
		}
		if item.FlowSource == "" || item.StockSource == "" || item.FlowFetchedAt.IsZero() || item.StockFetchedAt.IsZero() {
			return nil, fmt.Errorf("社融观测缺少来源或抓取时间: %s", item.Period)
		}
		parts := append([]pbc.FinancingComponent{{Key: "total", Name: "社会融资规模", Stock: item.Stock, StockYoY: item.StockYoY, Flow: item.Flow, YTDFlow: item.YTDFlow}}, item.Components...)
		for _, part := range parts {
			if part.Key == "" || part.Name == "" {
				return nil, fmt.Errorf("社融分项缺少标识: %s", item.Period)
			}
			prefix, indicator := "cn.financing."+part.Key, "financing."+part.Key
			if breaks := financingScopeBreaks[part.Key]; breaks != nil {
				prefix += "." + pbcScopeVersion(breaks, item.Period)
			}
			coverage := "社融口径，与金融机构人民币贷款统计范围不同；分项不假定相加等于总量"
			for _, spec := range []struct {
				measure, unit, basis, source, reason string
				fetchedAt                            time.Time
				value                                *float64
				cumulative                           bool
			}{
				{"level", "CNY_100_million", "month-end", item.StockSource, "source_value_missing", item.StockFetchedAt, part.Stock, false},
				{"yoy_percent", "percent", "month-end", item.StockSource, "source_value_missing", item.StockFetchedAt, part.StockYoY, false},
				{"monthly_flow", "CNY_100_million", "month", item.FlowSource, "source_value_missing", item.FlowFetchedAt, part.Flow, false},
				{"ytd_flow", "CNY_100_million", "year-to-date", item.FlowSource, "window_incomplete", item.FlowFetchedAt, part.YTDFlow, true},
			} {
				s := pbcSeries(prefix+"."+spec.measure, indicator, spec.measure, spec.unit, spec.basis, part.Name, coverage)
				o := Observation{Period: item.Period, Value: spec.value, Source: text(spec.source), FetchedAt: timestamp(spec.fetchedAt), DataVersion: "current_visible_revision"}
				reason := spec.reason
				if spec.cumulative {
					s.ValueOrigin, s.Formula, s.Methodology = "derived", text("sum(monthly_flow from January)"), item.YTDMethod
					if spec.value == nil {
						o.Note = "年初至当月存在增量缺失，累计值不计算"
						// 当月增量本身缺失时不归因于更早月份。
						if part.Flow == nil {
							reason, o.Note = "source_value_missing", "当月增量缺失，累计值不计算"
						}
					}
				}
				if spec.unit != "percent" {
					s = pbcAmountSeries(s)
				}
				if err := d.appendNBSReason(s, o, spec.cumulative, item.YTDPeriod, reason); err != nil {
					return nil, err
				}
			}
		}
	}
	return d, nil
}

// Loans 精确余额表、报告舍入余额和官方同比分开，分项不假定相加等于总额。
func Loans(items []pbc.LoanObservation, begin, end string) (*Dataset, error) {
	if items == nil {
		items = []pbc.LoanObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	coverage := "金融机构人民币信贷收支口径，与社融人民币贷款不同；分项不假定相加等于总额"
	for _, item := range items {
		if item.Unit != "亿元" || item.BalanceSource == "" || item.BalanceFetchedAt.IsZero() || (item.ReportSource == "") != (item.ReportFetchedAt == nil) {
			return nil, fmt.Errorf("人民币贷款统计口径不符合统一导出契约: %s", item.Period)
		}
		missing, note := "source_value_missing", ""
		if item.ReportSource == "" {
			missing, note = "report_not_found", pbcReportMissingNote
		}
		parts := append([]pbc.LoanComponent{{Key: "total", Name: "各项人民币贷款", Balance: item.Balance, MonthlyFlow: item.MonthlyFlow, YTDFlow: item.YTDFlow, MonthlyFlowMethod: item.MonthlyFlowMethod, MonthlyFlowApproximate: item.MonthlyFlowApproximate, PreviousYTDFlow: item.PreviousYTDFlow}}, item.Components...)
		for _, part := range parts {
			if part.Key == "" || part.Name == "" {
				return nil, fmt.Errorf("人民币贷款分项缺少标识: %s", item.Period)
			}
			prefix, indicator := "cn.loans."+part.Key+"."+pbcScopeVersion([]string{pbcInstitutionBreak}, item.Period), "loans."+part.Key
			level := Observation{Period: item.Period, Value: part.Balance, Source: text(item.BalanceSource), FetchedAt: timestamp(item.BalanceFetchedAt), DataVersion: "current_visible_revision"}
			if err := d.appendNBS(pbcAmountSeries(pbcSeries(prefix+".level", indicator, "level", "CNY_100_million", "month-end", part.Name, coverage)), level, false, ""); err != nil {
				return nil, err
			}
			if part.Key == "total" {
				for _, spec := range []struct {
					measure, unit string
					value         *float64
				}{{"reported_level", "CNY_100_million", item.ReportedBalance}, {"yoy_percent", "percent", item.YoY}} {
					o := Observation{Period: item.Period, Value: spec.value, Source: text(item.ReportSource), PublishedDate: text(item.ReportPublishedDate), DataVersion: "current_visible_revision", Note: note}
					if item.ReportFetchedAt != nil {
						o.FetchedAt = timestamp(*item.ReportFetchedAt)
					}
					s := pbcSeries(prefix+"."+spec.measure, indicator, spec.measure, spec.unit, "month-end", part.Name, coverage)
					if spec.unit != "percent" {
						s = pbcAmountSeries(s)
					}
					if err := d.appendNBSReason(s, o, false, "", missing); err != nil {
						return nil, err
					}
				}
			}
			flow := pbcFlow{monthly: part.MonthlyFlow, ytd: part.YTDFlow, previousYTD: part.PreviousYTDFlow, method: part.MonthlyFlowMethod, approximate: part.MonthlyFlowApproximate, period: item.Period, ytdPeriod: item.YTDPeriod, source: item.ReportSource, publishedDate: item.ReportPublishedDate, previous: item.PreviousReportSource, fetchedAt: item.ReportFetchedAt}
			if err := appendPBCFlows(d, prefix, indicator, part.Name, coverage, pbcAmountSeries, flow); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// Deposits 住户余额保留原值，计算同比不冒充官方可比同比；全部人民币存款同比不套用给住户。
func Deposits(result *pbc.DepositResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("住户存款结果不能为空")
	}
	if result.Frequency != "monthly" || result.Unit != "亿元" || result.Currency != "CNY" || result.Measurement != "month-end-stock" || result.Population == "" || result.SeasonallyAdjusted || result.Annualized {
		return nil, fmt.Errorf("住户存款统计口径不符合统一导出契约")
	}
	d, err := newDataset("CN", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	coverage := result.Population + "；月末存量，不是全部人民币存款、居民收入或当月新增储蓄"
	for _, item := range result.Observations {
		month, err := time.Parse("200601", item.Period)
		if err != nil || month.Format("200601") != item.Period {
			return nil, fmt.Errorf("无效所属月份: %s", item.Period)
		}
		if (item.ReportSource == "") != (item.ReportFetchedAt == nil) || (item.ReportSource == "" && item.ReportedHouseholdYoY != nil) {
			return nil, fmt.Errorf("住户存款报告来源或抓取时间不一致: %s", item.Period)
		}
		if item.Balance != nil && (item.Source == "" || item.FetchedAt == nil) {
			return nil, fmt.Errorf("住户存款余额缺少来源或抓取时间: %s", item.Period)
		}
		version := pbcScopeVersion([]string{pbcInstitutionBreak}, item.Period)
		householdPrefix := "cn.deposits.households." + version
		balance := func(v pbc.DepositBalance, key string, value *float64) Observation {
			o := Observation{Period: item.Period, Value: value, RawValue: text(v.RawValues[key]), Source: text(v.Source), DataVersion: "current_visible_revision"}
			if v.FetchedAt != nil {
				o.FetchedAt = timestamp(*v.FetchedAt)
			}
			return o
		}
		for _, spec := range []struct {
			id, key, name string
			value         *float64
		}{{"households", "balance", "住户存款", item.Balance}, {"demand", "demand", "住户活期存款", item.Demand}, {"time-and-other", "time-and-other", "住户定期及其他存款", item.TimeAndOther}} {
			if err := d.appendNBS(pbcSeries("cn.deposits."+spec.id+"."+version+".level", "deposits."+spec.id, "level", "CNY_100_million", "month-end", spec.name, coverage), balance(item.DepositBalance, spec.key, spec.value), false, ""); err != nil {
				return nil, err
			}
		}
		base := item.YearAgoBase
		if base.Period != month.AddDate(-1, 0, 0).Format("200601") {
			return nil, fmt.Errorf("住户存款同比基期不匹配: %s", base.Period)
		}
		if item.ComputedYoY != nil && (item.Balance == nil || base.Balance == nil || *base.Balance == 0) {
			return nil, fmt.Errorf("住户存款计算同比缺少有效当期或基期: %s", item.Period)
		}
		yoy := balance(item.DepositBalance, "", item.ComputedYoY)
		// 基期在纳入新机构之前时跨了统计范围，不输出比率；provider计算值保留在original。
		crossesBreak := base.Period < pbcInstitutionBreak && item.Period >= pbcInstitutionBreak
		if crossesBreak {
			yoy.Value, yoy.Note = nil, "基期在2023年1月纳入新机构之前，跨统计范围不计算同比"
		}
		yoy.RawValue, yoy.Base = nil, &Base{Period: base.Period, Value: base.Balance, RawValue: text(base.RawValues["balance"])}
		reason := "calculation_unavailable"
		if item.Balance == nil {
			reason = "current_value_missing"
		} else if base.Balance == nil {
			reason = "baseline_missing"
		} else if *base.Balance == 0 {
			reason = "zero_baseline"
		}
		if crossesBreak {
			reason = "methodology_break"
		}
		s := pbcSeries(householdPrefix+".yoy_percent", "deposits.households", "yoy_percent", "percent", "month-end", "住户存款计算同比", coverage)
		s.ValueOrigin, s.Formula, s.ChangeMethod = "derived", text("(current_level / base_level - 1) * 100"), "level_ratio_not_comparable_scope"
		if err := d.appendNBSReason(s, yoy, false, "", reason); err != nil {
			return nil, err
		}
		reported := Observation{Period: item.Period, Value: item.ReportedHouseholdYoY, Source: text(item.ReportSource), PublishedDate: text(item.ReportPublishedDate), DataVersion: "current_visible_revision"}
		reportedReason := "source_value_missing"
		if item.ReportFetchedAt != nil {
			reported.FetchedAt = timestamp(*item.ReportFetchedAt)
		}
		if item.ReportSource == "" {
			reportedReason, reported.Note = "report_not_found", pbcReportMissingNote
		} else if item.ReportedHouseholdYoY == nil {
			reported.Note = "报告未单独公布住户存款同比，全部人民币存款同比不套用"
		}
		if err := d.appendNBSReason(pbcSeries(householdPrefix+".reported_yoy_percent", "deposits.households", "reported_yoy_percent", "percent", "month-end", "住户存款官方同比", coverage), reported, false, "", reportedReason); err != nil {
			return nil, err
		}
		flow := pbcFlow{monthly: item.MonthlyFlow, ytd: item.YTDFlow, previousYTD: item.PreviousYTDFlow, method: item.MonthlyFlowMethod, approximate: item.MonthlyFlowApproximate, period: item.Period, ytdPeriod: item.Period[:4] + "01-" + item.Period, source: item.ReportSource, publishedDate: item.ReportPublishedDate, previous: item.PreviousReportSource, fetchedAt: item.ReportFetchedAt}
		if err := appendPBCFlows(d, householdPrefix, "deposits.households", "住户存款新增", result.Population+"新增额；不以余额相减代替", func(s Series) Series { return s }, flow); err != nil {
			return nil, err
		}
	}
	return d, nil
}
