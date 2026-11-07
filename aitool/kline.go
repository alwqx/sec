package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alwqx/sec/indicator"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/history"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
)

// recentBars 是 get_kline_summary 返回的最近 K 线根数，更早的只给区间统计。
const recentBars = 20

const klineSchema = `{
  "type": "object",
  "properties": {
    "code": {
      "type": "string",
      "description": "一只证券的代码或名称，最好用 search_security 返回的 ex_code，如 SH600519、HK00700、$AAPL。支持沪深、港股和美股，不支持北交所和场外基金",
      "minLength": 1
    },
    "freq": {
      "type": "string",
      "enum": ["day", "week", "month"],
      "description": "K 线周期，默认 day"
    },
    "begin": {
      "type": "string",
      "description": "开始日期 YYYY-MM-DD，含当天。默认日线取最近 1 年，周线 3 年，月线 10 年"
    },
    "end": {
      "type": "string",
      "description": "结束日期 YYYY-MM-DD，含当天，默认今天"
    },
    "fq": {
      "type": "string",
      "enum": ["qfq", "bfq", "hfq"],
      "description": "复权方式：qfq 前复权（默认，涨跌幅和回撤不受分红拆股影响）、bfq 不复权（当时的实际成交价）、hfq 后复权"
    }
  },
  "required": ["code"],
  "additionalProperties": false
}`

// KlineSummaryTool 返回 get_kline_summary：一只证券在一段区间的 K 线统计和最近 20 根 K 线。
func KlineSummaryTool() Tool {
	return Tool{
		Name: "get_kline_summary",
		Description: "查询一只沪深、港股或美股证券在一段区间的 K 线摘要：区间首尾收盘价和涨跌幅、最高最低价及日期、最大回撤、" +
			"最新的 MA5/MA20/MA60、平均成交量和成交额，再加最近 20 根 K 线。不返回全部 K 线。" +
			"用于回答走势、涨跌、回撤和均线的问题；最新价格请用 get_quote。默认前复权日线，取最近 1 年。",
		InputSchema: json.RawMessage(klineSchema),
		Call:        callKlineSummary,
	}
}

type klineInput struct {
	Code  string `json:"code"`
	Freq  string `json:"freq"`
	Begin string `json:"begin"`
	End   string `json:"end"`
	FQ    string `json:"fq"`
}

// klineBar 是一根 K 线。数据源没有的值是 null，不是 0。
type klineBar struct {
	Date      string   `json:"date"`
	Open      float64  `json:"open"`
	High      float64  `json:"high"`
	Low       float64  `json:"low"`
	Close     float64  `json:"close"`
	ChangePct *float64 `json:"change_pct"` // 相对上一根收盘的涨跌幅，单位 %
	Volume    int64    `json:"volume"`     // 成交量，单位：股
	Amount    *float64 `json:"amount"`     // 成交额，交易所计价币种
}

// pricePoint 是一个价格和它出现的日期。
type pricePoint struct {
	Price float64 `json:"price"`
	Date  string  `json:"date"`
}

// drawdown 是区间内按收盘价计算的最大回撤。
type drawdown struct {
	Pct        float64 `json:"pct"` // 从高点到低点的跌幅，单位 %，是负数或 0；不计收盘价 ≤ 0 的 K 线
	PeakDate   string  `json:"peak_date"`
	TroughDate string  `json:"trough_date"`
}

type klineSummary struct {
	Bars        int            `json:"bars"`
	FirstDate   string         `json:"first_date"`
	LastDate    string         `json:"last_date"`
	FirstClose  float64        `json:"first_close"`
	LastClose   float64        `json:"last_close"`
	ChangePct   *float64       `json:"change_pct"` // 最后一根相对第一根收盘价的涨跌幅，单位 %；第一根收盘价 ≤ 0 时为 null
	High        pricePoint     `json:"high"`       // 最高价，按每根 K 线的最高价
	Low         pricePoint     `json:"low"`        // 最低价，按每根 K 线的最低价
	MaxDrawdown drawdown       `json:"max_drawdown"`
	MA          movingAverages `json:"ma"`
	AvgVolume   float64        `json:"avg_volume"`
	AvgAmount   *float64       `json:"avg_amount"` // 数据源没有成交额时为 null
}

// movingAverages 是最后一根 K 线的收盘价均线，单位是根，周线、月线同样按根计算；根数不够时为 null。
type movingAverages struct {
	MA5  *float64 `json:"ma5"`
	MA20 *float64 `json:"ma20"`
	MA60 *float64 `json:"ma60"`
}

type klineData struct {
	ExCode  string       `json:"ex_code"`
	Name    string       `json:"name"`
	Freq    string       `json:"freq"`
	FQ      string       `json:"fq"`
	Begin   string       `json:"begin"` // 请求的区间，实际 K 线见 summary 的 first_date、last_date
	End     string       `json:"end"`
	Summary klineSummary `json:"summary"`
	Recent  []klineBar   `json:"recent"` // 最近的 K 线，按日期升序，最多 20 根
}

func callKlineSummary(ctx context.Context, input json.RawMessage) (*Result, error) {
	var in klineInput
	if err := Decode(input, &in); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, Inputf("code 不能为空，请给出一只证券的代码或名称")
	}
	freq, err := eastmoney.ParseFreq(in.Freq)
	if err != nil {
		return nil, Inputf("freq 只能是 day、week 或 month")
	}
	fqName, fqt, err := klineFQ(in.FQ)
	if err != nil {
		return nil, err
	}
	begin, end, err := klineRange(in.Begin, in.End, freq)
	if err != nil {
		return nil, err
	}

	secs, err := sina.Resolve(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("搜索证券失败：%w", err)
	}
	if len(secs) == 0 {
		return nil, Inputf("没有找到证券 %s，请检查代码，或先用 search_security 搜索", code)
	}
	sec := secs[0]
	market, err := history.Market(sec)
	if err != nil {
		return nil, Inputf("%s（%s）暂不支持 K 线，只支持沪深、港股和美股", sec.ExCode, sec.Name)
	}

	req := &eastmoney.GetQuoteHistoryReq{
		Code: sec.Code, MarketCode: market, FQT: fqt, Freq: freq,
		Begin: begin.Format(eastmoney.TimeYYMMDD), End: end.Format(eastmoney.TimeYYMMDD),
	}
	got, err := history.Get(ctx, req)
	switch {
	case errors.Is(err, history.ErrRangeNotCovered):
		return nil, Inputf("K 线备用数据覆盖不了这个区间，请缩短区间后重试：%v", err)
	case errors.Is(err, eastmoney.ErrNoData):
		// 前复权时 history 会提示切换为不复权，但东方财富本来就没有这只证券，切换也没用。
		return nil, Inputf("东方财富没有 %s（%s）的 K 线数据，可能已退市或不是股票、ETF", sec.ExCode, sec.Name)
	case err != nil:
		return nil, fmt.Errorf("K 线获取失败：%w", err)
	}
	quotes := got.Quotes
	if len(quotes) == 0 {
		return nil, Inputf("%s 在 %s 到 %s 之间没有 K 线，可能停牌、未上市或日期不对", sec.ExCode, begin.Format(time.DateOnly), end.Format(time.DateOnly))
	}

	data := &klineData{
		ExCode: sec.ExCode, Name: sec.Name, Freq: string(freq), FQ: fqName,
		Begin: begin.Format(time.DateOnly), End: end.Format(time.DateOnly),
		Summary: summarizeKline(quotes), Recent: recentKline(quotes),
	}
	source := "东方财富 K 线"
	if got.Note != "" {
		source = "新浪财经 K 线（备用）"
	}
	result := NewResult("get_kline_summary", source, data)
	result.AsOf = data.Summary.LastDate
	if got.Note != "" {
		result.Notes = append(result.Notes, got.Note)
	}
	switch fqName {
	case "qfq":
		result.Notes = append(result.Notes, "价格是前复权价：最新价和实际成交价一致，更早的价格按分红、拆股调整过，不是当时的成交价")
	case "hfq":
		result.Notes = append(result.Notes, "价格是后复权价，和实际成交价不同，只适合比较涨跌")
	}
	if len(quotes) > recentBars {
		result.Notes = append(result.Notes, fmt.Sprintf("区间共 %d 根 K 线，recent 只列最近 %d 根，summary 按全部 K 线计算", len(quotes), recentBars))
	}
	if data.Summary.ChangePct == nil {
		result.Notes = append(result.Notes, "第一根 K 线的收盘价 ≤ 0，change_pct 为 null。前复权按分红金额直接从更早的价格里减去，区间长时早期价格接近 0 甚至为负，max_drawdown 等百分比也会失真；需要区间涨跌幅和回撤时请缩短区间，或改用 bfq")
	}
	if data.Summary.AvgAmount == nil {
		result.Notes = append(result.Notes, "数据源没有成交额，avg_amount 和 amount 为 null")
	}
	return result, nil
}

// klineFQ 解析复权方式，默认前复权。
func klineFQ(text string) (string, eastmoney.FuQuanType, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "qfq":
		return "qfq", eastmoney.QuoteFQTFront, nil
	case "bfq":
		return "bfq", eastmoney.QuoteFQTDefault, nil
	case "hfq":
		return "hfq", eastmoney.QuoteFQTPost, nil
	}
	return "", 0, Inputf("fq 只能是 qfq、bfq 或 hfq")
}

// klineRange 解析区间。没给开始日期时按周期往前取：日线 1 年，周线 3 年，月线 10 年。
func klineRange(beginText, endText string, freq eastmoney.Freq) (time.Time, time.Time, error) {
	end := utils.ShanghaiToday()
	if endText = strings.TrimSpace(endText); endText != "" {
		parsed, err := time.Parse(time.DateOnly, endText)
		if err != nil {
			return time.Time{}, time.Time{}, Inputf("end 的格式应为 YYYY-MM-DD，如 2026-01-31")
		}
		end = parsed
	}
	begin := end.AddDate(-klineDefaultYears(freq), 0, 0)
	if beginText = strings.TrimSpace(beginText); beginText != "" {
		parsed, err := time.Parse(time.DateOnly, beginText)
		if err != nil {
			return time.Time{}, time.Time{}, Inputf("begin 的格式应为 YYYY-MM-DD，如 2026-01-01")
		}
		begin = parsed
	}
	if begin.After(end) {
		return time.Time{}, time.Time{}, Inputf("begin（%s）不能晚于 end（%s）", begin.Format(time.DateOnly), end.Format(time.DateOnly))
	}
	return begin, end, nil
}

// klineDefaultYears 是没给开始日期时取的年数，K 线根数大致在 120 到 250 之间。
func klineDefaultYears(freq eastmoney.Freq) int {
	switch freq {
	case eastmoney.FreqWeek:
		return 3
	case eastmoney.FreqMonth:
		return 10
	default:
		return 1
	}
}

// summarizeKline 计算区间统计。quotes 按日期升序，至少一根。
func summarizeKline(quotes []*eastmoney.Quote) klineSummary {
	first, last := quotes[0], quotes[len(quotes)-1]
	summary := klineSummary{
		Bars: len(quotes), FirstDate: first.Date.Format(time.DateOnly), LastDate: last.Date.Format(time.DateOnly),
		FirstClose: first.Close, LastClose: last.Close,
		High: pricePoint{first.High, first.Date.Format(time.DateOnly)},
		Low:  pricePoint{first.Low, first.Date.Format(time.DateOnly)},
	}
	if first.Close > 0 {
		pct := round((last.Close-first.Close)/first.Close*100, 2)
		summary.ChangePct = &pct
	}

	var volume, amount float64
	hasAmount := true
	peak := first
	summary.MaxDrawdown = drawdown{PeakDate: summary.FirstDate, TroughDate: summary.FirstDate}
	closes := make([]float64, len(quotes))
	for i, quote := range quotes {
		date := quote.Date.Format(time.DateOnly)
		closes[i] = quote.Close
		if quote.High > summary.High.Price {
			summary.High = pricePoint{quote.High, date}
		}
		if quote.Low < summary.Low.Price {
			summary.Low = pricePoint{quote.Low, date}
		}
		if quote.Close > peak.Close {
			peak = quote
		}
		// 前复权的早期价格可能 ≤ 0，算出的跌幅没有意义。
		if peak.Close > 0 && quote.Close > 0 {
			if pct := (quote.Close - peak.Close) / peak.Close * 100; pct < summary.MaxDrawdown.Pct {
				summary.MaxDrawdown = drawdown{Pct: pct, PeakDate: peak.Date.Format(time.DateOnly), TroughDate: date}
			}
		}
		volume += float64(quote.Volume)
		amount += quote.TurnOver
		hasAmount = hasAmount && !quote.NoTurnover
	}
	summary.MaxDrawdown.Pct = round(summary.MaxDrawdown.Pct, 2)
	summary.AvgVolume = round(volume/float64(len(quotes)), 0)
	if hasAmount {
		avg := round(amount/float64(len(quotes)), 0)
		summary.AvgAmount = &avg
	}
	summary.MA = movingAverages{lastMA(closes, 5), lastMA(closes, 20), lastMA(closes, 60)}
	return summary
}

// lastMA 返回最后一根的 period 日均线，根数不够时返回 nil。
func lastMA(closes []float64, period int) *float64 {
	if len(closes) < period {
		return nil
	}
	ma := round(indicator.SMA(closes, period)[len(closes)-1], 4)
	return &ma
}

// recentKline 返回最近 recentBars 根 K 线，按日期升序。
func recentKline(quotes []*eastmoney.Quote) []klineBar {
	quotes = quotes[max(0, len(quotes)-recentBars):]
	bars := make([]klineBar, 0, len(quotes))
	for _, quote := range quotes {
		bar := klineBar{
			Date: quote.Date.Format(time.DateOnly), Open: quote.Open, High: quote.High, Low: quote.Low, Close: quote.Close, Volume: quote.Volume,
		}
		if !quote.NoChange {
			pct := quote.ChangeRate
			bar.ChangePct = &pct
		}
		if !quote.NoTurnover {
			amount := quote.TurnOver
			bar.Amount = &amount
		}
		bars = append(bars, bar)
	}
	return bars
}
