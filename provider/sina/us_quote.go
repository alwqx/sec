package sina

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/alwqx/sec/utils"
)

// USQuote 是新浪美股行情里的估值字段。新浪的每股收益是最近四个季度合计，市盈率 = 价格 ÷ 每股收益，
// 市值 = 价格 × 总股本；亏损公司没有市盈率。
type USQuote struct {
	Name       string   `json:"name"` // 中文名
	Price      float64  `json:"price"`
	ChangeRate float64  `json:"change_rate"` // %
	Time       string   `json:"time"`        // 北京时间
	High52     *float64 `json:"high_52w"`
	Low52      *float64 `json:"low_52w"`
	MarketCap  *float64 `json:"market_cap"` // 美元
	EPS        *float64 `json:"eps"`        // 美元，亏损时为负数
	PE         *float64 `json:"pe"`
	Shares     *float64 `json:"shares"`
}

var usQuoteLine = regexp.MustCompile(`hq_str_gb_[\w$]+="([^"]*)"`)

// USQuoteKey 返回新浪美股行情的代码：小写，类别股用 $ 分隔，比如 BRK.B、BRK-B 都是 gb_brk$b。
func USQuoteKey(ticker string) string {
	ticker = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ticker), "$"))
	return "gb_" + strings.NewReplacer(".", "$", "-", "$").Replace(ticker)
}

// QueryUSQuote 查询美股行情和估值字段。ticker 比如 AAPL、$AAPL、BRK.B。
func QueryUSQuote(ctx context.Context, ticker string) (*USQuote, error) {
	key := USQuoteKey(ticker)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, "https://hq.sinajs.cn/list="+key, defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	if err := adjustRespBodyByEncode(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseUSQuote(string(body), ticker)
}

// parseUSQuote 解析 gb_ 行情：名称,价格,涨跌幅,时间,涨跌额,开盘,最高,最低,52周最高,52周最低,成交量,均量,市值,每股收益,市盈率,…,总股本(第 20 个)
func parseUSQuote(body, ticker string) (*USQuote, error) {
	match := usQuoteLine.FindStringSubmatch(body)
	if match == nil || match[1] == "" {
		return nil, fmt.Errorf("新浪没有 %s 的美股行情", ticker)
	}
	fields := strings.Split(match[1], ",")
	if len(fields) < 20 {
		return nil, fmt.Errorf("%s 美股行情字段不足", ticker)
	}
	price, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return nil, fmt.Errorf("%s 美股行情价格无效: %w", ticker, err)
	}
	positive := func(i int) *float64 {
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil || value <= 0 {
			return nil
		}
		return &value
	}
	// 每股收益可以是负数（亏损），为 0 时当作没有数据；亏损公司没有市盈率。
	var eps *float64
	if value, err := strconv.ParseFloat(fields[13], 64); err == nil && value != 0 {
		eps = &value
	}
	quote := &USQuote{Name: fields[0], Price: price, Time: fields[3], High52: positive(8), Low52: positive(9), MarketCap: positive(12), EPS: eps, PE: positive(14), Shares: positive(19)}
	quote.ChangeRate, _ = strconv.ParseFloat(fields[2], 64)
	return quote, nil
}
