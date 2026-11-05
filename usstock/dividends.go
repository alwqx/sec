package usstock

import (
	"context"
	"sort"
	"sync"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
)

// DividendYear 是一个财年的分红和回购。PerShare 是年报的每股宣告股息，Paid 是现金流量表里支付的普通股股息，
// Payout 是分红率（Paid ÷ 归母净利润 × 100，亏损时为空），Growth 是每股股息比上一财年的增长率（%）。
// 金额用公司的列报币种，没有申报的为空。
type DividendYear struct {
	FiscalYear int      `json:"fiscal_year"`
	End        string   `json:"end"`
	PerShare   *float64 `json:"per_share"`
	Growth     *float64 `json:"growth"`
	Paid       *float64 `json:"paid"`
	NetIncome  *float64 `json:"net_income"`
	Payout     *float64 `json:"payout"`
	Buyback    *float64 `json:"buyback"`
}

// Dividends 是一家美股公司按财年的分红表，Years 从新到旧。SEC XBRL 没有除权除息日、登记日和派息日。
// Splits 是从不复权日线识别出的拆股，每股股息已按它们统一到当前股本口径；SplitsUnchecked 表示日线没有读到，没有按拆股调整。
type Dividends struct {
	Ticker          string         `json:"ticker"`
	Name            string         `json:"name"`
	CIK             int            `json:"cik"`
	Currency        string         `json:"currency"`
	CurrencyName    string         `json:"currency_name"`
	Splits          []Split        `json:"splits"`
	SplitsUnchecked bool           `json:"splits_unchecked"`
	Years           []DividendYear `json:"years"`
}

// LoadDividends 并发读取 SEC XBRL 数据、新浪不复权日线和当前股数（识别拆股用），整理分红表。quote 是调用方已经读到的新浪行情
// （比如 sec info），为 nil 时自己读取；loadFacts 用来复用调用方缓存的 XBRL 数据（比如 Desktop），为 nil 时直接下载。
// 日线读取失败时不按拆股调整，SplitsUnchecked 为 true；当前股数读不到时只用申报的股数核对拆股。
func LoadDividends(ctx context.Context, client *edgar.Client, exCode string, quote *sina.USQuote, loadFacts func(context.Context, int) (*edgar.Facts, error)) (*Dividends, error) {
	company, err := client.Lookup(ctx, exCode)
	if err != nil {
		return nil, err
	}
	if loadFacts == nil {
		loadFacts = client.CompanyFacts
	}
	var (
		facts    *edgar.Facts
		factsErr error
		bars     []*sina.Bar
		barsErr  error
		current  float64
		wg       sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		facts, factsErr = loadFacts(ctx, company.CIK)
	}()
	go func() {
		defer wg.Done()
		bars, barsErr = sina.QueryUSDailyBars(ctx, exCode)
	}()
	go func() {
		defer wg.Done()
		q := quote
		if q == nil {
			q, _ = sina.QueryUSQuote(ctx, exCode)
		}
		if q != nil && q.Shares != nil {
			current = *q.Shares
		}
	}()
	wg.Wait()
	if factsErr != nil {
		return nil, factsErr
	}
	d := BuildDividends(company, facts, bars, current)
	d.SplitsUnchecked = barsErr != nil
	return d, nil
}

// BuildDividends 用年报的利润表（每股股息、归母净利润）和现金流量表（支付股息、回购）整理分红表，bars 是不复权日线，用来识别拆股。
// 只列有每股股息、支付股息或回购的财年；从不分红也不回购的公司 Years 为空。
func BuildDividends(company edgar.Company, facts *edgar.Facts, bars []*sina.Bar, current float64) *Dividends {
	var cover, weighted []SharePoint
	for _, p := range facts.SharesOutstanding() {
		cover = append(cover, SharePoint{Since: p.Date, Shares: p.Shares})
	}
	for _, p := range facts.WeightedDilutedShares() {
		weighted = append(weighted, SharePoint{Since: p.Filed, Shares: p.Shares})
	}
	d := &Dividends{Ticker: company.Ticker, Name: facts.Name, CIK: company.CIK, Currency: facts.Currency,
		CurrencyName: edgar.CurrencyName(facts.Currency), Splits: DetectSplits(bars, [][]SharePoint{cover, weighted}, current), Years: []DividendYear{}}
	// 年份取利润表和现金流量表报告期的并集：只有现金流量数据（分红、回购）的年份也列出。
	income, cash := map[string]edgar.Period{}, map[string]edgar.Period{}
	ends := []string{}
	for _, p := range facts.Build(edgar.Income, false, 0).Periods {
		income[p.End] = p
		ends = append(ends, p.End)
	}
	for _, p := range facts.Build(edgar.CashFlow, false, 0).Periods {
		cash[p.End] = p
		if _, ok := income[p.End]; !ok {
			ends = append(ends, p.End)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ends)))
	for _, end := range ends {
		p, c := income[end], cash[end]
		fiscalYear := p.FiscalYear
		if fiscalYear == 0 {
			fiscalYear = c.FiscalYear
		}
		year := DividendYear{FiscalYear: fiscalYear, End: end, PerShare: adjustForSplits(p.Values["dps"], p.Reported["dps"], d.Splits),
			Paid: c.Values["dividends"], NetIncome: p.Values["net_income"], Buyback: c.Values["buyback"]}
		if year.PerShare == nil && year.Paid == nil && year.Buyback == nil {
			continue
		}
		if year.Paid != nil && year.NetIncome != nil && *year.NetIncome > 0 {
			payout := *year.Paid / *year.NetIncome * 100
			year.Payout = &payout
		}
		d.Years = append(d.Years, year)
	}
	// 增长率和上一个财年比，相邻两年都有每股股息才计算；中间缺一年时不跨年比较。
	for i := 0; i+1 < len(d.Years); i++ {
		cur, prev := d.Years[i], d.Years[i+1]
		if cur.PerShare != nil && prev.PerShare != nil && *prev.PerShare > 0 && cur.FiscalYear == prev.FiscalYear+1 {
			growth := (*cur.PerShare / *prev.PerShare - 1) * 100
			d.Years[i].Growth = &growth
		}
	}
	return d
}

// adjustForSplits 把 reported 这天申报的每股数值按之后的拆股调整：拆股发生在报表发布之前时 SEC 要求追溯调整，
// 所以申报日早于拆股日的数值还是拆股前的口径，申报日在拆股日当天或之后的已经调整过。
func adjustForSplits(value *float64, reported string, splits []Split) *float64 {
	if value == nil || reported == "" {
		return value
	}
	adjusted := *value
	for _, s := range splits {
		if reported < s.Date {
			adjusted /= s.Factor
		}
	}
	return &adjusted
}
