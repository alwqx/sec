package macrodata

import (
	"fmt"
	"strings"

	"github.com/alwqx/sec/provider/mof"
	"github.com/alwqx/sec/provider/mofcom"
)

// rawText 把源字段的null标记视为缺失，不写成字符串。
func rawText(value string) *string {
	if value == "null" {
		return nil
	}
	return text(value)
}

// CNFiscal 分开累计金额、主同比、同口径同比和留抵退税调整后同比；两本预算不相加，不推算当月或赤字。
func CNFiscal(result *mof.FiscalResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("财政结果不能为空")
	}
	if result.Frequency != "monthly" || result.Measurement != "year-to-date-flow" || result.Unit != "亿元" || result.GrowthUnit != "%" || result.PriceBasis != "current-prices" || result.SeasonallyAdjusted || result.Annualized {
		return nil, fmt.Errorf("财政统计口径不符合统一导出契约")
	}
	d, err := newDataset("CN", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	for _, item := range result.Observations {
		released := item.Status != "missing-release"
		if released && (item.Source == "" || item.FetchedAt == nil) {
			return nil, fmt.Errorf("财政发布稿缺少来源或抓取时间: %s", item.Period)
		}
		if !released && item.Source != "" {
			return nil, fmt.Errorf("财政状态与来源不一致: %s", item.Period)
		}
		reason, releaseNote := "source_value_missing", ""
		if !released {
			reason, releaseNote = "report_not_found", item.Note
		}
		for _, metric := range item.Metrics {
			if metric.Key == "" || metric.Name == "" || (metric.Budget != "general" && metric.Budget != "fund") {
				return nil, fmt.Errorf("财政指标缺少标识: %s", item.Period)
			}
			if !released && (metric.YTDAmount != nil || metric.YTDYoY != nil) {
				return nil, fmt.Errorf("未发布月份不应有财政数值: %s", item.Period)
			}
			for _, spec := range []struct {
				measure, unit string
				value         *float64
				optional      bool
			}{
				{"ytd_level", "CNY_100_million", metric.YTDAmount, false},
				{"ytd_yoy_percent", "percent", metric.YTDYoY, false},
				{"ytd_comparable_yoy_percent", "percent", metric.ComparableYTDYoY, true},
				{"ytd_tax_refund_adjusted_yoy_percent", "percent", metric.TaxRefundAdjustedYTDYoY, true},
			} {
				// 另列的同比只有官方提到时才有，没提到不是数据缺口。
				if spec.optional && spec.value == nil {
					continue
				}
				s := pbcSeries("cn.fiscal."+metric.Key+"."+spec.measure, "fiscal."+metric.Key, spec.measure, spec.unit, "year-to-date", metric.Name, "全国"+metric.Name+"当年累计；两本预算不相加，收入减支出不等于官方赤字")
				o := Observation{Period: item.Period, Value: spec.value, Source: text(item.Source), PublishedDate: text(item.PublishedDate), PublishedAt: item.PublishedAt, RevisionID: item.RevisionID, DataVersion: "current_visible_revision", Note: releaseNote}
				if item.FetchedAt != nil {
					o.FetchedAt = timestamp(*item.FetchedAt)
				}
				if released && spec.value == nil {
					o.Note = metric.Note
				}
				if err := d.appendNBSReason(s, o, true, item.YTDPeriod, reason); err != nil {
					return nil, err
				}
			}
		}
	}
	return d, nil
}

// CNTrade 人民币和美元各成序列，不换算；伙伴行范围交叠，不相加也不由累计差分月值。
func CNTrade(result *mofcom.TradeResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("货物贸易结果不能为空")
	}
	if result.Measurement != "customs-goods-trade" || result.Frequency != "monthly" {
		return nil, fmt.Errorf("货物贸易统计口径不符合统一导出契约")
	}
	d, err := newDataset("CN", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	coverage := "海关货物贸易，不含服务贸易；人民币和美元分别为原始统计，不互相换算"
	flows := []struct {
		key, usdKey, name string
		amount            func(*mofcom.TradeCurrencyValues) mofcom.TradeAmount
	}{
		{"total", "total", "货物进出口总额", func(v *mofcom.TradeCurrencyValues) mofcom.TradeAmount { return v.Total }},
		{"exports", "export", "货物出口", func(v *mofcom.TradeCurrencyValues) mofcom.TradeAmount { return v.Exports }},
		{"imports", "import", "货物进口", func(v *mofcom.TradeCurrencyValues) mofcom.TradeAmount { return v.Imports }},
		{"balance", "imexgap", "货物贸易差额", func(v *mofcom.TradeCurrencyValues) mofcom.TradeAmount { return v.Balance }},
	}
	for _, item := range result.Observations {
		for _, currency := range []struct {
			id, unit, sourceUnit string
			values               *mofcom.TradeCurrencyValues
		}{{"cny", "CNY_100_million", "亿元人民币", item.CNY}, {"usd", "USD_100_million", "亿美元", item.USD}} {
			if currency.values != nil && (currency.values.Unit != currency.sourceUnit || currency.values.Source == "" || currency.values.FetchedAt.IsZero()) {
				return nil, fmt.Errorf("货物贸易币种或来源不符合统一导出契约: %s", item.Period)
			}
			for _, flow := range flows {
				amount := mofcom.TradeAmount{}
				raw := map[string]string{}
				if currency.values != nil {
					amount, raw = flow.amount(currency.values), currency.values.RawValues
				}
				rawKey := func(cny, usd string) string {
					if currency.id == "cny" {
						return raw[flow.key+"_"+cny]
					}
					return raw[flow.usdKey+"_"+usd]
				}
				measures := []struct {
					measure, unit, basis, raw string
					value                     *float64
					cumulative                bool
				}{
					{"level", currency.unit, "month", rawKey("monthly", "value"), amount.Monthly, false},
					{"ytd_level", currency.unit, "year-to-date", rawKey("ytd", "lj_value"), amount.YearToDate, true},
					{"yoy_percent", "percent", "month", rawKey("monthly_yoy_percent", "per"), amount.MonthlyYoY, false},
					{"ytd_yoy_percent", "percent", "year-to-date", rawKey("ytd_yoy_percent", "lj_per"), amount.YearToDateYoY, true},
				}
				// 环比只有人民币月报提供，美元没有这条序列。
				if currency.id == "cny" {
					measures = append(measures, struct {
						measure, unit, basis, raw string
						value                     *float64
						cumulative                bool
					}{"mom_percent", "percent", "month", rawKey("monthly_mom_percent", ""), amount.MonthlyMoM, false})
				}
				for _, spec := range measures {
					s := pbcSeries("cn.goods-trade."+currency.id+"."+flow.key+"."+spec.measure, "goods-trade."+flow.key, spec.measure, spec.unit, spec.basis, flow.name, coverage)
					o := Observation{Period: item.Period, Value: spec.value, RawValue: rawText(spec.raw), DataVersion: "current_visible_revision"}
					reason := "source_value_missing"
					if currency.values == nil {
						reason, o.Note = "report_not_found", "未检索到该币种的当月记录"
					} else {
						o.Source, o.FetchedAt = text(currency.values.Source), timestamp(currency.values.FetchedAt)
					}
					if err := d.appendNBSReason(s, o, spec.cumulative, item.Period[:4]+"01-"+item.Period, reason); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	for _, partner := range result.Partners {
		if err := appendTradePartner(d, partner); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// appendTradePartner 序列ID保留官方行名：组织和成员范围交叠，行名变化时序列也会变化。
func appendTradePartner(d *Dataset, p mofcom.TradePartner) error {
	unit, id := "CNY_100_million", "cny"
	if p.Currency == "USD" {
		unit, id = "USD_100_million", "usd"
	} else if p.Currency != "CNY" {
		return fmt.Errorf("未知伙伴币种: %s", p.Currency)
	}
	name := strings.TrimSpace(p.Name)
	if name == "" || p.Source == "" || p.FetchedAt.IsZero() {
		return fmt.Errorf("伙伴记录缺少名称、来源或抓取时间: %s", p.Period)
	}
	additive := false
	for _, flow := range []struct {
		key, usdKey          string
		monthly, ytd, ytdYoY *float64
	}{
		{"total", "total", p.TotalMonthly, p.TotalYTD, p.TotalYTDYoY},
		{"exports", "export", p.ExportsMonthly, p.ExportsYTD, p.ExportsYTDYoY},
		{"imports", "import", p.ImportsMonthly, p.ImportsYTD, p.ImportsYTDYoY},
	} {
		rawKey := func(cny, usd string) string {
			if id == "cny" {
				return p.RawValues[flow.key+"_"+cny]
			}
			return p.RawValues[flow.usdKey+"_"+usd]
		}
		measures := []struct {
			measure, unit, basis, raw string
			value                     *float64
			cumulative                bool
		}{
			{"ytd_level", unit, "year-to-date", rawKey("ytd", "lj_value"), flow.ytd, true},
			{"ytd_yoy_percent", "percent", "year-to-date", rawKey("ytd_yoy_percent", "lj_per"), flow.ytdYoY, true},
		}
		// 只有人民币伙伴表公布当月值，美元来源不由累计差分。
		if id == "cny" {
			measures = append(measures, struct {
				measure, unit, basis, raw string
				value                     *float64
				cumulative                bool
			}{"level", unit, "month", rawKey("monthly", ""), flow.monthly, false})
		}
		for _, spec := range measures {
			s := pbcSeries("cn.goods-trade.partner."+id+"."+name+"."+flow.key+"."+spec.measure, "goods-trade.partner."+flow.key, spec.measure, spec.unit, spec.basis, name, "海关主要贸易伙伴；总值、区域组织和成员行范围交叠，不能相加")
			if spec.unit != "percent" {
				s.ComponentsAdditive = &additive
			}
			o := Observation{Period: p.Period, Value: spec.value, RawValue: rawText(spec.raw), Source: text(p.Source), FetchedAt: timestamp(p.FetchedAt), PublishedDate: text(p.PublishedDate), DataVersion: "current_visible_revision"}
			if err := d.appendNBSReason(s, o, spec.cumulative, p.Period[:4]+"01-"+p.Period, "source_value_missing"); err != nil {
				return err
			}
		}
	}
	return nil
}

// CNServices 人民币和美元各成序列，总额和差额直接取官方字段，不用出口加减进口覆盖。
func CNServices(result *mofcom.ServicesResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("服务贸易结果不能为空")
	}
	if result.Indicator != "services_trade" || result.Frequency != "monthly" || result.PeriodBasis != "monthly" || result.PriceBasis != "nominal" || result.SeasonallyAdjusted || result.LevelAnnualized {
		return nil, fmt.Errorf("服务贸易统计口径不符合统一导出契约")
	}
	d, err := newDataset("CN", result.Begin, result.End, result)
	if err != nil {
		return nil, err
	}
	for _, item := range result.Observations {
		if (item.CNY != nil || item.USD != nil) && (item.Source == "" || item.FetchedAt == nil) {
			return nil, fmt.Errorf("服务贸易记录缺少来源或抓取时间: %s", item.Period)
		}
		for _, currency := range []struct {
			id, code, unit, sourceUnit, suffix string
			values                             *mofcom.ServicesValues
		}{{"cny", "CNY", "CNY_100_million", "亿元人民币", "RMB", item.CNY}, {"usd", "USD", "USD_100_million", "亿美元", "USD", item.USD}} {
			if currency.values != nil && (currency.values.Currency != currency.code || currency.values.Unit != currency.sourceUnit) {
				return nil, fmt.Errorf("服务贸易币种不符合统一导出契约: %s", item.Period)
			}
			values := mofcom.ServicesValues{}
			if currency.values != nil {
				values = *currency.values
			}
			for _, flow := range []struct {
				key, source, name string
				value             *float64
			}{{"total", "importAndExport", "服务进出口总额", values.Total}, {"exports", "export", "服务出口", values.Exports}, {"imports", "import", "服务进口", values.Imports}, {"balance", "difference", "服务贸易差额", values.Balance}} {
				s := pbcSeries("cn.services-trade."+currency.id+"."+flow.key+".level", "services-trade."+flow.key, "level", currency.unit, "month", flow.name, "商务部服务进出口月度统计，不是海关货物贸易；人民币和美元不互相换算")
				o := Observation{Period: item.Period, Value: flow.value, RawValue: rawJSONValue(item.RawValues[flow.source+currency.suffix]), Source: text(item.Source), PublishedAt: item.PublishedAt, RevisionID: item.RevisionID, DataVersion: "current_visible_revision"}
				if item.FetchedAt != nil {
					o.FetchedAt = timestamp(*item.FetchedAt)
				}
				reason := "source_value_missing"
				if currency.values == nil {
					reason, o.Note = "report_not_found", "该月未列示服务贸易记录，不拆分合并期或由累计推算"
				}
				if err := d.appendNBSReason(s, o, false, "", reason); err != nil {
					return nil, err
				}
			}
		}
	}
	return d, nil
}
