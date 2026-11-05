package valuation

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
)

type testFact struct {
	Start string  `json:"start,omitempty"`
	End   string  `json:"end"`
	Val   float64 `json:"val"`
	FP    string  `json:"fp"`
	Form  string  `json:"form"`
	Filed string  `json:"filed"`
	Accn  string  `json:"accn"`
}

// usFacts 构造一家财年截至 12 月底的公司：2024、2025 两个财年，每季度净利润 10、收入 100，季度结束 30 天后申报；
// 每季度末股东权益 1000；2024、2025 财年支付股息 20、24；封面股数 100，2026 年 6 月 1 日 1 拆 4 后的申报是 400。
func usFacts(t *testing.T) *edgar.Facts {
	t.Helper()
	return usFactsWith(t, nil)
}

// usFactsWith 在 usFacts 的基础上加入 gaap 里的科目（名称含 Shares 的单位是股）和更多封面股数；
// moreShares 只有一个统计日为空的元素时，表示封面上没有任何股数。
func usFactsWith(t *testing.T, gaap map[string][]testFact, moreShares ...testFact) *edgar.Facts {
	t.Helper()
	quarters := [][2]string{
		{"2024-01-01", "2024-03-31"}, {"2024-04-01", "2024-06-30"}, {"2024-07-01", "2024-09-30"}, {"2024-10-01", "2024-12-31"},
		{"2025-01-01", "2025-03-31"}, {"2025-04-01", "2025-06-30"}, {"2025-07-01", "2025-09-30"}, {"2025-10-01", "2025-12-31"},
		{"2026-01-01", "2026-03-31"}, {"2026-04-01", "2026-06-30"},
	}
	filed := func(end string) string {
		day, _ := time.Parse(time.DateOnly, end)
		return day.AddDate(0, 0, 30).Format(time.DateOnly)
	}
	income, revenue, equity := []testFact{}, []testFact{}, []testFact{}
	for _, q := range quarters {
		form, fp := "10-Q", "Q"
		if q[1][5:] == "12-31" {
			form, fp = "10-K", "FY"
		}
		income = append(income, testFact{q[0], q[1], 10, fp, form, filed(q[1]), "a"})
		revenue = append(revenue, testFact{q[0], q[1], 100, fp, form, filed(q[1]), "a"})
		equity = append(equity, testFact{"", q[1], 1000, fp, form, filed(q[1]), "a"})
	}
	income = append(income,
		testFact{"2024-01-01", "2024-12-31", 40, "FY", "10-K", "2025-01-30", "a"},
		testFact{"2025-01-01", "2025-12-31", 40, "FY", "10-K", "2026-01-30", "a"},
		// 2026-09-30 的申报重述了 2024 年第一季度，不能把"最新"的滚动利润退回到 2024 年。
		testFact{"2024-01-01", "2024-03-31", 12, "Q", "10-Q", "2026-09-30", "b"})
	dividends := []testFact{
		{"2024-01-01", "2024-12-31", 20, "FY", "10-K", "2025-01-30", "a"},
		{"2025-01-01", "2025-12-31", 24, "FY", "10-K", "2026-01-30", "a"},
	}
	shares := []testFact{
		{"", "2025-01-20", 100, "FY", "10-K", "2025-01-30", "a"},
		{"", "2026-04-20", 100, "Q", "10-Q", "2026-04-30", "c"},
		{"", "2026-07-20", 400, "Q", "10-Q", "2026-07-30", "d"},
	}
	shares = append(shares, moreShares...)
	if len(moreShares) == 1 && moreShares[0].End == "" {
		shares = nil
	}
	units := func(unit string, values []testFact) map[string]any {
		return map[string]any{"units": map[string][]testFact{unit: values}}
	}
	concepts := map[string]any{
		"NetIncomeLoss":       units("USD", income),
		"Revenues":            units("USD", revenue),
		"StockholdersEquity":  units("USD", equity),
		"PaymentsOfDividends": units("USD", dividends),
	}
	for name, values := range gaap {
		unit := "USD"
		if strings.Contains(name, "Shares") {
			unit = "shares"
		}
		concepts[name] = units(unit, values)
	}
	body, err := json.Marshal(map[string]any{"entityName": "Test Inc.", "facts": map[string]any{
		"us-gaap": concepts,
		"dei":     map[string]any{"EntityCommonStockSharesOutstanding": units("shares", shares)},
	}})
	require.NoError(t, err)
	facts, err := edgar.ParseFacts(1, body)
	require.NoError(t, err)
	return facts
}

// usBars 每个工作日一根，2026-06-01 之前收盘 40，之后 10（1 拆 4）。
func usBars(from, to string, splitDay string, before, after float64) []*sina.Bar {
	bars := []*sina.Bar{}
	day, _ := time.Parse(time.DateOnly, from)
	end, _ := time.Parse(time.DateOnly, to)
	for ; !day.After(end); day = day.AddDate(0, 0, 1) {
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			continue
		}
		date := day.Format(time.DateOnly)
		price := before
		if splitDay != "" && date >= splitDay {
			price = after
		}
		bars = append(bars, &sina.Bar{Date: date, Open: price, High: price, Low: price, Close: price})
	}
	return bars
}

func pointAt(t *testing.T, r *Result, date string) int {
	t.Helper()
	for i, d := range r.Dates {
		if d == date {
			return i
		}
	}
	t.Fatalf("没有 %s 的估值", date)
	return -1
}

func TestComputeUS(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2026-11-01")
	bars := usBars("2025-01-01", "2026-10-30", "2026-06-01", 40, 10)
	r, err := computeUS("$TEST", "Banks", "6021", usFacts(t), nil, bars, 0, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.Equal(t, "USD", r.Currency)
	require.Equal(t, "Banks（金融业）", r.Industry)
	require.NotEmpty(t, r.DCF.Warning, "金融业提示 DCF 参考价值低")
	require.Contains(t, r.PeersNote, "美股暂无同行业对比")

	// 2025-01-30 申报了 2024 年报；这之前股数还没有申报，没有估值。2024 年第一季度后来重述为 12，
	// 报告期按首次披露日期可用，数值用重述后的，滚动四个季度净利润是 42。
	require.Equal(t, "2025-01-30", r.Dates[0], "第一份封面股数在 2025-01-30 申报")
	i := pointAt(t, r, "2025-01-30")
	require.InDelta(t, 4000.0/42, *r.PE.Series[i], 1e-9)
	require.InDelta(t, 4000.0/1000, *r.PB.Series[i], 1e-9)
	require.InDelta(t, 4000.0/400, *r.PS.Series[i], 1e-9)

	// 拆股后、新股数申报前，股数按 4 倍调整，市值连续。
	i = pointAt(t, r, "2026-06-02")
	require.InDelta(t, 4000.0/40, *r.PE.Series[i], 1e-9)
	require.InDelta(t, 400.0, r.Shares, 1e-9, "最新股数是拆股后申报的 400")
	require.InDelta(t, 4000.0, r.MarketCap, 1e-9)

	// 重述较早的报告期不会把最新的滚动利润退回到 2024 年，仍是到 2026 年第二季度的四个季度。
	require.InDelta(t, 4000.0/40, *r.PE.Current, 1e-9)

	// 股息率用最近一个完整财年支付的股息：2025 财年 24 ÷ 市值 4000。
	require.NotNil(t, r.Dividends)
	require.InDelta(t, 24.0/4000*100, *r.Dividends.Yield, 1e-9)
	require.Equal(t, 2, r.Dividends.ConsecutiveYears)
	require.Equal(t, "2025", r.Dividends.FiscalYear)
	require.InDelta(t, 24.0/40*100, *r.Dividends.Years[0].Payout, 1e-9)

	require.Len(t, r.Annuals, 2)
	require.Equal(t, "2025", r.Annuals[0].Year)
}

// 收盘价真的跌一半、股数没变时，不能当成 1 拆 2。
func TestDetectSplitsChecksShares(t *testing.T) {
	bars := usBars("2026-05-25", "2026-06-05", "2026-06-01", 40, 20)
	shares := []edgar.SharePoint{{Date: "2026-04-20", Filed: "2026-04-30", Shares: 100}, {Date: "2026-07-20", Filed: "2026-07-30", Shares: 100}}
	require.Empty(t, detectSplits(bars, [][]sharePoint{coverShares(shares)}, 0))
	shares[1].Shares = 200
	require.Equal(t, []split{{"2026-06-01", 2}}, detectSplits(bars, [][]sharePoint{coverShares(shares)}, 0))
	// 合股：价格涨 10 倍，股数变成十分之一。
	shares[1].Shares = 10
	require.Equal(t, []split{{"2026-06-01", 0.1}}, detectSplits(usBars("2026-05-25", "2026-06-05", "2026-06-01", 4, 40), [][]sharePoint{coverShares(shares)}, 0))
}

func TestComputeUSNoData(t *testing.T) {
	_, err := computeUS("$TEST", "", "", usFacts(t), nil, nil, 0, DefaultDCFParams(), time.Now())
	require.ErrorIs(t, err, ErrNoData)
}

// 最近一次申报之后的跳变没有后续申报可核对，用新浪的当前股数核对；都没有时不算拆股。
func TestDetectSplitsAfterLatestFiling(t *testing.T) {
	bars := usBars("2026-05-25", "2026-06-05", "2026-06-01", 40, 20)
	shares := []edgar.SharePoint{{Date: "2026-04-20", Filed: "2026-04-30", Shares: 100}}
	require.Empty(t, detectSplits(bars, [][]sharePoint{coverShares(shares)}, 0), "无法核对时，可能是真的暴跌一半")
	require.Empty(t, detectSplits(bars, [][]sharePoint{coverShares(shares)}, 100), "当前股数没变")
	require.Equal(t, []split{{"2026-06-01", 2}}, detectSplits(bars, [][]sharePoint{coverShares(shares)}, 200))
}

// 停止分红后，最近财年没有股息记录，股息率为 0，连续分红为 0，不沿用更早财年的股息。
func TestComputeUSDividendStopped(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2027-03-01")
	// 2026 财年的年报（2027-01-30 申报）只有经营现金流，没有股息。
	facts := usFactsWith(t, map[string][]testFact{
		"NetCashProvidedByUsedInOperatingActivities": {
			{"2025-01-01", "2025-12-31", 50, "FY", "10-K", "2026-01-30", "a"},
			{"2026-01-01", "2026-12-31", 50, "FY", "10-K", "2027-01-30", "e"},
		},
	})
	r, err := computeUS("$TEST", "", "", facts, nil, usBars("2025-01-01", "2027-02-26", "", 40, 40), 0, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.NotNil(t, r.Dividends)
	require.Equal(t, 0.0, *r.Dividends.Yield)
	require.Equal(t, "2026", r.Dividends.FiscalYear)
	require.Equal(t, 0, r.Dividends.ConsecutiveYears)
	for _, s := range r.Signals {
		require.NotEqual(t, "股息率", s.Name)
	}
}

// 多类普通股（比如伯克希尔 A、B 股）封面股数相加和新浪的当前股数相差很大时，按新浪校准。
func TestComputeUSCalibratesShareClasses(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2026-11-01")
	facts := usFactsWith(t, nil, testFact{"", "2026-10-20", 1, "Q", "10-Q", "2026-10-25", "f"}, testFact{"", "2026-10-20", 99, "Q", "10-Q", "2026-10-25", "f"})
	bars := usBars("2026-10-01", "2026-10-30", "", 10, 10)
	r, err := computeUS("$TEST", "", "", facts, nil, bars, 1600, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.InDelta(t, 1600.0, r.Shares, 1e-9)
	// 只有一类股票时不校准。
	r, err = computeUS("$TEST", "", "", usFacts(t), nil, bars, 1600, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.InDelta(t, 400.0, r.Shares, 1e-9)
}

// 封面没有股数（比如只按股票类别申报）时用单季稀释加权股数；申报前发生的拆股已经调整，不能再乘一次。
func TestComputeUSWeightedShares(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2026-11-01")
	facts := usFactsWith(t, map[string][]testFact{
		"WeightedAverageNumberOfDilutedSharesOutstanding": {
			{"2025-10-01", "2025-12-31", 100, "FY", "10-K", "2026-01-30", "a"},
			{"2026-01-01", "2026-03-31", 100, "Q", "10-Q", "2026-04-30", "c"},
			// 2026-06-01 1 拆 4，第二季度的加权股数已经调整。
			{"2026-04-01", "2026-06-30", 400, "Q", "10-Q", "2026-07-30", "d"},
			// 年度数不是单季，不用。
			{"2025-01-01", "2025-12-31", 7, "FY", "10-K", "2026-01-30", "a"},
		},
	}, testFact{})
	r, err := computeUS("$TEST", "", "", facts, nil, usBars("2026-01-02", "2026-10-30", "2026-06-01", 40, 10), 0, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.Equal(t, "2026-01-30", r.Dates[0], "第一个加权股数在 2026-01-30 申报")
	i := pointAt(t, r, "2026-06-02")
	require.InDelta(t, 4000.0/40, *r.PE.Series[i], 1e-9, "拆股后、下一次申报前按 4 倍调整")
	require.InDelta(t, 400.0, r.Shares, 1e-9)
	require.InDelta(t, 4000.0, r.MarketCap, 1e-9)

	// 封面和加权股数都超过 15 个月没有更新时不估值。
	_, err = computeUS("$TEST", "", "", usFactsWith(t, nil, testFact{}), nil, usBars("2026-01-02", "2026-10-30", "", 40, 40), 0, DefaultDCFParams(), now)
	require.ErrorIs(t, err, ErrStaleShares)
}

// 按类别申报的封面股数折算成所查这类股票：伯克希尔 B 股 = B + w × A，w 用最近一次申报和新浪的当前股数解出。
func TestClassShares(t *testing.T) {
	a, b := "us-gaap_CommonClassAMember", "us-gaap_CommonClassBMember"
	covers := []edgar.ClassShares{
		// 2019 年以前的封面没有交易代码，按类别成员对应。
		{Date: "2018-07-26", Filed: "2018-08-06", Classes: []edgar.ShareClass{{Member: a, Shares: 800}, {Member: b, Shares: 1_300_000}}},
		{Date: "2026-07-29", Filed: "2026-08-10", Classes: []edgar.ShareClass{{Member: a, Symbol: "BRK.A", Shares: 500}, {Member: b, Symbol: "BRK.B", Shares: 1_400_000}}},
	}
	points, _ := classShares(covers, "$BRK.B", 1_400_000+500*1500, nil)
	require.Len(t, points, 2)
	require.InDelta(t, 1_300_000+800*1500.0, points[0].shares, 1e-6)
	require.Equal(t, sharePoint{"2026-07-29", "2026-07-29", "2026-08-10", 2_150_000, 2, fromClasses}, points[1])
	points, _ = classShares(covers, "BRK-A", 500+1_400_000/1500.0, nil)
	require.InDelta(t, 800+1_300_000/1500.0, points[0].shares, 1e-6)

	// 最近一次申报之后 B 股 1 拆 2：新浪的当前股本已经翻倍，先还原到申报日再解 w，w 仍是 1500。
	points, _ = classShares(covers, "$BRK.B", 2*(1_400_000+500*1500), []split{{"2026-09-01", 2}})
	require.Equal(t, 2_150_000.0, points[1].shares, "封面日的股数不含之后的拆股，拆股由 adjusted 按日期处理")

	points, note := classShares(covers, "$BRK.C", 1, nil)
	require.Nil(t, points, "封面上没有这个交易代码")
	require.Empty(t, note)
	points, note = classShares(covers, "$BRK.B", 0, nil)
	require.Nil(t, points)
	require.Contains(t, note, "没有新浪的当前股本")
	points, note = classShares(covers, "$BRK.B", 1_000_000, nil)
	require.Nil(t, points, "当前股数比这类股票还少")
	require.Contains(t, note, "不大于申报封面上这类股票的股数")
	// 只有一类股票时不需要当前股数。
	single := []edgar.ClassShares{{Date: "2026-07-29", Filed: "2026-08-10", Classes: []edgar.ShareClass{{Member: a, Symbol: "X", Shares: 100}}}}
	points, _ = classShares(single, "$X", 0, nil)
	require.Equal(t, 100.0, points[0].shares)
}

// 封面和加权股数都过期时，用按类别申报的封面股数，已经折算，不再按新浪校准。
func TestComputeUSClassShares(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2026-11-01")
	covers := []edgar.ClassShares{
		{Date: "2026-04-20", Filed: "2026-04-30", Classes: []edgar.ShareClass{{Member: "A", Symbol: "TEST.A", Shares: 1}, {Member: "B", Symbol: "TEST.B", Shares: 50}}},
		{Date: "2026-07-20", Filed: "2026-07-30", Classes: []edgar.ShareClass{{Member: "A", Symbol: "TEST.A", Shares: 1}, {Member: "B", Symbol: "TEST.B", Shares: 60}}},
	}
	facts := usFactsWith(t, nil, testFact{})
	bars := usBars("2026-05-01", "2026-10-30", "", 10, 10)
	_, err := computeUS("$TEST.B", "", "", facts, nil, bars, 100, DefaultDCFParams(), now)
	require.ErrorIs(t, err, ErrStaleShares)
	r, err := computeUS("$TEST.B", "", "", facts, covers, bars, 100, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.Equal(t, "2026-05-01", r.Dates[0])
	require.InDelta(t, 90.0*10/1000, *r.PB.Series[0], 1e-9, "1 股 A 折 40 股 B，市值 = 10 × (50 + 40 × 1)")
	require.Contains(t, r.SharesNote, "按申报封面上各类别的股数折算")

	// 新浪的当前股本不大于这类股票自身的股数时无法折算，错误里说明原因。
	_, err = computeUS("$TEST.B", "", "", facts, covers, bars, 50, DefaultDCFParams(), now)
	require.ErrorIs(t, err, ErrStaleShares)
	require.ErrorContains(t, err, "不大于申报封面上这类股票的股数")
	require.InDelta(t, 100.0, r.Shares, 1e-9)
}

// 能按类别折算时整段历史只用折算后的股数：封面股数之前的日子即使有加权股数（口径不同）也不用，直接跳过。
func TestComputeUSClassSharesOnly(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2026-11-01")
	facts := usFactsWith(t, map[string][]testFact{
		"WeightedAverageNumberOfDilutedSharesOutstanding": {
			{"2025-10-01", "2025-12-31", 3, "FY", "10-K", "2026-01-30", "a"},
		},
	}, testFact{})
	covers := []edgar.ClassShares{
		{Date: "2026-04-20", Filed: "2026-04-30", Classes: []edgar.ShareClass{{Member: "A", Symbol: "TEST.A", Shares: 1}, {Member: "B", Symbol: "TEST.B", Shares: 50}}},
	}
	r, err := computeUS("$TEST.B", "", "", facts, covers, usBars("2026-02-02", "2026-10-30", "", 10, 10), 90, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.Equal(t, "2026-04-30", r.Dates[0], "封面申报之前只有加权股数的日子不计算")
}

// companyfacts 的股数在最后一天可用时不读申报封面；过期（超过 15 个月）或没有时才读。
func TestHasRecentShares(t *testing.T) {
	facts := usFacts(t)
	require.True(t, hasRecentShares(facts, "2026-10-30"))
	require.False(t, hasRecentShares(facts, "2028-01-02"), "最近的封面股数是 2026-07-20，已超过 15 个月")
	require.False(t, hasRecentShares(usFactsWith(t, nil, testFact{}), "2026-10-30"), "没有股数")
}

// 以外币列报的公司估值仍只用美元数，结果里说明；美元列报时不说明。
func TestComputeUSCurrencyNote(t *testing.T) {
	now, _ := time.Parse(time.DateOnly, "2026-11-01")
	bars := usBars("2025-01-01", "2026-10-30", "2026-06-01", 40, 10)
	usd, err := computeUS("$TEST", "Banks", "6021", usFacts(t), nil, bars, 0, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.Empty(t, usd.CurrencyNote)
	cny, err := computeUS("$TEST", "Banks", "6021", usFacts(t).In("CNY"), nil, bars, 0, DefaultDCFParams(), now)
	require.NoError(t, err)
	require.Contains(t, cny.CurrencyNote, "以人民币列报")
	require.Equal(t, usd.PE, cny.PE, "估值用的仍是美元数")
}
