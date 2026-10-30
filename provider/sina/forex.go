package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/forex"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const forexQuoteURL = "https://hq.sinajs.cn/"
const forexHistoryURL = "https://vip.stock.finance.sina.com.cn/forex/api/jsonp.php/var%20_data=/NewForexService.getDayKLine"

type ForexClient struct{ forex.Cache }

func NewForexClient(cacheDir string) *ForexClient {
	return &ForexClient{Cache: forex.Cache{CacheDir: cacheDir}}
}
func forexSymbol(pair string) string {
	return "fx_s" + strings.ToLower(strings.ReplaceAll(pair, "/", ""))
}

var forexQuoteLine = regexp.MustCompile(`^var hq_str_(fx_s[a-z]+)="([^"\r\n]*)";$`)
var forexDayWrapper = regexp.MustCompile(`(?s)^\s*(?:/\*.*?\*/\s*)?var _data=\((".*")\);\s*$`)

func parseForexQuotes(raw []byte, pairs []string) ([]forex.Observation, error) {
	if !utf8.Valid(raw) {
		var err error
		raw, err = simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
		if err != nil {
			return nil, fmt.Errorf("新浪汇率编码无效：%w", err)
		}
	}
	bySymbol := map[string]string{}
	for _, pair := range pairs {
		bySymbol[forexSymbol(pair)] = pair
	}
	seen := map[string]bool{}
	items := []forex.Observation{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		match := forexQuoteLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			return nil, fmt.Errorf("新浪汇率响应格式变化")
		}
		pair, exists := bySymbol[match[1]]
		if !exists || seen[pair] {
			return nil, fmt.Errorf("新浪汇率返回多余或重复货币对")
		}
		seen[pair] = true
		fields := strings.Split(match[2], ",")
		if len(fields) != 18 {
			return nil, fmt.Errorf("新浪未提供 %s 行情或字段数量变化", pair)
		}
		observed, err := time.ParseInLocation("2006-01-02 15:04:05", fields[17]+" "+fields[0], forex.Beijing)
		if err != nil {
			return nil, fmt.Errorf("新浪汇率观测时间无效")
		}
		item := forex.NewObservation(pair, fields[17], "", time.Time{})
		item.ObservedAt = observed.Format(time.RFC3339)
		for i, value := range fields {
			item.RawValues[strconv.Itoa(i)] = value
		}
		for _, dest := range []struct {
			index int
			value **float64
		}{{8, &item.Value}, {1, &item.Bid}, {2, &item.Ask}, {5, &item.Open}, {6, &item.High}, {7, &item.Low}} {
			*dest.value, err = forex.Number(fields[dest.index])
			if err != nil {
				return nil, err
			}
		}
		if item.Value == nil || item.Bid == nil || item.Ask == nil || *item.Bid > *item.Ask {
			return nil, fmt.Errorf("新浪汇率最新价或买卖报价无效")
		}
		item.Note = fields[13]
		items = append(items, item)
	}
	if len(items) != len(pairs) {
		return nil, fmt.Errorf("新浪汇率缺少请求的货币对")
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Pair < items[j].Pair })
	return items, nil
}

// ForexQuotes 返回最近一次报价及上游原始计算说明，不宣称报价一定实时。
func (c *ForexClient) ForexQuotes(ctx context.Context, req *forex.Request) (*forex.Result, error) {
	if req == nil {
		return nil, fmt.Errorf("汇率请求不能为空")
	}
	if err := forex.ValidatePairs(req.Pairs, forex.MarketPairs); err != nil {
		return nil, err
	}
	symbols := []string{}
	for _, pair := range req.Pairs {
		symbols = append(symbols, forexSymbol(pair))
	}
	address := forexQuoteURL + "?list=" + strings.Join(symbols, ",")
	var items []forex.Observation
	_, fetched, err := c.Snapshot(ctx, address, 15*time.Second, defaultHTTPHeaders(), func(raw []byte) error { var err error; items, err = parseForexQuotes(raw, req.Pairs); return err })
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Source, items[i].FetchedAt = address, fetched
	}
	return &forex.Result{Provider: "Sina", PriceType: "indicative-quote", Frequency: "snapshot", Timezone: "Asia/Shanghai", DateBasis: "provider-quote-time", DataVersion: "current-visible-quote", Observations: items, Notes: []string{"最近一次上游参考行情，可能延迟或为休市时的旧报价；观测时间与抓取时间分开保存。", "部分在岸及交叉汇率由新浪计算得出，逐条保留上游说明；报价并非银行承诺的实际兑换价格。"}}, nil
}

func parseForexHistory(raw []byte, pair string) ([]forex.Observation, error) {
	match := forexDayWrapper.FindSubmatch(raw)
	if match == nil {
		return nil, fmt.Errorf("新浪汇率日线包装格式无效")
	}
	var body string
	if err := json.Unmarshal(match[1], &body); err != nil || body == "" {
		return nil, fmt.Errorf("新浪汇率日线内容为空或无效")
	}
	items := []forex.Observation{}
	previous := ""
	for _, record := range strings.Split(body, "|") {
		fields := strings.Split(strings.TrimSuffix(record, ","), ",")
		if len(fields) != 5 {
			return nil, fmt.Errorf("新浪汇率日线字段数量无效")
		}
		date, err := time.Parse("2006-01-02", fields[0])
		if err != nil || fields[0] <= previous {
			return nil, fmt.Errorf("新浪汇率日线日期重复、无效或未按日期排序")
		}
		previous = fields[0]
		item := forex.NewObservation(pair, date.Format("2006-01-02"), "", time.Time{})
		// 新浪日线原始顺序为 日期、开盘、最低、最高、收盘；不能套用股票 OHLC 顺序。
		for _, dest := range []struct {
			index int
			name  string
			value **float64
		}{{1, "open", &item.Open}, {2, "low", &item.Low}, {3, "high", &item.High}, {4, "close", &item.Close}} {
			*dest.value, err = forex.Number(fields[dest.index])
			if err != nil || *dest.value == nil {
				return nil, fmt.Errorf("新浪汇率日线%s无效", dest.name)
			}
			item.RawValues[dest.name] = fields[dest.index]
		}
		if *item.Low > *item.High || *item.Open < *item.Low || *item.Open > *item.High || *item.Close < *item.Low || *item.Close > *item.High {
			item.Open, item.Low, item.High, item.Close = nil, nil, nil, nil
			item.Note = "来源日线高低价关系异常，数值留空，原始字段保留在 JSON 中"
		}
		item.Value = item.Close
		items = append(items, item)
	}
	return items, nil
}

func (c *ForexClient) ForexHistory(ctx context.Context, req *forex.Request) (*forex.Result, error) {
	if err := req.Validate(forex.MarketPairs, ""); err != nil {
		return nil, err
	}
	result := &forex.Result{Provider: "Sina", PriceType: "provider-daily-bar", Frequency: "daily", Timezone: "unspecified", DateBasis: "provider-trading-date", Begin: req.Begin, End: req.End, DataVersion: "current-visible-history", Observations: []forex.Observation{}, SnapshotCoverage: map[string][2]string{}, Notes: []string{"汇率取来源日线收盘值；仅保留来源交易日期，上游未给出统一收盘时刻与时区，不推断为中国16:30或纽约收盘。", "最新日线可能尚未收盘；快照起止仅表示本次上游可见覆盖，缺失日期不补值，历史不可得不等于汇率为零。"}}
	for _, pair := range req.Pairs {
		address := forexHistoryURL + "?symbol=" + forexSymbol(pair)
		var items []forex.Observation
		_, fetched, err := c.Snapshot(ctx, address, 6*time.Hour, defaultHTTPHeaders(), func(raw []byte) error { var err error; items, err = parseForexHistory(raw, pair); return err })
		if err != nil {
			return nil, err
		}
		result.SnapshotCoverage[pair] = [2]string{items[0].Date, items[len(items)-1].Date}
		for _, item := range items {
			date := strings.ReplaceAll(item.Date, "-", "")
			if date >= req.Begin && date <= req.End {
				item.Source, item.FetchedAt = address, fetched
				result.Observations = append(result.Observations, item)
			}
		}
	}
	sort.Slice(result.Observations, func(i, j int) bool {
		a, b := result.Observations[i], result.Observations[j]
		if a.Date == b.Date {
			return a.Pair < b.Pair
		}
		return a.Date < b.Date
	})
	return result, nil
}
