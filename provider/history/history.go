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
	quotes, err := eastmoney.GetQuoteHistory(ctx, req)
	if err == nil {
		return &Result{Quotes: quotes}, nil
	}
	if ctx.Err() != nil {
		return nil, err
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
	quotes, fallbackErr = toQuotes(bars, req, required)
	if fallbackErr != nil {
		return nil, fmt.Errorf("东方财富: %w；新浪备用: %w", err, fallbackErr)
	}
	return &Result{Quotes: quotes, Note: FallbackNote}, nil
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

func toQuotes(bars []*sina.Bar, req *eastmoney.GetQuoteHistoryReq, required string) ([]*eastmoney.Quote, error) {
	// 取满上限时，更早的数据被截断了；不满上限说明已经到上市首日。
	if len(bars) >= sina.MaxBars && required != "" && compactDate(bars[0].Date) > compactDate(required) {
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
