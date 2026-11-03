package valuation

import (
	"context"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func paid(report, exDate string, cash, stock float64) *eastmoney.DividendPlan {
	return &eastmoney.DividendPlan{ReportDate: report, ExDate: exDate, Progress: "实施分配", CashPerTen: cash, StockPerTen: stock}
}

func priced(dates ...string) []*eastmoney.ValuationPoint {
	points := make([]*eastmoney.ValuationPoint, len(dates))
	for i, date := range dates {
		points[i] = &eastmoney.ValuationPoint{Date: date, Close: 10, MarketCap: 1e9, Shares: 1e8, PE: num(10), PB: num(1)}
	}
	return points
}

func TestFiscalPerShare(t *testing.T) {
	// 一年分两次：窗口内有三次除息，但只算最近完整财年。
	events := []dividendEvent{
		{"2024-12-31", "2025-07-14", 10, 0}, {"2025-06-30", "2025-12-15", 10, 0}, {"2025-12-31", "2026-05-13", 20, 0}, {"2026-06-30", "2026-10-21", 10, 0},
	}
	value, year := fiscalPerShare(events, "2026-11-02")
	require.Equal(t, "2025", year)
	require.InDelta(t, 3, value, 1e-9, "2026 中期分红不算，财年还没完整")
	value, year = fiscalPerShare(events, "2026-05-12")
	require.Equal(t, "2024", year, "2025 年度分红除息前，2024 是最近完整财年")
	require.InDelta(t, 1, value, 1e-9)

	// 中断超过 18 个月算停止分红。
	_, year = fiscalPerShare([]dividendEvent{{"2022-12-31", "2023-08-25", 10, 0}}, "2026-11-02")
	require.Empty(t, year)
	_, year = fiscalPerShare([]dividendEvent{{"2024-12-31", "2025-07-01", 10, 0}}, "2026-11-02")
	require.Equal(t, "2024", year, "除息日后移不算中断")
	_, year = fiscalPerShare(nil, "2026-11-02")
	require.Empty(t, year)
}

func TestFiscalPerShareWithoutAnnual(t *testing.T) {
	// 跳过年度红：不能用 4 年前的年度分红。
	events := []dividendEvent{{"2022-12-31", "2023-07-01", 10, 0}, {"2025-06-30", "2025-10-01", 5, 0}}
	value, year := fiscalPerShare(events, "2026-11-02")
	require.Equal(t, "2025", year)
	require.InDelta(t, 0.5, value, 1e-9)

	// 只分中期。
	value, year = fiscalPerShare([]dividendEvent{{"2025-06-30", "2025-10-01", 5, 0}}, "2026-05-02")
	require.Equal(t, "2025", year)
	require.InDelta(t, 0.5, value, 1e-9)

	// 只送转的年度分红不算现金分红。
	value, year = fiscalPerShare([]dividendEvent{{"2024-12-31", "2025-07-01", 10, 0}, {"2025-12-31", "2026-07-01", 0, 10}}, "2026-08-01")
	require.Equal(t, "2024", year)
	require.InDelta(t, 0.5, value, 1e-9, "去年的分红按今年的送转摊薄")
}

func TestYieldHistorySkipsDaysBeforeDividends(t *testing.T) {
	points := make([]*eastmoney.ValuationPoint, 0, 1200)
	start := time.Date(2022, 1, 3, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 1200; i++ {
		points = append(points, &eastmoney.ValuationPoint{Date: start.AddDate(0, 0, i).Format("2006-01-02"), Close: 10})
	}
	events := []dividendEvent{{"2023-12-31", "2024-01-10", 2, 0}}
	history := yieldHistory(points, events, 2)
	require.Equal(t, "2024-01-10", history.Since)
	require.Equal(t, 2.0, history.Min, "开始分红之前的 0 不算")
	require.Less(t, history.Samples, 1200)
	require.Equal(t, 100.0, *history.Percentile)
	require.Nil(t, yieldHistory(points, events, 0).Percentile, "当前没有分红时不算分位")
	require.Nil(t, yieldHistory(points, nil, 1))
}

func TestAdjustedForStockDividends(t *testing.T) {
	// 比亚迪 2025 年 10 送转 20 派 39.74，之前和当天的分红都摊薄，之后的不摊薄。
	events := []dividendEvent{{"2023-12-31", "2024-07-29", 30, 0}, {"2024-12-31", "2025-07-29", 39.74, 20}, {"2025-12-31", "2026-07-31", 3.58, 0}}
	require.InDelta(t, 1, adjusted(events, events[0], "2026-11-02"), 1e-9)
	require.InDelta(t, 3.974/3, adjusted(events, events[1], "2026-11-02"), 1e-9)
	require.InDelta(t, 0.358, adjusted(events, events[2], "2026-11-02"), 1e-9)
	require.InDelta(t, 3, adjusted(events, events[0], "2025-01-02"), 1e-9, "送转之前不摊薄")
}

func TestBuildDividendYears(t *testing.T) {
	plans := []*eastmoney.DividendPlan{
		paid("2025-12-31", "2026-07-10", 10, 0), paid("2025-06-30", "2026-01-16", 10, 0),
		paid("2024-06-30", "2025-01-10", 10, 0),
		{ReportDate: "2023-12-31", Progress: "董事会决议通过", CashPerTen: 50},
	}
	plans[0].EPS = num(5)
	plans[2].EPS = num(9)
	years := buildDividendYears(plans, "2026-11-02")
	require.Len(t, years, 2, "预案不算已分红")
	require.Equal(t, "2025", years[0].Year)
	require.InDelta(t, 2, years[0].PerShare, 1e-9, "中期和年度分红合计")
	require.InDelta(t, 40, *years[0].Payout, 1e-9)
	require.Nil(t, years[1].Payout, "中期报告的每股收益不能算全年分红率")
}

// 分红和每股收益都要用当时的口径：比亚迪 2024 年每股分红 3.974，当时的每股收益 13.84，分红率约 28.7%。
func TestPayoutUsesEPSOfThatTime(t *testing.T) {
	plan := paid("2024-12-31", "2025-07-29", 39.74, 20)
	plan.EPS = num(13.84)
	years := buildDividendYears([]*eastmoney.DividendPlan{plan}, "2026-11-02")
	require.InDelta(t, 28.71, *years[0].Payout, 0.01, "年度分红自身的送转不影响")
}

func TestBuildDividendYearsAsOfAndSplit(t *testing.T) {
	future := paid("2025-12-31", "2026-11-03", 10, 0)
	require.Empty(t, buildDividendYears([]*eastmoney.DividendPlan{future}, "2026-11-02"), "还没除息的不算已分红")

	// 中期 10 转 10，之后的年度分红和每股收益是新股本的口径，不能相加后计算分红率。
	interim, final := paid("2025-06-30", "2025-10-01", 10, 10), paid("2025-12-31", "2026-07-01", 5, 0)
	final.EPS = num(1)
	years := buildDividendYears([]*eastmoney.DividendPlan{final, interim}, "2026-11-02")
	require.InDelta(t, 1.5, years[0].PerShare, 1e-9)
	require.Nil(t, years[0].Payout)
}

func TestConsecutiveYears(t *testing.T) {
	years := []DividendYear{{Year: "2025"}, {Year: "2024"}, {Year: "2022"}}
	require.Equal(t, 2, consecutiveYears(years), "中断后不再累计")
	require.Equal(t, 0, consecutiveYears(nil))
}

func TestStoppedDividendsHaveNoStreak(t *testing.T) {
	r := Compute(priced("2020-01-02", "2025-12-01"), nil, DefaultDCFParams())
	applyDividends(r, priced("2020-01-02", "2025-12-01"), []*eastmoney.DividendPlan{paid("2023-12-31", "2024-03-01", 10, 0), paid("2022-12-31", "2023-03-01", 10, 0)})
	require.Equal(t, 0.0, *r.Dividends.Yield)
	require.Zero(t, r.Dividends.ConsecutiveYears, "股息率是 0 时不能显示连续分红")
	require.Len(t, r.Dividends.Years, 2)
}

func TestPlanCurrent(t *testing.T) {
	plan := &eastmoney.DividendPlan{ReportDate: "2024-12-31"}
	require.True(t, plan.Current("2026-01-05"), "跨年时还在进行的预案要保留")
	require.False(t, plan.Current("2027-01-05"))
}

func TestApplyDividends(t *testing.T) {
	r := Compute(priced("2020-01-02", "2026-11-02"), nil, DefaultDCFParams())
	plans := []*eastmoney.DividendPlan{
		paid("2025-12-31", "2026-07-10", 10, 0),
		{ReportDate: "2026-06-30", Progress: "董事会决议通过", Profile: "10派5元", CashPerTen: 5},
		{ReportDate: "2019-12-31", Progress: "股东大会决议通过", Profile: "早已放弃", CashPerTen: 9},
		{ReportDate: "2026-06-30", Progress: "预披露", Profile: "比例 35%"},
	}
	applyDividends(r, priced("2020-01-02", "2026-11-02"), plans)
	d := r.Dividends
	require.InDelta(t, 1, d.PerShare, 1e-9)
	require.Equal(t, "2025", d.FiscalYear)
	require.InDelta(t, 10, *d.Yield, 1e-9)
	require.InDelta(t, 2, *d.Average5Y, 1e-9, "5 个财年里只有 1 个有分红")
	require.Len(t, d.Pending, 1, "只列最近的、有金额的预案")
	require.Equal(t, "10派5元", d.Pending[0].Plan)
	require.True(t, d.History.Short, "样本不足不算分位")
	require.Nil(t, d.History.Percentile)
}

func TestApplyDividendsWithoutCash(t *testing.T) {
	r := Compute(flat(300, 10, 1, 2), nil, DefaultDCFParams())
	before := len(r.Signals)
	points := flat(300, 10, 1, 2)
	applyDividends(r, points, []*eastmoney.DividendPlan{{ReportDate: "2025-12-31", Progress: "董事会决议通过", Profile: "不分配不转增"}})
	require.Equal(t, 0.0, *r.Dividends.Yield, "有记录但没有现金分红是真实的 0")
	require.Nil(t, r.Dividends.History)
	require.Equal(t, []DividendYear{}, r.Dividends.Years)
	require.Equal(t, []PendingDividend{}, r.Dividends.Pending)
	require.Len(t, r.Signals, before)
}

func TestApplyDividendsWithoutRecords(t *testing.T) {
	r := Compute(flat(300, 10, 1, 2), nil, DefaultDCFParams())
	applyDividends(r, flat(300, 10, 1, 2), nil)
	require.Nil(t, r.Dividends, "无法区分没有分红和上游缺数据，不能当成 0")
	require.Contains(t, r.DividendsNote, "没有分红记录")
}

func TestAverageNeedsFiveYears(t *testing.T) {
	r := Compute(priced("2024-01-02", "2026-11-02"), nil, DefaultDCFParams())
	applyDividends(r, priced("2024-01-02", "2026-11-02"), []*eastmoney.DividendPlan{paid("2025-12-31", "2026-07-10", 10, 0)})
	require.Nil(t, r.Dividends.Average5Y, "上市不足 5 年时不计算")
}

func TestDividendSignalDirection(t *testing.T) {
	history := func(p float64) *History { return &History{Since: "2018-01-02", Percentile: &p} }
	high, ok := dividendSignal(&Dividends{Yield: num(5), History: history(90)})
	require.True(t, ok)
	require.Equal(t, "low", high.Level, "股息率处于高位说明估值偏低")
	low, _ := dividendSignal(&Dividends{Yield: num(1), History: history(10)})
	require.Equal(t, "high", low.Level)
	_, ok = dividendSignal(&Dividends{Yield: num(0), History: history(5)})
	require.False(t, ok, "近 12 个月没有分红时不计入")
	_, ok = dividendSignal(&Dividends{Yield: num(5), History: &History{Short: true}})
	require.False(t, ok)
}

func TestAnalyzeDividendFailureKeepsValuation(t *testing.T) {
	defer gock.Off()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_SHAREBONUS_DET").Reply(500)
	mockPeerUpstream()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("filter", "BOARD_CODE").Reply(200).BodyString(emptyReports)
	r, err := Analyze(context.Background(), "SH600036", DefaultDCFParams())
	require.NoError(t, err)
	require.Nil(t, r.Dividends)
	require.Contains(t, r.DividendsNote, "未计入股息率")
}

func TestAnalyzeReadsDividendPlans(t *testing.T) {
	defer gock.Off()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_SHAREBONUS_DET").Reply(200).
		BodyString(`{"success":true,"code":0,"result":{"data":[{"REPORT_DATE":"2025-12-31 00:00:00","EX_DIVIDEND_DATE":"2026-07-10 00:00:00","ASSIGN_PROGRESS":"实施分配","IMPL_PLAN_PROFILE":"10送8转12派39.74元","PRETAX_BONUS_RMB":39.74,"BONUS_IT_RATIO":20,"BASIC_EPS":13.84}]}}`)
	mockPeerUpstream()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("filter", "BOARD_CODE").Reply(200).BodyString(emptyReports)
	r, err := Analyze(context.Background(), "SH600036", DefaultDCFParams())
	require.NoError(t, err)
	require.InDelta(t, 3.974/3, r.Dividends.PerShare, 1e-9, "每股金额按当前股本摊薄")
	require.InDelta(t, 3.974/13.84*100, *r.Dividends.Years[0].Payout, 1e-9, "分红率用当时的口径")
}
