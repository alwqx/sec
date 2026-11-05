package financials

import (
	"errors"
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/edgar"
)

// FromSEC 用 SEC XBRL 数据（edgar.Facts）整理美股的图表数据，格式和 A 股相同，前端共用一套图表。最近 10 个财年或 12 个单季，
// 金额是公司的列报币种（CurrencyName）。科目比 A 股少：利润只有归母净利润，所以税后到归母净利润之间的终止经营、少数股东损益等合为一项；
// 现金流量表没有期初、期末现金，用资产负债表上期和本期的现金，受限现金和汇率影响合为一项。
func FromSEC(ticker string, facts *edgar.Facts, quarterly bool) (*Analysis, error) {
	limit := annualPeriods
	if quarterly {
		limit = quarterPeriods
	}
	// IFRS 公司在 SEC 只提交 20-F 年报，没有季度数据，和财务报表的说明一致。
	if quarterly && facts.Standard == edgar.IFRS {
		return nil, ErrIFRSQuarterly
	}
	income := facts.Build(edgar.Income, quarterly, 0).Periods
	balance := map[string]edgar.Period{}
	for _, p := range facts.Build(edgar.Balance, quarterly, 0).Periods {
		balance[p.End] = p
	}
	cash := map[string]edgar.Period{}
	for _, p := range facts.Build(edgar.CashFlow, quarterly, 0).Periods {
		cash[p.End] = p
	}
	kind := "general"
	if hasBankIncome(income[:min(limit, len(income))]) {
		kind = "bank"
	}
	analysis := &Analysis{Code: ticker, CompanyType: kind, Quarterly: quarterly, Source: "sec", CurrencyName: edgar.CurrencyName(facts.Currency), Periods: []Period{}}
	// Build 的报告期从新到旧，图表按日期升序。
	for i := min(limit, len(income)) - 1; i >= 0; i-- {
		p := income[i]
		// 算不出财年时（比如还没交过年报）不给标注，前端按日期标注。
		label := ""
		if p.FiscalYear > 0 {
			label = fmt.Sprint(p.FiscalYear)
			if quarterly {
				label = fmt.Sprintf("FY%d%s", p.FiscalYear, p.FiscalPeriod)
			}
		}
		b, c := balance[p.End], cash[p.End]
		// 期初现金是紧挨着的上一期末的现金：上一个报告期和本期相隔一年（单季一个季度），而且有资产负债表；
		// 中间缺期时不画现金瀑布图，否则缺的那一期的变化都会落进差额。
		var previous *edgar.Period
		if i+1 < len(income) && adjacent(income[i+1].End, p.End, quarterly) {
			if q, ok := balance[income[i+1].End]; ok {
				previous = &q
			}
		}
		analysis.Periods = append(analysis.Periods, Period{
			Date: p.End, Label: label, Income: secIncome(p.Values), Cash: secCash(previous, b.Values, c.Values),
			Assets: secAssets(b.Values), Metrics: secMetrics(p.Values, b.Values, c.Values),
		})
	}
	if len(analysis.Periods) == 0 {
		return nil, ErrNoSECData
	}
	return analysis, nil
}

var (
	// ErrNoSECData 表示 SEC XBRL 数据里没有利润表，无法画图。
	ErrNoSECData = errors.New("SEC XBRL 数据里没有这家公司的利润表")
	// ErrIFRSQuarterly 表示按 IFRS 申报的公司没有季度数据。
	ErrIFRSQuarterly = errors.New("按 IFRS 申报的外国公司在 SEC 只提交 20-F 年报，没有季度数据，请查看年度图表")
)

// adjacent 判断 prev 是不是 end 的上一个报告期：年度相隔 350—380 天，单季 80—100 天，和 edgar 判断全年、单季的范围一致。
func adjacent(prev, end string, quarterly bool) bool {
	a, err1 := time.Parse(time.DateOnly, prev)
	b, err2 := time.Parse(time.DateOnly, end)
	if err1 != nil || err2 != nil {
		return false
	}
	days := b.Sub(a).Hours() / 24
	if quarterly {
		return days >= 80 && days <= 100
	}
	return days >= 350 && days <= 380
}

// hasBankIncome 判断是否是银行等金融公司：有净利息收入，没有营业成本和毛利。
func hasBankIncome(periods []edgar.Period) bool {
	bank := false
	for _, p := range periods {
		if p.Values["cost"] != nil || p.Values["gross_profit"] != nil {
			return false
		}
		if p.Values["net_interest_income"] != nil {
			bank = true
		}
	}
	return bank
}

func value(values map[string]*float64, key string) (float64, bool) {
	if v := values[key]; v != nil {
		return *v, true
	}
	return 0, false
}

// item 是一个费用或收益科目：sign 为 -1 时正数是费用。绝对值小于 small 的不单列，由差额合入“其他”。
func item(values map[string]*float64, key, label string, sign, small float64) []Item {
	v, ok := value(values, key)
	if !ok || v == 0 || v*sign > -small && v*sign < small {
		return nil
	}
	return []Item{{label, v * sign}}
}

// secIncome 拆解利润：营业收入 →（营业成本）毛利 →（研发、销售及管理费用）营业利润 → 税前利润 →（所得税）归母净利润。
// 没有的合计跳过，比如银行没有营业成本和营业利润；银行有净利息收入时，先把营业收入拆成净利息收入和非利息收入。
func secIncome(values map[string]*float64) *Bridge {
	revenue, ok := value(values, "revenue")
	net, okNet := value(values, "net_income")
	if !ok || !okNet || revenue <= 0 {
		return nil
	}
	small := revenue * minItemShare
	bridge := &Bridge{Start: &Item{"营业收入", revenue}}
	if nii, ok := value(values, "net_interest_income"); ok && nii > 0 && nii < revenue {
		bridge.Start = nil
		bridge.Steps = append(bridge.Steps, Step{[]Item{{"净利息收入", nii}, {"非利息收入", revenue - nii}}, Item{"营业收入", revenue}})
	}
	from := revenue
	cost, hasCost := value(values, "cost")
	gross, hasGross := value(values, "gross_profit")
	switch {
	case hasGross:
		bridge.Steps = append(bridge.Steps, Step{residual(nil, gross-revenue, "其他", "营业成本"), Item{"毛利", gross}})
		from = gross
	case hasCost:
		from = revenue - cost
		bridge.Steps = append(bridge.Steps, Step{[]Item{{"营业成本", -cost}}, Item{"毛利", from}})
	}
	if operating, ok := value(values, "operating_income"); ok {
		items := append(item(values, "rnd", "研发费用", -1, small), item(values, "sga", "销售及管理费用", -1, small)...)
		bridge.Steps = append(bridge.Steps, Step{residual(items, operating-from-sum(items), "其他营业收益", "其他营业费用"), Item{"营业利润", operating}})
		from = operating
	}
	if pretax, ok := value(values, "pretax_income"); ok {
		// 没有营业利润的公司（比如银行）在这一步扣除信用损失准备和各项费用，差额主要是营业费用（银行的非利息支出）。
		var items []Item
		positive, negative := "营业外收益", "营业外支出"
		if _, ok := value(values, "operating_income"); !ok {
			items = append(item(values, "credit_loss", "信用损失准备", -1, small), append(item(values, "rnd", "研发费用", -1, small), item(values, "sga", "销售及管理费用", -1, small)...)...)
			positive, negative = "其他收益", "营业费用及其他"
		}
		bridge.Steps = append(bridge.Steps, Step{residual(items, pretax-from-sum(items), positive, negative), Item{"税前利润", pretax}})
		from = pretax
	}
	items := item(values, "income_tax", "所得税", -1, 0)
	bridge.Steps = append(bridge.Steps, Step{residual(items, net-from-sum(items), "其他收益（终止经营等）", "其他（少数股东损益、终止经营等）"), Item{"归母净利润", net}})
	return bridge
}

// secCash 是上期末现金加上三类现金流到本期末现金，受限现金、汇率影响等差额合为一项。
func secCash(previous *edgar.Period, balance, cash map[string]*float64) *Bridge {
	if previous == nil {
		return nil
	}
	begin, ok1 := value(previous.Values, "cash")
	end, ok2 := value(balance, "cash")
	if _, ok := value(cash, "operating_cash_flow"); !ok1 || !ok2 || !ok {
		return nil
	}
	items := append(append(item(cash, "operating_cash_flow", "经营活动", 1, 0), item(cash, "investing_cash_flow", "投资活动", 1, 0)...), item(cash, "financing_cash_flow", "筹资活动", 1, 0)...)
	return &Bridge{Start: &Item{"期初现金", begin}, Steps: []Step{{residual(items, end-begin-sum(items), "汇率变动及其他", "汇率变动及其他"), Item{"期末现金", end}}}}
}

// secAssets 是资产构成：现金及等价物、其他流动资产和非流动资产；没有流动资产合计的公司（比如银行）只拆出现金。
func secAssets(balance map[string]*float64) []Item {
	total, ok := value(balance, "total_assets")
	cash, okCash := value(balance, "cash")
	if !ok || total <= 0 || !okCash {
		return nil
	}
	items := []Item{{"现金及等价物", cash}}
	if current, ok := value(balance, "current_assets"); ok && current >= cash && current <= total {
		items = append(items, Item{"其他流动资产", current - cash}, Item{"非流动资产", total - current})
		return items
	}
	if total-cash < -1 {
		return nil
	}
	return residual(items, total-cash, "其他资产", "其他资产")
}

func secMetrics(income, balance, cash map[string]*float64) Metrics {
	m := Metrics{Revenue: income["revenue"], NetProfit: income["net_income"], ParentNetProfit: income["net_income"]}
	if gross := income["gross_profit"]; gross != nil {
		m.GrossMargin = ratio(gross, m.Revenue, 100)
	} else if cost := income["cost"]; cost != nil && m.Revenue != nil {
		m.GrossMargin = ratio(ptr(*m.Revenue-*cost), m.Revenue, 100)
	}
	m.SGARatio = ratio(income["sga"], m.Revenue, 100)
	m.ResearchRatio = ratio(income["rnd"], m.Revenue, 100)
	m.NetMargin = ratio(m.NetProfit, m.Revenue, 100)
	// 不少公司不申报负债合计，用总资产减股东权益合计。
	liabilities := balance["total_liabilities"]
	if liabilities == nil && balance["total_assets"] != nil && balance["total_equity"] != nil {
		liabilities = ptr(*balance["total_assets"] - *balance["total_equity"])
	}
	m.DebtRatio = ratio(liabilities, balance["total_assets"], 100)
	m.OperatingCash, m.InvestingCash, m.FinancingCash = cash["operating_cash_flow"], cash["investing_cash_flow"], cash["financing_cash_flow"]
	m.CashToProfit = ratio(m.OperatingCash, m.NetProfit, 1)
	return m
}
