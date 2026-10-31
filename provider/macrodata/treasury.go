package macrodata

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/fiscaldata"
)

var dtsSlugSeparator = regexp.MustCompile(`[^a-z0-9]+`)

// dtsLabel 合并官方类别和细项；"null"是源字段的空值标记。
func dtsLabel(parts ...string) string {
	kept := []string{}
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" && part != "null" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " / ")
}

// dtsSlug 由官方标签生成序列ID。"(-)"是官方方向标签，保留以免与无标签的历史行共用序列。
func dtsSlug(label string) string {
	label = strings.ReplaceAll(strings.ToLower(label), "(-)", " minus ")
	return strings.Trim(dtsSlugSeparator.ReplaceAllString(label, "-"), "-")
}

// dtsSeries 按官方标签懒加载序列；不同标签生成同一ID时报错，不合并不同口径。
type dtsSeries struct {
	d    *Dataset
	seen map[string]string
}

func (b *dtsSeries) add(s Series) error {
	if definition, ok := b.seen[s.ID]; ok {
		if definition != s.Definition {
			return fmt.Errorf("不同官方标签生成相同序列ID: %s", s.ID)
		}
		return nil
	}
	b.seen[s.ID] = s.Definition
	b.d.Series = append(b.d.Series, s)
	return nil
}

// dtsDate 把记录日转换为所属期，并校验财年累计使用的日历。
func dtsDate(date string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Format("2006-01-02") != date {
		return time.Time{}, fmt.Errorf("无效DTS记录日: %s", date)
	}
	return parsed, nil
}

// dtsMeasures 是DTS金额的三种官方口径，序列和观测都由它生成。
var dtsMeasures = []struct{ measure, basis, key string }{
	{"daily", "record-date", "today"},
	{"month_to_date", "month-to-date", "mtd"},
	{"fiscal_year_to_date", "fiscal-year-to-date", "fytd"},
}

// dtsObservation 设置所属期及累计范围；美国财年从前一年10月开始。
func dtsObservation(seriesID, key string, date time.Time, value *float64, source string, fetchedAt time.Time, note string) Observation {
	period := date.Format("20060102")
	o := Observation{SeriesID: seriesID, Period: period, PeriodBegin: period, PeriodEnd: period, Value: value, Source: text(source), FetchedAt: timestamp(fetchedAt), DataVersion: "current_visible_revision", Note: note}
	switch key {
	case "mtd":
		o.PeriodBegin = date.Format("200601") + "01"
	case "fytd":
		o.FiscalYear = date.Year()
		if date.Month() >= time.October {
			o.FiscalYear++
		}
		o.PeriodBegin = fmt.Sprintf("%04d1001", o.FiscalYear-1)
	}
	return o
}

// appendDTSAmounts 分别输出官方日值、月累计和财年累计，累计值不由日值重算；金额全空时输出缺失观测。
func appendDTSAmounts(b *dtsSeries, base Series, date time.Time, amounts [3]*float64, raw map[string]string, prefix, source string, fetchedAt time.Time, note string) error {
	if source == "" || fetchedAt.IsZero() {
		return fmt.Errorf("DTS明细缺少来源或抓取时间: %s", date.Format("2006-01-02"))
	}
	for i, spec := range dtsMeasures {
		s := base
		s.ID, s.Measure, s.PeriodBasis = base.ID+"."+spec.measure, spec.measure, spec.basis
		if err := b.add(s); err != nil {
			return err
		}
		o := dtsObservation(s.ID, spec.key, date, amounts[i], source, fetchedAt, note)
		o.RawValue = text(raw[prefix+"_"+spec.key+"_amt"])
		if err := b.d.append(o, "source_value_missing"); err != nil {
			return err
		}
	}
	return nil
}

// dtsDuplicateNote 说明合并了哪些上游重复行；没有重复时为空。
func dtsDuplicateNote(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return "上游同日重复行（源行号" + strings.Join(lines, "、") + "）内容相同，按一条输出；完整行保留在original。"
}

// Debt 保留DTS表IIIA官方明细及明确标记的计算汇总；记账口径不等于现金、拍卖面值或公众净融资。
func Debt(result *fiscaldata.DebtResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("国债交易结果不能为空")
	}
	if result.Indicator != "treasury_debt_transactions" || result.Frequency != "daily" || result.DateBasis != "DTS record date" || result.Unit != "百万美元" || result.Source == "" {
		return nil, fmt.Errorf("国债交易统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, "daily", result)
	if err != nil {
		return nil, err
	}
	base := Series{Indicator: "treasury-debt", Unit: "USD_million", Frequency: "daily", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Coverage: "DTS表IIIA记账发行与赎回，含官方更正、利息和通胀调整；不等于现金流、拍卖面值或公众净融资"}
	builder := &dtsSeries{d, map[string]string{}}
	markets := map[string]string{"Marketable": "marketable", "Nonmarketable": "nonmarketable"}
	transactions := map[string]string{"Issues": "issues", "Redemptions": "redemptions"}
	seen := map[string]bool{}
	rows, repeats := fiscaldata.UniqueDebtRows(result.Rows)
	totals := map[string]float64{}
	for i, row := range rows {
		market, transaction := markets[row.Market], transactions[row.Transaction]
		label := dtsLabel(row.Security, row.Description)
		if market == "" || transaction == "" || label == "" {
			return nil, fmt.Errorf("国债交易分类不符合统一导出契约: %s", row.Date)
		}
		date, err := dtsDate(row.Date)
		if err != nil {
			return nil, err
		}
		s := base
		s.ID, s.Definition = "us.treasury-debt."+market+"."+transaction+"."+dtsSlug(label), row.Market+" / "+row.Transaction+" / "+label
		// 去重后同一标签仍有多行，说明金额不同，不能猜测取舍。
		if key := row.Date + "/" + s.ID; seen[key] {
			return nil, fmt.Errorf("国债交易明细重复且金额不同: %s", key)
		} else {
			seen[key] = true
		}
		if row.Today != nil {
			totals[row.Date+"/"+row.Market+"/"+row.Transaction] += *row.Today
		}
		if err := appendDTSAmounts(builder, s, date, [3]*float64{row.Today, row.MTD, row.FYTD}, row.Raw, "transaction", row.Source, row.FetchedAt, dtsDuplicateNote(repeats[i])); err != nil {
			return nil, err
		}
	}
	for _, summary := range result.Summaries {
		market := markets[summary.Market]
		if market == "" || !summary.IsDerived {
			return nil, fmt.Errorf("国债交易汇总不符合统一导出契约: %s", summary.Date)
		}
		date, err := dtsDate(summary.Date)
		if err != nil {
			return nil, err
		}
		if key := summary.Date + "/" + market; seen[key] {
			return nil, fmt.Errorf("国债交易汇总重复: %s", key)
		} else {
			seen[key] = true
		}
		for transaction, total := range map[string]*float64{"Issues": summary.Issues, "Redemptions": summary.Redemptions} {
			if total != nil && math.Abs(*total-totals[summary.Date+"/"+summary.Market+"/"+transaction]) > 1e-6 {
				return nil, fmt.Errorf("国债交易汇总与明细不一致: %s", summary.Date)
			}
		}
		if summary.Net != nil && (summary.Issues == nil || summary.Redemptions == nil || math.Abs(*summary.Net-(*summary.Issues-*summary.Redemptions)) > 1e-6) {
			return nil, fmt.Errorf("国债净记账发行与汇总不一致: %s", summary.Date)
		}
		for _, spec := range []struct {
			measure, formula string
			value            *float64
		}{
			{"issues_total", "sum(issues.today)", summary.Issues},
			{"redemptions_total", "sum(redemptions.today)", summary.Redemptions},
			{"net_recorded_issuance", "issues_total - redemptions_total", summary.Net},
		} {
			s := base
			s.ID, s.Measure, s.PeriodBasis, s.ValueOrigin, s.Formula, s.Definition = "us.treasury-debt."+market+"."+spec.measure, spec.measure, "record-date", "derived", text(spec.formula), summary.Market+" / "+spec.measure
			if err := builder.add(s); err != nil {
				return nil, err
			}
			o := Observation{SeriesID: s.ID, Period: date.Format("20060102"), Value: spec.value, Source: text(result.Source), FetchedAt: timestamp(result.FetchedAt), DataVersion: "current_visible_revision"}
			if err := d.append(o, "calculation_unavailable"); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// DebtCash 官方现金总额和计算净额分开，调整明细只作溯源，不相加重建总额。
func DebtCash(result *fiscaldata.CashResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("国债现金结果不能为空")
	}
	if result.Indicator != "treasury_debt_cash" || result.Frequency != "daily" || result.Basis != "DTS cash basis; record date" || result.Unit != "百万美元" || len(result.Sources) == 0 {
		return nil, fmt.Errorf("国债现金统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, "daily", result)
	if err != nil {
		return nil, err
	}
	base := Series{Indicator: "treasury-debt-cash", Unit: "USD_million", Frequency: "daily", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "nominal", ValueOrigin: "official", Coverage: "DTS表II转引IIIB的现金发行与赎回；不等于记账值、拍卖面值或财政赤字"}
	builder := &dtsSeries{d, map[string]string{}}
	transactions := map[string]string{"Issues": "issues", "Redemptions": "redemptions"}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		date, err := dtsDate(item.Date)
		if err != nil {
			return nil, err
		}
		if seen[item.Date] {
			return nil, fmt.Errorf("国债现金记录日重复: %s", item.Date)
		}
		seen[item.Date] = true
		if !item.NetIsDerived {
			return nil, fmt.Errorf("净现金发行必须标记为计算值: %s", item.Date)
		}
		for _, side := range []struct {
			name, transaction string
			row               *fiscaldata.CashRow
		}{{"issues", "Issues", item.Issues}, {"redemptions", "Redemptions", item.Redemptions}} {
			s := base
			s.ID, s.Definition = "us.treasury-debt-cash."+side.name, "Public Debt Cash "+side.transaction+" (official total)"
			if side.row == nil {
				if err := appendDTSAmounts(builder, s, date, [3]*float64{}, nil, "transaction", result.Sources[0], result.FetchedAt, "当日未列示该侧官方现金总额，未推算"); err != nil {
					return nil, err
				}
				continue
			}
			if side.row.Transaction != side.transaction || side.row.Date != item.Date {
				return nil, fmt.Errorf("国债现金总额与所属行不匹配: %s", item.Date)
			}
			if err := appendDTSAmounts(builder, s, date, [3]*float64{side.row.Today, side.row.MTD, side.row.FYTD}, side.row.Raw, "transaction", side.row.Source, side.row.FetchedAt, ""); err != nil {
				return nil, err
			}
		}
		if err := appendCashNet(builder, base, date, item, result); err != nil {
			return nil, err
		}
		rows := map[string]bool{}
		adjustments, repeats := fiscaldata.UniqueCashRows(item.Adjustments)
		for i, row := range adjustments {
			transaction := transactions[row.Transaction]
			label := dtsLabel(row.Category, row.Description)
			if transaction == "" || label == "" || row.Date != item.Date {
				return nil, fmt.Errorf("国债现金调整明细不符合统一导出契约: %s", item.Date)
			}
			s := base
			s.ID, s.Definition, s.Coverage = "us.treasury-debt-cash.adjustment."+transaction+"."+dtsSlug(label), row.Transaction+" / "+label, "DTS表IIIB记账转现金口径明细；(-)是官方方向标签，数值不翻转符号，也不相加重建总额"
			if rows[s.ID] {
				return nil, fmt.Errorf("国债现金调整明细重复且金额不同: %s", s.ID)
			}
			rows[s.ID] = true
			if err := appendDTSAmounts(builder, s, date, [3]*float64{row.Today, row.MTD, row.FYTD}, row.Raw, "adj", row.Source, row.FetchedAt, dtsDuplicateNote(repeats[i])); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// appendCashNet 对日值、月累计和财年累计各自作差，不跨字段或用调整项补缺。
func appendCashNet(b *dtsSeries, base Series, date time.Time, item fiscaldata.CashObservation, result *fiscaldata.CashResult) error {
	amounts := func(row *fiscaldata.CashRow) fiscaldata.CashAmounts {
		if row == nil {
			return fiscaldata.CashAmounts{}
		}
		return row.CashAmounts
	}
	rawOf := func(row *fiscaldata.CashRow, key string) *string {
		if row == nil {
			return nil
		}
		return text(row.Raw["transaction_"+key+"_amt"])
	}
	issues, redemptions := amounts(item.Issues), amounts(item.Redemptions)
	period := date.Format("20060102")
	for i, spec := range dtsMeasures {
		net, a, c := [3]*float64{item.Net.Today, item.Net.MTD, item.Net.FYTD}[i], [3]*float64{issues.Today, issues.MTD, issues.FYTD}[i], [3]*float64{redemptions.Today, redemptions.MTD, redemptions.FYTD}[i]
		if net != nil && (a == nil || c == nil || math.Abs(*net-(*a-*c)) > 1e-6) {
			return fmt.Errorf("净现金发行与官方总额不一致: %s", item.Date)
		}
		s := base
		s.ID, s.Measure, s.PeriodBasis, s.ValueOrigin, s.Formula, s.Definition = "us.treasury-debt-cash.net."+spec.measure, spec.measure, spec.basis, "derived", text("official_cash_issues - official_cash_redemptions"), "net cash issuance"
		if err := b.add(s); err != nil {
			return err
		}
		note := ""
		if net == nil {
			note = "官方现金发行或赎回缺失，不计算净额"
		}
		o := dtsObservation(s.ID, spec.key, date, net, result.Sources[0], result.FetchedAt, note)
		o.Inputs = []Base{{Period: period, Value: a, RawValue: rawOf(item.Issues, spec.key)}, {Period: period, Value: c, RawValue: rawOf(item.Redemptions, spec.key)}}
		if err := b.d.append(o, "calculation_unavailable"); err != nil {
			return err
		}
	}
	return nil
}

// Auctions 同一日期可有多只证券，观测用event_id区分；各金额可能交叠，不相加重建发行额。
func Auctions(result *fiscaldata.AuctionsResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("拍卖结果不能为空")
	}
	periodBasis := map[string]string{"issue_date": "issue-date", "auction_date": "auction-date"}[result.DateBasis]
	if periodBasis == "" || result.Indicator != "treasury_auctions" || result.Frequency != "event" || result.Market != "marketable" || result.AmountBasis != "par value" || result.AmountUnit != "USD" {
		return nil, fmt.Errorf("拍卖统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("US", result.Begin, result.End, "event", result)
	if err != nil {
		return nil, err
	}
	additive := false
	amounts := []struct {
		measure, rawKey string
		value           func(fiscaldata.AuctionAmounts) *float64
	}{
		{"offering", "offering_amt", func(a fiscaldata.AuctionAmounts) *float64 { return a.Offering }},
		{"total_accepted", "total_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.TotalAccepted }},
		{"competitive_accepted", "comp_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.Competitive }},
		{"noncompetitive_accepted", "noncomp_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.Noncompetitive }},
		{"soma_accepted", "soma_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.SOMA }},
		{"fima_noncompetitive_accepted", "fima_noncomp_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.FIMA }},
		{"primary_dealer_accepted", "primary_dealer_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.PrimaryDealers }},
		{"direct_bidder_accepted", "direct_bidder_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.DirectBidders }},
		{"indirect_bidder_accepted", "indirect_bidder_accepted", func(a fiscaldata.AuctionAmounts) *float64 { return a.IndirectBidders }},
	}
	base := Series{Indicator: "treasury-auctions", Frequency: "event", PeriodBasis: periodBasis, SeasonalAdjustment: "not-seasonally-adjusted", ValueOrigin: "official", Coverage: "财政部可流通证券拍卖结果；不是现金募集额或净融资"}
	for _, amount := range amounts {
		s := base
		s.ID, s.Measure, s.Unit, s.PriceBasis, s.Definition, s.ComponentsAdditive = "us.treasury-auctions."+amount.measure, amount.measure, "USD", "par-value", amount.rawKey, &additive
		d.Series = append(d.Series, s)
	}
	price := base
	price.ID, price.Measure, price.Unit, price.PriceBasis, price.Definition = "us.treasury-auctions.price_per_100", "price_per_100", "USD_per_100_par", "not_applicable", "price_per100"
	d.Series = append(d.Series, price)
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if item.CUSIP == "" || item.Source == "" || item.FetchedAt.IsZero() {
			return nil, fmt.Errorf("拍卖事件缺少CUSIP、来源或抓取时间: %s", item.CUSIP)
		}
		issue, auction, err := auctionDates(item)
		if err != nil {
			return nil, err
		}
		id := item.CUSIP + "/" + issue + "/" + auction
		if seen[id] {
			return nil, fmt.Errorf("拍卖事件重复: %s", id)
		}
		seen[id] = true
		period := issue
		if result.DateBasis == "auction_date" {
			period = auction
		}
		d.Events = append(d.Events, Event{ID: id, CUSIP: item.CUSIP, SecurityType: item.SecurityType, Term: item.Term, AnnouncementDate: item.AnnouncementDate, AuctionDate: item.AuctionDate, IssueDate: item.IssueDate, MaturityDate: item.MaturityDate, OriginalIssueDate: item.OriginalIssueDate, Reopening: item.Reopening, FloatingRate: item.FloatingRate, InflationIndexed: item.InflationIndexed, SOMAInsideOffering: item.SOMAIncluded, FIMACompetitiveInsideOffering: item.FIMAIncluded})
		appendValue := func(seriesID, rawKey string, value *float64) error {
			o := Observation{SeriesID: seriesID, EventID: id, Period: period, Value: value, RawValue: text(item.Raw[rawKey]), Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), DataVersion: "current_visible_revision"}
			return d.append(o, "source_value_missing")
		}
		for _, amount := range amounts {
			if err := appendValue("us.treasury-auctions."+amount.measure, amount.rawKey, amount.value(item.Amounts)); err != nil {
				return nil, err
			}
		}
		if err := appendValue(price.ID, "price_per100", item.PricePer100); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func auctionDates(item fiscaldata.AuctionObservation) (string, string, error) {
	issue, err := dtsDate(item.IssueDate)
	if err != nil {
		return "", "", fmt.Errorf("无效拍卖发行日: %s", item.IssueDate)
	}
	auction, err := dtsDate(item.AuctionDate)
	if err != nil {
		return "", "", fmt.Errorf("无效拍卖日: %s", item.AuctionDate)
	}
	return issue.Format("20060102"), auction.Format("20060102"), nil
}
