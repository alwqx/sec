package nbs

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var enterpriseProfitAnnotation = regexp.MustCompile(`^(?:[（(]注[0-9]*[）)]|注[0-9]*)$`)

func enterpriseNumber(raw json.RawMessage, profitGrowth bool) (*float64, bool, error) {
	if profitGrowth {
		var text string
		if json.Unmarshal(raw, &text) == nil && enterpriseProfitAnnotation.MatchString(compact(text)) {
			return nil, true, nil
		}
	}
	number, err := parseNumber(raw)
	return number, false, err
}

func enterpriseObservation(month string, metric EnterpriseMetric) EnterpriseObservation {
	item := EnterpriseObservation{Metric: metric.Metric, Period: month, Measurement: metric.Measurement, ComparisonScopeYear: month[:4], Status: "no_value", SurveyFrequency: "monthly_except_january"}
	date, _ := time.Parse("200601", month)
	if metric.Measurement == "year_to_date_flow" {
		period := month[:4] + "01-" + month
		item.YTDPeriod = &period
		item.YoYBasis = "year_to_date_comparable_scope"
	} else {
		asOf := date.AddDate(0, 1, -1).Format("2006-01-02")
		item.AsOfDate = &asOf
		item.YoYBasis = "month_end_comparable_scope"
	}
	switch {
	case month < "200701":
		item.ScopeStandard = "all_state_owned_and_nonstate_revenue_5m"
		item.SurveyScope = "全部国有及主营业务收入500万元及以上非国有工业法人单位"
	case month < "201101":
		item.ScopeStandard = "industrial_legal_entities_revenue_5m"
		item.SurveyScope = "主营业务收入500万元及以上工业法人单位"
		item.SurveyFrequency = "feb_may_aug_nov"
	default:
		item.ScopeStandard = "industrial_legal_entities_revenue_20m"
		item.SurveyScope = "主营业务收入2000万元及以上工业法人单位"
	}
	if metric.Metric == "receivables" {
		switch {
		case month < "201901":
			item.AccountingScope = "accounts_receivable_before_2019"
		case month < "202001":
			item.AccountingScope = "2019_combined_bills_and_receivables_instead"
		default:
			item.AccountingScope = "separate_accounts_receivable_since_2020"
		}
	}
	return item
}

func finishEnterpriseObservation(item *EnterpriseObservation, metric EnterpriseMetric) {
	add := func(note string) {
		if item.Note != "" {
			item.Note += "；"
		}
		item.Note += note
	}
	switch {
	case item.Value != nil && item.YoY != nil:
		item.Status = "available"
	case item.Value != nil || item.YoY != nil:
		item.Status = "partial"
		add("部分单元格未提供数值，不补零或反推")
	default:
		add("发布库未提供数值，可能未收录或尚未更新，不等同于官方未发布")
		// 按季调查的年份没有1月调查，先判断调查频率，不把这些1月算作免报。
		if item.SurveyFrequency == "feb_may_aug_nov" && item.Period[4:] != "02" && item.Period[4:] != "05" && item.Period[4:] != "08" && item.Period[4:] != "11" {
			item.Status = "not_surveyed"
			add("2007—2010年仅2、5、8、11月调查，本月缺口不插值")
		} else if item.Period[4:] == "01" {
			item.Status = "january_exempt"
			add("1月免报，不拆出1—2月数据")
		} else if item.Metric == "receivables" && strings.HasPrefix(item.Period, "2019") {
			item.Status = "definition_suspended"
			add("2019年改发应收票据及应收账款，不用合并口径填应收账款缺口")
		}
		if item.Period < metric.VerifiedFirstNonempty {
			add("该系列当前已核验非空起点为" + metric.VerifiedFirstNonempty)
		}
	}
	if item.Period[4:] == "02" {
		if item.Measurement == "year_to_date_flow" {
			add("1—2月合并累计利润，不是2月单月利润")
		} else {
			add("2月末余额，不是1—2月累计或平均值")
		}
	}
	if item.Metric == "receivables" && item.Period >= "202001" {
		add("2020年起恢复应收账款单列，不能与2019年票据及应收账款合并系列直接比较")
	}
}
