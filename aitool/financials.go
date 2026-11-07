package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/financials"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
)

// 返回的期数：年度 5 期，单季 8 期。financials 包多给的几期只用来算同比。
const (
	annualFinancials  = 5
	quarterFinancials = 8
)

const financialsSchema = `{
  "type": "object",
  "properties": {
    "code": {
      "type": "string",
      "description": "一只 A 股或美股的代码或名称，最好用 search_security 返回的 ex_code，如 SH600519、$AAPL。不支持港股、基金和指数",
      "minLength": 1
    },
    "period": {
      "type": "string",
      "enum": ["annual", "quarter"],
      "description": "annual 是最近 5 个年度（默认），quarter 是最近 8 个单季"
    }
  },
  "required": ["code"],
  "additionalProperties": false
}`

// FinancialsTool 返回 get_financials：A 股和美股最近 5 年或 8 个单季的关键财务项及同比。
func FinancialsTool() Tool {
	return Tool{
		Name: "get_financials",
		Description: "查询一只 A 股或美股最近 5 个年度或 8 个单季的关键财务项：营业收入、归母净利润、扣非归母净利润（仅 A 股）、" +
			"毛利率、净利率、经营活动现金流、现金流与净利润之比、资产负债率，以及收入和利润的同比、毛利率的同比变化。" +
			"A 股来自东方财富，美股来自 SEC EDGAR XBRL，两者结构相同。金额单位是亿（报表币种见 currency）。不支持港股、基金和指数。",
		InputSchema: json.RawMessage(financialsSchema),
		Call:        callFinancials,
	}
}

type financialsInput struct {
	Code   string `json:"code"`
	Period string `json:"period"`
}

// financialPeriod 是一期的关键项。金额单位是亿，比率单位是 %；缺失或算不出时是 null，不是 0。
type financialPeriod struct {
	Date  string `json:"date"`            // 报告期末
	Label string `json:"label,omitempty"` // 美股的财年标注，如 2026、FY2026Q1；A 股按日期
	// 单季时利润表和现金流量表是单季值，资产负债表是期末值。
	Revenue            *float64 `json:"revenue"`
	RevenueYoY         *float64 `json:"revenue_yoy"`
	NetProfit          *float64 `json:"net_profit"` // 归属于母公司股东的净利润
	NetProfitYoY       *float64 `json:"net_profit_yoy"`
	DeductNetProfit    *float64 `json:"deduct_net_profit"` // 扣除非经常性损益后的归母净利润，只有 A 股
	DeductNetProfitYoY *float64 `json:"deduct_net_profit_yoy"`
	GrossMargin        *float64 `json:"gross_margin"`        // 银行、证券、保险没有
	GrossMarginChange  *float64 `json:"gross_margin_change"` // 毛利率比上年同期增减的百分点
	NetMargin          *float64 `json:"net_margin"`
	CostIncomeRatio    *float64 `json:"cost_income_ratio"` // 只有 A 股金融企业：业务及管理费 ÷ 营业收入
	OperatingCash      *float64 `json:"operating_cash"`
	CashToProfit       *float64 `json:"cash_to_profit"` // 经营活动现金流 ÷ 净利润，单位是倍；净利润 ≤ 0 时为 null
	DebtRatio          *float64 `json:"debt_ratio"`
}

type financialsData struct {
	ExCode      string            `json:"ex_code"`
	Name        string            `json:"name"`
	Period      string            `json:"period"`
	Currency    string            `json:"currency"`     // 报表币种代码，如 CNY、USD
	Unit        string            `json:"unit"`         // 金额单位，固定为「亿」
	CompanyType string            `json:"company_type"` // general、bank、securities、insurance
	Standard    string            `json:"standard"`     // 会计准则：中国企业会计准则、US GAAP 或 IFRS
	Periods     []financialPeriod `json:"periods"`      // 从新到旧
}

// loadSECFacts 读取美股的 SEC XBRL 数据，测试时替换。
var loadSECFacts = func(ctx context.Context, ticker string) (edgar.Company, *edgar.Facts, error) {
	// 读取失败时 Load 返回默认配置。MCP 由 agent 启动，环境不一定完整（比如没有 HOME），用默认邮箱继续，不让配置挡住查询。
	cfg, err := config.Load(ctx)
	if err != nil {
		slog.WarnContext(ctx, "读取配置失败，使用默认 SEC 联系邮箱", "error", err)
	}
	client := &edgar.Client{Email: cfg.SECEmail}
	company, err := client.Lookup(ctx, ticker)
	if err != nil {
		return company, nil, err
	}
	facts, err := client.CompanyFacts(ctx, company.CIK)
	return company, facts, err
}

func callFinancials(ctx context.Context, input json.RawMessage) (*Result, error) {
	var in financialsInput
	if err := Decode(input, &in); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, Inputf("code 不能为空，请给出一只 A 股或美股的代码或名称")
	}
	var quarterly bool
	switch strings.ToLower(strings.TrimSpace(in.Period)) {
	case "", "annual":
	case "quarter":
		quarterly = true
	default:
		return nil, Inputf("period 只能是 annual 或 quarter")
	}

	secs, err := sina.Resolve(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("搜索证券失败：%w", err)
	}
	if len(secs) == 0 {
		return nil, Inputf("没有找到证券 %s，请检查代码，或先用 search_security 搜索", code)
	}
	sec := secs[0]
	if sec.SecurityType != types.SecurityTypeStock || indexCode.MatchString(sec.ExCode) || !(types.IsACode(sec.ExCode) || strings.HasPrefix(sec.ExCode, "$")) {
		return nil, Inputf("%s 暂不支持财报，只支持 A 股和美股的公司", securityLabel(sec.ExCode, sec.Name))
	}

	data := &financialsData{ExCode: sec.ExCode, Name: sec.Name, Period: "annual", Unit: "亿", Periods: []financialPeriod{}}
	if quarterly {
		data.Period = "quarter"
	}
	var analysis *financials.Analysis
	var source string
	if strings.HasPrefix(sec.ExCode, "$") {
		source = "SEC EDGAR XBRL"
		company, facts, err := loadSECFacts(ctx, strings.TrimPrefix(sec.ExCode, "$"))
		switch {
		case errors.Is(err, edgar.ErrNotFound) || errors.Is(err, edgar.ErrNoXBRL):
			return nil, Inputf("SEC EDGAR 没有 %s 的财报数据，可能不向 SEC 申报", securityLabel(sec.ExCode, sec.Name))
		case err != nil:
			return nil, fmt.Errorf("SEC 财报获取失败：%w", err)
		}
		analysis, err = financials.FromSEC(company.Ticker, facts, quarterly)
		if errors.Is(err, financials.ErrNoSECData) || errors.Is(err, financials.ErrIFRSQuarterly) {
			return nil, Inputf("%s：%v", sec.ExCode, err)
		}
		if err != nil {
			return nil, fmt.Errorf("SEC 财报整理失败：%w", err)
		}
		data.Currency, data.Standard = facts.Currency, string(facts.Standard)
		// 新浪搜索失败时 Resolve 按代码返回，没有名称，用 SEC 的公司名。
		if data.Name == "" {
			data.Name = facts.Name
		}
	} else {
		source = "东方财富财务报表"
		analysis, err = financials.Analyze(ctx, sec.ExCode, quarterly)
		if errors.Is(err, financials.ErrNoData) {
			return nil, Inputf("%s 没有财务报表数据", securityLabel(sec.ExCode, sec.Name))
		}
		if err != nil {
			return nil, fmt.Errorf("财报获取失败：%w", err)
		}
		data.Currency, data.Standard = "CNY", "中国企业会计准则"
	}
	data.CompanyType = analysis.CompanyType

	limit := annualFinancials
	if quarterly {
		limit = quarterFinancials
	}
	var suspect []string
	data.Periods, suspect = financialPeriods(analysis.Periods, limit)

	result := NewResult("get_financials", source, data)
	result.AsOf = data.Periods[0].Date
	result.Notes = append(result.Notes, fmt.Sprintf("金额单位是亿%s，比率单位是 %%，cash_to_profit 单位是倍", currencyLabel(data.Currency)))
	if quarterly {
		result.Notes = append(result.Notes, "单季数据：利润和现金流是该季度单季值，资产负债率是季末值；同比和上年同一季度比较")
	}
	result.Notes = append(result.Notes, "同比在工具里计算：上年同期缺失、为 0 或为负时同比是 null，盈亏变化请直接比较两期金额")
	if strings.HasPrefix(sec.ExCode, "$") {
		result.Notes = append(result.Notes, "美股按公司的财年标注（label），财年不一定和自然年一致；SEC 数据没有扣非净利润")
	}
	if len(suspect) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("%s 的毛利率超过 100%%，营业收入或营业成本数据有误，这些期的营收、毛利率、净利率和以它们为基数的同比已置为 null", strings.Join(suspect, "、")))
	}
	if len(data.Periods) < limit {
		result.Notes = append(result.Notes, fmt.Sprintf("只有 %d 期数据", len(data.Periods)))
	}
	return result, nil
}

// financialPeriods 返回最近 limit 期，从新到旧，并和上年同期比较。periods 按日期升序。
// suspect 是营收数据有误（见 badRevenue）、影响了返回结果的报告期，从新到旧。
func financialPeriods(periods []financials.Period, limit int) (out []financialPeriod, suspect []string) {
	out = make([]financialPeriod, 0, limit)
	bad := map[string]bool{}
	for i := len(periods) - 1; i >= 0 && len(out) < limit; i-- {
		m := periods[i].Metrics
		item := financialPeriod{
			Date: periods[i].Date, Label: periods[i].Label,
			Revenue: yi(m.Revenue), NetProfit: yi(m.ParentNetProfit), DeductNetProfit: yi(m.DeductNetProfit),
			GrossMargin: rounded(m.GrossMargin, 2), NetMargin: rounded(m.NetMargin, 2), CostIncomeRatio: rounded(m.CostIncomeRatio, 2),
			OperatingCash: yi(m.OperatingCash), CashToProfit: rounded(m.CashToProfit, 2), DebtRatio: rounded(m.DebtRatio, 2),
		}
		if previous := yearAgo(periods[:i], periods[i].Date); previous != nil {
			p := previous.Metrics
			item.RevenueYoY = growth(m.Revenue, p.Revenue)
			item.NetProfitYoY = growth(m.ParentNetProfit, p.ParentNetProfit)
			item.DeductNetProfitYoY = growth(m.DeductNetProfit, p.DeductNetProfit)
			if m.GrossMargin != nil && p.GrossMargin != nil {
				item.GrossMarginChange = rounded(ptrTo(*m.GrossMargin-*p.GrossMargin), 2)
			}
			if badRevenue(p) {
				item.RevenueYoY, item.GrossMarginChange = nil, nil
				bad[previous.Date] = true
			}
		}
		if badRevenue(m) {
			item.Revenue, item.RevenueYoY, item.GrossMargin, item.GrossMarginChange, item.NetMargin = nil, nil, nil, nil, nil
			bad[item.Date] = true
		}
		out = append(out, item)
	}
	for date := range bad {
		suspect = append(suspect, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(suspect)))
	return out, suspect
}

// badRevenue 判断营收数据是否有误：毛利率超过 100% 意味着毛利大于营收，营收或营业成本取错了科目（比如蔚来 FY2023 的营收）。
// 不按同比大小判断，营收确实可能成倍增长。
func badRevenue(m financials.Metrics) bool {
	return m.GrossMargin != nil && *m.GrossMargin > 100
}

// yearAgo 在 earlier 里找上年同期：报告期末相隔 350 到 380 天，和 edgar 判断全年的范围一致，美股财年末不固定在月底也能匹配。
// 年度和单季都是和一年前比较，单季不和上一季度比较。
func yearAgo(earlier []financials.Period, date string) *financials.Period {
	end, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return nil
	}
	for i := len(earlier) - 1; i >= 0; i-- {
		start, err := time.Parse(time.DateOnly, earlier[i].Date)
		if err != nil {
			continue
		}
		if days := end.Sub(start).Hours() / 24; days >= 350 && days <= 380 {
			return &earlier[i]
		}
	}
	return nil
}

// growth 是同比，单位 %。上年同期缺失或 ≤ 0 时百分比没有意义，返回 nil。
func growth(current, previous *float64) *float64 {
	if current == nil || previous == nil || *previous <= 0 {
		return nil
	}
	return rounded(ptrTo((*current-*previous) / *previous * 100), 2)
}

// yi 把金额换成亿，保留 4 位小数（精确到万）。
func yi(value *float64) *float64 {
	if value == nil {
		return nil
	}
	return rounded(ptrTo(*value/1e8), 4)
}

func rounded(value *float64, digits int) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return ptrTo(round(*value, digits))
}

func ptrTo(value float64) *float64 { return &value }

// securityLabel 是报错里的证券，没有名称时（新浪搜索失败、按代码返回）只写代码。
func securityLabel(exCode, name string) string {
	if name == "" {
		return exCode
	}
	return exCode + "（" + name + "）"
}

func currencyLabel(code string) string {
	if code == "CNY" {
		return "元（人民币）"
	}
	return edgar.CurrencyName(code)
}
