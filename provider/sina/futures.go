package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alwqx/sec/utils"
)

// Commodity 是一个大宗商品期货品种。国内品种用新浪的主力连续合约（代码以 0 结尾），换月时价格会跳空，
// 适合看走势，不适合回测；国际品种用新浪的外盘期货。
type Commodity struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
	Unit     string `json:"unit"`
	Domestic bool   `json:"domestic"`
	symbol   string
}

// Commodities 是支持的品种，按国内、国际排列。
var Commodities = []Commodity{
	{"sc", "原油", "上海国际能源交易中心", "元/桶", true, "SC0"},
	{"cu", "铜", "上海期货交易所", "元/吨", true, "CU0"},
	{"i", "铁矿石", "大连商品交易所", "元/吨", true, "I0"},
	{"rb", "螺纹钢", "上海期货交易所", "元/吨", true, "RB0"},
	{"m", "豆粕", "大连商品交易所", "元/吨", true, "M0"},
	{"cl", "WTI 原油", "纽约商品交易所", "美元/桶", false, "CL"},
	{"brent", "布伦特原油", "洲际交易所", "美元/桶", false, "OIL"},
	{"hg", "COMEX 铜", "纽约商品交易所", "美分/磅", false, "HG"},
}

// FindCommodity 按代码查找品种，不区分大小写。
func FindCommodity(id string) (Commodity, error) {
	key := strings.ToLower(strings.TrimSpace(id))
	for _, item := range Commodities {
		if key == item.ID {
			return item, nil
		}
	}
	return Commodity{}, fmt.Errorf("不支持的品种 %s，可选：%s", id, CommodityList())
}

// CommodityList 列出全部品种，用于报错和命令行帮助。
func CommodityList() string {
	names := make([]string, 0, len(Commodities))
	for _, item := range Commodities {
		names = append(names, item.ID+"（"+item.Name+"）")
	}
	return strings.Join(names, "、")
}

func (c Commodity) quoteKey() string {
	if c.Domestic {
		return "nf_" + c.symbol
	}
	return "hf_" + c.symbol
}

// CommodityQuote 是一个品种的最新行情。涨跌相对昨日结算价，没有昨结算时为 nil。
type CommodityQuote struct {
	Commodity
	Date       string   `json:"date"`
	Time       string   `json:"time"`
	Price      float64  `json:"price"`
	Open       float64  `json:"open"`
	High       float64  `json:"high"`
	Low        float64  `json:"low"`
	PrevSettle *float64 `json:"prev_settle"`
	Change     *float64 `json:"change"`
	ChangeRate *float64 `json:"change_rate"` // 百分比
}

var futuresLine = regexp.MustCompile(`^var hq_str_((?:nf|hf)_[A-Z0-9]+)="([^"\r\n]*)";?$`)

func numberAt(fields []string, i int) (float64, error) {
	if i >= len(fields) {
		return 0, fmt.Errorf("缺少第 %d 个字段", i)
	}
	return strconv.ParseFloat(strings.TrimSpace(fields[i]), 64)
}

// parseCommodityQuote 解析一行行情。
// 国内：名称,时间 HHMMSS,开盘,最高,最低,收盘,买价,卖价,最新价,结算价,昨结算,买量,卖量,持仓量,成交量,交易所,品种,日期,…
// 国际：最新价,,买价,卖价,最高,最低,时间 HH:MM:SS,昨结算,开盘,持仓量,买量,卖量,日期,名称,…
func parseCommodityQuote(item Commodity, value string) (*CommodityQuote, error) {
	fields := strings.Split(value, ",")
	quote := &CommodityQuote{Commodity: item}
	var idx struct{ price, open, high, low, prev, date, time int }
	if item.Domestic {
		idx = struct{ price, open, high, low, prev, date, time int }{8, 2, 3, 4, 10, 17, 1}
	} else {
		idx = struct{ price, open, high, low, prev, date, time int }{0, 8, 4, 5, 7, 12, 6}
	}
	if len(fields) <= max(idx.date, idx.prev) {
		return nil, fmt.Errorf("%s 行情字段不足", item.Name)
	}
	var err error
	for _, target := range []struct {
		ptr *float64
		i   int
	}{{&quote.Price, idx.price}, {&quote.Open, idx.open}, {&quote.High, idx.high}, {&quote.Low, idx.low}} {
		if *target.ptr, err = numberAt(fields, target.i); err != nil {
			return nil, fmt.Errorf("%s 行情格式无效: %w", item.Name, err)
		}
	}
	if quote.Price <= 0 {
		return nil, fmt.Errorf("%s 没有最新价", item.Name)
	}
	quote.Date = fields[idx.date]
	quote.Time = fields[idx.time]
	if item.Domestic && len(quote.Time) == 6 {
		quote.Time = quote.Time[:2] + ":" + quote.Time[2:4] + ":" + quote.Time[4:]
	}
	if prev, err := numberAt(fields, idx.prev); err == nil && prev > 0 {
		change := quote.Price - prev
		rate := change / prev * 100
		quote.PrevSettle, quote.Change, quote.ChangeRate = &prev, &change, &rate
	}
	return quote, nil
}

// QueryCommodities 一次请求查询多个品种的最新行情，按传入顺序返回。个别品种缺少行情或格式无效时，
// 其余品种照常返回，failed 说明失败的品种；请求本身失败时返回错误。
func QueryCommodities(ctx context.Context, items []Commodity) (quotes []*CommodityQuote, failed []string, err error) {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.quoteKey())
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, "https://hq.sinajs.cn/list="+strings.Join(keys, ","), defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, nil, err
	}
	if err := adjustRespBodyByEncode(resp); err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	quotes, failed = parseCommodityQuotes(string(body), items)
	return quotes, failed, nil
}

func parseCommodityQuotes(body string, items []Commodity) ([]*CommodityQuote, []string) {
	values := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		if match := futuresLine.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			values[match[1]] = match[2]
		}
	}
	quotes := make([]*CommodityQuote, 0, len(items))
	failed := []string{}
	for _, item := range items {
		value := values[item.quoteKey()]
		if value == "" {
			failed = append(failed, item.Name+"：新浪没有返回行情")
			continue
		}
		quote, err := parseCommodityQuote(item, value)
		if err != nil {
			failed = append(failed, err.Error())
			continue
		}
		quotes = append(quotes, quote)
	}
	return quotes, failed
}

// CommodityBar 是一天的期货行情。国内品种的涨跌相对前一天的结算价，前一天没有结算价（早年数据常见）时相对收盘价；
// 国际品种没有结算价，相对前一天的收盘价。Basis 说明用的是哪一个，第一天没有前一天时涨跌为 nil。
type CommodityBar struct {
	Date       string   `json:"date"`
	Open       float64  `json:"open"`
	High       float64  `json:"high"`
	Low        float64  `json:"low"`
	Close      float64  `json:"close"`
	Volume     float64  `json:"volume"`
	Settle     *float64 `json:"settle"`
	PrevClose  *float64 `json:"yclose"` // 涨跌的基准，见 Basis
	Basis      string   `json:"basis"`  // settle 或 close，第一天为空
	Change     *float64 `json:"change"`
	ChangeRate *float64 `json:"change_rate"` // 百分比
}

var jsonpBody = regexp.MustCompile(`(?s)var t=\((\[.*\])\);?\s*$`)

// historyRow 同时兼容国内（d、o、h、l、c、v、s）和国际（date、open…、volume）两种字段名，值都是字符串。
type historyRow struct {
	D      string `json:"d"`
	O      string `json:"o"`
	H      string `json:"h"`
	L      string `json:"l"`
	C      string `json:"c"`
	V      string `json:"v"`
	S      string `json:"s"`
	Date   string `json:"date"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}

func parseCommodityHistory(item Commodity, body string) ([]CommodityBar, error) {
	match := jsonpBody.FindStringSubmatch(body)
	if match == nil {
		return nil, fmt.Errorf("%s 历史行情格式无效", item.Name)
	}
	var rows []historyRow
	if err := json.Unmarshal([]byte(match[1]), &rows); err != nil {
		return nil, fmt.Errorf("%s 历史行情格式无效: %w", item.Name, err)
	}
	bars := make([]CommodityBar, 0, len(rows))
	for _, row := range rows {
		if item.Domestic {
			row.Date, row.Open, row.High, row.Low, row.Close, row.Volume = row.D, row.O, row.H, row.L, row.C, row.V
		}
		bar := CommodityBar{Date: row.Date}
		values := []*float64{&bar.Open, &bar.High, &bar.Low, &bar.Close}
		valid := true
		for i, text := range []string{row.Open, row.High, row.Low, row.Close} {
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || value <= 0 {
				valid = false
				break
			}
			*values[i] = value
		}
		if !valid || len(bar.Date) != 10 {
			continue
		}
		bar.Volume, _ = strconv.ParseFloat(row.Volume, 64)
		if settle, err := strconv.ParseFloat(row.S, 64); err == nil && settle > 0 && item.Domestic {
			bar.Settle = &settle
		}
		bars = append(bars, bar)
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("%s 没有历史行情", item.Name)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].Date < bars[j].Date })
	for i := 1; i < len(bars); i++ {
		base, basis := bars[i-1].Close, "close"
		if bars[i-1].Settle != nil {
			base, basis = *bars[i-1].Settle, "settle"
		}
		change := bars[i].Close - base
		rate := change / base * 100
		bars[i].PrevClose, bars[i].Change, bars[i].ChangeRate, bars[i].Basis = &base, &change, &rate, basis
	}
	return bars, nil
}

// QueryCommodityHistory 查询一个品种的日行情，按日期区间筛选；start、end 为 YYYY-MM-DD。
// 涨跌在筛选前计算，区间第一天也有涨跌。
func QueryCommodityHistory(ctx context.Context, item Commodity, start, end string) ([]CommodityBar, error) {
	all, err := QueryCommodityHistoryAll(ctx, item)
	if err != nil {
		return nil, err
	}
	return FilterCommodityBars(all, start, end), nil
}

// FilterCommodityBars 返回 start 到 end（YYYY-MM-DD，含两端）之间的日行情。
func FilterCommodityBars(all []CommodityBar, start, end string) []CommodityBar {
	bars := []CommodityBar{}
	for _, bar := range all {
		if bar.Date >= start && bar.Date <= end {
			bars = append(bars, bar)
		}
	}
	return bars
}

// QueryCommodityHistoryAll 查询一个品种的全部日行情，日期升序。
func QueryCommodityHistoryAll(ctx context.Context, item Commodity) ([]CommodityBar, error) {
	link := "https://stock2.finance.sina.com.cn/futures/api/jsonp.php/var%20t=/GlobalFuturesService.getGlobalFuturesDailyKLine?symbol=" + item.symbol
	if item.Domestic {
		link = "https://stock2.finance.sina.com.cn/futures/api/jsonp.php/var%20t=/InnerFuturesNewService.getDailyKLine?symbol=" + item.symbol
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, link, defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseCommodityHistory(item, string(body))
}
