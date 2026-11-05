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

// ErrIFRS 表示公司按国际会计准则（IFRS）申报，比如提交 20-F 的外国公司，科目体系和 US GAAP 不同，暂不支持。
var ErrIFRS = errors.New("这家公司按国际会计准则（IFRS）申报，美股财报暂只支持 US GAAP")

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
// 每个报告期取第一个有值的科目。Unit 是 USD、USD/shares 或 shares。
type LineItem struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Unit     string   `json:"unit"`
	Concepts []string `json:"-"`
}

// Lines 是各报表的行。银行等金融公司没有营业成本、毛利，用净利息收入、信用损失准备等行。
var Lines = map[Statement][]LineItem{
	Income: {
		{"revenue", "营业收入", "USD", []string{"Revenues", "RevenuesNetOfInterestExpense", "RevenueFromContractWithCustomerExcludingAssessedTax", "SalesRevenueNet", "RevenueFromContractWithCustomerIncludingAssessedTax"}},
		{"cost", "营业成本", "USD", []string{"CostOfGoodsAndServicesSold", "CostOfRevenue", "CostOfGoodsSold", "CostOfGoodsAndServiceExcludingDepreciationDepletionAndAmortization"}},
		{"gross_profit", "毛利", "USD", []string{"GrossProfit"}},
		{"rnd", "研发费用", "USD", []string{"ResearchAndDevelopmentExpense", "ResearchAndDevelopmentExpenseExcludingAcquiredInProcessCost"}},
		{"sga", "销售及管理费用", "USD", []string{"SellingGeneralAndAdministrativeExpense"}},
		{"operating_income", "营业利润", "USD", []string{"OperatingIncomeLoss"}},
		{"net_interest_income", "净利息收入", "USD", []string{"InterestIncomeExpenseNet"}},
		{"credit_loss", "信用损失准备", "USD", []string{"ProvisionForLoanLeaseAndOtherLosses", "ProvisionForLoanAndLeaseLosses", "ProvisionForCreditLosses"}},
		{"pretax_income", "税前利润", "USD", []string{"IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest", "IncomeLossFromContinuingOperationsBeforeIncomeTaxesMinorityInterestAndIncomeLossFromEquityMethodInvestments"}},
		{"income_tax", "所得税", "USD", []string{"IncomeTaxExpenseBenefit"}},
		{"net_income", "归母净利润", "USD", []string{"NetIncomeLoss", "NetIncomeLossAvailableToCommonStockholdersBasic", "ProfitLoss"}},
		{"eps_basic", "基本每股收益", "USD/shares", []string{"EarningsPerShareBasic"}},
		{"eps_diluted", "稀释每股收益", "USD/shares", []string{"EarningsPerShareDiluted", "EarningsPerShareBasicAndDiluted"}},
		{"diluted_shares", "稀释加权股数", "shares", []string{"WeightedAverageNumberOfDilutedSharesOutstanding"}},
		{"dps", "每股股息", "USD/shares", []string{"CommonStockDividendsPerShareDeclared", "CommonStockDividendsPerShareCashPaid"}},
	},
	Balance: {
		{"cash", "现金及等价物", "USD", []string{"CashAndCashEquivalentsAtCarryingValue", "CashCashEquivalentsRestrictedCashAndRestrictedCashEquivalents", "Cash"}},
		{"current_assets", "流动资产", "USD", []string{"AssetsCurrent"}},
		{"total_assets", "总资产", "USD", []string{"Assets"}},
		{"current_liabilities", "流动负债", "USD", []string{"LiabilitiesCurrent"}},
		{"long_term_debt", "长期债务", "USD", []string{"LongTermDebtNoncurrent", "LongTermDebt"}},
		{"total_liabilities", "总负债", "USD", []string{"Liabilities"}},
		{"equity", "归母股东权益", "USD", []string{"StockholdersEquity"}},
		{"total_equity", "股东权益合计", "USD", []string{"StockholdersEquityIncludingPortionAttributableToNoncontrollingInterest", "StockholdersEquity"}},
		{"liabilities_and_equity", "负债和权益合计", "USD", []string{"LiabilitiesAndStockholdersEquity"}},
	},
	CashFlow: {
		{"operating_cash_flow", "经营活动现金流", "USD", []string{"NetCashProvidedByUsedInOperatingActivities", "NetCashProvidedByUsedInOperatingActivitiesContinuingOperations"}},
		{"capex", "资本支出", "USD", []string{"PaymentsToAcquirePropertyPlantAndEquipment", "PaymentsToAcquireProductiveAssets"}},
		{"free_cash_flow", "自由现金流", "USD", nil},
		{"investing_cash_flow", "投资活动现金流", "USD", []string{"NetCashProvidedByUsedInInvestingActivities", "NetCashProvidedByUsedInInvestingActivitiesContinuingOperations"}},
		{"financing_cash_flow", "筹资活动现金流", "USD", []string{"NetCashProvidedByUsedInFinancingActivities", "NetCashProvidedByUsedInFinancingActivitiesContinuingOperations"}},
		{"dividends", "支付股息", "USD", []string{"PaymentsOfDividendsCommonStock", "PaymentsOfDividends"}},
		{"buyback", "回购股票", "USD", []string{"PaymentsForRepurchaseOfCommonStock"}},
	},
}

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

// Facts 是一家公司的 US GAAP 数值，按科目和单位索引。
type Facts struct {
	CIK      int
	Name     string
	concepts map[string]map[string][]fact
	dei      map[string]map[string][]fact // 封面信息，比如流通股数
}

type factConcept struct {
	Units map[string][]fact `json:"units"`
}

// latestReportedFact 比较货币金额的报告期，同期用最新提交时间。股数等通用数据不代表会计准则，
// 旧期间的重述、未来承诺金额也不能掩盖准则切换。
func latestReportedFact(concepts map[string]factConcept) fact {
	var latest fact
	for _, concept := range concepts {
		for unit, values := range concept.Units {
			if len(unit) != 3 || strings.ToUpper(unit) != unit {
				continue
			}
			for _, v := range values {
				if _, err := time.Parse(time.DateOnly, v.End); err != nil || (v.Filed != "" && v.End > v.Filed) {
					continue
				}
				if v.End > latest.End || (v.End == latest.End && v.Filed > latest.Filed) {
					latest = v
				}
			}
		}
	}
	return latest
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
	gaap := raw.Facts["us-gaap"]
	latestIFRS, latestGAAP := latestReportedFact(raw.Facts["ifrs-full"]), latestReportedFact(gaap)
	if latestIFRS.End != "" && (latestIFRS.End > latestGAAP.End ||
		(latestIFRS.End == latestGAAP.End && latestIFRS.Filed >= latestGAAP.Filed)) {
		return nil, ErrIFRS
	}
	if len(gaap) == 0 {
		if len(raw.Facts["ifrs-full"]) > 0 {
			return nil, ErrIFRS
		}
		return nil, errors.New("SEC 没有这家公司的 XBRL 财务数据")
	}
	f := &Facts{CIK: cik, Name: raw.EntityName, concepts: map[string]map[string][]fact{}, dei: map[string]map[string][]fact{}}
	for name, concept := range gaap {
		f.concepts[name] = concept.Units
	}
	for name, concept := range raw.Facts["dei"] {
		f.dei[name] = concept.Units
	}
	return f, nil
}

// Period 是一个报告期。Values 中没有的行是公司没有报告或这一期不适用；Derived 列出推算的行：
// 第四季度的期间数用全年减前三季度累计，自由现金流用经营现金流减资本支出。
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
	Statement Statement  `json:"statement"`
	Quarterly bool       `json:"quarterly"`
	Lines     []LineItem `json:"lines"`
	Periods   []Period   `json:"periods"`
}

func days(start, end string) int {
	a, err1 := time.Parse(time.DateOnly, start)
	b, err2 := time.Parse(time.DateOnly, end)
	if err1 != nil || err2 != nil {
		return -1
	}
	return int(b.Sub(a).Hours() / 24)
}

// 期间长度：全年约 52-53 周，单季约 13 周，前三季度累计约 39 周。
func isYear(d int) bool    { return d >= 350 && d <= 380 }
func isQuarter(d int) bool { return d >= 80 && d <= 100 }

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
		for _, item := range Lines[statement] {
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
		for _, item := range Lines[statement] {
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
		for _, item := range Lines[statement] {
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
		for _, item := range Lines[statement] {
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
	if item.Unit != "USD" || fyStart == "" {
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
	lines := Lines[statement]
	report := &Report{Statement: statement, Quarterly: quarterly, Lines: lines, Periods: []Period{}}
	yearEnds := f.fiscalYearEnds()
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
			if item.Concepts == nil {
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
