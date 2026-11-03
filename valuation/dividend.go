package valuation

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
)

// 最近一次现金分红超过这么多个月时，认为公司已经停止分红，股息率为 0。
// 比一年略长，除息日逐年后移时不会误判。
const dividendStaleMonths = 18

// DividendYear 是一个财年的现金分红，按报告期归属。金额和每股收益都是当时的股本口径，没有按后来的送转调整。
type DividendYear struct {
	Year     string   `json:"year"`
	PerShare float64  `json:"per_share"` // 税前每股现金分红
	EPS      *float64 `json:"eps"`       // 年报基本每股收益
	Payout   *float64 `json:"payout"`    // 分红率，年报分红实施后才计算，单位 %
}

// PendingDividend 是还没有除息的分红，不计入股息率。
type PendingDividend struct {
	Report   string   `json:"report"` // 报告期 YYYY-MM-DD
	Progress string   `json:"progress"`
	Plan     string   `json:"plan"`
	PerShare *float64 `json:"per_share"`
	ExDate   string   `json:"ex_date"` // 没有确定时为空字符串
}

// Dividends 是分红和股息率。股息率是最近完整财年的税前每股现金分红除以收盘价，不含送转、配股和回购。分红记录为空时整体不可用。
type Dividends struct {
	Yield            *float64          `json:"yield"`             // 单位 %；已停止分红或有记录但没有现金分红时为 0，没有收盘价时为 nil
	PerShare         float64           `json:"per_share"`         // 最近完整财年的每股现金分红，按当前股本
	FiscalYear       string            `json:"fiscal_year"`       // 最近完整财年，股息率为 0 时为空字符串
	Average5Y        *float64          `json:"average_5y"`        // 近 5 个完整财年平均分红对应的股息率，单位 %；数据不足时为 nil
	ConsecutiveYears int               `json:"consecutive_years"` // 连续现金分红的财年数
	History          *History          `json:"history"`           // 有分红的交易日的股息率分布，没有时为 nil
	Years            []DividendYear    `json:"years"`             // 按财年降序
	Pending          []PendingDividend `json:"pending"`
}

// dividendEvent 是一次已确定除息日的分红。每 10 股的金额和送转比例都是当时的股本口径。
type dividendEvent struct {
	report string
	exDate string
	cash   float64
	ratio  float64
}

func dividendEvents(plans []*eastmoney.DividendPlan) []dividendEvent {
	events := []dividendEvent{}
	for _, plan := range plans {
		if plan.Implemented() && plan.ExDate != "" && len(plan.ReportDate) >= 10 {
			events = append(events, dividendEvent{plan.ReportDate, plan.ExDate, plan.CashPerTen, plan.StockPerTen})
		}
	}
	return events
}

// adjusted 返回一次分红的每股金额，按 asOf 当天的股本调整：除息日当天或之后发生送转的，按比例摊薄。
func adjusted(events []dividendEvent, event dividendEvent, asOf string) float64 {
	value := event.cash / 10
	for _, other := range events {
		if other.exDate >= event.exDate && other.exDate <= asOf && other.ratio > 0 {
			value /= 1 + other.ratio/10
		}
	}
	return value
}

// monthsBefore 返回若干个月前的日期；日期无效时返回空字符串。
func monthsBefore(date string, months int) string {
	value, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return value.AddDate(0, -months, 0).Format("2006-01-02")
}

// fiscalPerShare 返回截至 asOf 最近完整财年的每股现金分红和财年。同一财年的多次分红合计，
// 这样一年分几次都只算一个财年，不会像按除息日累计那样偶尔算出一年半或两年的分红。
// 年度分红除息后财年才算完整；最近有现金分红的财年比它晚超过一年时，说明公司不分年度红，用那个财年已分的现金。
func fiscalPerShare(events []dividendEvent, asOf string) (float64, string) {
	annual, latest, latestCash := "", "", ""
	for _, event := range events {
		if event.exDate > asOf || event.cash <= 0 {
			continue
		}
		year := event.report[:4]
		latest = max(latest, year)
		latestCash = max(latestCash, event.exDate)
		if event.report[5:] == "12-31" {
			annual = max(annual, year)
		}
	}
	if latest == "" || latestCash <= monthsBefore(asOf, dividendStaleMonths) {
		return 0, ""
	}
	year := latest
	if annual != "" && latest <= nextYear(annual) {
		year = annual
	}
	total := 0.0
	for _, event := range events {
		if event.report[:4] == year && event.exDate <= asOf && event.cash > 0 {
			total += adjusted(events, event, asOf)
		}
	}
	return total, year
}

// nextYear 返回下一年；无效时原样返回。
func nextYear(year string) string {
	value, err := strconv.Atoi(year)
	if err != nil {
		return year
	}
	return strconv.Itoa(value + 1)
}

// yieldHistory 只统计有分红的交易日，否则开始分红之前的 0 会把任何正的股息率都推到高分位。
func yieldHistory(points []*eastmoney.ValuationPoint, events []dividendEvent, current float64) *History {
	values := make([]float64, 0, len(points))
	since := ""
	for _, point := range points {
		if point.Close <= 0 {
			continue
		}
		perShare, _ := fiscalPerShare(events, point.Date)
		if perShare <= 0 {
			continue
		}
		if since == "" {
			since = point.Date
		}
		values = append(values, perShare/point.Close*100)
	}
	if len(values) == 0 {
		return nil
	}
	if current <= 0 {
		return buildHistory(values, since, nil)
	}
	return buildHistory(values, since, &current)
}

// buildDividendYears 按财年汇总截至 asOf 已除息的分红。每股收益取分红记录里的值：它是当时的口径，业绩报表的值可能已按后来的送转调整，和分红金额对不上。
// 第一次分红和年度分红之间发生送转时，各次金额的股本不同，不计算分红率。
func buildDividendYears(plans []*eastmoney.DividendPlan, asOf string) []DividendYear {
	cash := map[string]float64{}
	eps := map[string]*float64{}
	first := map[string]string{}
	annual := map[string]string{}
	events := dividendEvents(plans)
	for _, event := range events {
		if event.exDate > asOf || event.cash <= 0 {
			continue
		}
		year := event.report[:4]
		cash[year] += event.cash / 10
		if first[year] == "" || event.exDate < first[year] {
			first[year] = event.exDate
		}
		if event.report[5:] == "12-31" {
			annual[year] = event.exDate
		}
	}
	for _, plan := range plans {
		if plan.ExDate != "" && len(plan.ReportDate) >= 10 && plan.ExDate == annual[plan.ReportDate[:4]] && plan.ReportDate[5:] == "12-31" {
			eps[plan.ReportDate[:4]] = plan.EPS
		}
	}
	years := make([]DividendYear, 0, len(cash))
	for year, perShare := range cash {
		item := DividendYear{Year: year, PerShare: perShare, EPS: eps[year]}
		split := false
		for _, event := range events {
			split = split || event.ratio > 0 && event.exDate >= first[year] && event.exDate < annual[year]
		}
		if item.EPS != nil && *item.EPS > 0 && !split {
			item.Payout = ptr(perShare / *item.EPS * 100)
		}
		years = append(years, item)
	}
	sort.Slice(years, func(i, j int) bool { return years[i].Year > years[j].Year })
	return years
}

// consecutiveYears 从最近分红的财年往前数，遇到没有分红的财年就停止。
func consecutiveYears(years []DividendYear) int {
	if len(years) == 0 {
		return 0
	}
	latest, err := strconv.Atoi(years[0].Year)
	if err != nil {
		return 0
	}
	count := 0
	for _, item := range years {
		if item.Year != strconv.Itoa(latest-count) {
			break
		}
		count++
	}
	return count
}

// average5Y 返回最近完整财年往前 5 个财年的平均每股分红，没有分红的财年算 0。
func average5Y(events []dividendEvent, fiscalYear, asOf string) float64 {
	latest, err := strconv.Atoi(fiscalYear)
	if err != nil {
		return 0
	}
	total := 0.0
	for _, event := range events {
		year, err := strconv.Atoi(event.report[:4])
		if err == nil && year <= latest && year > latest-5 && event.exDate <= asOf && event.cash > 0 {
			total += adjusted(events, event, asOf)
		}
	}
	return total / 5
}

// applyDividends 写入分红数据，并把股息率的历史位置计入规则汇总。plans 是所有分红记录。
// 一条记录都没有时，无法区分公司没有分红和上游缺数据（上游不一定记录不分配的年度），不能当成 0。
func applyDividends(r *Result, points []*eastmoney.ValuationPoint, plans []*eastmoney.DividendPlan) {
	if len(plans) == 0 {
		r.DividendsNote = "没有分红记录，规则汇总未计入股息率"
		return
	}
	events := dividendEvents(plans)
	d := &Dividends{Years: buildDividendYears(plans, r.TradeDate), Pending: []PendingDividend{}}
	d.PerShare, d.FiscalYear = fiscalPerShare(events, r.TradeDate)
	for _, plan := range plans {
		if plan.Implemented() && plan.ExDate != "" && plan.ExDate <= r.TradeDate || plan.CashPerTen <= 0 || !plan.Current(r.TradeDate) {
			continue
		}
		d.Pending = append(d.Pending, PendingDividend{Report: plan.ReportDate, Progress: plan.Progress, Plan: plan.Profile, ExDate: plan.ExDate, PerShare: ptr(plan.CashPerTen / 10)})
	}
	// 已经停止分红时，即使以前连续分过也不算。
	if d.FiscalYear != "" {
		d.ConsecutiveYears = consecutiveYears(d.Years)
	}
	if r.Price > 0 {
		d.Yield = ptr(d.PerShare / r.Price * 100)
		// 估值数据不足 5 年时，平均值会把上市前的财年算成没有分红，不计算。
		if d.FiscalYear != "" && len(points) > 0 && points[0].Date <= monthsBefore(r.TradeDate, 60) {
			d.Average5Y = ptr(average5Y(events, d.FiscalYear, r.TradeDate) / r.Price * 100)
		}
		if hasCash(events) {
			d.History = yieldHistory(points, events, *d.Yield)
		}
	}
	r.Dividends = d
	if signal, ok := dividendSignal(d); ok {
		r.Signals = append(r.Signals, signal)
		r.Assessment = assess(r.Signals)
	}
}

func hasCash(events []dividendEvent) bool {
	for _, event := range events {
		if event.cash > 0 {
			return true
		}
	}
	return false
}

// dividendSignal 判断股息率的历史位置。股息率越高估值越低，所以方向和 PE 分位相反。没有分红时不计入。
func dividendSignal(d *Dividends) (Signal, bool) {
	if d.Yield == nil || *d.Yield <= 0 || d.History == nil || d.History.Percentile == nil {
		return Signal{}, false
	}
	value := *d.History.Percentile
	return Signal{"股息率", percentileLevel(100 - value), fmt.Sprintf("%.2f%%，处于 %s 以来 %.0f%% 分位（高于 70%% 偏低，低于 30%% 偏高）", *d.Yield, d.History.Since, value)}, true
}
