package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/valuation"
)

const valuationSchema = `{
  "type": "object",
  "properties": {
    "code": {
      "type": "string",
      "description": "一只 A 股或美股的代码或名称，最好用 search_security 返回的 ex_code，如 SH600519、$AAPL。不支持港股、基金、指数和外国公司的 ADS",
      "minLength": 1
    },
    "growth": {
      "type": "number",
      "description": "DCF 前 5 年的增长率，单位 %，-50 到 100。不传时用近 5 年归母净利润复合增长率"
    },
    "wacc": {
      "type": "number",
      "description": "DCF 折现率，单位 %，1 到 50，默认 8"
    },
    "terminal_growth": {
      "type": "number",
      "description": "DCF 永续增长率，单位 %，-5 到 20，默认 3；须比折现率至少低 1 个百分点"
    },
    "margin_of_safety": {
      "type": "number",
      "description": "DCF 安全边际，单位 %，0 到 90，默认 20"
    }
  },
  "required": ["code"],
  "additionalProperties": false
}`

// ValuationTool 返回 get_valuation：A 股和美股的估值倍数、历史分位、行业对比、股息率、DCF 和规则汇总。
func ValuationTool() Tool {
	return Tool{
		Name: "get_valuation",
		Description: "查询一只 A 股或美股的估值：PE、PB、PS 的当前值和历史分位，股息率，PEG，格雷厄姆数，同行业对比（仅 A 股），" +
			"DCF 每股价值，以及按规则得出的 signals 和 assessment（偏低、中性、偏高区间）。所有数值都由 sec 按固定规则机械计算，" +
			"回答时要保留 notes 里的口径说明和 disclaimer，不能把 assessment 或 DCF 结果说成买卖建议或目标价。" +
			"可以传 growth、wacc 等参数看 DCF 在不同假设下的结果。不支持港股、基金、指数和外国公司的 ADS。",
		InputSchema: json.RawMessage(valuationSchema),
		Call:        callValuation,
	}
}

type valuationInput struct {
	Code           string   `json:"code"`
	Growth         *float64 `json:"growth"`
	WACC           *float64 `json:"wacc"`
	TerminalGrowth *float64 `json:"terminal_growth"`
	MarginOfSafety *float64 `json:"margin_of_safety"`
}

// multipleSummary 是一种估值倍数的当前值和历史分布，不含逐日序列。
type multipleSummary struct {
	Current    *float64 `json:"current"` // 倍数 ≤ 0（亏损、净资产为负）或缺失时为 null，原值见 raw
	Raw        *float64 `json:"raw"`
	Percentile *float64 `json:"percentile"` // 历史上不高于当前值的交易日占比，单位 %；样本不足或当前值为 null 时为 null
	Since      string   `json:"since,omitempty"`
	Samples    int      `json:"samples"`
	Min        *float64 `json:"min"`
	Median     *float64 `json:"median"`
	Max        *float64 `json:"max"`
}

type growthSummary struct {
	Rate     *float64 `json:"rate"` // 归母净利润复合增长率，单位 %
	FromYear string   `json:"from_year"`
	ToYear   string   `json:"to_year"`
}

type pendingDividend struct {
	Report   string   `json:"report"`
	Progress string   `json:"progress"`
	PerShare *float64 `json:"per_share"`
	ExDate   string   `json:"ex_date"`
}

type dividendSummary struct {
	Yield            *float64          `json:"yield"`     // 最近完整财年的税前每股现金分红 ÷ 收盘价，单位 %
	PerShare         float64           `json:"per_share"` // 最近完整财年的每股现金分红，按当前股本
	FiscalYear       string            `json:"fiscal_year"`
	Average5Y        *float64          `json:"average_5y"` // 近 5 个完整财年平均分红对应的股息率，单位 %
	ConsecutiveYears int               `json:"consecutive_years"`
	YieldPercentile  *float64          `json:"yield_percentile"` // 当前股息率在有分红的交易日里的分位，单位 %
	Pending          []pendingDividend `json:"pending"`          // 还没有除息、不计入股息率的分红
}

type peerSummary struct {
	Median     *float64 `json:"median"`
	Rank       *int     `json:"rank"` // 从低到高，1 是最低
	Samples    int      `json:"samples"`
	Percentile *float64 `json:"percentile"`
	Premium    *float64 `json:"premium"` // 相对行业中位数，单位 %
}

type peersSummary struct {
	Board string      `json:"board"`
	Date  string      `json:"date"`
	PE    peerSummary `json:"pe"`
	PB    peerSummary `json:"pb"`
	PS    peerSummary `json:"ps"`
}

type dcfSummary struct {
	Applicable     bool     `json:"applicable"`
	Growth         *float64 `json:"growth"` // 实际使用的增长率，单位 %
	GrowthSource   string   `json:"growth_source"`
	WACC           float64  `json:"wacc"`
	TerminalGrowth float64  `json:"terminal_growth"`
	MarginOfSafety float64  `json:"margin_of_safety"`
	PerShare       *float64 `json:"per_share"`      // 每股价值
	FairPrice      *float64 `json:"fair_price"`     // 扣除安全边际后的每股价值
	Upside         *float64 `json:"upside"`         // fair_price 相对收盘价，单位 %
	TerminalShare  *float64 `json:"terminal_share"` // 终值现值占比，单位 %
}

type valuationData struct {
	ExCode       string             `json:"ex_code"`
	Name         string             `json:"name"`
	Industry     string             `json:"industry"`
	Currency     string             `json:"currency"`
	TradeDate    string             `json:"trade_date"`
	Price        float64            `json:"price"`      // 收盘价，不复权
	MarketCap    float64            `json:"market_cap"` // 单位：亿
	PE           multipleSummary    `json:"pe"`         // TTM
	PB           multipleSummary    `json:"pb"`         // 最近一期
	PS           multipleSummary    `json:"ps"`         // TTM
	EPS          *float64           `json:"eps"`
	BVPS         *float64           `json:"bvps"`
	ROE          *float64           `json:"roe"`
	ROEYear      string             `json:"roe_year"`
	ProfitTTM    *float64           `json:"profit_ttm"`  // 单位：亿
	RevenueTTM   *float64           `json:"revenue_ttm"` // 单位：亿
	Growth       growthSummary      `json:"growth"`
	PEG          *float64           `json:"peg"`
	Graham       *float64           `json:"graham"`
	GrahamMargin *float64           `json:"graham_margin"` // 格雷厄姆数相对收盘价，单位 %
	Dividends    *dividendSummary   `json:"dividends"`     // 不可用时为 null，原因见 notes
	Peers        *peersSummary      `json:"peers"`         // 不可用时为 null，原因见 notes
	DCF          dcfSummary         `json:"dcf"`
	Signals      []valuation.Signal `json:"signals"`
	Assessment   string             `json:"assessment"`
}

// analyzeA 和 analyzeUS 计算估值，测试时替换。
var (
	analyzeA  = valuation.Analyze
	analyzeUS = func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error) {
		client := secClient(ctx)
		return valuation.AnalyzeUS(ctx, client, exCode, params, secFacts.Loader(client))
	}
)

func callValuation(ctx context.Context, input json.RawMessage) (*Result, error) {
	var in valuationInput
	if err := Decode(input, &in); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, Inputf("code 不能为空，请给出一只 A 股或美股的代码或名称")
	}
	params := valuation.DefaultDCFParams()
	params.Growth = in.Growth
	for _, set := range []struct {
		value  *float64
		target *float64
	}{{in.WACC, &params.WACC}, {in.TerminalGrowth, &params.TerminalGrowth}, {in.MarginOfSafety, &params.MarginOfSafety}} {
		if set.value != nil {
			*set.target = *set.value
		}
	}
	if err := params.Validate(); err != nil {
		return nil, Inputf("DCF 参数不合法：%v", err)
	}

	secs, err := sina.Resolve(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("搜索证券失败：%w", err)
	}
	if len(secs) == 0 {
		return nil, Inputf("没有找到证券 %s，请检查代码，或先用 search_security 搜索", code)
	}
	// 和 sec valuation 一样：纯数字代码依次试所有沪深北结果（000001 是上证指数也是平安银行），其他只试第一条。
	var noData error
	for _, sec := range valuation.Candidates(secs, code) {
		var r *valuation.Result
		if strings.HasPrefix(sec.ExCode, "$") {
			r, err = analyzeUS(ctx, sec.ExCode, params)
		} else {
			r, err = analyzeA(ctx, sec.ExCode, params)
		}
		switch {
		case errors.Is(err, valuation.ErrNoData):
			if noData == nil {
				noData = Inputf("%s 没有估值数据：%v", securityLabel(sec.ExCode, sec.Name), err)
			}
			continue
		case errors.Is(err, valuation.ErrForeign):
			return nil, Inputf("%s：%v", securityLabel(sec.ExCode, sec.Name), err)
		case err != nil:
			return nil, fmt.Errorf("估值计算失败：%w", err)
		}
		return valuationResult(sec, r), nil
	}
	return nil, noData
}

// valuationResult 把完整估值精简成给模型的结果：去掉逐日序列、历史情景价格、年报明细和同行业逐家数据。
func valuationResult(sec *sina.BasicSecurity, r *valuation.Result) *Result {
	data := &valuationData{
		ExCode: sec.ExCode, Name: sec.Name, Industry: r.Industry, Currency: r.Currency, TradeDate: r.TradeDate,
		Price: r.Price, MarketCap: round(r.MarketCap/1e8, 2),
		PE: summarizeMultiple(r.PE), PB: summarizeMultiple(r.PB), PS: summarizeMultiple(r.PS),
		EPS: rounded(r.EPS, 4), BVPS: rounded(r.BVPS, 4), ROE: rounded(r.ROE, 2), ROEYear: r.ROEYear,
		ProfitTTM: yi(r.ProfitTTM), RevenueTTM: yi(r.RevenueTTM),
		Growth: growthSummary{Rate: rounded(r.Growth.Rate, 2), FromYear: r.Growth.FromYear, ToYear: r.Growth.ToYear},
		PEG:    rounded(r.PEG, 2), Graham: rounded(r.Graham, 2), GrahamMargin: rounded(r.GrahamMargin, 2),
		Dividends: summarizeDividends(r.Dividends), Peers: summarizePeers(r.Peers),
		DCF: dcfSummary{
			Applicable: r.DCF.Applicable, Growth: rounded(r.DCF.Params.Growth, 2), GrowthSource: r.DCF.GrowthSource,
			WACC: r.DCF.Params.WACC, TerminalGrowth: r.DCF.Params.TerminalGrowth, MarginOfSafety: r.DCF.Params.MarginOfSafety,
			PerShare: rounded(r.DCF.PerShare, 2), FairPrice: rounded(r.DCF.FairPrice, 2), Upside: rounded(r.DCF.Upside, 2),
			TerminalShare: rounded(r.DCF.TerminalShare, 2),
		},
		Signals: r.Signals, Assessment: r.Assessment,
	}
	// 和其他工具一样用新浪的名称；新浪搜索失败、按代码返回时用估值数据里的名称。
	if data.Name == "" {
		data.Name = r.Name
	}
	if data.Signals == nil {
		data.Signals = []valuation.Signal{}
	}

	source := "东方财富估值与业绩报表"
	if r.Currency == "USD" {
		source = "SEC EDGAR XBRL 与新浪美股日线"
	}
	result := NewResult("get_valuation", source, data)
	result.AsOf = r.TradeDate
	result.Disclaimer = r.Disclaimer
	result.Notes = append(result.Notes, fmt.Sprintf("金额单位是亿%s，比率和分位单位是 %%；PE、PS 用最近四个季度（TTM），PB 用最近一期净资产", currencyLabel(r.Currency)))
	for _, note := range []string{r.SharesNote, r.DividendsNote, r.PeersNote, r.Growth.Note, r.DCF.Note, r.DCF.Warning} {
		if note != "" {
			result.Notes = append(result.Notes, note)
		}
	}
	for _, m := range []struct {
		name string
		m    valuation.Multiple
	}{{"PE", r.PE}, {"PB", r.PB}, {"PS", r.PS}} {
		if h := m.m.History; h != nil && h.Short {
			result.Notes = append(result.Notes, fmt.Sprintf("%s 历史样本只有 %d 个交易日，不计算分位", m.name, h.Samples))
		}
	}
	if r.Dividends != nil && len(r.Dividends.Mismatches) > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("近 5 年有 %d 次分红和新浪的记录不一致（东方财富缺记录或金额不同），股息率可能偏低", len(r.Dividends.Mismatches)))
	}
	if r.DCF.Applicable {
		result.Notes = append(result.Notes, "DCF 对增长率和折现率高度敏感，per_share 和 fair_price 是按所列参数机械计算的情景值，不是目标价")
	}
	return result
}

func summarizeMultiple(m valuation.Multiple) multipleSummary {
	s := multipleSummary{Current: rounded(m.Current, 2), Raw: rounded(m.Raw, 2)}
	if h := m.History; h != nil {
		s.Since, s.Samples = h.Since, h.Samples
		s.Min, s.Median, s.Max = rounded(&h.Min, 2), rounded(&h.Median, 2), rounded(&h.Max, 2)
		s.Percentile = rounded(h.Percentile, 2)
	}
	return s
}

func summarizeDividends(d *valuation.Dividends) *dividendSummary {
	if d == nil {
		return nil
	}
	s := &dividendSummary{
		Yield: rounded(d.Yield, 2), PerShare: round(d.PerShare, 4), FiscalYear: d.FiscalYear, Average5Y: rounded(d.Average5Y, 2),
		ConsecutiveYears: d.ConsecutiveYears, Pending: []pendingDividend{},
	}
	if d.History != nil {
		s.YieldPercentile = rounded(d.History.Percentile, 2)
	}
	for _, p := range d.Pending {
		s.Pending = append(s.Pending, pendingDividend{Report: p.Report, Progress: p.Progress, PerShare: rounded(p.PerShare, 4), ExDate: p.ExDate})
	}
	return s
}

func summarizePeers(p *valuation.Peers) *peersSummary {
	if p == nil {
		return nil
	}
	peer := func(m valuation.PeerMetric) peerSummary {
		return peerSummary{Median: rounded(m.Median, 2), Rank: m.Rank, Samples: m.Samples, Percentile: rounded(m.Percentile, 2), Premium: rounded(m.Premium, 2)}
	}
	return &peersSummary{Board: p.Board, Date: p.Date, PE: peer(p.PE), PB: peer(p.PB), PS: peer(p.PS)}
}
