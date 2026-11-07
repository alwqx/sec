// Package history 提供 K 线行情，东方财富不可用时改用新浪备用数据。
package history

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
)

// FallbackNote 说明备用数据和东方财富的差异。
const FallbackNote = "东方财富行情暂不可用，已改用新浪备用数据：不复权，没有成交额和换手率，最多 1023 根 K 线；区间开头的指标可能缺少预热数据。"

// ErrRangeNotCovered 表示备用数据覆盖不了请求的区间，重试没有用，需要缩短区间。
var ErrRangeNotCovered = errors.New("备用数据覆盖不了请求的区间")

// ErrAdjustedUnavailable 表示复权行情没有备用来源。新浪日线不复权，不能当成复权数据返回。
var ErrAdjustedUnavailable = errors.New("复权行情没有备用数据源，可切换为不复权")

// Result 是一次查询的结果。Note 不为空时表示用了备用数据。
type Result struct {
	Quotes []*eastmoney.Quote
	Note   string
}

// Get 查询 K 线。东方财富失败时，沪深不复权行情改用新浪；备用数据覆盖不了请求区间时返回错误，不输出部分结果。
func Get(ctx context.Context, req *eastmoney.GetQuoteHistoryReq) (*Result, error) {
	return GetFrom(ctx, req, req.Begin)
}

// GetFrom 和 Get 相同，但 req.Begin 可以早于 required：两者之间是计算指标用的预热数据。
// 备用数据只需覆盖 required，预热数据尽力而为。
func GetFrom(ctx context.Context, req *eastmoney.GetQuoteHistoryReq, required string) (*Result, error) {
	quotes, err := getEastMoney(ctx, req)
	if err == nil {
		return &Result{Quotes: quotes}, nil
	}
	if ctx.Err() != nil {
		return nil, err
	}
	if isUS(req.MarketCode) {
		return usFallback(ctx, req, required, err)
	}
	symbol, ok := sinaSymbol(req)
	if !ok {
		return nil, err
	}
	if req.FQT != eastmoney.QuoteFQTDefault {
		return nil, fmt.Errorf("%w: %w", ErrAdjustedUnavailable, err)
	}
	slog.WarnContext(ctx, "东方财富行情失败，改用新浪备用数据", "code", req.Code, "error", err)
	bars, fallbackErr := sina.QueryBars(ctx, symbol, sinaScale(req.Freq), sina.MaxBars)
	if fallbackErr != nil {
		return nil, fmt.Errorf("东方财富: %w；新浪备用: %w", err, fallbackErr)
	}
	// 新浪也没有数据时，不能当成这段时间没有行情。
	if len(bars) == 0 {
		return nil, fmt.Errorf("东方财富: %w；新浪备用没有返回数据", err)
	}
	quotes, fallbackErr = toQuotes(bars, req, required, true)
	if fallbackErr != nil {
		return nil, fmt.Errorf("东方财富: %w；新浪备用: %w", err, fallbackErr)
	}
	return &Result{Quotes: quotes, Note: FallbackNote}, nil
}

// ErrUnsupportedMarket 表示东方财富 K 线不支持这只证券所在的市场，如北交所和场外基金。
var ErrUnsupportedMarket = errors.New("不支持的交易所")

// Market 返回搜索结果对应的东方财富市场代码，用作 GetQuoteHistoryReq.MarketCode。
// 新浪搜索把美股都标成纳斯达克，这里一律返回 105，纽交所的股票由 Get 换成 106、107 再查。
func Market(sec *sina.BasicSecurity) (int, error) {
	switch sec.ExChange {
	case "sh":
		return int(eastmoney.MarketTypeSse), nil
	case "sz":
		return int(eastmoney.MarketTypeSzSe), nil
	case types.ExChangeHKex:
		return int(eastmoney.MarketTypeHK), nil
	case types.ExChangeNasdaq, types.ExChangeNyse:
		return int(eastmoney.MarketTypeNasdaq), nil
	}
	return 0, fmt.Errorf("%w: %s", ErrUnsupportedMarket, sec.ExChange)
}

// usMarkets 是东方财富的美股市场代码。新浪搜索把美股都标成纳斯达克，纽交所的股票用 105 查不到，依次换成 106、107。
var usMarkets = []int{int(eastmoney.MarketTypeNasdaq), int(eastmoney.MarketTypeNyse), int(eastmoney.MarketTypeAmex)}

func isUS(market int) bool {
	for _, code := range usMarkets {
		if market == code {
			return true
		}
	}
	return false
}

// getEastMoney 查询东方财富。美股返回"没有数据"时换其他美股市场代码再查，查到后把正确的代码写回 req。
func getEastMoney(ctx context.Context, req *eastmoney.GetQuoteHistoryReq) ([]*eastmoney.Quote, error) {
	if isUS(req.MarketCode) {
		// 东方财富的类别股用下划线，比如 BRK.B 是 BRK_B；新浪日线仍用原代码，所以查完恢复。
		original := req.Code
		req.Code = strings.NewReplacer(".", "_", "-", "_").Replace(original)
		defer func() { req.Code = original }()
	}
	quotes, err := eastmoney.GetQuoteHistory(ctx, req)
	if !isUS(req.MarketCode) || !errors.Is(err, eastmoney.ErrNoData) {
		return quotes, err
	}
	original := req.MarketCode
	for _, market := range usMarkets {
		if market == original {
			continue
		}
		req.MarketCode = market
		if quotes, err = eastmoney.GetQuoteHistory(ctx, req); !errors.Is(err, eastmoney.ErrNoData) {
			return quotes, err
		}
	}
	req.MarketCode = original
	return nil, err
}

// USFallbackNote 说明美股备用数据和东方财富的差异。
const USFallbackNote = "东方财富行情暂不可用，已改用新浪美股日线：不复权（拆股当天价格会跳变），没有成交额、换手率，只有日线。"

// usFallback 东方财富失败时，美股不复权日线改用新浪。新浪美股只有日线，没有周线、月线和复权价格。
func usFallback(ctx context.Context, req *eastmoney.GetQuoteHistoryReq, required string, err error) (*Result, error) {
	if req.FQT != eastmoney.QuoteFQTDefault {
		return nil, fmt.Errorf("%w: %w", ErrAdjustedUnavailable, err)
	}
	// 没有指定周期时是日线，和东方财富一致。
	if req.Freq != "" && req.Freq != eastmoney.FreqDay {
		return nil, fmt.Errorf("东方财富: %w；新浪美股备用数据只有日线", err)
	}
	slog.WarnContext(ctx, "东方财富美股行情失败，改用新浪备用数据", "code", req.Code, "error", err)
	bars, fallbackErr := sina.QueryUSDailyBars(ctx, req.Code)
	if fallbackErr != nil {
		return nil, fmt.Errorf("东方财富: %w；新浪备用: %w", err, fallbackErr)
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("东方财富: %w；新浪备用没有返回数据", err)
	}
	quotes, fallbackErr := toQuotes(bars, req, required, false)
	if fallbackErr != nil {
		return nil, fmt.Errorf("东方财富: %w；新浪备用: %w", err, fallbackErr)
	}
	return &Result{Quotes: quotes, Note: USFallbackNote}, nil
}

func sinaScale(freq eastmoney.Freq) int {
	switch freq {
	case eastmoney.FreqWeek:
		return sina.ScaleWeek
	case eastmoney.FreqMonth:
		return sina.ScaleMonth
	}
	return sina.ScaleDay
}

func sinaSymbol(req *eastmoney.GetQuoteHistoryReq) (string, bool) {
	switch req.MarketCode {
	case int(eastmoney.MarketTypeSse):
		return "sh" + req.Code, true
	case int(eastmoney.MarketTypeSzSe):
		return "sz" + req.Code, true
	}
	return "", false
}

// compactDate 把 2026-01-02 转为 20260102，和请求里的日期比较。
func compactDate(date string) string {
	return strings.ReplaceAll(date, "-", "")
}

func round(value float64, digits int) float64 {
	scale := math.Pow10(digits)
	return math.Round(value*scale) / scale
}

// toQuotes 把新浪 K 线转成东方财富的格式。capped 表示接口最多返回 sina.MaxBars 条（沪深 K 线）：
// 取满上限时更早的数据被截断了，覆盖不了请求区间就报错。新浪美股日线返回上市以来的全部数据，不用判断。
func toQuotes(bars []*sina.Bar, req *eastmoney.GetQuoteHistoryReq, required string, capped bool) ([]*eastmoney.Quote, error) {
	if capped && len(bars) >= sina.MaxBars && required != "" && compactDate(bars[0].Date) > compactDate(required) {
		return nil, fmt.Errorf("%w: 只有 %s 之后的数据，请求从 %s 开始", ErrRangeNotCovered, bars[0].Date, required)
	}
	quotes := make([]*eastmoney.Quote, 0, len(bars))
	for i, bar := range bars {
		date := compactDate(bar.Date)
		if (req.Begin != "" && date < compactDate(req.Begin)) || (req.End != "" && date > compactDate(req.End)) {
			continue
		}
		parsed, err := time.Parse("2006-01-02", bar.Date)
		if err != nil {
			return nil, fmt.Errorf("日期 %s 无效: %w", bar.Date, err)
		}
		quote := &eastmoney.Quote{
			Date: parsed, Code: req.Code, Market: eastmoney.MarketType(req.MarketCode),
			Open: bar.Open, Close: bar.Close, High: bar.High, Low: bar.Low, Volume: bar.Volume, NoTurnover: true, NoChange: true,
		}
		// 涨跌和振幅需要前一日收盘价。返回数据的第一条没有，它可能是上市首日，也可能只是被截断。
		if i > 0 && bars[i-1].Close > 0 {
			quote.NoChange = false
			previous := bars[i-1].Close
			// 和东方财富一样保留小数位，去掉浮点噪声。
			quote.Change = round(bar.Close-previous, 3)
			quote.ChangeRate = round((bar.Close-previous)/previous*100, 2)
			quote.Amplitude = round((bar.High-bar.Low)/previous*100, 2)
		}
		quotes = append(quotes, quote)
	}
	return quotes, nil
}

// IndicatorWarmup 返回计算技术指标时开始取数的日期。EMA 和 RSI 依赖起点：至少多取一年，长周期按周期放大，
// 同一天的值基本不随查询区间变化。sec strategy 和 Desktop 技术分析共用。
func IndicatorWarmup(begin time.Time, required int, freq eastmoney.Freq) time.Time {
	return begin.AddDate(0, 0, -max(365, required*8*freq.BarDays()))
}
