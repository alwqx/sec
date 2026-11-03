// Package financials 把三张财务报表整理成图表用的数据：利润和现金的逐项拆解（桑基图和瀑布图），以及各期的比率。
// 每一步都和报表里的合计科目对齐，没有列出的科目合成“其他”，不会出现加总对不上的情况。
package financials

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/alwqx/sec/provider/eastmoney"
)

// ErrNoData 表示没有可用的财务报表。
var ErrNoData = errors.New("暂无财务报表，仅支持沪深北 A 股")

const (
	annualPeriods  = 10
	quarterPeriods = 12
	// 绝对值小于起点这个比例的科目合入“其他”，否则图上会有一堆看不清的细条。
	minItemShare = 0.005
)

// Item 是一个科目。Value 带符号：正数增加下一个合计，负数减少。
type Item struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// Step 是从上一个合计到下一个合计的变化，Items 加上一个合计等于 Total。
type Step struct {
	Items []Item `json:"items"`
	Total Item   `json:"total"`
}

// Bridge 是一串首尾相接的合计。Start 为 nil 时，第一步的科目直接构成第一个合计，比如银行的营业收入。
type Bridge struct {
	Start *Item  `json:"start"`
	Steps []Step `json:"steps"`
}

// Metrics 是一期的比率和主要金额。比率单位是 %，分母小于等于 0 或科目缺失时为 nil。
type Metrics struct {
	Revenue         *float64 `json:"revenue"`
	NetProfit       *float64 `json:"net_profit"`
	ParentNetProfit *float64 `json:"parent_net_profit"`
	DeductNetProfit *float64 `json:"deduct_net_profit"` // 扣除非经常性损益后的归母净利润
	GrossMargin     *float64 `json:"gross_margin"`      // 只有一般企业
	NetMargin       *float64 `json:"net_margin"`
	SaleRatio       *float64 `json:"sale_ratio"`
	ManageRatio     *float64 `json:"manage_ratio"`
	ResearchRatio   *float64 `json:"research_ratio"`
	FinanceRatio    *float64 `json:"finance_ratio"`
	CostIncomeRatio *float64 `json:"cost_income_ratio"` // 只有金融企业：业务及管理费 ÷ 营业收入
	DebtRatio       *float64 `json:"debt_ratio"`
	OperatingCash   *float64 `json:"operating_cash"`
	InvestingCash   *float64 `json:"investing_cash"`
	FinancingCash   *float64 `json:"financing_cash"`
	CashToProfit    *float64 `json:"cash_to_profit"` // 经营活动现金流 ÷ 净利润，净利润小于等于 0 时为 nil
}

// Period 是一个报告期。
type Period struct {
	Date    string  `json:"date"`
	Income  *Bridge `json:"income"` // 合计科目缺失时为 nil
	Cash    *Bridge `json:"cash"`
	Assets  []Item  `json:"assets"` // 资产构成，只有一般企业和银行，拆不平时为 nil
	Metrics Metrics `json:"metrics"`
}

// Analysis 是一家公司的多期财务数据，Periods 按日期升序。
type Analysis struct {
	Code        string   `json:"code"`
	CompanyType string   `json:"company_type"` // general、bank、securities、insurance
	Quarterly   bool     `json:"quarterly"`    // 单季时利润表和现金流量表是单季值，资产负债表是期末值
	Periods     []Period `json:"periods"`
}

var companyTypeNames = map[eastmoney.CompanyType]string{
	eastmoney.CompanyGeneral: "general", eastmoney.CompanyBank: "bank", eastmoney.CompanySecurities: "securities", eastmoney.CompanyInsurance: "insurance",
}

// Analyze 查询最近 10 个年度或 12 个单季的报表。任一张报表失败都返回错误，不输出部分结果。
func Analyze(ctx context.Context, exCode string, quarterly bool) (*Analysis, error) {
	exCode = strings.ToUpper(strings.TrimSpace(exCode))
	kind, err := eastmoney.CompanyTypeOf(ctx, exCode)
	if errors.Is(err, eastmoney.ErrUnsupportedCode) {
		return nil, ErrNoData
	}
	if err != nil {
		return nil, err
	}
	dates, err := eastmoney.StatementDates(ctx, exCode, kind, !quarterly)
	if err != nil {
		return nil, err
	}
	limit := annualPeriods
	if quarterly {
		limit = quarterPeriods
	}
	if len(dates) == 0 {
		return nil, ErrNoData
	}
	dates = dates[:min(limit, len(dates))]

	statements := []eastmoney.Statement{eastmoney.StatementIncome, eastmoney.StatementBalance, eastmoney.StatementCash}
	rows := make([]map[string]map[string]any, len(statements))
	errs := make([]error, len(statements))
	var wg sync.WaitGroup
	for i, statement := range statements {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows[i], errs[i] = eastmoney.GetStatements(ctx, exCode, kind, statement, quarterly, dates)
		}()
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	analysis := &Analysis{Code: exCode, CompanyType: companyTypeNames[kind], Quarterly: quarterly, Periods: []Period{}}
	for _, date := range dates {
		if income := rows[0][date]; income != nil {
			analysis.Periods = append(analysis.Periods, Build(date, kind, income, rows[1][date], rows[2][date]))
		}
	}
	if len(analysis.Periods) == 0 {
		return nil, ErrNoData
	}
	sort.Slice(analysis.Periods, func(i, j int) bool { return analysis.Periods[i].Date < analysis.Periods[j].Date })
	return analysis, nil
}

// Build 整理一期报表。balance 和 cash 可以为 nil。
func Build(date string, kind eastmoney.CompanyType, income, balance, cash map[string]any) Period {
	period := Period{Date: date, Cash: cashBridge(cash), Metrics: metrics(kind, income, balance, cash)}
	if kind == eastmoney.CompanyGeneral {
		period.Income = generalIncome(income)
		period.Assets = generalAssets(balance)
		// 租赁、期货等多元金融公司按行业归为一般企业，但报表可能是金融格式，只有营业收入没有营业总收入。
		if _, ok := num(income, "TOTAL_OPERATE_INCOME"); !ok && period.Income == nil {
			period.Income = financialIncome(kind, income)
		}
	} else {
		period.Income = financialIncome(kind, income)
		if kind == eastmoney.CompanyBank {
			period.Assets = bankAssets(balance)
		}
	}
	return period
}

func num(row map[string]any, key string) (float64, bool) {
	value, ok := row[key].(float64)
	return value, ok
}

// spec 是一个科目。sign 为 1 时正数是收益，为 -1 时正数是费用；方向相反时用 opposite 作为名称，比如财务费用为负时是净收益。
type spec struct {
	key, label, opposite string
	sign                 float64
}

func cost(key, label, opposite string) spec   { return spec{key, label, opposite, -1} }
func income(key, label, opposite string) spec { return spec{key, label, opposite, 1} }

// collect 取出科目并带上符号。绝对值小于 small 的科目不单列，由调用方算差额时合入“其他”。
func collect(row map[string]any, specs []spec, small float64) []Item {
	items := []Item{}
	for _, s := range specs {
		value, ok := num(row, s.key)
		if !ok || value == 0 {
			continue
		}
		signed := value * s.sign
		if math.Abs(signed) < small {
			continue
		}
		label := s.label
		if (signed > 0) != (s.sign > 0) && s.opposite != "" {
			label = s.opposite
		}
		items = append(items, Item{label, signed})
	}
	return items
}

// residual 是让这一步加总对上合计的差额，不到 1 元时忽略。
func residual(items []Item, value float64, positive, negative string) []Item {
	if math.Abs(value) < 1 {
		return items
	}
	label := positive
	if value < 0 {
		label = negative
	}
	return append(items, Item{label, value})
}

func sum(items []Item) float64 {
	total := 0.0
	for _, item := range items {
		total += item.Value
	}
	return total
}

// step 列出科目，再用差额补齐到合计。不单列的小科目也在差额里。
func step(row map[string]any, from float64, specs []spec, small float64, total Item, positive, negative string) Step {
	items := collect(row, specs, math.Abs(small))
	return Step{residual(items, total.Value-from-sum(items), positive, negative), total}
}

// tail 是营业利润之后的三步：营业外收支、所得税和少数股东损益。每步只有一两个科目，再小也单列。
func tail(row map[string]any, operatingProfit float64) ([]Step, bool) {
	totalProfit, ok1 := num(row, "TOTAL_PROFIT")
	netProfit, ok2 := num(row, "NETPROFIT")
	parent, ok3 := num(row, "PARENT_NETPROFIT")
	if !ok1 || !ok2 || !ok3 {
		return nil, false
	}
	return []Step{
		step(row, operatingProfit, []spec{income("NONBUSINESS_INCOME", "营业外收入", ""), cost("NONBUSINESS_EXPENSE", "营业外支出", "")}, 0, Item{"利润总额", totalProfit}, "其他营业外收益", "其他营业外损失"),
		step(row, totalProfit, []spec{cost("INCOME_TAX", "所得税", "所得税收益")}, 0, Item{"净利润", netProfit}, "其他", "其他"),
		step(row, netProfit, []spec{cost("MINORITY_INTEREST", "少数股东损益", "少数股东承担的亏损")}, 0, Item{"归母净利润", parent}, "其他", "其他"),
	}, true
}

// generalIncome 拆解一般企业的利润：营业总收入 → 毛利 → 营业利润 → 利润总额 → 净利润 → 归母净利润。
func generalIncome(row map[string]any) *Bridge {
	revenue, ok1 := num(row, "TOTAL_OPERATE_INCOME")
	totalCost, ok2 := num(row, "TOTAL_OPERATE_COST")
	operatingProfit, ok3 := num(row, "OPERATE_PROFIT")
	if !ok1 || !ok2 || !ok3 || revenue <= 0 {
		return nil
	}
	small := revenue * minItemShare
	bridge := &Bridge{Start: &Item{"营业总收入", revenue}}
	from := revenue
	costs := []spec{
		cost("OPERATE_TAX_ADD", "税金及附加", ""), cost("SALE_EXPENSE", "销售费用", ""), cost("MANAGE_EXPENSE", "管理费用", ""),
		cost("RESEARCH_EXPENSE", "研发费用", ""), cost("FINANCE_EXPENSE", "财务费用", "财务净收益"),
	}
	// 2019 年以前的报表把资产减值损失算在营业总成本里，之后改为单列的收益科目；两个都有值时只用新科目，否则会算两次。
	if _, ok := num(row, "ASSET_IMPAIRMENT_INCOME"); !ok {
		costs = append(costs, cost("ASSET_IMPAIRMENT_LOSS", "资产减值损失", "资产减值转回"))
	}
	operateCost, hasCost := num(row, "OPERATE_COST")
	if hasCost {
		from = revenue - operateCost
		bridge.Steps = append(bridge.Steps, Step{[]Item{{"营业成本", -operateCost}}, Item{"毛利", from}})
	}
	costItems := collect(row, costs, small)
	// 营业总成本里没有单列的科目（如利息支出、手续费支出）和不单列的小科目，合计后对上营业总成本。
	costItems = residual(costItems, -(totalCost-operateCost)-sum(costItems), "其他成本项（净收益）", "其他营业成本")
	gains := []spec{
		income("INVEST_INCOME", "投资收益", "投资损失"), income("FAIRVALUE_CHANGE_INCOME", "公允价值变动收益", "公允价值变动损失"),
		income("OTHER_INCOME", "其他收益", ""), income("ASSET_DISPOSAL_INCOME", "资产处置收益", "资产处置损失"),
		income("CREDIT_IMPAIRMENT_INCOME", "信用减值转回", "信用减值损失"), income("ASSET_IMPAIRMENT_INCOME", "资产减值转回", "资产减值损失"),
	}
	gainItems := collect(row, gains, small)
	items := append(costItems, gainItems...)
	bridge.Steps = append(bridge.Steps, Step{residual(items, operatingProfit-from-sum(items), "其他经营收益", "其他经营损失"), Item{"营业利润", operatingProfit}})
	steps, ok := tail(row, operatingProfit)
	if !ok {
		return nil
	}
	bridge.Steps = append(bridge.Steps, steps...)
	return bridge
}

var financialRevenue = map[eastmoney.CompanyType][]spec{
	eastmoney.CompanyBank: {
		income("INTEREST_NI", "利息净收入", "利息净支出"), income("FEE_COMMISSION_NI", "手续费及佣金净收入", "手续费及佣金净支出"),
		income("INVEST_INCOME", "投资收益", "投资损失"), income("FAIRVALUE_CHANGE_INCOME", "公允价值变动收益", "公允价值变动损失"),
		income("EXCHANGE_INCOME", "汇兑收益", "汇兑损失"), income("OTHER_BUSINESS_INCOME", "其他业务收入", ""),
	},
	eastmoney.CompanySecurities: {
		income("FEE_COMMISSION_NI", "手续费及佣金净收入", "手续费及佣金净支出"), income("INTEREST_NI", "利息净收入", "利息净支出"),
		income("INVEST_INCOME", "投资收益", "投资损失"), income("FAIRVALUE_CHANGE_INCOME", "公允价值变动收益", "公允价值变动损失"),
		income("EXCHANGE_INCOME", "汇兑收益", "汇兑损失"), income("OTHER_BUSINESS_INCOME", "其他业务收入", ""),
	},
	eastmoney.CompanyInsurance: {
		income("EARNED_PREMIUM", "已赚保费", ""), income("BANK_INTEREST_NI", "银行业务利息净收入", "银行业务利息净支出"),
		income("UNINSURANCE_CNI", "非保险业务手续费及佣金净收入", ""), income("INVEST_INCOME", "投资收益", "投资损失"),
		income("FAIRVALUE_CHANGE", "公允价值变动收益", "公允价值变动损失"), income("EXCHANGE_INCOME", "汇兑收益", "汇兑损失"),
		income("OTHER_BUSINESS_INCOME", "其他业务收入", ""),
	},
}

// financialIncome 拆解金融企业的利润：收入构成 → 营业收入 → 营业利润 → 利润总额 → 净利润 → 归母净利润。
func financialIncome(kind eastmoney.CompanyType, row map[string]any) *Bridge {
	revenue, ok1 := num(row, "OPERATE_INCOME")
	operatingProfit, ok2 := num(row, "OPERATE_PROFIT")
	if !ok1 || !ok2 || revenue <= 0 {
		return nil
	}
	small := revenue * minItemShare
	expenses := []spec{
		cost("OPERATE_TAX_ADD", "税金及附加", ""), cost("BUSINESS_MANAGE_EXPENSE", "业务及管理费", ""),
		cost("CREDIT_IMPAIRMENT_LOSS", "信用减值损失", "信用减值转回"), cost("ASSET_IMPAIRMENT_LOSS", "资产减值损失", "资产减值转回"),
		cost("OTHER_BUSINESS_COST", "其他业务成本", ""),
	}
	// 银行的利息支出已经在利息净收入里扣掉，只有保险把利息支出列在营业支出里。
	if kind == eastmoney.CompanyInsurance {
		expenses = append(expenses, cost("INTEREST_EXPENSE", "利息支出", ""))
	}
	bridge := &Bridge{Steps: []Step{
		step(row, 0, financialRevenue[kind], small, Item{"营业收入", revenue}, "其他收入", "其他收入（为负）"),
		step(row, revenue, expenses, small, Item{"营业利润", operatingProfit}, "其他营业收益", "其他营业支出"),
	}}
	steps, ok := tail(row, operatingProfit)
	if !ok {
		return nil
	}
	bridge.Steps = append(bridge.Steps, steps...)
	return bridge
}

// cashBridge 是期初现金加上三类现金流和汇率影响，到期末现金。
func cashBridge(row map[string]any) *Bridge {
	begin, ok1 := num(row, "BEGIN_CCE")
	end, ok2 := num(row, "END_CCE")
	if !ok1 || !ok2 {
		return nil
	}
	flows := []spec{
		income("NETCASH_OPERATE", "经营活动", ""), income("NETCASH_INVEST", "投资活动", ""),
		income("NETCASH_FINANCE", "筹资活动", ""), income("RATE_CHANGE_EFFECT", "汇率变动", ""),
	}
	return &Bridge{Start: &Item{"期初现金", begin}, Steps: []Step{step(row, begin, flows, 0, Item{"期末现金", end}, "其他", "其他")}}
}

// assetGroup 是资产构成里的一项，由若干科目相加。
type assetGroup struct {
	label string
	keys  []string
}

// composition 列出资产构成，剩下的合为“其他资产”。拆出的科目超过总资产时说明科目有重叠，不返回。
func composition(row map[string]any, groups []assetGroup) []Item {
	total, ok := num(row, "TOTAL_ASSETS")
	if !ok || total <= 0 {
		return nil
	}
	items := []Item{}
	for _, group := range groups {
		value := 0.0
		for _, key := range group.keys {
			if v, ok := num(row, key); ok {
				value += v
			}
		}
		if value > 0 {
			items = append(items, Item{group.label, value})
		}
	}
	other := total - sum(items)
	if len(items) == 0 || other < -1 {
		return nil
	}
	return residual(items, other, "其他资产", "其他资产")
}

func generalAssets(row map[string]any) []Item {
	receivable := []string{"NOTE_ACCOUNTS_RECE", "FINANCE_RECE"}
	if _, ok := num(row, "NOTE_ACCOUNTS_RECE"); !ok {
		receivable = []string{"ACCOUNTS_RECE", "NOTE_RECE", "FINANCE_RECE"}
	}
	return composition(row, []assetGroup{
		{"货币资金", []string{"MONETARYFUNDS"}}, {"应收票据及账款", receivable}, {"存货", []string{"INVENTORY"}},
		{"固定资产和在建工程", []string{"FIXED_ASSET", "CIP"}},
	})
}

func bankAssets(row map[string]any) []Item {
	return composition(row, []assetGroup{
		{"发放贷款和垫款", []string{"LOAN_ADVANCE"}}, {"金融投资", []string{"FVTPL_FINASSET", "AMORTIZE_COST_FINASSET", "FVTOCI_FINASSET", "FVTOCI_EQUITY_INVEST"}},
		{"现金及存放央行", []string{"CASH_DEPOSIT_PBC"}}, {"同业资产", []string{"DEPOSIT_INTERBANK", "LEND_FUND", "BUY_RESALE_FINASSET"}},
	})
}

func ptr(value float64) *float64 { return &value }

func value(row map[string]any, key string) *float64 {
	if v, ok := num(row, key); ok {
		return &v
	}
	return nil
}

// ratio 返回 a ÷ b × scale，b 小于等于 0 或任一科目缺失时为 nil。
func ratio(a, b *float64, scale float64) *float64 {
	if a == nil || b == nil || *b <= 0 {
		return nil
	}
	return ptr(*a / *b * scale)
}

func metrics(kind eastmoney.CompanyType, income, balance, cash map[string]any) Metrics {
	m := Metrics{NetProfit: value(income, "NETPROFIT"), ParentNetProfit: value(income, "PARENT_NETPROFIT"), DeductNetProfit: value(income, "DEDUCT_PARENT_NETPROFIT")}
	if kind == eastmoney.CompanyGeneral {
		m.Revenue = value(income, "TOTAL_OPERATE_INCOME")
		if m.Revenue == nil {
			m.Revenue = value(income, "OPERATE_INCOME")
		}
		// 毛利率按营业收入计算，不含金融子公司的利息收入。
		sales := value(income, "OPERATE_INCOME")
		if sales == nil || *sales <= 0 {
			sales = m.Revenue
		}
		if operateCost := value(income, "OPERATE_COST"); operateCost != nil && sales != nil {
			m.GrossMargin = ratio(ptr(*sales-*operateCost), sales, 100)
		}
		m.SaleRatio = ratio(value(income, "SALE_EXPENSE"), m.Revenue, 100)
		m.ManageRatio = ratio(value(income, "MANAGE_EXPENSE"), m.Revenue, 100)
		m.ResearchRatio = ratio(value(income, "RESEARCH_EXPENSE"), m.Revenue, 100)
		m.FinanceRatio = ratio(value(income, "FINANCE_EXPENSE"), m.Revenue, 100)
	} else {
		m.Revenue = value(income, "OPERATE_INCOME")
		m.CostIncomeRatio = ratio(value(income, "BUSINESS_MANAGE_EXPENSE"), m.Revenue, 100)
	}
	m.NetMargin = ratio(m.NetProfit, m.Revenue, 100)
	m.DebtRatio = ratio(value(balance, "TOTAL_LIABILITIES"), value(balance, "TOTAL_ASSETS"), 100)
	m.OperatingCash, m.InvestingCash, m.FinancingCash = value(cash, "NETCASH_OPERATE"), value(cash, "NETCASH_INVEST"), value(cash, "NETCASH_FINANCE")
	m.CashToProfit = ratio(m.OperatingCash, m.NetProfit, 1)
	return m
}
