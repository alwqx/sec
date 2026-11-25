package history

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
)

// 等比前复权：东方财富的前复权按金额减去分红，区间一长早期价格接近 0 甚至为负。这里用不复权日线和新浪复权因子，
// 按每次除权除息的比例缩放更早的价格，最新价和实际成交价一致。口径和核对记录见 docs/adjust.md。

// RatioFallbackNote 说明沪深等比前复权用了新浪备用日线。
const RatioFallbackNote = "东方财富行情暂不可用，已改用新浪日线计算等比前复权：没有成交额和换手率，最多 1023 根日线（约 4 年）；区间开头的指标可能缺少预热数据。"

// RatioUSFallbackNote 说明美股等比前复权用了新浪美股日线。
const RatioUSFallbackNote = "东方财富行情暂不可用，已改用新浪美股日线计算等比前复权：没有成交额和换手率。"

// ErrAdjustMismatch 表示新浪复权因子和东方财富前复权对不上，算出的等比复权价格不可信。
var ErrAdjustMismatch = errors.New("新浪复权因子和东方财富前复权对不上")

// adjustTolerance 是核对时允许的价格误差：东方财富前复权保留 2 到 3 位小数；价格很高时按 1e-5 的相对误差放宽。
func adjustTolerance(price float64) float64 {
	return max(0.006, math.Abs(price)*1e-5)
}

// getRatio 计算等比前复权。不复权日线从请求开始前一到两个周期取到最新：周线、月线的第一根要有完整周期和前收盘，
// 请求区间之后的除权也会影响区间内的价格。周线、月线由复权后的日线合成，再按日期筛选：
// 结束日期在周期中间时不返回这个周期，和东方财富一致（周期的日期是它最后一个交易日，晚于结束日期）。
func getRatio(ctx context.Context, req *eastmoney.GetQuoteHistoryReq, required string) (*Result, error) {
	exCode, err := adjustExCode(req.MarketCode, req.Code)
	if err != nil {
		return nil, err
	}
	raw := *req
	raw.FQT, raw.Freq, raw.End = eastmoney.QuoteFQTDefault, eastmoney.FreqDay, "20500101"
	if begin, err := time.Parse("20060102", compactDate(req.Begin)); err == nil {
		raw.Begin = begin.AddDate(0, 0, -(2*req.Freq.BarDays() + 14)).Format("20060102")
	}
	got, err := GetFrom(ctx, &raw, required)
	if err != nil {
		return nil, err
	}
	// 美股按纽交所等其他市场代码查到时，和 Get 一样写回请求。
	req.MarketCode = raw.MarketCode
	segments, err := sina.QueryAdjustFactors(ctx, exCode)
	if err != nil {
		return nil, fmt.Errorf("等比复权: %w", err)
	}
	if cashAdjusted(segments) {
		if got.Note != "" {
			return nil, fmt.Errorf("%w: 东方财富暂不可用，无法核对 %s 的复权因子", ErrAdjustedUnavailable, exCode)
		}
		front := raw
		front.FQT = eastmoney.QuoteFQTFront
		qfq, err := getEastMoney(ctx, &front)
		if err != nil {
			return nil, fmt.Errorf("等比复权核对: %w", err)
		}
		if segments, err = checkSegments(segments, got.Quotes, qfq); err != nil {
			return nil, fmt.Errorf("%s: %w", exCode, err)
		}
	}
	daily, err := ratioAdjust(got.Quotes, segments)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", exCode, err)
	}
	bars := aggregate(daily, req.Freq)
	fillChange(bars)
	result := &Result{Quotes: make([]*eastmoney.Quote, 0, len(bars))}
	for _, bar := range bars {
		date := bar.Date.Format("20060102")
		if (req.Begin == "" || date >= compactDate(req.Begin)) && (req.End == "" || date <= compactDate(req.End)) {
			result.Quotes = append(result.Quotes, bar)
		}
	}
	switch got.Note {
	case "":
	case USFallbackNote:
		result.Note = RatioUSFallbackNote
	default:
		result.Note = RatioFallbackNote
	}
	return result, nil
}

// adjustExCode 返回查询新浪复权因子用的代码，如 SH600036、HK00700、$AAPL。
func adjustExCode(market int, code string) (string, error) {
	switch {
	case market == int(eastmoney.MarketTypeSse):
		return "SH" + code, nil
	case market == int(eastmoney.MarketTypeSzSe):
		return "SZ" + code, nil
	case market == int(eastmoney.MarketTypeHK):
		return "HK" + code, nil
	case isUS(market):
		return "$" + code, nil
	}
	return "", fmt.Errorf("%w: 市场代码 %d 没有复权因子", ErrUnsupportedMarket, market)
}

// cashAdjusted 判断复权因子里有没有按金额调整的分红（美股、沪深基金）。这时新浪的换算公式和东方财富前复权相同，可以逐根核对。
func cashAdjusted(segments []sina.AdjustSegment) bool {
	for _, segment := range segments {
		if segment.Offset != 0 {
			return true
		}
	}
	return false
}

// segmentIndex 返回每根 K 线所在的复权段。
func segmentIndex(quotes []*eastmoney.Quote, segments []sina.AdjustSegment) ([]int, error) {
	index := make([]int, len(quotes))
	for i, quote := range quotes {
		date := quote.Date.Format(time.DateOnly)
		j := sort.Search(len(segments), func(k int) bool { return segments[k].Date > date }) - 1
		if j < 0 {
			return nil, fmt.Errorf("复权因子没有覆盖 %s", date)
		}
		index[i] = j
	}
	return index, nil
}

// checkSegments 逐根核对每段换算出的开高低收和东方财富前复权。对不上的段如果和前一段一致，是多出来的记录
// （如南方中证 500ETF 份额折算基准日 2022-08-26 的一段），去掉；否则报错。区间里没有 K 线的段无从核对，原样保留。
func checkSegments(segments []sina.AdjustSegment, raw, qfq []*eastmoney.Quote) ([]sina.AdjustSegment, error) {
	front := make(map[string]*eastmoney.Quote, len(qfq))
	for _, quote := range qfq {
		front[quote.Date.Format(time.DateOnly)] = quote
	}
	index, err := segmentIndex(raw, segments)
	if err != nil {
		return nil, err
	}
	bars := make([][]*eastmoney.Quote, len(segments))
	for i, quote := range raw {
		bars[index[i]] = append(bars[index[i]], quote)
	}
	matches := func(segment sina.AdjustSegment, quotes []*eastmoney.Quote) bool {
		for _, quote := range quotes {
			adjusted, ok := front[quote.Date.Format(time.DateOnly)]
			if !ok {
				continue
			}
			for _, pair := range [][2]float64{{quote.Open, adjusted.Open}, {quote.High, adjusted.High}, {quote.Low, adjusted.Low}, {quote.Close, adjusted.Close}} {
				if math.Abs(pair[0]*segment.Scale+segment.Offset-pair[1]) > adjustTolerance(pair[1]) {
					return false
				}
			}
		}
		return true
	}
	kept := make([]sina.AdjustSegment, 0, len(segments))
	for j, segment := range segments {
		if matches(segment, bars[j]) {
			kept = append(kept, segment)
			continue
		}
		if len(kept) > 0 && matches(kept[len(kept)-1], bars[j]) {
			continue
		}
		return nil, fmt.Errorf("%w: %s 起的一段", ErrAdjustMismatch, segment.Date)
	}
	return kept, nil
}

// ratioAdjust 把不复权日线换成等比前复权。相邻两根 K 线落在不同的复权段时，中间发生了除权除息（停牌时可能不止一次），
// 比例 = 除权参考价 ÷ 前收盘，除权参考价是前收盘按前一段换算成前复权价、再按后一段换算回不复权价。
// 每根 K 线乘以它之后所有比例的乘积，最新一根不变，保留 6 位小数去掉浮点噪声（早期价格可能只有几分钱，不能只留 2、3 位）。
// 成交量和成交额不调整，和东方财富前复权一致。
func ratioAdjust(daily []*eastmoney.Quote, segments []sina.AdjustSegment) ([]*eastmoney.Quote, error) {
	adjusted := make([]*eastmoney.Quote, len(daily))
	if len(segments) == 0 {
		// 指数没有复权因子。
		for i, quote := range daily {
			copied := *quote
			copied.Fqt = int(eastmoney.QuoteFQTRatio)
			adjusted[i] = &copied
		}
		return adjusted, nil
	}
	index, err := segmentIndex(daily, segments)
	if err != nil {
		return nil, err
	}
	factor := 1.0
	for i := len(daily) - 1; i >= 0; i-- {
		copied := *daily[i]
		copied.Open, copied.High, copied.Low, copied.Close = round(copied.Open*factor, 6), round(copied.High*factor, 6), round(copied.Low*factor, 6), round(copied.Close*factor, 6)
		copied.Fqt = int(eastmoney.QuoteFQTRatio)
		adjusted[i] = &copied
		if i == 0 || index[i-1] == index[i] {
			continue
		}
		before, after, previous := segments[index[i-1]], segments[index[i]], daily[i-1].Close
		ratio := (previous*before.Scale + before.Offset - after.Offset) / (previous * after.Scale)
		if !(ratio > 0) || math.IsInf(ratio, 0) {
			return nil, fmt.Errorf("%s 的除权比例 %v 无效", daily[i].Date.Format(time.DateOnly), ratio)
		}
		factor *= ratio
	}
	return adjusted, nil
}

// aggregate 把日线合成周线、月线，和东方财富一致：周期按自然周、自然月划分，日期是最后一个交易日，
// 开盘取第一天、收盘取最后一天，最高、最低取极值，成交量、成交额、换手率相加。
func aggregate(daily []*eastmoney.Quote, freq eastmoney.Freq) []*eastmoney.Quote {
	if freq != eastmoney.FreqWeek && freq != eastmoney.FreqMonth {
		return daily
	}
	period := func(date time.Time) int {
		if freq == eastmoney.FreqWeek {
			year, week := date.ISOWeek()
			return year*100 + week
		}
		return date.Year()*100 + int(date.Month())
	}
	bars := make([]*eastmoney.Quote, 0, len(daily)/4+1)
	var current *eastmoney.Quote
	for _, quote := range daily {
		if current == nil || period(quote.Date) != period(current.Date) {
			copied := *quote
			current = &copied
			bars = append(bars, current)
			continue
		}
		current.Date, current.Close = quote.Date, quote.Close
		current.High, current.Low = max(current.High, quote.High), min(current.Low, quote.Low)
		current.Volume += quote.Volume
		current.TurnOver += quote.TurnOver
		current.Velocity += quote.Velocity
		current.NoTurnover = current.NoTurnover || quote.NoTurnover
	}
	return bars
}

// fillChange 按复权后的前收盘重算涨跌额、涨跌幅和振幅。除权日的涨跌幅因此是真实的持有收益，不是不复权价格的跳变。
func fillChange(bars []*eastmoney.Quote) {
	for i, bar := range bars {
		previous := 0.0
		if i > 0 {
			previous = bars[i-1].Close
		}
		setChange(bar, previous)
	}
}
