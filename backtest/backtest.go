// Package backtest 用日线和买卖信号模拟单只 A 股的交易：信号在收盘时产生，下一交易日开盘成交，
// 每次全仓买入或全部卖出。按 A 股规则处理 T+1、100 股一手、涨跌停开盘无法成交和交易成本。
package backtest

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/alwqx/sec/indicator"
	"github.com/alwqx/sec/provider/eastmoney"
)

// Bar 是一个交易日。价格应当是复权价，否则除权日会出现虚假的涨跌。
type Bar struct {
	Date      string // YYYY-MM-DD
	Open      float64
	Close     float64
	PrevClose float64 // 前一交易日收盘价，用来判断开盘是否涨停或跌停；0 表示不知道
	Volume    int64   // 0 表示停牌，当天不能成交
	Limit     float64 // 涨跌停幅度，比如主板 0.1，见 PriceLimit；0 表示不限制
}

// Config 是资金和交易成本。费率都是小数，比如 0.00025 表示万分之二点五。
type Config struct {
	Cash          float64 `json:"cash"`
	Commission    float64 `json:"commission"`     // 佣金费率，买卖都收
	MinCommission float64 `json:"min_commission"` // 每笔最低佣金
	StampTax      float64 `json:"stamp_tax"`      // 印花税，只在卖出时收
	Slippage      float64 `json:"slippage"`       // 滑点，买入价上浮、卖出价下浮的比例
}

// DefaultConfig 是常见的交易成本：佣金万分之二点五、最低 5 元，印花税千分之零点五，滑点千分之一。
func DefaultConfig() Config {
	return Config{Cash: 100000, Commission: 0.00025, MinCommission: 5, StampTax: 0.0005, Slippage: 0.001}
}

// Validate 检查参数范围。
func (c Config) Validate() error {
	switch {
	case c.Cash < 1000 || c.Cash > 1e10:
		return errors.New("初始资金须在 1000 元至 100 亿元之间")
	case c.Commission < 0 || c.Commission > 0.01:
		return errors.New("佣金费率须在 0 至 1% 之间")
	case c.MinCommission < 0 || c.MinCommission > 100:
		return errors.New("最低佣金须在 0 至 100 元之间")
	case c.StampTax < 0 || c.StampTax > 0.01:
		return errors.New("印花税率须在 0 至 1% 之间")
	case c.Slippage < 0 || c.Slippage > 0.05:
		return errors.New("滑点须在 0 至 5% 之间")
	}
	return nil
}

// lot 是 A 股一手的股数。
const lot = 100

// Trade 是一笔成交。Fee 包含佣金和印花税，Profit 只在卖出时有值：这一轮买卖扣除全部成本后的盈亏。
type Trade struct {
	SignalDate string   `json:"signal_date"`
	Date       string   `json:"date"`
	Side       string   `json:"side"` // buy 或 sell
	Price      float64  `json:"price"`
	Shares     int64    `json:"shares"`
	Amount     float64  `json:"amount"`
	Fee        float64  `json:"fee"`
	Reason     string   `json:"reason"`
	Profit     *float64 `json:"profit"`
}

// Event 是没有成交或推迟成交的信号，比如涨停开盘买不进、资金不足一手。
type Event struct {
	Date   string `json:"date"`
	Side   string `json:"side"`
	Reason string `json:"reason"`
}

// Point 是每个交易日收盘时的资产。
type Point struct {
	Date      string  `json:"date"`
	Equity    float64 `json:"equity"`
	Benchmark float64 `json:"benchmark"` // 第一天开盘买入并持有
	Holding   bool    `json:"holding"`
}

// Performance 是一条资产曲线的表现。年化收益在区间不足 30 天时为 nil，避免把几天的涨跌放大成年化。
type Performance struct {
	TotalReturn   float64  `json:"total_return"` // 单位 %
	Annualized    *float64 `json:"annualized"`   // 单位 %
	MaxDrawdown   float64  `json:"max_drawdown"` // 单位 %，为非负数
	DrawdownStart string   `json:"drawdown_start"`
	DrawdownEnd   string   `json:"drawdown_end"`
	FinalEquity   float64  `json:"final_equity"`
}

// Result 是一次回测的结果。
type Result struct {
	Config       Config      `json:"config"`
	Begin        string      `json:"begin"`
	End          string      `json:"end"`
	Strategy     Performance `json:"strategy"`
	Benchmark    Performance `json:"benchmark"`
	RoundTrips   int         `json:"round_trips"` // 完成的买卖轮数
	WinRate      *float64    `json:"win_rate"`    // 盈利轮数占比，单位 %；没有完成的轮次时为 nil
	TotalFees    float64     `json:"total_fees"`
	Exposure     float64     `json:"exposure"`      // 收盘时持仓的交易日占比，单位 %
	OpenPosition bool        `json:"open_position"` // 区间结束时仍持仓，按最后收盘价计算资产，没有扣卖出成本
	Trades       []Trade     `json:"trades"`
	Events       []Event     `json:"events"`
	Curve        []Point     `json:"curve"`
}

// ErrNoBars 表示区间内没有交易日。
var ErrNoBars = errors.New("区间内没有交易日")

func round(value float64) float64 { return math.Round(value*100) / 100 }

func fee(amount float64, cfg Config, sell bool) float64 {
	commission := math.Max(amount*cfg.Commission, cfg.MinCommission)
	if sell {
		commission += amount * cfg.StampTax
	}
	return round(commission)
}

// blocked 判断开盘能否成交：停牌不能成交，开盘涨停买不进，开盘跌停卖不出。
// 留出千分之二的余量，复权价和四舍五入会让涨停价有细微偏差。
func blocked(bar Bar, side string) string {
	if bar.Volume <= 0 || bar.Open <= 0 {
		return "停牌，顺延"
	}
	if bar.Limit <= 0 || bar.PrevClose <= 0 {
		return ""
	}
	change := bar.Open/bar.PrevClose - 1
	if side == indicator.Buy && change >= bar.Limit-0.002 {
		return "开盘涨停，买不进，顺延"
	}
	if side == indicator.Sell && change <= -bar.Limit+0.002 {
		return "开盘跌停，卖不出，顺延"
	}
	return ""
}

// buyShares 返回能买的整手股数，扣除佣金后不超过现金。
func buyShares(cash, price float64, cfg Config) int64 {
	shares := int64(cash/(price*(1+cfg.Commission))/lot) * lot
	for shares > 0 && float64(shares)*price+fee(float64(shares)*price, cfg, false) > cash {
		shares -= lot
	}
	return shares
}

type pending struct {
	side, date, reason string
}

// Run 按信号模拟交易。bars 是回测区间内的交易日，按日期升序；signals 的下标对应 bars。
// 信号在当天收盘产生、下一个能成交的交易日开盘成交；没有成交前出现反向信号时，原信号作废。
func Run(bars []Bar, signals []indicator.Signal, cfg Config) (*Result, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(bars) == 0 {
		return nil, ErrNoBars
	}
	// 前复权按分红减价，上市早、分红多的股票往前很多年会出现非正的价格，没法按它成交。
	for _, bar := range bars {
		if bar.Close <= 0 || (bar.Volume > 0 && bar.Open <= 0) {
			return nil, fmt.Errorf("%s 的复权价格不是正数，无法回测，请缩短区间或改用后复权", bar.Date)
		}
	}
	bySignal := map[int]indicator.Signal{}
	for _, signal := range signals {
		if signal.Index >= 0 && signal.Index < len(bars) {
			bySignal[signal.Index] = signal
		}
	}
	result := &Result{Config: cfg, Begin: bars[0].Date, End: bars[len(bars)-1].Date, Trades: []Trade{}, Events: []Event{}, Curve: make([]Point, 0, len(bars))}
	cash, shares, costBasis := cfg.Cash, int64(0), 0.0
	benchmarkCash, benchmarkShares, benchmarkBought := cfg.Cash, int64(0), false
	held, wins := 0, 0
	var order *pending
	for i, bar := range bars {
		// 基准：第一个能成交的交易日开盘买入并一直持有。
		if !benchmarkBought && blocked(bar, indicator.Buy) == "" {
			price := bar.Open * (1 + cfg.Slippage)
			if n := buyShares(benchmarkCash, price, cfg); n > 0 {
				amount := float64(n) * price
				benchmarkCash -= amount + fee(amount, cfg, false)
				benchmarkShares = n
			}
			benchmarkBought = true
		}
		if order != nil {
			if reason := blocked(bar, order.side); reason != "" {
				result.Events = append(result.Events, Event{bar.Date, order.side, reason})
			} else if order.side == indicator.Buy {
				price := bar.Open * (1 + cfg.Slippage)
				n := buyShares(cash, price, cfg)
				if n == 0 {
					result.Events = append(result.Events, Event{bar.Date, order.side, "资金不足一手，放弃"})
				} else {
					amount := float64(n) * price
					f := fee(amount, cfg, false)
					cash -= amount + f
					shares, costBasis = n, amount+f
					result.TotalFees += f
					result.Trades = append(result.Trades, Trade{order.date, bar.Date, order.side, round(price*1000) / 1000, n, round(amount), f, order.reason, nil})
				}
				order = nil
			} else {
				price := bar.Open * (1 - cfg.Slippage)
				amount := float64(shares) * price
				f := fee(amount, cfg, true)
				profit := round(amount - f - costBasis)
				cash += amount - f
				result.TotalFees += f
				result.RoundTrips++
				if profit > 0 {
					wins++
				}
				result.Trades = append(result.Trades, Trade{order.date, bar.Date, order.side, round(price*1000) / 1000, shares, round(amount), f, order.reason, &profit})
				shares, costBasis, order = 0, 0, nil
			}
		}
		if shares > 0 {
			held++
		}
		result.Curve = append(result.Curve, Point{bar.Date, round(cash + float64(shares)*bar.Close), round(benchmarkCash + float64(benchmarkShares)*bar.Close), shares > 0})
		signal, ok := bySignal[i]
		if !ok {
			continue
		}
		switch {
		case order != nil && order.side != signal.Type:
			result.Events = append(result.Events, Event{bar.Date, order.side, "出现反向信号，未成交的委托作废"})
			order = nil
		case order != nil:
			continue
		case signal.Type == indicator.Buy && shares == 0, signal.Type == indicator.Sell && shares > 0:
			if i == len(bars)-1 {
				result.Events = append(result.Events, Event{bar.Date, signal.Type, "区间最后一天的信号，没有下一个交易日成交"})
				continue
			}
			order = &pending{signal.Type, bar.Date, signal.Reason}
		}
	}
	if order != nil {
		result.Events = append(result.Events, Event{bars[len(bars)-1].Date, order.side, "到区间结束仍未成交"})
	}
	result.OpenPosition = shares > 0
	result.TotalFees = round(result.TotalFees)
	result.Exposure = float64(held) / float64(len(bars)) * 100
	if result.RoundTrips > 0 {
		rate := float64(wins) / float64(result.RoundTrips) * 100
		result.WinRate = &rate
	}
	result.Strategy = performance(result.Curve, cfg.Cash, func(p Point) float64 { return p.Equity })
	result.Benchmark = performance(result.Curve, cfg.Cash, func(p Point) float64 { return p.Benchmark })
	return result, nil
}

// performance 计算一条资产曲线的收益和最大回撤。
func performance(curve []Point, initial float64, value func(Point) float64) Performance {
	last := curve[len(curve)-1]
	p := Performance{FinalEquity: value(last), TotalReturn: (value(last)/initial - 1) * 100}
	first, err1 := time.Parse("2006-01-02", curve[0].Date)
	end, err2 := time.Parse("2006-01-02", last.Date)
	if days := end.Sub(first).Hours() / 24; err1 == nil && err2 == nil && days >= 30 && value(last) > 0 {
		annualized := (math.Pow(value(last)/initial, 365/days) - 1) * 100
		p.Annualized = &annualized
	}
	peak, peakDate := initial, curve[0].Date
	for _, point := range curve {
		v := value(point)
		if v > peak {
			peak, peakDate = v, point.Date
		}
		if drawdown := (1 - v/peak) * 100; drawdown > p.MaxDrawdown {
			p.MaxDrawdown, p.DrawdownStart, p.DrawdownEnd = drawdown, peakDate, point.Date
		}
	}
	return p
}

// PriceLimit 返回 A 股的涨跌停幅度：北交所 30%，科创板 20%，创业板 2020-08-24 起 20%，其余 10%。
// ST 股票的 5% 限制无法从代码判断，按普通股票处理。
func PriceLimit(exCode, date string) float64 {
	code := strings.ToUpper(exCode)
	switch {
	case strings.HasPrefix(code, "BJ"):
		return 0.3
	case strings.HasPrefix(code, "SH688") || strings.HasPrefix(code, "SH689"):
		return 0.2
	case (strings.HasPrefix(code, "SZ300") || strings.HasPrefix(code, "SZ301")) && date >= "2020-08-24":
		return 0.2
	}
	return 0.1
}

// Supported 判断能否回测：只支持 A 股个股。港股没有涨跌停、可以 T+0，每手股数也不同；
// 指数不能交易，基金没有印花税、涨跌停规则也不同。
func Supported(exCode string) error {
	code := strings.ToUpper(exCode)
	if len(code) == 8 {
		for _, prefix := range []string{"SH60", "SH688", "SH689", "SZ00", "SZ30", "BJ4", "BJ8", "BJ92"} {
			if strings.HasPrefix(code, prefix) {
				return nil
			}
		}
	}
	return fmt.Errorf("回测只支持 A 股个股，%s 不支持（指数、基金、港股等不能回测）", exCode)
}

// RunQuotes 用 quotes[first:] 回测，quotes[first] 之前的行情只用来取前收盘价。signals 的下标对应 quotes，
// 区间之前的信号忽略。sec strategy 和 Desktop 技术分析共用。
func RunQuotes(exCode string, quotes []*eastmoney.Quote, first int, signals []indicator.Signal, cfg Config) (*Result, error) {
	if err := Supported(exCode); err != nil {
		return nil, err
	}
	bars := make([]Bar, 0, len(quotes)-first)
	for i := first; i < len(quotes); i++ {
		date := quotes[i].Date.Format("2006-01-02")
		bar := Bar{Date: date, Open: quotes[i].Open, Close: quotes[i].Close, Volume: quotes[i].Volume, Limit: PriceLimit(exCode, date)}
		if i > 0 {
			bar.PrevClose = quotes[i-1].Close
		}
		bars = append(bars, bar)
	}
	items := []indicator.Signal{}
	for _, signal := range signals {
		if signal.Index >= first && signal.Index < len(quotes) {
			items = append(items, indicator.Signal{Index: signal.Index - first, Type: signal.Type, Reason: signal.Reason})
		}
	}
	return Run(bars, items, cfg)
}
