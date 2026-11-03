// Package valuation 提供 CLI 与 Desktop 共用的估值计算。
//
// 当前 PE、PB、PS 及其历史来自东方财富每日估值，年度数据来自业绩报表。每股收益、每股净资产、
// TTM 净利润和营收由估值倍数反推，与倍数同一口径。
package valuation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/alwqx/sec/provider/eastmoney"
)

const (
	dcfYears         = 5
	dcfCashRatio     = 0.7
	dcfDefaultGrowth = 5.0
	dcfMaxAutoGrowth = 15.0 // 自动取值的上下限
	growthYears      = 5
	minGrowthSpan    = 2
	// 少于约一年的样本不足以判断估值高低。
	minPercentileSamples = 250
)

// ErrNoData 表示没有这只证券的估值，通常是指数、基金或港美股。
var ErrNoData = errors.New("暂无估值数据，估值仅支持沪深北 A 股")

// DCFParams 是 DCF 参数，单位都是 %。Growth 为 nil 时自动取值，0 表示零增长。
type DCFParams struct {
	Growth         *float64 `json:"growth"`
	TerminalGrowth float64  `json:"terminal_growth"`
	WACC           float64  `json:"wacc"`
	MarginOfSafety float64  `json:"margin_of_safety"`
}

// DefaultDCFParams 返回默认 DCF 参数。
func DefaultDCFParams() DCFParams {
	return DCFParams{TerminalGrowth: 3, WACC: 8, MarginOfSafety: 20}
}

// Validate 校验参数。
func (p DCFParams) Validate() error {
	growth := 0.0
	if p.Growth != nil {
		growth = *p.Growth
	}
	for _, check := range []struct {
		name      string
		value     float64
		low, high float64
	}{{"增长率", growth, -50, 100}, {"永续增长率", p.TerminalGrowth, -5, 20}, {"折现率", p.WACC, 1, 50}, {"安全边际", p.MarginOfSafety, 0, 90}} {
		if math.IsNaN(check.value) || check.value < check.low || check.value > check.high {
			return fmt.Errorf("%s须在 %g%% 至 %g%% 之间", check.name, check.low, check.high)
		}
	}
	if p.WACC-p.TerminalGrowth < 1 {
		return errors.New("折现率须比永续增长率至少高 1 个百分点")
	}
	return nil
}

// History 是估值倍数的历史分布，只统计大于 0 的交易日。
type History struct {
	Since      string   `json:"since"`
	Samples    int      `json:"samples"`
	Min        float64  `json:"min"`
	Median     float64  `json:"median"`
	Max        float64  `json:"max"`
	Percentile *float64 `json:"percentile"` // 不高于当前值的交易日占比；当前值不可用或样本不足时为 nil
	Short      bool     `json:"short"`      // 样本不足，不计算分位和情景
}

// Scenario 是把估值倍数调整到历史水平后对应的股价。
type Scenario struct {
	Label    string  `json:"label"`
	Multiple float64 `json:"multiple"`
	Price    float64 `json:"price"`
	Change   float64 `json:"change"` // 相对当前价，单位 %
}

// Multiple 是一种估值倍数。Current 只在大于 0 时有值，Raw 保留上游原值。
type Multiple struct {
	Current   *float64   `json:"current"`
	Raw       *float64   `json:"raw"`
	History   *History   `json:"history"`
	Series    []*float64 `json:"series"` // 与 Result.Dates 对齐，小于等于 0 或缺失时为 null
	Scenarios []Scenario `json:"scenarios"`
}

// Annual 是一期年报，数值原样来自业绩报表。
type Annual struct {
	Year    string   `json:"year"`
	EPS     *float64 `json:"eps"`
	BPS     *float64 `json:"bps"`
	ROE     *float64 `json:"roe"`
	Revenue *float64 `json:"revenue"`
	Profit  *float64 `json:"profit"`
}

// Growth 是归母净利润的复合增长率。
type Growth struct {
	Rate     *float64 `json:"rate"`
	FromYear string   `json:"from_year"`
	ToYear   string   `json:"to_year"`
	Note     string   `json:"note"`
}

// Signal 是综合评估用到的一条规则结果。Level 取 low、neutral 或 high。
type Signal struct {
	Name  string `json:"name"`
	Level string `json:"level"`
	Rule  string `json:"rule"`
}

// DCF 是折现结果，Params.Growth 是实际使用的增长率。不适用时只有 Note 和 Params。
type DCF struct {
	Applicable   bool      `json:"applicable"`
	Note         string    `json:"note"`
	Warning      string    `json:"warning"`
	Params       DCFParams `json:"params"`
	GrowthSource string    `json:"growth_source"`
	// 以下数值在 DCF 不适用时为 nil；Upside 在没有收盘价时也为 nil。
	BaseCash      *float64 `json:"base_cash"`
	GrowthPV      *float64 `json:"growth_pv"`
	TerminalPV    *float64 `json:"terminal_pv"`
	TerminalShare *float64 `json:"terminal_share"` // 终值现值占比，单位 %
	Value         *float64 `json:"value"`
	PerShare      *float64 `json:"per_share"`
	FairPrice     *float64 `json:"fair_price"`
	Upside        *float64 `json:"upside"` // 单位 %
}

// Result 是一只证券的完整估值。无法计算的指标为 nil，不用 0 表示。
type Result struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Industry     string   `json:"industry"`
	TradeDate    string   `json:"trade_date"`
	Price        float64  `json:"price"`
	MarketCap    float64  `json:"market_cap"`
	Shares       float64  `json:"shares"`
	PE           Multiple `json:"pe"`
	PB           Multiple `json:"pb"`
	PS           Multiple `json:"ps"`
	Dates        []string `json:"dates"`
	EPS          *float64 `json:"eps"`
	BVPS         *float64 `json:"bvps"`
	ProfitTTM    *float64 `json:"profit_ttm"`
	RevenueTTM   *float64 `json:"revenue_ttm"`
	ROE          *float64 `json:"roe"`
	ROEYear      string   `json:"roe_year"`
	Growth       Growth   `json:"growth"`
	PEG          *float64 `json:"peg"`
	Graham       *float64 `json:"graham"`
	GrahamMargin *float64 `json:"graham_margin"` // 格雷厄姆数相对当前价，单位 %
	Annuals      []Annual `json:"annuals"`
	Peers        *Peers   `json:"peers"`      // 不可用时为 nil，原因见 PeersNote
	PeersNote    string   `json:"peers_note"` // 同行业数据不可用时说明原因
	DCF          DCF      `json:"dcf"`
	Signals      []Signal `json:"signals"`
	Assessment   string   `json:"assessment"`
	Disclaimer   string   `json:"disclaimer"`
}

// Analyze 查询数据并计算估值，params 应已通过 Validate。估值或业绩报表失败时返回错误，不输出部分结果；同行业数据失败时只写入 PeersNote。
func Analyze(ctx context.Context, exCode string, params DCFParams) (*Result, error) {
	var (
		points  []*eastmoney.ValuationPoint
		reports []*eastmoney.PerformanceReport
		err1    error
		err2    error
		wg      sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		points, err1 = eastmoney.GetValuationHistory(ctx, exCode)
	}()
	go func() {
		defer wg.Done()
		reports, err2 = eastmoney.GetPerformanceReports(ctx, exCode)
	}()
	wg.Wait()
	if errors.Is(err1, eastmoney.ErrUnsupportedCode) {
		return nil, ErrNoData
	}
	if err1 != nil {
		return nil, fmt.Errorf("获取估值数据失败: %w", err1)
	}
	if err2 != nil {
		return nil, fmt.Errorf("获取业绩报表失败: %w", err2)
	}
	if len(points) == 0 {
		return nil, ErrNoData
	}
	result := Compute(points, reports, params)
	result.Code = strings.ToUpper(exCode)
	// 同行业对比是附加信息，失败时不影响其他结果，但要说明规则汇总少了行业规则。
	var rows []*eastmoney.PeerValuation
	if latest := points[len(points)-1]; latest.BoardCode != "" {
		var err error
		rows, err = eastmoney.GetBoardValuations(ctx, latest.BoardCode, latest.Date)
		if err != nil {
			slog.WarnContext(ctx, "获取同行业估值失败", "code", exCode, "error", err)
			result.PeersNote = "同行业数据获取失败，规则汇总未计入行业规则"
			return result, nil
		}
	}
	applyPeers(result, rows)
	return result, nil
}

// Compute 根据按日期升序的估值和按报告期降序的业绩报表计算估值。points 不能为空。
func Compute(points []*eastmoney.ValuationPoint, reports []*eastmoney.PerformanceReport, params DCFParams) *Result {
	latest := points[len(points)-1]
	r := &Result{Name: latest.Name, Industry: latest.Board, TradeDate: latest.Date, Price: latest.Close, MarketCap: latest.MarketCap, Shares: latest.Shares,
		Annuals: []Annual{}, Disclaimer: "估值结果由规则和假设机械计算，不构成投资建议。"}
	r.Dates = make([]string, len(points))
	for i, point := range points {
		r.Dates[i] = point.Date
	}
	r.PE = buildMultiple(points, r.Price, func(p *eastmoney.ValuationPoint) *float64 { return p.PE })
	r.PB = buildMultiple(points, r.Price, func(p *eastmoney.ValuationPoint) *float64 { return p.PB })
	r.PS = buildMultiple(points, r.Price, func(p *eastmoney.ValuationPoint) *float64 { return p.PS })

	// 亏损时 PE 为负，反推出的每股收益和净利润也为负。
	if raw := r.PE.Raw; raw != nil && *raw != 0 {
		r.EPS = ptr(r.Price / *raw)
		if r.MarketCap > 0 {
			r.ProfitTTM = ptr(r.MarketCap / *raw)
		}
	}
	if raw := r.PB.Raw; raw != nil && *raw != 0 {
		r.BVPS = ptr(r.Price / *raw)
	}
	if current := r.PS.Current; current != nil && r.MarketCap > 0 {
		r.RevenueTTM = ptr(r.MarketCap / *current)
	}

	for _, report := range reports {
		if report.IsAnnual() {
			r.Annuals = append(r.Annuals, Annual{report.Date[:4], report.EPS, report.BPS, report.ROE, report.Revenue, report.Profit})
		}
	}
	if len(r.Annuals) > 0 {
		r.ROE, r.ROEYear = r.Annuals[0].ROE, r.Annuals[0].Year
	}
	r.Growth = computeGrowth(r.Annuals)
	if r.PE.Current != nil && r.Growth.Rate != nil && *r.Growth.Rate > 0 {
		r.PEG = ptr(*r.PE.Current / *r.Growth.Rate)
	}
	if r.EPS != nil && r.BVPS != nil && *r.EPS > 0 && *r.BVPS > 0 {
		r.Graham = ptr(math.Sqrt(22.5 * *r.EPS * *r.BVPS))
		if r.Price > 0 {
			r.GrahamMargin = ptr((*r.Graham/r.Price - 1) * 100)
		}
	}
	r.DCF = computeDCF(r, params)
	r.Signals = computeSignals(r)
	r.Assessment = assess(r.Signals)
	return r
}

func ptr(value float64) *float64 { return &value }

func buildMultiple(points []*eastmoney.ValuationPoint, price float64, field func(*eastmoney.ValuationPoint) *float64) Multiple {
	m := Multiple{Series: make([]*float64, len(points)), Scenarios: []Scenario{}}
	valid := []float64{}
	since := ""
	for i, point := range points {
		if value := field(point); value != nil && *value > 0 {
			m.Series[i] = value
			valid = append(valid, *value)
			if since == "" {
				since = point.Date
			}
		}
	}
	m.Raw = field(points[len(points)-1])
	if m.Raw != nil && *m.Raw > 0 {
		m.Current = m.Raw
	}
	if len(valid) == 0 {
		return m
	}
	sort.Float64s(valid)
	n := len(valid)
	median := medianSorted(valid)
	m.History = &History{Since: since, Samples: n, Min: valid[0], Median: median, Max: valid[n-1]}
	if n < minPercentileSamples {
		m.History.Short = true
		return m
	}
	if m.Current == nil {
		return m
	}
	notAbove := sort.Search(n, func(i int) bool { return valid[i] > *m.Current })
	m.History.Percentile = ptr(float64(notAbove) / float64(n) * 100)
	for _, item := range []struct {
		label string
		value float64
	}{{"历史最低", valid[0]}, {"历史中位", median}, {"历史最高", valid[n-1]}} {
		target := price * item.value / *m.Current
		m.Scenarios = append(m.Scenarios, Scenario{item.label, item.value, target, (item.value / *m.Current - 1) * 100})
	}
	return m
}

// computeGrowth 用最近年报和最多 5 年前的年报计算 CAGR。亏损年份不能开方，也不跳过，否则跨度和结果都不对。
func computeGrowth(annuals []Annual) Growth {
	if len(annuals) == 0 {
		return Growth{Note: "没有年报数据"}
	}
	latest := annuals[0]
	latestYear, err := strconv.Atoi(latest.Year)
	if err != nil {
		return Growth{Note: "年报日期无效"}
	}
	base, span := latest, 0
	for _, annual := range annuals[1:] {
		year, err := strconv.Atoi(annual.Year)
		if err != nil || latestYear-year > growthYears {
			break
		}
		base, span = annual, latestYear-year
	}
	growth := Growth{FromYear: base.Year, ToYear: latest.Year}
	if span < minGrowthSpan {
		growth.Note = fmt.Sprintf("年报少于 %d 年，无法计算增长率", minGrowthSpan+1)
		return growth
	}
	if latest.Profit == nil || base.Profit == nil {
		growth.Note = "起止年份的净利润缺失"
		return growth
	}
	if *latest.Profit <= 0 || *base.Profit <= 0 {
		growth.Note = "起止年份存在亏损，增长率没有意义"
		return growth
	}
	growth.Rate = ptr((math.Pow(*latest.Profit / *base.Profit, 1/float64(span)) - 1) * 100)
	return growth
}

// financialBoards 是净利润不能近似现金流的行业。
var financialBoards = []string{"银行", "保险", "证券", "多元金融"}

// computeDCF 是两阶段股权现金流折现：现金流近似为净利润的 70%，折现结果直接视为股权价值，不再扣净负债。
func computeDCF(r *Result, params DCFParams) DCF {
	dcf := DCF{Params: params}
	for _, board := range financialBoards {
		if strings.Contains(r.Industry, board) {
			dcf.Warning = "金融行业的利润不能近似为可分配现金流，DCF 结果参考价值很低。"
		}
	}
	if r.ProfitTTM == nil || *r.ProfitTTM <= 0 {
		dcf.Note = "最近四个季度亏损或利润不可用，DCF 不适用"
		return dcf
	}
	if r.Shares <= 0 {
		dcf.Note = "总股本不可用，无法计算每股价值"
		return dcf
	}
	growth := dcfDefaultGrowth
	if dcf.Params.Growth != nil {
		growth = *dcf.Params.Growth
		dcf.GrowthSource = "手动输入"
	} else if rate := r.Growth.Rate; rate != nil {
		growth = math.Max(-dcfMaxAutoGrowth, math.Min(*rate, dcfMaxAutoGrowth))
		dcf.GrowthSource = fmt.Sprintf("%s–%s 年净利润 CAGR %.1f%%", r.Growth.FromYear, r.Growth.ToYear, *rate)
		if math.Abs(*rate) > dcfMaxAutoGrowth {
			dcf.GrowthSource += fmt.Sprintf("，自动取值限制在 ±%g%%", dcfMaxAutoGrowth)
		}
	} else {
		dcf.GrowthSource = fmt.Sprintf("没有可用的历史增长率，使用默认 %g%%", dcfDefaultGrowth)
	}
	dcf.Params.Growth = ptr(growth)
	wacc := dcf.Params.WACC / 100
	cash := *r.ProfitTTM * dcfCashRatio
	dcf.BaseCash = ptr(cash)
	growthPV := 0.0
	for year := 1; year <= dcfYears; year++ {
		cash *= 1 + growth/100
		growthPV += cash / math.Pow(1+wacc, float64(year))
	}
	terminal := cash * (1 + dcf.Params.TerminalGrowth/100) / (wacc - dcf.Params.TerminalGrowth/100)
	terminalPV := terminal / math.Pow(1+wacc, dcfYears)
	value := growthPV + terminalPV
	perShare := value / r.Shares
	fairPrice := perShare * (1 - dcf.Params.MarginOfSafety/100)
	dcf.GrowthPV, dcf.TerminalPV, dcf.Value = ptr(growthPV), ptr(terminalPV), ptr(value)
	dcf.TerminalShare = ptr(terminalPV / value * 100)
	dcf.PerShare, dcf.FairPrice = ptr(perShare), ptr(fairPrice)
	dcf.Applicable = true
	if r.Price > 0 {
		dcf.Upside = ptr((fairPrice/r.Price - 1) * 100)
	}
	return dcf
}

// percentileBands 说明 percentileLevel 的区间。
const percentileBands = "低于 30% 偏低，高于 70% 偏高"

func percentileLevel(value float64) string {
	switch {
	case value < 30:
		return "low"
	case value > 70:
		return "high"
	}
	return "neutral"
}

// medianSorted 返回升序非空切片的中位数。
func medianSorted(values []float64) float64 {
	n := len(values)
	if n%2 == 0 {
		return (values[n/2-1] + values[n/2]) / 2
	}
	return values[n/2]
}

func percentileSignal(name string, m Multiple) (Signal, bool) {
	if m.History == nil || m.History.Percentile == nil {
		return Signal{}, false
	}
	value := *m.History.Percentile
	return Signal{name, percentileLevel(value), fmt.Sprintf("处于 %s 以来 %.0f%% 分位（%s）", m.History.Since, value, percentileBands)}, true
}

func computeSignals(r *Result) []Signal {
	signals := []Signal{}
	if signal, ok := percentileSignal("PE", r.PE); ok {
		signals = append(signals, signal)
	}
	if signal, ok := percentileSignal("PB", r.PB); ok {
		signals = append(signals, signal)
	}
	if r.PEG != nil {
		level := "neutral"
		if *r.PEG < 0.8 {
			level = "low"
		} else if *r.PEG > 2 {
			level = "high"
		}
		signals = append(signals, Signal{"PEG", level, fmt.Sprintf("%.2f（低于 0.8 偏低，高于 2 偏高）", *r.PEG)})
	}
	if r.Graham != nil {
		level := "neutral"
		if r.Price < *r.Graham {
			level = "low"
		}
		signals = append(signals, Signal{"格雷厄姆数", level, fmt.Sprintf("%.2f，当前价 %.2f（低于它偏低，否则不计入高估）", *r.Graham, r.Price)})
	}
	return signals
}

func assess(signals []Signal) string {
	if len(signals) < 2 {
		return "数据不足"
	}
	low, high := 0, 0
	for _, signal := range signals {
		switch signal.Level {
		case "low":
			low++
		case "high":
			high++
		}
	}
	switch {
	case low > high:
		return "偏低区间"
	case high > low:
		return "偏高区间"
	}
	return "合理区间"
}
