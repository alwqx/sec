package sina

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/alwqx/sec/types"
)

// fundCode 匹配新浪搜索返回的基金代码，如 OF005827、OF510300。ETF、LOF 也是 OF 开头，不带交易所。
var fundCode = regexp.MustCompile(`^(?i)of(\d{6})$`)

// IsFundCode 报告 exCode 是否新浪的基金代码。
func IsFundCode(exCode string) bool {
	return fundCode.MatchString(exCode)
}

// listedFundMarket 返回可能在交易所上市的基金的交易所：上交所基金代码以 5 开头，深交所以 15、16、18 开头。
// 其他是场外基金，返回空字符串。按代码判断不一定准（如 519 开头的多是场外基金），
// 所以同时请求净值，交易所没有价格时用净值。
func listedFundMarket(code string) string {
	switch {
	case strings.HasPrefix(code, "5"):
		return "sh"
	case strings.HasPrefix(code, "15"), strings.HasPrefix(code, "16"), strings.HasPrefix(code, "18"):
		return "sz"
	}
	return ""
}

// fundQuoteKeys 返回基金在新浪行情接口里的 key：可能上市的基金是交易所代码和净值 f_ 两个，其他只有净值。
func fundQuoteKeys(exCode string) []string {
	code := strings.ToUpper(exCode)[2:]
	if market := listedFundMarket(code); market != "" {
		return []string{market + code, "f_" + code}
	}
	return []string{"f_" + code}
}

// QuoteKeyCount 返回一只证券在新浪行情请求里占几个 key，分批请求时用来控制每批的 key 数。
func QuoteKeyCount(exCode string) int {
	if IsFundCode(exCode) {
		return len(fundQuoteKeys(exCode))
	}
	return 1
}

// quoteLineKey 拆出 var hq_str_<key>="<value>"; 的 key 和 value，key 转成小写。
func quoteLineKey(line string) (key, value string, ok bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "var hq_str_")
	if !ok {
		return "", "", false
	}
	key, value, ok = strings.Cut(rest, "=")
	return strings.ToLower(key), strings.Trim(strings.TrimSuffix(value, ";"), `"`), ok
}

// fundQuote 从新浪返回的行里挑出一只基金的行情：交易所有价格（现价或昨收）时用交易所行情，
// 否则用最新单位净值。没有行情、货币基金或解析失败时返回 nil，只跳过这一只，不影响同一批的其他证券。
func fundQuote(exCode string, lines map[string]string) *SecurityQuote {
	code := strings.ToUpper(exCode)[2:]
	var quote *SecurityQuote
	if market := listedFundMarket(code); market != "" && lines[market+code] != "" {
		listed, err := parseSecQuoteOfAstock(lines[market+code])
		if err != nil {
			slog.Warn("基金交易所行情解析失败，改用净值", "code", exCode, "error", err)
		} else if listed.Current > 0 || listed.YClose > 0 {
			quote = listed
		}
	}
	if quote == nil {
		line := lines["f_"+code]
		if line == "" {
			return nil
		}
		nav, err := parseFundNAV(line)
		if err != nil {
			slog.Warn("基金净值解析失败，跳过", "code", exCode, "error", err)
			return nil
		}
		if nav == nil {
			slog.Debug("货币基金没有单位净值，跳过", "code", exCode)
			return nil
		}
		quote = nav
	}
	quote.ExCode = strings.ToUpper(exCode)
	return quote
}

// parseFundNAV 解析新浪基金净值，格式为 名称,单位净值,累计净值,上一日单位净值,净值日期,份额（亿份）。
// 例如 易方达蓝筹精选混合,1.4164,1.4164,1.4009,2026-11-05,129.913。
// 货币基金的格式是 名称,万份收益,七日年化收益率,,日期,份额，第 4 列为空，不是净值，返回 nil。
func parseFundNAV(line string) (*SecurityQuote, error) {
	items := strings.Split(line, ",")
	if len(items) < 5 {
		return nil, fmt.Errorf("invalid fund NAV line: %q", line)
	}
	if strings.TrimSpace(items[3]) == "" {
		return nil, nil
	}
	nav, err := strconv.ParseFloat(strings.TrimSpace(items[1]), 64)
	if err != nil {
		return nil, err
	}
	prev, err := strconv.ParseFloat(strings.TrimSpace(items[3]), 64)
	if err != nil {
		return nil, err
	}
	return &SecurityQuote{
		Name: strings.TrimSpace(items[0]), Current: nav, YClose: prev,
		TradeDate: strings.TrimSpace(items[4]), Timezone: types.TimezoneShanghai, NAV: true,
	}, nil
}

// splitFundCodes 把基金代码和其他代码分开。
func splitFundCodes(exCodes []string) (funds, others []string) {
	for _, exCode := range exCodes {
		if IsFundCode(exCode) {
			funds = append(funds, exCode)
		} else {
			others = append(others, exCode)
		}
	}
	return funds, others
}

// inCodeOrder 按请求顺序排列行情：基金行情单独挑出后放在最后，要换回请求的顺序。
func inCodeOrder(quotes []*SecurityQuote, exCodes []string) []*SecurityQuote {
	position := make(map[string]int, len(exCodes))
	for i, exCode := range exCodes {
		position[strings.ToUpper(exCode)] = i
	}
	sort.SliceStable(quotes, func(i, j int) bool {
		return position[strings.ToUpper(quotes[i].ExCode)] < position[strings.ToUpper(quotes[j].ExCode)]
	})
	return quotes
}
