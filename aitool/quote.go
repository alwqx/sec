package aitool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
)

// maxQuoteCodes 和 sec quote 一样，一次最多查 5 只。
const maxQuoteCodes = 5

const quoteSchema = `{
  "type": "object",
  "properties": {
    "codes": {
      "type": "array",
      "description": "证券代码或名称，1 到 5 个。A 股如 600519 或 SH600519，港股如 00700 或 HK00700，美股如 $AAPL，也可以写名称或拼音如 贵州茅台、zsyh",
      "items": {"type": "string", "minLength": 1},
      "minItems": 1,
      "maxItems": 5
    }
  },
  "required": ["codes"],
  "additionalProperties": false
}`

// QuoteTool 返回 get_quote：A 股、港股、美股和基金的最新行情。
func QuoteTool() Tool {
	return Tool{
		Name: "get_quote",
		Description: "查询 A 股、港股、美股和基金的最新行情：价格、涨跌、涨跌幅、开高低、成交量和成交额，以及行情的日期、时间和时区。" +
			"一次 1 到 5 只，代码或名称都可以。查不到的证券列在 data.missing 里，其他证券照常返回。" +
			"基金的 nav 为 true 时价格是单位净值。只是当前快照，历史走势请用 K 线工具。",
		InputSchema: json.RawMessage(quoteSchema),
		Call:        callQuote,
	}
}

type quoteInput struct {
	Codes []string `json:"codes"`
}

// quoteItem 是一只证券的行情。不可用的值是 null，不用 0 表示。
type quoteItem struct {
	ExCode    string   `json:"ex_code"`
	Name      string   `json:"name"`
	TradeDate string   `json:"trade_date"`
	Time      string   `json:"time"`     // 交易所当地时间，基金净值为空
	Timezone  string   `json:"timezone"` // trade_date 和 time 所在的 IANA 时区
	Price     *float64 `json:"price"`
	// PriceIsYClose 表示当前没有成交价（如开盘前），Price 是昨收。
	PriceIsYClose bool     `json:"price_is_yclose"`
	YClose        *float64 `json:"yclose"`
	Open          *float64 `json:"open"`
	High          *float64 `json:"high"`
	Low           *float64 `json:"low"`
	Change        *float64 `json:"change"`     // 价格 - 昨收，保留 4 位小数；没有成交价时为 null
	ChangePct     *float64 `json:"change_pct"` // 涨跌幅，单位 %，保留 2 位小数
	Volume        *int64   `json:"volume"`     // 成交量，单位：股或份；基金净值没有成交，为 null
	Amount        *float64 `json:"amount"`     // 成交额，交易所计价币种；基金净值没有成交，为 null
	NAV           bool     `json:"nav"`        // 价格是基金单位净值，trade_date 是净值日期，没有时间、开高低和成交
}

// missingItem 是一个没有查到行情的输入。
type missingItem struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

type quoteData struct {
	Quotes  []quoteItem   `json:"quotes"`
	Missing []missingItem `json:"missing"`
}

func callQuote(ctx context.Context, input json.RawMessage) (*Result, error) {
	var in quoteInput
	if err := Decode(input, &in); err != nil {
		return nil, err
	}
	codes, err := quoteCodes(in.Codes)
	if err != nil {
		return nil, err
	}

	resolved := resolveCodes(ctx, codes)
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("搜索证券超时或被取消：%w", err)
	}
	secs, missing := resolved.secs, resolved.missing
	if len(secs) == 0 {
		// 都是找不到时是参数问题，让模型改代码重试；有搜索失败时是上游问题。
		if !resolved.searchFailed {
			return nil, Inputf("没有找到这些证券：%s。请检查代码，或改用名称、拼音重试", missingText(missing))
		}
		return nil, fmt.Errorf("没有查到任何证券：%s", missingText(missing))
	}

	exCodes := make([]string, 0, len(secs))
	for _, sec := range secs {
		exCodes = append(exCodes, sec.ExCode)
	}
	quotes, err := sina.QueryQuoteList(ctx, exCodes)
	if err != nil {
		return nil, fmt.Errorf("行情获取失败：%w", err)
	}
	byCode := make(map[string]*sina.SecurityQuote, len(quotes))
	for _, quote := range quotes {
		byCode[strings.ToUpper(quote.ExCode)] = quote
	}

	data := quoteData{Quotes: []quoteItem{}, Missing: missing}
	result := NewResult("get_quote", "新浪财经行情", &data)
	result.Notes = append(result.Notes, resolved.merged...)
	for _, sec := range secs {
		quote, ok := byCode[strings.ToUpper(sec.ExCode)]
		if !ok {
			data.Missing = append(data.Missing, missingItem{sec.ExCode, "没有行情数据，可能停牌或已退市"})
			continue
		}
		item := newQuoteItem(sec, quote)
		data.Quotes = append(data.Quotes, item)
		if item.PriceIsYClose {
			result.Notes = append(result.Notes, item.ExCode+" 当前没有成交价，price 是昨收，没有计算涨跌")
		}
		if item.NAV {
			result.Notes = append(result.Notes, item.ExCode+" 是基金单位净值，trade_date 是净值日期，不是盘中成交价")
		}
	}
	if len(data.Quotes) == 0 {
		return nil, fmt.Errorf("没有查到任何行情：%s", missingText(data.Missing))
	}
	if len(data.Missing) > 0 {
		result.Notes = append(result.Notes, "部分证券没有查到，见 data.missing")
	}
	result.AsOf = commonDate(data.Quotes)
	return result, nil
}

// quoteCodes 去掉空白和重复的代码，并检查数量。
func quoteCodes(raw []string) ([]string, error) {
	codes := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, code := range raw {
		code = strings.TrimSpace(code)
		if code == "" || seen[strings.ToUpper(code)] {
			continue
		}
		seen[strings.ToUpper(code)] = true
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return nil, Inputf("codes 不能为空，请给出至少一个证券代码或名称")
	}
	if len(codes) > maxQuoteCodes {
		return nil, Inputf("一次最多查询 %d 只证券，当前 %d 只，请分批查询", maxQuoteCodes, len(codes))
	}
	return codes, nil
}

// resolvedCodes 是搜索结果。
type resolvedCodes struct {
	secs         []*sina.BasicSecurity
	missing      []missingItem
	merged       []string // 搜到同一只证券的输入被合并的说明
	searchFailed bool     // 有输入因为搜索接口失败而没有结果，而不是找不到
}

// resolveCodes 并发搜索，结果保持输入顺序，搜到同一只证券的输入只保留第一个，并说明合并了哪些。
func resolveCodes(ctx context.Context, codes []string) resolvedCodes {
	out := resolvedCodes{secs: []*sina.BasicSecurity{}, missing: []missingItem{}}
	for _, result := range sina.ResolveAll(ctx, codes) {
		switch {
		case result.Err != nil:
			out.missing = append(out.missing, missingItem{result.Key, fmt.Sprintf("搜索失败：%v", result.Err)})
			out.searchFailed = true
		case result.NotFound():
			out.missing = append(out.missing, missingItem{result.Key, "未找到证券"})
		case result.DuplicateOf != "":
			out.merged = append(out.merged, result.DuplicateNote())
		default:
			out.secs = append(out.secs, result.Security)
		}
	}
	return out
}

func newQuoteItem(sec *sina.BasicSecurity, quote *sina.SecurityQuote) quoteItem {
	price, isYClose := quote.EffectivePrice()
	name := quote.Name
	if name == "" {
		name = sec.Name
	}
	item := quoteItem{
		ExCode: sec.ExCode, Name: name, TradeDate: quote.TradeDate, Time: quote.Time, Timezone: quote.Timezone,
		Price: utils.PositiveOrNil(price), PriceIsYClose: isYClose, YClose: utils.PositiveOrNil(quote.YClose), Open: utils.PositiveOrNil(quote.Open),
		High: utils.PositiveOrNil(quote.High), Low: utils.PositiveOrNil(quote.Low), NAV: quote.NAV,
	}
	item.Volume, item.Amount = quote.Trade()
	if change, pct, ok := quote.Change(); ok {
		change, pct = round(change, 4), round(pct, 2)
		item.Change, item.ChangePct = &change, &pct
	}
	return item
}

// commonDate 在所有行情的日期和时区都相同时返回该日期，否则返回空，日期以各条行情为准。
func commonDate(quotes []quoteItem) string {
	if len(quotes) == 0 {
		return ""
	}
	date, zone := quotes[0].TradeDate, quotes[0].Timezone
	for _, quote := range quotes[1:] {
		if quote.TradeDate != date || quote.Timezone != zone {
			return ""
		}
	}
	return date
}

func missingText(missing []missingItem) string {
	parts := make([]string, 0, len(missing))
	for _, item := range missing {
		parts = append(parts, item.Code+" "+item.Reason)
	}
	return strings.Join(parts, "；")
}

// round 把计算出的值四舍五入到 digits 位小数，去掉浮点误差，也让模型引用时不必再取舍。
func round(value float64, digits int) float64 {
	scale := math.Pow(10, float64(digits))
	return math.Round(value*scale) / scale
}
