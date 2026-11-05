package edgar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const companyFactsURL = "https://data.sec.gov/api/xbrl/companyfacts/"

// Standard 是财报的会计准则。
type Standard string

const (
	USGAAP Standard = "US GAAP"
	IFRS   Standard = "IFRS"
)

// Statement 是一张报表。
type Statement string

const (
	Income   Statement = "income"
	Balance  Statement = "balance"
	CashFlow Statement = "cashflow"
)

// Statements 是全部报表，按利润表、资产负债表、现金流量表排列。
var Statements = []Statement{Income, Balance, CashFlow}

// Name 返回报表的中文名。
func (s Statement) Name() string {
	switch s {
	case Income:
		return "利润表"
	case Balance:
		return "资产负债表"
	case CashFlow:
		return "现金流量表"
	}
	return string(s)
}

// LineItem 是报表的一行。Concepts 是候选的 US GAAP 科目，按优先级排列：同一个指标在不同年份可能用不同科目
// （比如苹果的收入 2018 年前是 Revenues，之后是 RevenueFromContractWithCustomerExcludingAssessedTax），
// 每个报告期取第一个有值的科目。Unit 是金额的币种、每股金额（币种/shares）或 shares；Lines 里用 USD 表示金额，
// 报表按公司的列报币种取数（见 Facts.Currency），比如阿里巴巴是 CNY 和 CNY/shares。
//
// IFRS 是按国际会计准则申报时的候选科目（ifrs-full），为空的行（比如没有对应科目的信用损失准备）不显示。
// Derived 表示推算行（自由现金流），没有科目，由 Build 用其他行计算。
type LineItem struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Unit     string   `json:"unit"`
	Concepts []string `json:"-"`
	IFRS     []string `json:"-"`
	Derived  bool     `json:"-"`
}

// Lines 是各报表的行。银行等金融公司没有营业成本、毛利，用净利息收入、信用损失准备等行。
// IFRS 的销售及管理费用只用合并列报的科目，分别列报销售、管理费用的公司（比如 SAP）这一行为空，不拼凑。
// IFRS 的归母净利润：汇丰 2023 年起从归属于母公司所有者改为归属于母公司普通股股东（不含优先股、永续债的派息），差别很小，不另外处理。
var Lines = map[Statement][]LineItem{
	Income: {
		{"revenue", "营业收入", "USD", []string{"Revenues", "RevenuesNetOfInterestExpense", "RevenueFromContractWithCustomerExcludingAssessedTax", "SalesRevenueNet", "RevenueFromContractWithCustomerIncludingAssessedTax"}, []string{"Revenue", "RevenueFromContractsWithCustomers"}, false},
		{"cost", "营业成本", "USD", []string{"CostOfGoodsAndServicesSold", "CostOfRevenue", "CostOfGoodsSold", "CostOfGoodsAndServiceExcludingDepreciationDepletionAndAmortization"}, []string{"CostOfSales"}, false},
		{"gross_profit", "毛利", "USD", []string{"GrossProfit"}, []string{"GrossProfit"}, false},
		{"rnd", "研发费用", "USD", []string{"ResearchAndDevelopmentExpense", "ResearchAndDevelopmentExpenseExcludingAcquiredInProcessCost"}, []string{"ResearchAndDevelopmentExpense"}, false},
		{"sga", "销售及管理费用", "USD", []string{"SellingGeneralAndAdministrativeExpense"}, []string{"SellingGeneralAndAdministrativeExpense"}, false},
		{"operating_income", "营业利润", "USD", []string{"OperatingIncomeLoss"}, []string{"ProfitLossFromOperatingActivities"}, false},
		{"net_interest_income", "净利息收入", "USD", []string{"InterestIncomeExpenseNet"}, []string{"InterestRevenueExpense"}, false},
		{"credit_loss", "信用损失准备", "USD", []string{"ProvisionForLoanLeaseAndOtherLosses", "ProvisionForLoanAndLeaseLosses", "ProvisionForCreditLosses"}, nil, false},
		{"pretax_income", "税前利润", "USD", []string{"IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest", "IncomeLossFromContinuingOperationsBeforeIncomeTaxesMinorityInterestAndIncomeLossFromEquityMethodInvestments"}, []string{"ProfitLossBeforeTax"}, false},
		{"income_tax", "所得税", "USD", []string{"IncomeTaxExpenseBenefit"}, []string{"IncomeTaxExpenseContinuingOperations"}, false},
		{"net_income", "归母净利润", "USD", []string{"NetIncomeLoss", "NetIncomeLossAvailableToCommonStockholdersBasic", "ProfitLoss"}, []string{"ProfitLossAttributableToOwnersOfParent", "ProfitLossAttributableToOrdinaryEquityHoldersOfParentEntity"}, false},
		{"eps_basic", "基本每股收益", "USD/shares", []string{"EarningsPerShareBasic"}, []string{"BasicEarningsLossPerShare"}, false},
		{"eps_diluted", "稀释每股收益", "USD/shares", []string{"EarningsPerShareDiluted", "EarningsPerShareBasicAndDiluted"}, []string{"DilutedEarningsLossPerShare"}, false},
		{"diluted_shares", "稀释加权股数", "shares", []string{"WeightedAverageNumberOfDilutedSharesOutstanding"}, []string{"AdjustedWeightedAverageShares"}, false},
		{"dps", "每股股息", "USD/shares", []string{"CommonStockDividendsPerShareDeclared", "CommonStockDividendsPerShareCashPaid"}, []string{"DividendsRecognisedAsDistributionsToOwnersPerShare", "DividendsPaidOrdinarySharesPerShare"}, false},
	},
	Balance: {
		{"cash", "现金及等价物", "USD", []string{"CashAndCashEquivalentsAtCarryingValue", "CashCashEquivalentsRestrictedCashAndRestrictedCashEquivalents", "Cash"}, []string{"CashAndCashEquivalents"}, false},
		{"current_assets", "流动资产", "USD", []string{"AssetsCurrent"}, []string{"CurrentAssets"}, false},
		{"total_assets", "总资产", "USD", []string{"Assets"}, []string{"Assets"}, false},
		{"current_liabilities", "流动负债", "USD", []string{"LiabilitiesCurrent"}, []string{"CurrentLiabilities"}, false},
		{"long_term_debt", "长期债务", "USD", []string{"LongTermDebtNoncurrent", "LongTermDebt"}, []string{"LongtermBorrowings"}, false},
		{"total_liabilities", "总负债", "USD", []string{"Liabilities"}, []string{"Liabilities"}, false},
		{"equity", "归母股东权益", "USD", []string{"StockholdersEquity"}, []string{"EquityAttributableToOwnersOfParent"}, false},
		{"total_equity", "股东权益合计", "USD", []string{"StockholdersEquityIncludingPortionAttributableToNoncontrollingInterest", "StockholdersEquity"}, []string{"Equity"}, false},
		{"liabilities_and_equity", "负债和权益合计", "USD", []string{"LiabilitiesAndStockholdersEquity"}, []string{"EquityAndLiabilities"}, false},
	},
	CashFlow: {
		{"operating_cash_flow", "经营活动现金流", "USD", []string{"NetCashProvidedByUsedInOperatingActivities", "NetCashProvidedByUsedInOperatingActivitiesContinuingOperations"}, []string{"CashFlowsFromUsedInOperatingActivities"}, false},
		{"capex", "资本支出", "USD", []string{"PaymentsToAcquirePropertyPlantAndEquipment", "PaymentsToAcquireProductiveAssets"}, []string{"PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities", "PurchaseOfPropertyPlantAndEquipmentIntangibleAssetsOtherThanGoodwillInvestmentPropertyAndOtherNoncurrentAssets"}, false},
		{Key: "free_cash_flow", Label: "自由现金流", Unit: "USD", Derived: true},
		{"investing_cash_flow", "投资活动现金流", "USD", []string{"NetCashProvidedByUsedInInvestingActivities", "NetCashProvidedByUsedInInvestingActivitiesContinuingOperations"}, []string{"CashFlowsFromUsedInInvestingActivities"}, false},
		{"financing_cash_flow", "筹资活动现金流", "USD", []string{"NetCashProvidedByUsedInFinancingActivities", "NetCashProvidedByUsedInFinancingActivitiesContinuingOperations"}, []string{"CashFlowsFromUsedInFinancingActivities"}, false},
		{"dividends", "支付股息", "USD", []string{"PaymentsOfDividendsCommonStock", "PaymentsOfDividends"}, []string{"DividendsPaidClassifiedAsFinancingActivities", "DividendsPaidToEquityHoldersOfParentClassifiedAsFinancingActivities"}, false},
		{"buyback", "回购股票", "USD", []string{"PaymentsForRepurchaseOfCommonStock"}, []string{"PaymentsToAcquireOrRedeemEntitysShares"}, false},
	},
}

// ifrsFallback 是公司完全没有申报某一行的 IFRS 候选科目时才用的科目：不申报归母净利润的公司（比如没有少数股东的诺和诺德）
// 用净利润；申报过归母净利润的公司缺个别年份时不用它补，避免同一行混用含少数股东损益的口径。
var ifrsFallback = map[string]string{"net_income": "ProfitLoss"}

// fact 是 companyfacts 中的一条数值。Start 为空表示时点数（资产负债表），否则是期间数。
// fy、fp 是所在申报的财年和期间，前期比较数也标成本次申报的 fy，所以不能用来判断数值所属的年份。
type fact struct {
	Start string  `json:"start"`
	End   string  `json:"end"`
	Val   float64 `json:"val"`
	FP    string  `json:"fp"`
	Form  string  `json:"form"`
	Filed string  `json:"filed"`
	Accn  string  `json:"accn"`
}

// Facts 是一家公司的 XBRL 数值（US GAAP 或 IFRS，见 Standard），按科目和单位索引。
type Facts struct {
	CIK      int
	Name     string
	Standard Standard
	// Currency 是报表金额的币种：公司的列报币种，按最近一份年报里数值最多的币种识别。外国公司的年报常常附带
	// 最近一年的美元便利折算数，不能当成列报币种，否则只能读到零散几年、按各年汇率折算的美元数。
	Currency string
	// AnnualForm 是最近一份年报的表格类型（修订也记为原类型，不带 /A），比如 10-K；外国私人发行人提交 20-F、40-F（见 ForeignIssuer）。
	AnnualForm string
	concepts   map[string]map[string][]fact
	dei        map[string]map[string][]fact // 封面信息，比如流通股数
	resolved   map[Statement][]LineItem     // 按 Currency 换好单位的各报表行，见 resolveLines
}

type factConcept struct {
	Units map[string][]fact `json:"units"`
}

// annualFiling 是一种会计准则下最近一份年报里报表科目的金额数值，用来判断会计准则和列报币种。
// 最近一份按申报日取，不按截止日：期后事项的截止日晚于报告期，不能代表报表。
type annualFiling struct {
	filed, accn string
	form        string                     // 表格类型，比如 10-K、20-F
	end         string                     // 这份年报里最晚的截止日，即报告期
	counts      map[string]int             // 这份年报里各币种的数值条数
	years       map[string]map[string]bool // 所有年报中各币种有全年数据的截止日
}

// total 返回这份年报的数值条数。
func (a annualFiling) total() int {
	n := 0
	for _, c := range a.counts {
		n += c
	}
	return n
}

// latestAnnualFiling 只看报表用到的金额科目，不看附注（比如美元公司的欧元债务、日元套期名义金额），
// 也不看股数等不代表会计准则的通用数据。晚于申报日的截止日是未来承诺金额，不是报告期。
func latestAnnualFiling(concepts map[string]factConcept, standard Standard) annualFiling {
	amount := map[string]bool{}
	for _, lines := range Lines {
		for _, item := range lines {
			if item.Unit == "USD" {
				for _, concept := range item.concepts(standard) {
					amount[concept] = true
				}
				if fallback, ok := ifrsFallback[item.Key]; ok && standard == IFRS {
					amount[fallback] = true
				}
			}
		}
	}
	annual := func(v fact) bool {
		_, err := time.Parse(time.DateOnly, v.End)
		return err == nil && isAnnualForm(v.Form) && (v.Filed == "" || v.End <= v.Filed)
	}
	filing := annualFiling{counts: map[string]int{}, years: map[string]map[string]bool{}}
	for name := range amount {
		for unit, values := range concepts[name].Units {
			if !isCurrency(unit) {
				continue
			}
			for _, v := range values {
				if !annual(v) {
					continue
				}
				if v.Filed > filing.filed || (v.Filed == filing.filed && v.Accn > filing.accn) {
					filing.filed, filing.accn, filing.form = v.Filed, v.Accn, v.Form
				}
				if v.Start != "" && isYear(days(v.Start, v.End)) {
					if filing.years[unit] == nil {
						filing.years[unit] = map[string]bool{}
					}
					filing.years[unit][v.End] = true
				}
			}
		}
	}
	for name := range amount {
		for unit, values := range concepts[name].Units {
			if !isCurrency(unit) {
				continue
			}
			for _, v := range values {
				if filing.filed != "" && v.Filed == filing.filed && v.Accn == filing.accn && annual(v) {
					filing.counts[unit]++
					filing.end = max(filing.end, v.End)
				}
			}
		}
	}
	return filing
}

// accountingStandard 选择会计准则：两种准则最近的年报是同一份时，用其中报表科目数值更多的一方（避免 US GAAP 公司
// 零星标注的 ifrs-full 数值让报表整体切到 IFRS 而变空）；不是同一份时用报告期更晚的一方，报告期相同时用申报更晚的一方。
// 都没有年报数值（比如首份年报之前只有 10-Q）时用 US GAAP，只有 IFRS 数据时用 IFRS。
func accountingStandard(gaap, ifrs annualFiling, hasGAAP bool) Standard {
	var useIFRS bool
	switch {
	case ifrs.filed == "" && gaap.filed == "":
		useIFRS = !hasGAAP
	case ifrs.filed == "" || gaap.filed == "":
		useIFRS = gaap.filed == ""
	case ifrs.filed == gaap.filed && ifrs.accn == gaap.accn:
		useIFRS = ifrs.total() >= gaap.total()
	case ifrs.end != gaap.end:
		useIFRS = ifrs.end > gaap.end
	default:
		useIFRS = ifrs.filed > gaap.filed
	}
	if useIFRS {
		return IFRS
	}
	return USGAAP
}

// reportingCurrency 返回公司的列报币种：最近一份年报里数值条数最多的币种。本币除了当年数还有前两年的比较数，
// 附带的美元便利折算一般只有当年，所以本币明显更多；条数相同时优先 USD。选出的币种没有任何全年数据时，
// 退回到全年数据期数最多的币种，避免报表变空。没有年报数值时用 USD。
func reportingCurrency(filing annualFiling) string {
	currency := best("USD", filing.counts)
	if len(filing.years[currency]) == 0 {
		periods := map[string]int{}
		for unit, ends := range filing.years {
			periods[unit] = len(ends)
		}
		currency = best(currency, periods)
	}
	return currency
}

// best 返回数量最多的币种；数量相同时优先 USD，其次按代码排序，结果不受 map 遍历顺序影响。
func best(fallback string, counts map[string]int) string {
	currency := fallback
	for unit, n := range counts {
		if n > counts[currency] || (n == counts[currency] && currency != "USD" && (unit == "USD" || unit < currency)) {
			currency = unit
		}
	}
	return currency
}

// isCurrency 判断单位是否是币种代码（ISO 4217 三个大写字母），比如 USD、CNY。
func isCurrency(unit string) bool {
	return len(unit) == 3 && strings.ToUpper(unit) == unit
}

// resolveLines 按 Currency 生成各报表的行，Build 和识别报告期时反复用到，只算一次。
func (f *Facts) resolveLines() {
	f.resolved = map[Statement][]LineItem{}
	for statement := range Lines {
		f.resolved[statement] = f.convertLines(statement)
	}
}

// unit 把 Lines 里的单位换成报表的币种：USD 换成 Currency，USD/shares 换成 Currency/shares。
func (f *Facts) unit(unit string) string {
	switch unit {
	case "USD":
		return f.Currency
	case "USD/shares":
		return f.Currency + "/shares"
	}
	return unit
}

// lines 返回报表的行，单位换成报表的币种。
func (f *Facts) lines(statement Statement) []LineItem {
	if lines, ok := f.resolved[statement]; ok {
		return lines
	}
	return f.convertLines(statement)
}

// convertLines 按准则选候选科目、按币种换单位；IFRS 没有对应科目的行去掉，推算行保留。
func (f *Facts) convertLines(statement Statement) []LineItem {
	lines := make([]LineItem, 0, len(Lines[statement]))
	for _, item := range Lines[statement] {
		item.Concepts = item.concepts(f.Standard)
		if fallback, ok := ifrsFallback[item.Key]; ok && f.Standard == IFRS && !f.reports(item.Concepts) {
			item.Concepts = []string{fallback}
		}
		if item.Concepts == nil && !item.Derived {
			continue
		}
		item.Unit = f.unit(item.Unit)
		lines = append(lines, item)
	}
	return lines
}

// reports 判断公司是否申报过其中任一科目。
func (f *Facts) reports(concepts []string) bool {
	for _, concept := range concepts {
		if len(f.concepts[concept]) > 0 {
			return true
		}
	}
	return false
}

// concepts 返回这一行在某个准则下的候选科目。
func (item LineItem) concepts(standard Standard) []string {
	if standard == IFRS {
		return item.IFRS
	}
	return item.Concepts
}

// CompanyFacts 读取公司的全部 XBRL 数值。
func (c *Client) CompanyFacts(ctx context.Context, cik int) (*Facts, error) {
	body, err := c.get(ctx, fmt.Sprintf("%sCIK%010d.json", companyFactsURL, cik))
	if err != nil {
		return nil, err
	}
	return parseFacts(cik, body)
}

// ParseFacts 解析 companyfacts 接口返回的 JSON，例如读取缓存或测试数据。
func ParseFacts(cik int, body []byte) (*Facts, error) { return parseFacts(cik, body) }

func parseFacts(cik int, body []byte) (*Facts, error) {
	var raw struct {
		EntityName string                            `json:"entityName"`
		Facts      map[string]map[string]factConcept `json:"facts"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("SEC XBRL 数据格式无效：%w", err)
	}
	// 最近的报告期按 IFRS 申报时用 IFRS（比如 2021 年转用 IFRS 的丰田），不回退到更早的 US GAAP 数据。
	gaap, ifrs := raw.Facts["us-gaap"], raw.Facts["ifrs-full"]
	if len(gaap) == 0 && len(ifrs) == 0 {
		return nil, errors.New("SEC 没有这家公司的 XBRL 财务数据")
	}
	gaapFiling, ifrsFiling := latestAnnualFiling(gaap, USGAAP), latestAnnualFiling(ifrs, IFRS)
	standard, data, filing := USGAAP, gaap, gaapFiling
	if accountingStandard(gaapFiling, ifrsFiling, len(gaap) > 0) == IFRS {
		standard, data, filing = IFRS, ifrs, ifrsFiling
	}
	f := &Facts{CIK: cik, Name: raw.EntityName, Standard: standard, Currency: reportingCurrency(filing), AnnualForm: strings.TrimSuffix(filing.form, "/A"), concepts: map[string]map[string][]fact{}, dei: map[string]map[string][]fact{}}
	for name, concept := range data {
		f.concepts[name] = concept.Units
	}
	for name, concept := range raw.Facts["dei"] {
		f.dei[name] = concept.Units
	}
	f.resolveLines()
	return f, nil
}

// Period 是一个报告期。Values 中没有的行是公司没有报告或这一期不适用；Derived 列出推算的行：
// 第四季度的期间数用全年减前三季度累计，自由现金流用经营现金流减资本支出，没有申报的归母股东权益用合计减少数股东权益。
type Period struct {
	End          string              `json:"end"`
	Start        string              `json:"start,omitempty"` // 期间数的起始日，资产负债表为空
	FiscalYear   int                 `json:"fiscal_year"`     // 截止日所在财年，用截止日的年份
	FiscalPeriod string              `json:"fiscal_period"`   // FY、Q1、Q2、Q3、Q4
	Values       map[string]*float64 `json:"values"`
	Derived      []string            `json:"derived,omitempty"`
	// Filed 是每一行数值第一次披露的日期（推算值取用到的数值中最晚的），在这天之后才能看到；
	// 估值历史据此避免用到当时还没公布的数据。按行记录，后来才补报的其他行不会推迟这一行。
	Filed map[string]string `json:"-"`
}

// Report 是一张报表，报告期从新到旧。
type Report struct {
	Statement Statement `json:"statement"`
	Quarterly bool      `json:"quarterly"`
	Standard  Standard  `json:"standard"` // US GAAP 或 IFRS
	// Currency 是金额的币种代码，CurrencyName 是中文名，比如 CNY、人民币。
	Currency     string     `json:"currency"`
	CurrencyName string     `json:"currency_name"`
	Lines        []LineItem `json:"lines"`
	Periods      []Period   `json:"periods"`
}

func days(start, end string) int {
	a, err1 := time.Parse(time.DateOnly, start)
	b, err2 := time.Parse(time.DateOnly, end)
	if err1 != nil || err2 != nil {
		return -1
	}
	return int(b.Sub(a).Hours() / 24)
}

// currencyNames 是常见列报币种的中文名。
var currencyNames = map[string]string{
	"USD": "美元", "CNY": "人民币", "HKD": "港元", "TWD": "新台币", "JPY": "日元", "KRW": "韩元",
	"EUR": "欧元", "GBP": "英镑", "CHF": "瑞士法郎", "DKK": "丹麦克朗", "SEK": "瑞典克朗", "NOK": "挪威克朗",
	"CAD": "加元", "AUD": "澳元", "SGD": "新加坡元", "INR": "印度卢比", "BRL": "巴西雷亚尔", "MXN": "墨西哥比索",
	"ILS": "以色列新谢克尔", "ZAR": "南非兰特",
}

// CurrencyName 返回币种的中文名，不认识的币种返回代码本身。
func CurrencyName(code string) string {
	if name, ok := currencyNames[code]; ok {
		return name
	}
	return code
}

// 期间长度：全年约 52-53 周，单季约 13 周，前三季度累计约 39 周。
func isYear(d int) bool    { return d >= 350 && d <= 380 }
func isQuarter(d int) bool { return d >= 80 && d <= 100 }

// minorityInterest 是少数股东权益，用于没有申报归母股东权益的公司（比如 Visa、AT&T 只申报含少数股东权益的合计）推算归母数。
var minorityInterest = LineItem{Key: "minority_interest", Unit: "USD", Concepts: []string{"MinorityInterest"}}

// noMinorityInterest 判断公司从哪天起没有少数股东：最后一期少数股东权益之后，又有一份年报没有少数股东权益（Visa 最后一期是 2010 年，
// 2011 年年报里没有），截止日晚于 last 的报告期视为没有少数股东，从这份年报的申报日 filed 起才能确认。
// 从没申报过少数股东权益（可能用了别的科目），或之后还没有这样的年报（比如只在年报里标注少数股东权益）时，filed 为空。
func (f *Facts) noMinorityInterest(yearEnds []string, total LineItem) (last, filed string) {
	for _, v := range f.concepts["MinorityInterest"][f.Currency] {
		if v.Start == "" && v.End > last {
			last = v.End
		}
	}
	if last == "" {
		return "", ""
	}
	for _, end := range yearEnds {
		if end <= last {
			continue
		}
		if v, ok := f.pick(total, end, func(v fact) bool { return v.Start == "" }); ok {
			return last, v.Filed
		}
	}
	return last, ""
}

// ForeignIssuer 判断公司是否是外国私人发行人：最近一份年报是 20-F 或 40-F（含修订）。
func (f *Facts) ForeignIssuer() bool {
	switch f.AnnualForm {
	case "20-F", "40-F":
		return true
	}
	return false
}

func isAnnualForm(form string) bool {
	switch strings.TrimSuffix(form, "/A") {
	case "10-K", "10-KT", "20-F", "40-F":
		return true
	}
	return false
}

// fiscalQuarter 只接受距财年第一天约 13、26、39、52 周的截止日，排除重组等产生的过渡期间。
func fiscalQuarter(start, end string) int {
	d := days(start, end)
	switch {
	case isQuarter(d):
		return 1
	case d >= 170 && d <= 195:
		return 2
	case d >= 260 && d <= 285:
		return 3
	case isYear(d):
		return 4
	}
	return 0
}

func quarterNumber(period string) int {
	switch period {
	case "Q1":
		return 1
	case "Q2":
		return 2
	case "Q3":
		return 3
	}
	return 0
}

// pick 在候选科目中找截止日为 end、满足 match 的数值，按科目顺序取第一个有值的科目；
// 同一科目有多个值（比如后来的申报重述了前期数）时用最新申报的。
func (f *Facts) pick(item LineItem, end string, match func(fact) bool) (fact, bool) {
	for _, concept := range item.Concepts {
		var best fact
		found := false
		for _, v := range f.concepts[concept][item.Unit] {
			if v.End == end && match(v) && (!found || v.Filed > best.Filed) {
				best, found = v, true
			}
		}
		if found {
			// 数值用最新申报的（可能是重述），Filed 改成同一期间第一次披露的日期：估值历史判断"当时能看到"时，
			// 不能因为下一年的申报又列出这个比较数，就把它推迟到下一年才可见。
			for _, v := range f.concepts[concept][item.Unit] {
				if v.End == end && v.Start == best.Start && match(v) && v.Filed != "" && v.Filed < best.Filed {
					best.Filed = v.Filed
				}
			}
			return best, true
		}
	}
	return fact{}, false
}

// fiscalYearEnds 返回各财年的截止日，从利润表和现金流量表的全年数中收集，从旧到新。
func (f *Facts) fiscalYearEnds() []string {
	seen := map[string]bool{}
	for _, statement := range []Statement{Income, CashFlow} {
		for _, item := range f.lines(statement) {
			for _, concept := range item.Concepts {
				for _, v := range f.concepts[concept][item.Unit] {
					if v.Start != "" && isYear(days(v.Start, v.End)) && isAnnualForm(v.Form) {
						seen[v.End] = true
					}
				}
			}
		}
	}
	return sortedKeys(seen)
}

// quarterEnds 返回各季度的截止日：单季数和年初累计数（6 个月、9 个月）的截止日，加上财年截止日（第四季度），从旧到新。
// 现金流量表只有累计数，有的公司某个季度也只报告累计数。
func (f *Facts) quarterEnds(yearEnds []string) []string {
	seen := map[string]bool{}
	starts := map[string]string{}
	for _, end := range yearEnds {
		seen[end] = true
	}
	for _, statement := range []Statement{Income, CashFlow} {
		for _, item := range f.lines(statement) {
			for _, concept := range item.Concepts {
				for _, v := range f.concepts[concept][item.Unit] {
					start, ok := starts[v.End]
					if !ok {
						start = f.fiscalStart(v.End, yearEnds)
						starts[v.End] = start
					}
					if q := fiscalQuarter(start, v.End); q >= 1 && q <= 3 &&
						(isQuarter(days(v.Start, v.End)) || nearStart(v.Start, start)) {
						seen[v.End] = true
					}
				}
			}
		}
	}
	return sortedKeys(seen)
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// fiscalLabel 返回截止日所在的财年和季度。财年用财年截止日的年份（苹果 2026-09-26 是 2026 财年），
// 季度按距离财年截止日的月数推算，截止日晚于已知最后一个财年时按一年后推算。
func fiscalLabel(end string, yearEnds []string) (int, string) {
	date, err := time.Parse(time.DateOnly, end)
	if err != nil || len(yearEnds) == 0 {
		return 0, ""
	}
	var fy time.Time
	for _, text := range yearEnds {
		if text >= end {
			fy, _ = time.Parse(time.DateOnly, text)
			break
		}
	}
	if fy.IsZero() {
		last, _ := time.Parse(time.DateOnly, yearEnds[len(yearEnds)-1])
		fy = last.AddDate(1, 0, 0)
	}
	// 距财年截止日约 0、3、6、9 个月分别是 Q4、Q3、Q2、Q1。
	months := int((fy.Sub(date).Hours()/24 + 15) / 30.4)
	quarter := 4 - (months+1)/3
	if quarter < 1 {
		quarter = 1
	}
	return fy.Year(), fmt.Sprintf("Q%d", quarter)
}

// fiscalStart 返回截止日所在财年的第一天：上一个财年截止日的次日；没有上一个财年时用全年数，
// 尚无年报时再从 10-Q 的累计期间或最新单季期间识别。
func (f *Facts) fiscalStart(end string, yearEnds []string) string {
	previous := ""
	for _, text := range yearEnds {
		if text < end {
			previous = text
		}
	}
	if previous != "" {
		day, _ := time.Parse(time.DateOnly, previous)
		return day.AddDate(0, 0, 1).Format(time.DateOnly)
	}
	for _, statement := range []Statement{Income, CashFlow} {
		for _, item := range f.lines(statement) {
			for _, concept := range item.Concepts {
				for _, v := range f.concepts[concept][item.Unit] {
					if v.Start != "" && v.End >= end && isYear(days(v.Start, v.End)) && v.Start <= end {
						return v.Start
					}
				}
			}
		}
	}
	return f.quarterlyStart(end)
}

// quarterlyStart 在尚无年报时从 10-Q 的年初累计数识别财年起点。fp 属于整份申报，
// 因此只有长度也符合 fp 的期间才能当作累计数，不能把 Q3 申报里的 Q2 单季比较数当成 Q1。
func (f *Facts) quarterlyStart(end string) string {
	start, latestEnd := "", ""
	var latest fact
	for _, statement := range []Statement{Income, CashFlow} {
		for _, item := range f.lines(statement) {
			for _, concept := range item.Concepts {
				for _, v := range f.concepts[concept][item.Unit] {
					q := quarterNumber(v.FP)
					if strings.TrimSuffix(v.Form, "/A") != "10-Q" || q == 0 {
						continue
					}
					if v.End >= end && v.Start <= end && fiscalQuarter(v.Start, v.End) == q && v.Start > start {
						start = v.Start
					}
					// 缺少累计数时，只用最新报告期的单季元数据，避免误用前期比较数的 fp。
					if v.End > latestEnd {
						latestEnd = v.End
					}
					if isQuarter(days(v.Start, v.End)) && (v.End > latest.End || (v.End == latest.End && v.Filed > latest.Filed)) {
						latest = v
					}
				}
			}
		}
	}
	if start != "" {
		return start
	}
	if latest.End != "" && latest.End == latestEnd {
		first, _ := time.Parse(time.DateOnly, latest.Start)
		inferred := first.AddDate(0, -3*(quarterNumber(latest.FP)-1), 0).Format(time.DateOnly)
		if end <= latestEnd && fiscalQuarter(inferred, end) > 0 {
			return inferred
		}
	}
	return ""
}

// nearStart 判断两个起始日是否相差不超过 3 天：财年第一天在不同申报里偶尔差一两天。
func nearStart(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	d := days(a, b)
	return d >= -3 && d <= 3
}

// quarterValue 返回期间数的单季值：有单季数时直接用；没有时用年初累计数相减，
// 比如现金流量表只报告 6 个月、9 个月累计，第二季度 = 6 个月 - 3 个月，第四季度 = 全年 - 9 个月。
// prevEnd 是同一财年上一季度的截止日，第一季度为空。
// 第四个返回值是用到的数值中最晚的申报日。
func (f *Facts) quarterValue(item LineItem, end, prevEnd, fyStart string) (float64, bool, bool, string) {
	if v, ok := f.pick(item, end, func(v fact) bool { return v.Start != "" && isQuarter(days(v.Start, v.End)) }); ok {
		return v.Val, false, true, v.Filed
	}
	// 每股收益和股数不能相减。
	if item.Unit != f.Currency || fyStart == "" {
		return 0, false, false, ""
	}
	ytd := func(at string) (fact, bool) {
		return f.pick(item, at, func(v fact) bool { return v.Start != "" && nearStart(v.Start, fyStart) })
	}
	if fiscalQuarter(fyStart, end) == 1 {
		if current, ok := ytd(end); ok {
			return current.Val, false, true, current.Filed
		}
	}
	if prevEnd == "" || !isQuarter(days(prevEnd, end)) {
		return 0, false, false, ""
	}
	// 同一行的候选科目可能口径不同，累计数相减必须在同一个科目内完成。
	for _, concept := range item.Concepts {
		single := item
		single.Concepts = []string{concept}
		current, ok := f.pick(single, end, func(v fact) bool { return nearStart(v.Start, fyStart) })
		if !ok {
			continue
		}
		previous, ok := f.pick(single, prevEnd, func(v fact) bool { return nearStart(v.Start, current.Start) && nearStart(v.Start, fyStart) })
		if ok {
			return current.Val - previous.Val, true, true, max(current.Filed, previous.Filed)
		}
	}
	return 0, false, false, ""
}

// Build 生成报表，报告期从新到旧，最多 limit 期（0 表示全部）。年度用年报的全年数；季度用单季数，
// 没有单季数时用年初累计数相减推算（见 quarterValue），每股收益和股数不能相减，留空。
func (f *Facts) Build(statement Statement, quarterly bool, limit int) *Report {
	lines := f.lines(statement)
	report := &Report{Statement: statement, Quarterly: quarterly, Standard: f.Standard, Currency: f.Currency, CurrencyName: CurrencyName(f.Currency), Lines: lines, Periods: []Period{}}
	// IFRS 公司在 SEC 只提交 20-F 年报，6-K 里的数值不成体系（多是年报的比较数，个别公司有半年数），不整理季度报表。
	if quarterly && f.Standard == IFRS {
		return report
	}
	yearEnds := f.fiscalYearEnds()
	// 没有申报归母股东权益时，用合计减少数股东权益推算（见 noMinorityInterest）。
	var nciLast, nciNoneFiled string
	var totalEquity, nci LineItem
	deriveEquity := statement == Balance && f.Standard == USGAAP
	if deriveEquity {
		for _, item := range lines {
			if item.Key == "total_equity" {
				totalEquity = item
			}
		}
		nci = minorityInterest
		nci.Unit = f.unit(nci.Unit)
		nciLast, nciNoneFiled = f.noMinorityInterest(yearEnds, totalEquity)
	}
	ends := yearEnds
	if quarterly {
		ends = f.quarterEnds(yearEnds)
	}
	for i := len(ends) - 1; i >= 0; i-- {
		end := ends[i]
		period := Period{End: end, Values: map[string]*float64{}, Filed: map[string]string{}}
		period.FiscalYear, period.FiscalPeriod = fiscalLabel(end, yearEnds)
		// 同一财年的上一季度截止日，用于年初累计数相减。
		fyStart, prevEnd := "", ""
		if quarterly {
			fyStart = f.fiscalStart(end, yearEnds)
			if period.FiscalYear == 0 && fyStart != "" {
				first, _ := time.Parse(time.DateOnly, fyStart)
				period.FiscalYear = first.AddDate(1, 0, -1).Year()
				period.FiscalPeriod = fmt.Sprintf("Q%d", fiscalQuarter(fyStart, end))
			}
			if i > 0 && fyStart != "" && ends[i-1] >= fyStart {
				prevEnd = ends[i-1]
			}
		}
		if !quarterly {
			period.FiscalPeriod = "FY"
		}
		for _, item := range lines {
			if item.Derived {
				continue
			}
			if statement == Balance {
				if v, ok := f.pick(item, end, func(v fact) bool { return v.Start == "" }); ok {
					period.Values[item.Key] = &v.Val
					period.Filed[item.Key] = v.Filed
				}
				continue
			}
			if !quarterly {
				if v, ok := f.pick(item, end, func(v fact) bool { return v.Start != "" && isYear(days(v.Start, v.End)) }); ok {
					period.Values[item.Key] = &v.Val
					period.Start = v.Start
					period.Filed[item.Key] = v.Filed
				}
				continue
			}
			if value, derived, ok, filed := f.quarterValue(item, end, prevEnd, fyStart); ok {
				period.Values[item.Key] = &value
				period.Filed[item.Key] = filed
				if derived {
					period.Derived = append(period.Derived, item.Key)
				}
			}
		}
		if total := period.Values["total_equity"]; deriveEquity && period.Values["equity"] == nil && total != nil {
			if v, ok := f.pick(nci, end, func(v fact) bool { return v.Start == "" }); ok {
				value := *total - v.Val
				period.Values["equity"] = &value
				period.Filed["equity"] = max(period.Filed["total_equity"], v.Filed)
				period.Derived = append(period.Derived, "equity")
			} else if nciNoneFiled != "" && end > nciLast {
				value := *total
				period.Values["equity"] = &value
				period.Filed["equity"] = max(period.Filed["total_equity"], nciNoneFiled)
				period.Derived = append(period.Derived, "equity")
			}
		}
		if statement == CashFlow {
			ocf, capex := period.Values["operating_cash_flow"], period.Values["capex"]
			if ocf != nil && capex != nil {
				value := *ocf - *capex
				period.Values["free_cash_flow"] = &value
				period.Derived = append(period.Derived, "free_cash_flow")
			}
		}
		if len(period.Values) == 0 {
			continue
		}
		report.Periods = append(report.Periods, period)
		if limit > 0 && len(report.Periods) >= limit {
			break
		}
	}
	return report
}

// SharePoint 是一次申报封面上的流通股数：Date 是股数的统计日，Filed 是申报日。
type SharePoint struct {
	Date    string
	Filed   string
	Shares  float64
	Classes int // 封面列出的普通股类别数
}

// DepositaryReceiptRatio 返回封面上最近申报的存托凭证比例（1 股 ADS 对应的普通股数）。多数公司按证券类别标注，
// companyfacts 不收录，读不到时 ok 为 false。
func (f *Facts) DepositaryReceiptRatio() (ratio float64, ok bool) {
	var latest fact
	for _, v := range f.dei["EntityListingDepositoryReceiptRatio"]["pure"] {
		if v.Val > 0 && (!ok || v.Filed > latest.Filed || (v.Filed == latest.Filed && v.End > latest.End)) {
			latest, ok = v, true
		}
	}
	return latest.Val, ok
}

// SharesOutstanding 返回各次申报封面上的普通股流通股数，按统计日升序。有多类普通股（比如 A、B、C 股）的公司，
// 同一次申报会分别列出各类股数，这里直接相加，不知道各类之间的折算比例（比如伯克希尔 1 股 A 折 1500 股 B）。
func (f *Facts) SharesOutstanding() []SharePoint {
	type key struct{ accn, end string }
	sums := map[key]*SharePoint{}
	for _, v := range f.dei["EntityCommonStockSharesOutstanding"]["shares"] {
		if v.Val <= 0 || v.End == "" {
			continue
		}
		k := key{v.Accn, v.End}
		if sums[k] == nil {
			sums[k] = &SharePoint{Date: v.End, Filed: v.Filed}
		}
		sums[k].Shares += v.Val
		sums[k].Classes++
	}
	points := make([]SharePoint, 0, len(sums))
	for _, p := range sums {
		points = append(points, *p)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Date != points[j].Date {
			return points[i].Date < points[j].Date
		}
		return points[i].Filed < points[j].Filed
	})
	return points
}

// WeightedDilutedShares 返回各次申报里单季度的稀释加权平均股数，保留每次申报当时的数值（不用后来的重述），按统计日升序。
// 有些公司封面只按股票类别申报股数（companyfacts 不含），这时用它近似流通股数；Date 是季度截止日，
// 拆股发生在申报之前时，申报的数值已经按拆股调整。多类股票按各自的折算比例合计（比如伯克希尔折成 A 股）。
func (f *Facts) WeightedDilutedShares() []SharePoint {
	points := []SharePoint{}
	for _, v := range f.concepts["WeightedAverageNumberOfDilutedSharesOutstanding"]["shares"] {
		if v.Val <= 0 || v.Start == "" || v.Filed == "" || !isQuarter(days(v.Start, v.End)) {
			continue
		}
		points = append(points, SharePoint{Date: v.End, Filed: v.Filed, Shares: v.Val})
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Date != points[j].Date {
			return points[i].Date < points[j].Date
		}
		return points[i].Filed < points[j].Filed
	})
	return points
}
