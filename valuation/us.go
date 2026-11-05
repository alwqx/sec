package valuation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
)

// ErrStaleShares 表示 SEC 没有最近的流通股数，无法计算市值。
var ErrStaleShares = fmt.Errorf("%w：SEC 没有最近 15 个月的流通股数（封面股数、按类别申报的封面股数和季度稀释加权股数都没有）", ErrNoData)

// usHistoryYears 是美股估值历史的年数，和 A 股的估值历史长度相近。
const usHistoryYears = 10

// AnalyzeUS 计算美股估值：价格来自新浪美股日线（不复权），利润、收入、净资产、股息和股本来自 SEC XBRL。
// 每天的市值 = 收盘价 × 当时的流通股数，估值倍数只用当天之前已经申报的数据，不用后来才公布的数值。
// loadFacts 用来复用调用方缓存的 XBRL 数据，为 nil 时直接下载。
func AnalyzeUS(ctx context.Context, client *edgar.Client, exCode string, params DCFParams, loadFacts func(context.Context, int) (*edgar.Facts, error)) (*Result, error) {
	company, err := client.Lookup(ctx, exCode)
	if errors.Is(err, edgar.ErrNotFound) {
		return nil, ErrNoData
	}
	if err != nil {
		return nil, err
	}
	if loadFacts == nil {
		loadFacts = client.CompanyFacts
	}
	var (
		facts             *edgar.Facts
		profile           *edgar.Profile
		quote             *sina.USQuote
		bars              []*sina.Bar
		factsErr, barsErr error
		wg                sync.WaitGroup
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		facts, factsErr = loadFacts(ctx, company.CIK)
	}()
	go func() {
		defer wg.Done()
		// 行业只用于显示和判断金融业，获取失败不影响估值。
		profile, _ = client.Profile(ctx, company.CIK)
	}()
	go func() {
		defer wg.Done()
		// 新浪的当前股数用来核对拆股和多类股票的折算，获取失败时不核对。
		quote, _ = sina.QueryUSQuote(ctx, exCode)
	}()
	go func() {
		defer wg.Done()
		bars, barsErr = sina.QueryUSDailyBars(ctx, exCode)
	}()
	wg.Wait()
	if factsErr != nil {
		return nil, fmt.Errorf("获取 SEC 财务数据失败: %w", factsErr)
	}
	if barsErr != nil {
		return nil, fmt.Errorf("获取美股日线失败: %w", barsErr)
	}
	industry, sic := "", ""
	if profile != nil {
		industry, sic = profile.Industry, profile.SIC
	}
	current := 0.0
	if quote != nil && quote.Shares != nil {
		current = *quote.Shares
	}
	code, now := strings.ToUpper(exCode), time.Now()
	// companyfacts 里最后一天没有可用的股数时（比如伯克希尔、Visa 只按类别申报），才逐份读取申报封面页：第一次较慢，之后读缓存。
	if len(bars) == 0 || hasRecentShares(facts, bars[len(bars)-1].Date) {
		return computeUS(code, industry, sic, facts, nil, bars, current, params, now)
	}
	since := now.AddDate(-usHistoryYears, -15, 0).Format(time.DateOnly)
	covers, coverErr := client.CoverShares(ctx, company.CIK, since)
	if coverErr != nil {
		// 两个错误都包装，调用方能用 errors.Is 识别股数过期、取消和超时。
		return nil, fmt.Errorf("%w（读取申报封面股数：%w）", ErrStaleShares, coverErr)
	}
	result, err := computeUS(code, industry, sic, facts, covers.Points, bars, current, params, now)
	if err == nil && covers.Failed > 0 {
		result.SharesNote += fmt.Sprintf("有 %d 份申报的封面没有读到（可能被 SEC 限流），对应时期的股数缺失，估值历史有缺口，稍后重试可以补齐。", covers.Failed)
	}
	return result, err
}

// hasRecentShares 判断 companyfacts 的封面股数或加权股数在 date 当天是否可用（不超过 15 个月），
// 和 computeUS 选股数的规则一致；不可用时才需要读取申报封面页。
func hasRecentShares(facts *edgar.Facts, date string) bool {
	return newUSShares(coverShares(facts.SharesOutstanding()), weightedShares(facts.WeightedDilutedShares())).at(date) != nil
}

// available 是一个在某天申报后才能看到的数值。
type available struct {
	filed, end string
	value      float64
}

// asOf 按申报日顺序扫描，返回在 date 当天能看到的、截止日最新的数值。entries 须按申报日升序，dates 须升序。
type asOf struct {
	entries []available
	next    int
	best    *available
}

func newAsOf(entries []available) *asOf {
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].filed < entries[j].filed })
	return &asOf{entries: entries}
}

func (a *asOf) at(date string) *available {
	for a.next < len(a.entries) && a.entries[a.next].filed <= date {
		entry := a.entries[a.next]
		// 后来的申报重述了较早的报告期时，不能把"最新"退回到较早的期间。
		if a.best == nil || entry.end >= a.best.end {
			a.best = &entry
		}
		a.next++
	}
	return a.best
}

// ttmEntries 把季度数整理成滚动四个季度的合计：每个季度和前三个季度连续（相隔约 3 个月）且都有数值时才有合计，
// 申报日取四个季度中最晚的。periods 须按截止日升序。
func ttmEntries(periods []edgar.Period, key string) []available {
	entries := []available{}
	for i := 3; i < len(periods); i++ {
		sum, filed, ok := 0.0, "", true
		for j := i - 3; j <= i; j++ {
			value := periods[j].Values[key]
			if value == nil || periods[j].Filed[key] == "" || (j > i-3 && !isQuarterGap(periods[j-1].End, periods[j].End)) {
				ok = false
				break
			}
			sum += *value
			filed = max(filed, periods[j].Filed[key])
		}
		if ok {
			entries = append(entries, available{filed, periods[i].End, sum})
		}
	}
	return entries
}

func isQuarterGap(a, b string) bool {
	x, err1 := time.Parse(time.DateOnly, a)
	y, err2 := time.Parse(time.DateOnly, b)
	d := y.Sub(x).Hours() / 24
	return err1 == nil && err2 == nil && d >= 80 && d <= 100
}

// pointEntries 取时点数（比如股东权益）。
func pointEntries(periods []edgar.Period, key string) []available {
	entries := []available{}
	for _, p := range periods {
		if value := p.Values[key]; value != nil && p.Filed[key] != "" {
			entries = append(entries, available{p.Filed[key], p.End, *value})
		}
	}
	return entries
}

func ascending(periods []edgar.Period) []edgar.Period {
	result := make([]edgar.Period, len(periods))
	for i, p := range periods {
		result[len(periods)-1-i] = p
	}
	return result
}

// shareSource 是股数的来源。
type shareSource int

const (
	fromCover    shareSource = iota // companyfacts 里的封面股数，多类股票直接相加
	fromClasses                     // 申报封面页按类别列出的股数，已折算成所查这类股票
	fromWeighted                    // 单季稀释加权股数
)

// sharePoint 是一次申报的股数。since 之后发生的拆股还没有反映在 shares 里：封面股数从统计日算起；
// 加权股数从申报日算起，因为申报前发生的拆股已经追溯调整。
type sharePoint struct {
	end, since, filed string
	shares            float64
	classes           int // 封面列出的普通股类别数，加权股数为 0
	source            shareSource
}

func coverShares(list []edgar.SharePoint) []sharePoint {
	points := make([]sharePoint, 0, len(list))
	for _, p := range list {
		points = append(points, sharePoint{p.Date, p.Date, p.Filed, p.Shares, p.Classes, fromCover})
	}
	return points
}

func weightedShares(list []edgar.SharePoint) []sharePoint {
	points := make([]sharePoint, 0, len(list))
	for _, p := range list {
		points = append(points, sharePoint{p.Date, p.Filed, p.Filed, p.Shares, 0, fromWeighted})
	}
	return points
}

// symbolKey 去掉交易代码里的分隔符，BRK.B、BRK-B、BRK/B 都是 BRKB。
func symbolKey(symbol string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, strings.ToUpper(symbol))
}

// classShares 把申报封面页按类别列出的股数折算成所查这类股票（code）的股数：
// 股数 = 这类股票的股数 + w × 其他类别股数之和。各类别之间的折算比例不在封面上（比如伯克希尔 1 股 A 折 1500 股 B，
// Visa 的 B、C 类按不同比例折成 A 类），w 用最近一次申报和新浪的当前股数 current（已按所查这类股票折算）解出，
// 历史上各类别的构成变化时会有误差。所查这类股票按封面上的交易代码识别，2019 年以前的封面没有交易代码，
// 按同一个类别成员对应。识别不出、没有当前股数或解出的 w 不是正数时返回 nil 和原因。
// 新浪的当前股数已经包含最近一次申报之后的拆股，解 w 之前先按 splits 还原到那次申报的统计日。
func classShares(list []edgar.ClassShares, code string, current float64, splits []split) ([]sharePoint, string) {
	key := symbolKey(code)
	member, found := "", false
	for _, p := range list {
		for _, c := range p.Classes {
			if c.Symbol != "" && symbolKey(c.Symbol) == key {
				member, found = c.Member, true
			}
		}
	}
	if !found {
		return nil, ""
	}
	type part struct{ own, others float64 }
	parts := make([]part, len(list))
	for i, p := range list {
		for _, c := range p.Classes {
			if c.Member == member {
				parts[i].own = c.Shares
			} else {
				parts[i].others += c.Shares
			}
		}
	}
	w := 0.0
	for i := len(list) - 1; i >= 0; i-- {
		if parts[i].own > 0 {
			if parts[i].others > 0 {
				if current <= 0 {
					return nil, "没有新浪的当前股本，无法把其他类别的股数折算成所查这类股票"
				}
				atCover := current
				for _, s := range splits {
					if s.date > list[i].Date {
						atCover /= s.factor
					}
				}
				if w = (atCover - parts[i].own) / parts[i].others; w <= 0 {
					return nil, fmt.Sprintf("新浪的当前股本（%.0f）不大于申报封面上这类股票的股数（%.0f），可能只统计了这一类，无法按类别折算", atCover, parts[i].own)
				}
			}
			break
		}
	}
	points := []sharePoint{}
	for i, p := range list {
		if parts[i].own <= 0 || p.Date == "" {
			continue
		}
		points = append(points, sharePoint{p.Date, p.Date, p.Filed, parts[i].own + w*parts[i].others, len(p.Classes), fromClasses})
	}
	return points, ""
}

// ownClassShares 返回申报封面页上所查这类股票自身的股数，不折算其他类别，只用来核对拆股：
// 折算比例要用拆股之后的当前股本解出，识别拆股时还不能用折算后的股数。
func ownClassShares(list []edgar.ClassShares, code string) []sharePoint {
	key, member := symbolKey(code), ""
	for _, p := range list {
		for _, c := range p.Classes {
			if c.Symbol != "" && symbolKey(c.Symbol) == key {
				member = c.Member
			}
		}
	}
	points := []sharePoint{}
	for _, p := range list {
		for _, c := range p.Classes {
			if member != "" && c.Member == member && c.Shares > 0 && p.Date != "" {
				points = append(points, sharePoint{p.Date, p.Date, p.Filed, c.Shares, len(p.Classes), fromClasses})
			}
		}
	}
	return points
}

// shareAsOf 和 asOf 一样按申报日扫描，返回 date 当天能看到的、统计日最新的股数。
type shareAsOf struct {
	points []sharePoint
	next   int
	best   *sharePoint
}

func newShareAsOf(points []sharePoint) *shareAsOf {
	sorted := append([]sharePoint(nil), points...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].filed < sorted[j].filed })
	return &shareAsOf{points: sorted}
}

func (a *shareAsOf) at(date string) *sharePoint {
	for a.next < len(a.points) && a.points[a.next].filed <= date {
		p := a.points[a.next]
		if a.best == nil || p.end >= a.best.end {
			a.best = &p
		}
		a.next++
	}
	return a.best
}

// usShares 选每天用的股数：优先用封面股数；封面股数超过 15 个月没有更新时（比如伯克希尔、谷歌、Meta
// 只按股票类别申报，companyfacts 里没有），依次改用申报封面页按类别列出的股数、最近的单季稀释加权股数；
// 都没有时返回 nil。
type usShares struct{ sources []*shareAsOf }

func newUSShares(sources ...[]sharePoint) *usShares {
	u := &usShares{}
	for _, points := range sources {
		u.sources = append(u.sources, newShareAsOf(points))
	}
	return u
}

func (u *usShares) at(date string) *sharePoint {
	stale := monthsBefore(date, 15)
	var found *sharePoint
	// 每个来源都要扫描到 date，后面的日期才能接着用。
	for _, source := range u.sources {
		if p := source.at(date); found == nil && p != nil && p.end >= stale {
			found = p
		}
	}
	return found
}

// adjusted 返回 date 当天按 p 之后的拆股调整后的股数。
func (p *sharePoint) adjusted(date string, splits []split) float64 {
	shares := p.shares
	for _, s := range splits {
		if s.date > p.since && s.date <= date {
			shares *= s.factor
		}
	}
	return shares
}

// split 是一次拆股或合股：date 当天起，股数乘以 factor。
type split struct {
	date   string
	factor float64
}

// splitFactors 是常见的拆股比例，合股用倒数。
var splitFactors = []float64{2, 3, 4, 5, 6, 7, 8, 10, 15, 20, 1.5}

// detectSplits 从不复权日线的跳变识别拆股：相邻两天收盘价之比接近常见拆股比例（误差 4% 以内）且变化超过 40%。
// 还要用前后的股数核对：同一来源（封面或加权股数）在拆股前最后一次和之后第一次的股数（之后还没有申报时用新浪的当前股数 current）
// 之比与拆股比例相差不超过 25% 才算拆股；没有可核对的股数时不算，避免把真的暴跌一半当成 1 拆 2。
func detectSplits(bars []*sina.Bar, sources [][]sharePoint, current float64) []split {
	splits := []split{}
	for i := 1; i < len(bars); i++ {
		if bars[i].Close <= 0 || bars[i-1].Close <= 0 {
			continue
		}
		ratio := bars[i-1].Close / bars[i].Close
		if ratio < 1.4 && ratio > 1/1.4 {
			continue
		}
		factor := 0.0
		for _, k := range splitFactors {
			if math.Abs(ratio/k-1) < 0.04 {
				factor = k
			} else if math.Abs(ratio*k-1) < 0.04 {
				factor = 1 / k
			}
		}
		if factor == 0 {
			continue
		}
		date := bars[i].Date
		for _, points := range sources {
			var before, after *sharePoint
			for j := range points {
				if points[j].since < date {
					if before == nil || points[j].since >= before.since {
						before = &points[j]
					}
				} else if after == nil || points[j].since < after.since {
					after = &points[j]
				}
			}
			later := current
			if after != nil {
				later = after.shares
			}
			if before != nil && later > 0 && math.Abs(later/before.shares/factor-1) <= 0.25 {
				splits = append(splits, split{date, factor})
				break
			}
		}
	}
	return splits
}

// computeUS 用 XBRL 数据和日线构造估值序列，交给 Compute 计算分位、PEG、格雷厄姆数、DCF 和规则评估。
// current 是新浪的当前股数，没有时为 0；covers 是申报封面页按类别列出的股数，没有读取时为 nil。
func computeUS(code, industry, sic string, facts *edgar.Facts, covers []edgar.ClassShares, bars []*sina.Bar, current float64, params DCFParams, now time.Time) (*Result, error) {
	if len(bars) == 0 {
		return nil, ErrNoData
	}
	income := ascending(facts.Build(edgar.Income, true, 0).Periods)
	balance := ascending(facts.Build(edgar.Balance, true, 0).Periods)
	profit := newAsOf(ttmEntries(income, "net_income"))
	revenue := newAsOf(ttmEntries(income, "revenue"))
	// 股息率和 A 股一样用最近一个完整财年：季度划分会让最近四个季度包含 5 次派息（比如可口可乐 53 周的财年）。
	annualCashPeriods := facts.Build(edgar.CashFlow, false, 0).Periods
	dividends := newAsOf(pointEntries(annualCashPeriods, "dividends"))
	// 最近申报的财年没有支付股息时（比如停止分红），股息率是 0，不能一直沿用更早财年的股息。
	fiscalYears := newAsOf(pointEntries(annualCashPeriods, "operating_cash_flow"))
	equity := newAsOf(pointEntries(balance, "equity"))
	cover, weighted := coverShares(facts.SharesOutstanding()), weightedShares(facts.WeightedDilutedShares())
	// 先用不需要折算的股数识别拆股，再用拆股前的当前股本解类别折算比例。
	splits := detectSplits(bars, [][]sharePoint{cover, weighted, ownClassShares(covers, code)}, current)
	classes, classNote := classShares(covers, code, current, splits)
	scale, scaleSource := 1.0, shareSource(-1)
	var sharesAt *usShares
	sharesNote := ""
	if len(classes) > 0 {
		// 能按类别折算时整段历史只用折算后的股数：其他来源是各类别直接相加或按 A 股折算的，口径不同，混用会让估值序列跳变；
		// 缺少封面股数的日子不计算。
		sharesAt = newUSShares(classes)
		sharesNote = "股数按申报封面上各类别的股数折算成所查这类股票，折算比例由新浪的当前股本推算，历史上各类别构成变化时有误差。"
	} else {
		// 多类普通股按不同比例折算时（比如伯克希尔 1 股 A 折 1500 股 B），封面股数直接相加没有意义，加权股数按 A 股折算；
		// 最后一天用的股数和新浪的当前股数（按所查这类股票折算）相差超过 25% 时，同一来源的股数都按新浪校准。
		// 只有一类普通股的封面股数不校准。
		if last := newUSShares(cover, weighted).at(bars[len(bars)-1].Date); last != nil && current > 0 &&
			!(last.source == fromCover && last.classes == 1) {
			if latest := last.adjusted(bars[len(bars)-1].Date, splits); math.Abs(latest/current-1) > 0.25 {
				scale, scaleSource = current/latest, last.source
			}
		}
		sharesAt = newUSShares(cover, weighted)
	}

	if strings.HasPrefix(sic, "6") && len(sic) == 4 && sic < "6800" {
		industry += "（金融业）"
	}
	since := now.AddDate(-usHistoryYears, 0, 0).Format(time.DateOnly)
	points := []*eastmoney.ValuationPoint{}
	yields := []float64{}
	yieldSince := ""
	var lastDividend *available
	for _, bar := range bars {
		date := bar.Date
		p, rv, dv, eq, sh := profit.at(date), revenue.at(date), dividends.at(date), equity.at(date), sharesAt.at(date)
		if fy := fiscalYears.at(date); fy != nil && dv != nil && fy.end > dv.end {
			dv = &available{fy.filed, fy.end, 0}
		}
		if date < since || sh == nil || bar.Close <= 0 {
			continue
		}
		// 申报之后发生的拆股，在下一次申报前按比例调整。
		shares := sh.adjusted(date, splits)
		if sh.source == scaleSource {
			shares *= scale
		}
		marketCap := bar.Close * shares
		point := &eastmoney.ValuationPoint{Date: date, Name: facts.Name, Board: industry, Close: bar.Close, MarketCap: marketCap, Shares: shares}
		if p != nil && p.value != 0 {
			point.PE = ptr(marketCap / p.value)
		}
		if eq != nil && eq.value != 0 {
			point.PB = ptr(marketCap / eq.value)
		}
		if rv != nil && rv.value > 0 {
			point.PS = ptr(marketCap / rv.value)
		}
		points = append(points, point)
		lastDividend = dv
		if dv != nil && dv.value > 0 {
			yields = append(yields, dv.value/marketCap*100)
			if yieldSince == "" {
				yieldSince = date
			}
		}
	}
	if len(points) == 0 || points[len(points)-1].Date != bars[len(bars)-1].Date {
		if classNote != "" {
			return nil, fmt.Errorf("%w；%s", ErrStaleShares, classNote)
		}
		return nil, ErrStaleShares
	}

	annualIncome := facts.Build(edgar.Income, false, 0).Periods
	annualBalance := map[string]edgar.Period{}
	for _, p := range facts.Build(edgar.Balance, false, 0).Periods {
		annualBalance[p.End] = p
	}
	annualCash := map[string]edgar.Period{}
	for _, p := range annualCashPeriods {
		annualCash[p.End] = p
	}
	reports := []*eastmoney.PerformanceReport{}
	years := []DividendYear{}
	for _, p := range annualIncome {
		report := &eastmoney.PerformanceReport{Date: fmt.Sprintf("%d%s", p.FiscalYear, p.End[4:]), Annual: true,
			EPS: p.Values["eps_diluted"], Revenue: p.Values["revenue"], Profit: p.Values["net_income"]}
		equity := annualBalance[p.End].Values["equity"]
		if equity != nil && *equity != 0 {
			if shares := p.Values["diluted_shares"]; shares != nil && *shares > 0 {
				report.BPS = ptr(*equity / *shares)
			}
			if profit := p.Values["net_income"]; profit != nil && *equity > 0 {
				report.ROE = ptr(*profit / *equity * 100)
			}
		}
		reports = append(reports, report)
		paid := annualCash[p.End].Values["dividends"]
		if paid == nil || *paid <= 0 {
			continue
		}
		year := DividendYear{Year: strconv.Itoa(p.FiscalYear), EPS: p.Values["eps_diluted"]}
		if dps := p.Values["dps"]; dps != nil && *dps > 0 {
			year.PerShare = *dps
		} else if shares := p.Values["diluted_shares"]; shares != nil && *shares > 0 {
			year.PerShare = *paid / *shares
		}
		if profit := p.Values["net_income"]; profit != nil && *profit > 0 {
			year.Payout = ptr(*paid / *profit * 100)
		}
		years = append(years, year)
	}

	result := Compute(points, reports, params)
	result.Code = code
	result.Currency = "USD"
	result.Disclaimer = "美股估值由 SEC XBRL 财务数据和新浪不复权日线计算：市值 = 收盘价 × 当时申报的流通股数（封面股数；只按类别申报时从申报封面页读取各类股数，按新浪的当前股数折算成所查这类股票；都没有时用季度稀释加权股数；拆股后按比例调整），PE、PS 用最近四个季度，PB 用最近一期股东权益，股息率用最近一个完整财年支付的现金股息，都只用当天之前已经申报的数值；报告期后来被重述时，历史估值也用重述后的数字。" + result.Disclaimer
	result.PeersNote = "美股暂无同行业对比，规则汇总未计入行业规则"
	result.SharesNote = sharesNote
	result.Dividends, result.DividendsNote = usDividends(years, lastDividend, result, yields, yieldSince, annualCash)
	// 股息率规则依赖分红数据，Compute 时还没有，补上后重新汇总。
	if result.Dividends != nil {
		if signal, ok := dividendSignal(result.Dividends); ok {
			result.Signals = append(result.Signals, signal)
			result.Assessment = assess(result.Signals)
		}
	}
	return result, nil
}

// usDividends 汇总美股分红：股息率 = 最近一个完整财年支付的现金股息 ÷ 市值，不受拆股影响；
// 每股股息取年报披露的每股宣告股息，没有时用支付总额除以稀释股数。
func usDividends(years []DividendYear, latest *available, r *Result, yields []float64, since string, annualCash map[string]edgar.Period) (*Dividends, string) {
	hasHistory := false
	for _, p := range annualCash {
		if p.Values["dividends"] != nil {
			hasHistory = true
		}
	}
	if !hasHistory && latest == nil {
		return nil, "SEC 数据中没有现金股息记录，可能不分红"
	}
	d := &Dividends{Years: years, Pending: []PendingDividend{}, Mismatches: []DividendMismatch{}}
	// 财年和连续分红从最近申报的财年算起：最近财年没有分红时连续分红为 0。
	if latest != nil {
		if p, ok := annualCash[latest.end]; ok {
			d.FiscalYear = strconv.Itoa(p.FiscalYear)
		}
	}
	if len(years) > 0 && years[0].Year == d.FiscalYear {
		d.ConsecutiveYears = consecutiveYears(years)
	}
	if r.MarketCap > 0 {
		paid := 0.0
		if latest != nil {
			paid = latest.value
		}
		d.Yield = ptr(paid / r.MarketCap * 100)
		if r.Shares > 0 {
			d.PerShare = paid / r.Shares
		}
		// 近 5 个完整财年平均支付的股息对应的股息率，没有分红的财年算 0。
		ends := make([]string, 0, len(annualCash))
		for end := range annualCash {
			ends = append(ends, end)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(ends)))
		if len(ends) >= 5 {
			total := 0.0
			for _, end := range ends[:5] {
				if value := annualCash[end].Values["dividends"]; value != nil {
					total += *value
				}
			}
			d.Average5Y = ptr(total / 5 / r.MarketCap * 100)
		}
	}
	if len(yields) > 0 {
		d.History = buildHistory(yields, since, d.Yield)
	}
	return d, ""
}
