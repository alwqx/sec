package history

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// ratioDays 是不复权日线：2026-07-01 每股分红 1 元，前收 10，除权参考价 9，等比比例 0.9。
var ratioDays = []string{
	"2026-06-26,9,10,10,9,1,100,0,0,0,0.1",
	"2026-06-29,10,10,11,10,1,100,0,0,0,0.1",
	"2026-06-30,10,10,10,9.5,1,100,0,0,0,0.1",
	"2026-07-01,9,9.9,9.5,8.5,1,100,0,0,0,0.1",
	"2026-07-02,9.9,9.9,10,9.8,1,100,0,0,0,0.1",
}

// cashDays 是 ratioDays 按金额减去 1 元分红的前复权，即东方财富的前复权。
var cashDays = []string{
	"2026-06-26,8,9,9,8,1,100,0,0,0,0.1",
	"2026-06-29,9,9,10,9,1,100,0,0,0,0.1",
	"2026-06-30,9,9,9,8.5,1,100,0,0,0,0.1",
	"2026-07-01,9,9.9,9.5,8.5,1,100,0,0,0,0.1",
	"2026-07-02,9.9,9.9,10,9.8,1,100,0,0,0,0.1",
}

func mockKline(market int, code, fqt string, days []string) {
	body := `{"data":{"code":"` + code + `","market":` + map[int]string{1: "1", 105: "105"}[market] + `,"klines":["` + strings.Join(days, `","`) + `"]}}`
	gock.New("https://push2his.eastmoney.com").Get(klinePath).MatchParam("fqt", fqt).MatchParam("klt", "101").MatchParam("end", "20500101").Reply(200).BodyString(body)
}

func mockFactors(path, body string) {
	gock.New("https://finance.sina.com.cn").Get(path).Reply(200).BodyString(body + "\n/* abc */")
}

const cmbFactors = `var sh600036qfq={"total":2,"data":[{"d":"2026-07-01", "f":"1"},{"d":"1900-01-01", "f":"1.1111111111111112"}]}`

func ratioReq(code string, market int, freq eastmoney.Freq) *eastmoney.GetQuoteHistoryReq {
	return &eastmoney.GetQuoteHistoryReq{Code: code, MarketCode: market, FQT: eastmoney.QuoteFQTRatio, Freq: freq, Begin: "20260629", End: "20260702"}
}

func closes(quotes []*eastmoney.Quote) []float64 {
	values := make([]float64, len(quotes))
	for i, quote := range quotes {
		values[i] = round(quote.Close, 6)
	}
	return values
}

func TestRatioDaily(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).MatchParam("beg", "20260613").MatchParam("fqt", "0").
		Reply(200).BodyString(`{"data":{"code":"600036","market":1,"klines":["` + strings.Join(ratioDays, `","`) + `"]}}`)
	mockFactors("/realstock/company/sh600036/qfq.js", cmbFactors)
	result, err := Get(context.Background(), ratioReq("600036", 1, eastmoney.FreqDay))
	require.NoError(t, err)
	require.True(t, gock.IsDone(), "多取 16 天的不复权日线，不请求东方财富前复权")
	require.Empty(t, result.Note)
	require.Equal(t, []float64{9, 9, 9.9, 9.9}, closes(result.Quotes), "除权日之前乘以 0.9，最新价不变")

	first := result.Quotes[0]
	require.Equal(t, "2026-06-29", first.Date.Format(time.DateOnly))
	require.False(t, first.NoChange, "多取的前一天提供前收盘")
	require.Equal(t, 0.0, first.ChangeRate)
	require.InDelta(t, 9.9, first.High, 1e-9)
	require.Equal(t, int64(100), first.Volume, "成交量不调整")
	require.Equal(t, int(eastmoney.QuoteFQTRatio), first.Fqt)

	exDay := result.Quotes[2]
	require.Equal(t, 10.0, exDay.ChangeRate, "除权日的涨跌幅是相对除权参考价 9 的真实收益")
	require.Equal(t, 0.9, exDay.Change)
	require.Equal(t, 11.11, exDay.Amplitude)
}

func TestRatioWeekly(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).MatchParam("beg", "20260601").MatchParam("klt", "101").
		Reply(200).BodyString(`{"data":{"code":"600036","market":1,"klines":["` + strings.Join(ratioDays, `","`) + `"]}}`)
	mockFactors("/realstock/company/sh600036/qfq.js", cmbFactors)
	result, err := Get(context.Background(), ratioReq("600036", 1, eastmoney.FreqWeek))
	require.NoError(t, err)
	require.True(t, gock.IsDone(), "周线也取日线，多取 28 天")
	require.Len(t, result.Quotes, 1, "2026-06-26 那一周的日期早于开始日期")
	week := result.Quotes[0]
	require.Equal(t, "2026-07-02", week.Date.Format(time.DateOnly), "日期是一周最后一个交易日")
	require.InDelta(t, 9, week.Open, 1e-9)
	require.InDelta(t, 10, week.High, 1e-9)
	require.InDelta(t, 8.5, week.Low, 1e-9)
	require.InDelta(t, 9.9, week.Close, 1e-9)
	require.Equal(t, int64(400), week.Volume)
	require.InDelta(t, 400, week.TurnOver, 1e-9)
	require.InDelta(t, 0.4, week.Velocity, 1e-9)
	require.Equal(t, 10.0, week.ChangeRate, "相对上一周复权后的收盘 9")
}

func TestRatioCashFactorsAreChecked(t *testing.T) {
	// 美股复权因子按金额减去分红，和东方财富前复权一致；2026-06-30 一段多出来（和前一段不同、和价格对不上），去掉。
	const factors = `var AAPL_qfq= {"total":3,"data":[{"d":"2026-07-01","f":"1","c":"0"},{"d":"2026-06-30","f":"1","c":"0"},{"d":"1970-01-01","f":"1","c":"-1"}]}`
	defer gock.Off()
	mockKline(105, "AAPL", "0", ratioDays)
	mockKline(105, "AAPL", "1", cashDays)
	mockFactors("/us_stock/company/reinstatement/AAPL_qfq.js", factors)
	result, err := Get(context.Background(), ratioReq("AAPL", 105, eastmoney.FreqDay))
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, []float64{9, 9, 9.9, 9.9}, closes(result.Quotes), "比例 = (10 − 1) ÷ 10")

	// 交易时段里最后一根还在变，前复权请求晚一点，价格已经不同：不算对不上。
	mockKline(105, "AAPL", "0", ratioDays)
	mockKline(105, "AAPL", "1", append(append([]string{}, cashDays[:4]...), "2026-07-02,9.9,10.1,10.2,9.8,1,100,0,0,0,0.1"))
	mockFactors("/us_stock/company/reinstatement/AAPL_qfq.js", factors)
	_, err = Get(context.Background(), ratioReq("AAPL", 105, eastmoney.FreqDay))
	require.NoError(t, err)

	// 新浪说分红 1 元，东方财富按 2 元减：对不上，报错。
	defer gock.Off()
	mockKline(105, "AAPL", "0", ratioDays)
	mockKline(105, "AAPL", "1", []string{"2026-06-26,7,8,8,7,1,100,0,0,0,0.1", "2026-06-29,8,8,9,8,1,100,0,0,0,0.1", "2026-06-30,8,8,8,7.5,1,100,0,0,0,0.1", ratioDays[3], ratioDays[4]})
	mockFactors("/us_stock/company/reinstatement/AAPL_qfq.js", factors)
	_, err = Get(context.Background(), ratioReq("AAPL", 105, eastmoney.FreqDay))
	require.ErrorIs(t, err, ErrAdjustMismatch)
}

func TestRatioErrors(t *testing.T) {
	defer gock.Off()
	gock.New("https://finance.sina.com.cn").Get("/realstock/company/sh600036/qfq.js").Reply(404)
	_, err := Get(context.Background(), ratioReq("600036", 1, eastmoney.FreqDay))
	require.ErrorIs(t, err, sina.ErrNoAdjustFactors)
	require.True(t, gock.IsDone(), "先查复权因子，没有因子时不下载日线")

	_, err = Get(context.Background(), ratioReq("430047", 0x7f, eastmoney.FreqDay))
	require.ErrorIs(t, err, ErrUnsupportedMarket)

	_, err = eastmoney.GetQuoteHistory(context.Background(), ratioReq("600036", 1, eastmoney.FreqDay))
	require.ErrorContains(t, err, "不支持复权方式", "东方财富接口不能把等比复权当成不复权")
}

func TestRatioFallback(t *testing.T) {
	sinaDays := []map[string]string{
		row("2026-06-26", "9", "10", "9", "10"), row("2026-06-29", "10", "11", "10", "10"), row("2026-06-30", "10", "10", "9.5", "10"),
		row("2026-07-01", "9", "9.5", "8.5", "9.9"), row("2026-07-02", "9.9", "10", "9.8", "9.9"),
	}
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(503)
	mockSina(sinaDays)
	mockFactors("/realstock/company/sh600036/qfq.js", cmbFactors)
	result, err := Get(context.Background(), ratioReq("600036", 1, eastmoney.FreqDay))
	require.NoError(t, err)
	require.Equal(t, RatioFallbackNote, result.Note)
	require.Equal(t, []float64{9, 9, 9.9, 9.9}, closes(result.Quotes))

	// 沪深基金的因子按金额调整，要用东方财富前复权核对，备用数据下核对不了。
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(503)
	mockSina(sinaDays)
	mockFactors("/realstock/company/sh510500/qfq.js", `var sh510500qfq={"total":2,"data":[{"d":"2026-07-01", "f":"1", "s":"1", "u":"0"},{"d":"1900-01-01", "f":"1", "s":"1", "u":"1"}]}`)
	_, err = Get(context.Background(), ratioReq("510500", 1, eastmoney.FreqDay))
	require.ErrorIs(t, err, ErrAdjustedUnavailable)
}

func TestRatioIndexIsUnchanged(t *testing.T) {
	defer gock.Off()
	mockKline(1, "000001", "0", ratioDays)
	mockFactors("/realstock/company/sh000001/qfq.js", `var sh000001qfq={"total":0, "data":[]}`)
	result, err := Get(context.Background(), ratioReq("000001", 1, eastmoney.FreqDay))
	require.NoError(t, err)
	require.Equal(t, []float64{10, 10, 9.9, 9.9}, closes(result.Quotes), "指数没有复权因子")
}

// 停牌期间连续两次除权：比例按前后两根 K 线所在的段一次算出，不是把同一个前收盘分别代入两次。
func TestRatioAdjustSeveralEventsBetweenBars(t *testing.T) {
	day := func(date string, closePrice float64) *eastmoney.Quote {
		parsed, _ := time.Parse(time.DateOnly, date)
		return &eastmoney.Quote{Date: parsed, Open: closePrice, High: closePrice, Low: closePrice, Close: closePrice}
	}
	segments := []sina.AdjustSegment{{Date: "1970-01-01", Scale: 0.5, Offset: -2}, {Date: "2026-02-02", Scale: 0.5, Offset: -1}, {Date: "2026-02-03", Scale: 1, Offset: 0}}
	adjusted, err := ratioAdjust([]*eastmoney.Quote{day("2026-01-30", 20), day("2026-02-04", 7)}, segments)
	require.NoError(t, err)
	// 前收盘 20 的前复权价 20×0.5−2 = 8，按最新一段换回不复权价是 8，比例 0.4。
	require.InDelta(t, 8, adjusted[0].Close, 1e-9)
	require.InDelta(t, 7, adjusted[1].Close, 1e-9)

	_, err = ratioAdjust([]*eastmoney.Quote{day("2026-01-30", 20), day("2026-02-04", 7)}, []sina.AdjustSegment{{Date: "1970-01-01", Scale: 1, Offset: -30}, {Date: "2026-02-03", Scale: 1}})
	require.ErrorContains(t, err, "除权比例", "分红超过前收盘")
}

// 结束日期在一周中间时不返回这一周，和东方财富一致：东方财富 end=20250716（周三）的周线最后一根是 07-11。
func TestRatioWeeklyEndsMidWeek(t *testing.T) {
	defer gock.Off()
	mockKline(1, "600036", "0", ratioDays)
	mockFactors("/realstock/company/sh600036/qfq.js", cmbFactors)
	req := ratioReq("600036", 1, eastmoney.FreqWeek)
	req.Begin, req.End = "20260622", "20260701"
	result, err := Get(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Quotes, 1)
	require.Equal(t, "2026-06-26", result.Quotes[0].Date.Format(time.DateOnly), "07-02 那一周晚于结束日期")
}

func TestRatioDashedBegin(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).MatchParam("beg", "20260613").
		Reply(200).BodyString(`{"data":{"code":"600036","market":1,"klines":["` + strings.Join(ratioDays, `","`) + `"]}}`)
	mockFactors("/realstock/company/sh600036/qfq.js", cmbFactors)
	req := ratioReq("600036", 1, eastmoney.FreqDay)
	req.Begin, req.End = "2026-06-29", "2026-07-02"
	result, err := Get(context.Background(), req)
	require.NoError(t, err)
	require.True(t, gock.IsDone(), "带横线的开始日期也要多取")
	require.False(t, result.Quotes[0].NoChange)
}

func TestRatioUSFallbackNote(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get(klinePath).Reply(503)
	gock.New("https://stock.finance.sina.com.cn").Get("/usstock/api/jsonp.php/var.t=/US_MinKService.getDailyK").MatchParam("symbol", "brk.b").
		Reply(200).BodyString(`var t=([{"d":"2026-06-30","o":"10","h":"10","l":"9.5","c":"10","v":"1"},{"d":"2026-07-01","o":"9","h":"9.5","l":"8.5","c":"9.9","v":"1"}]);`)
	// 只有拆股、没有分红金额，不用核对，备用数据也能算。
	mockFactors("/us_stock/company/reinstatement/BRK.B_qfq.js", `var BRK$B_qfq= {"total":1,"data":[{"d":"1900-01-01","f":"1","c":"0"}]}`)
	req := ratioReq("BRK.B", 105, eastmoney.FreqDay)
	req.Begin = "20260630"
	result, err := Get(context.Background(), req)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, RatioUSFallbackNote, result.Note, "新浪美股日线是全部历史，不是最多 1023 根")
	require.Equal(t, []float64{10, 9.9}, closes(result.Quotes))
}

func TestRatioAdjustRoundsPrices(t *testing.T) {
	parse := func(date string) time.Time { parsed, _ := time.Parse(time.DateOnly, date); return parsed }
	daily := []*eastmoney.Quote{{Date: parse("2026-06-30"), Open: 9.9, High: 9.9, Low: 9.9, Close: 10}, {Date: parse("2026-07-01"), Open: 9, High: 9, Low: 9, Close: 9}}
	adjusted, err := ratioAdjust(daily, []sina.AdjustSegment{{Date: "1900-01-01", Scale: 0.9}, {Date: "2026-07-01", Scale: 1}})
	require.NoError(t, err)
	require.Equal(t, 8.91, adjusted[0].Open, "9.9 × 0.9 不能输出 8.910000000000002")
}

func TestCheckSegmentsTolerance(t *testing.T) {
	parse := func(date string) time.Time { parsed, _ := time.Parse(time.DateOnly, date); return parsed }
	// 第二根是最后一根，只核对开盘价。
	quote := func(price float64) []*eastmoney.Quote {
		return []*eastmoney.Quote{{Date: parse("2026-06-30"), Open: price, High: price, Low: price, Close: price}, {Date: parse("2026-07-01"), Open: price, Close: 1}}
	}
	segments := []sina.AdjustSegment{{Date: "1900-01-01", Scale: 1, Offset: -1}}
	_, err := checkSegments(segments, quote(700000), quote(700002))
	require.NoError(t, err, "高价证券按相对误差放宽")
	_, err = checkSegments(segments, quote(700000), quote(700100))
	require.ErrorIs(t, err, ErrAdjustMismatch)
	_, err = checkSegments(segments, quote(10), quote(9.01))
	require.ErrorIs(t, err, ErrAdjustMismatch, "低价证券仍按 0.006")

	// 最后一根是除权日：最新一段只有这一根，开盘价仍要核对。
	raw := []*eastmoney.Quote{{Date: parse("2026-06-30"), Open: 10, High: 10, Low: 10, Close: 10}, {Date: parse("2026-07-01"), Open: 9, High: 9, Low: 9, Close: 9}}
	qfq := []*eastmoney.Quote{{Date: parse("2026-06-30"), Open: 9, High: 9, Low: 9, Close: 9}, {Date: parse("2026-07-01"), Open: 9, High: 9.5, Low: 8, Close: 8.8}}
	_, err = checkSegments([]sina.AdjustSegment{{Date: "1900-01-01", Scale: 1, Offset: -1}, {Date: "2026-07-01", Scale: 1, Offset: 0}}, raw, qfq)
	require.NoError(t, err, "最后一根的最高、最低和收盘还在变")
	_, err = checkSegments([]sina.AdjustSegment{{Date: "1900-01-01", Scale: 1, Offset: -1}, {Date: "2026-07-01", Scale: 1, Offset: 0.5}}, raw, qfq)
	require.ErrorIs(t, err, ErrAdjustMismatch, "最新一段的因子不对，开盘价对不上")
}
