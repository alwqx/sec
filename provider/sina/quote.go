package sina

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	// 内置时区数据，Windows 等没有系统时区库的机器也能换算美东时间。
	_ "time/tzdata"

	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	"github.com/gorilla/websocket"
)

// QuoteWs 通过 websocket 请求多个证券行情信息
// exCodes = {"$AMD", "SH600036", "HK09992"}
func QuoteWs(ctx context.Context, exCodes []string) ([]*SecurityQuote, error) {
	formatKeys := formatQuoteKeys(exCodes)
	url := fmt.Sprintf("wss://hq.sinajs.cn/wskt?list=%s", strings.Join(formatKeys, ","))
	headers := make(http.Header)
	headers.Add("Origin", SinaReferer)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, headers)
	if err != nil {
		slog.ErrorContext(ctx, err.Error())
		return nil, err
	}
	defer conn.Close()

	_, msg, err := conn.ReadMessage()
	if err != nil {
		slog.ErrorContext(ctx, err.Error())
		return nil, err
	}

	return parseQuoteWsBody(string(msg))
}

// parseQuoteWsBody 解析 QuoteWs 请求的多个 证券信息
// sh600036=招商银行,36.350,35.630,37.610,38.000,35.920,37.610,37.620,256101260,9443438268.000,690801,37.610,286600,37.600,17000,37.590,55400,37.580,12200,37.570,161925,37.620,90600,37.630,50400,37.640,104100,37.650,126000,37.660,2024-09-30,15:00:00,00,
// sh688047=龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,
func parseQuoteWsBody(msg string) ([]*SecurityQuote, error) {
	slog.Debug("parseQuoteWsBody", "msg", msg)
	if msg == "" {
		return nil, errors.New("msg is empty")
	}

	lines := strings.Split(msg, "\n")
	res := make([]*SecurityQuote, 0, len(lines))
	for _, line := range lines {
		if line == "" || strings.Contains(line, "sys_nxkey") {
			continue
		}
		items := strings.Split(line, "=")
		if len(items) != 2 {
			slog.Error("parseQuoteWsBody invalid ws quote", "line", line)
			return nil, fmt.Errorf("items num shoud be 2 but get %d", len(items))
		}
		slog.Debug("parseQuoteWsBody", "items", items)
		quote, err := parseSecQuote(strings.ToUpper(items[0]), items[1])
		if err != nil {
			slog.Error("parseSecQuote", "error", err)
			return nil, err
		}
		res = append(res, quote)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})

	return res, nil
}

// formatQuoteKeys
// A股格式 SH600036
// 港股格式 rt_hk00700
// 港股指数 rt_hkHSI
// 美股格式 gb_baba
func formatQuoteKeys(keys []string) []string {
	res := make([]string, 0, len(keys))
	for _, key := range keys {
		var newKey string
		if strings.HasPrefix(key, "hk") {
			reg := regexp.MustCompile("^hk")
			tmp := reg.ReplaceAllString(key, "")
			newKey = "hk" + strings.ToUpper(tmp)
		} else if strings.HasPrefix(key, "$") {
			newKey = strings.ReplaceAll(key, "$", "gb_")
			newKey = strings.ToLower(newKey)
		} else {
			newKey = strings.ToLower(key)
		}
		res = append(res, newKey)
	}

	return res
}

// QuerySecQuote 查询证券行情
// exCode SH600036 HK00700 $AMD
func QuerySecQuote(ctx context.Context, exCode string) (*SecurityQuote, error) {
	if IsFundCode(exCode) {
		quotes, err := QueryQuoteList(ctx, []string{exCode})
		if err != nil {
			return nil, err
		}
		if len(quotes) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, exCode)
		}
		return quotes[0], nil
	}
	// 复用 formatQuoteKeys 做行情 key 归一化：
	// 美股 $AMD -> gb_amd，港股 HK00700 -> hk00700，A 股 SH600036 -> sh600036。
	// 直接 strings.ToLower 会把 $ 前缀漏掉，导致新浪返回空行情。
	key := formatQuoteKeys([]string{exCode})[0]
	reqUrl := fmt.Sprintf("https://hq.sinajs.cn/list=%s", key)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqUrl, defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	err = adjustRespBodyByEncode(resp)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// var hq_str_sh688047=\"龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,\";\n
	regstr := regexp.MustCompile(`\"(.*)\"`)
	lines := regstr.FindAll(body, -1)
	if len(lines) != 1 {
		slog.ErrorContext(ctx, "get invalid body", "url", reqUrl, "body", body)
		return nil, fmt.Errorf("body result num not 1, body %s", body)
	}
	quote, err := parseSecQuote(exCode, string(lines[0]))

	return quote, err
}

// parseSecQuote 从返回结果解析到结构化数据
// A 股 "龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,"
// 港股 "TENCENT,腾讯控股,508.500,510.000,514.500,507.000,512.000,2.000,0.392,512.00000,512.50000,7662280393,14986877,0.000,0.000,542.266,345.980,2025/05/27,16:08";
func parseSecQuote(exCode, quoteLine string) (quote *SecurityQuote, err error) {
	if types.IsACode(exCode) {
		quote, err = parseSecQuoteOfAstock(quoteLine)
	} else if types.IsHCode(exCode) {
		quote, err = parseSecQuoteOfHstock(quoteLine)
	} else if types.IsMCode(exCode) {
		quote, err = parseSecQuoteOfMstock(quoteLine)
	} else {
		err = fmt.Errorf("unsupported code %s", exCode)
	}

	if err != nil {
		slog.Error("parseSecQuote unsupported code", "code", exCode, "line", quoteLine)
	} else {
		quote.ExCode = exCode
	}

	return
}

// parseSecQuoteOfAstock 从 A 股返回结果解析到结构化数据
// A 股 "龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,"
func parseSecQuoteOfAstock(quoteLine string) (*SecurityQuote, error) {
	// 将首尾的双引号去掉
	newQuote := strings.TrimPrefix(quoteLine, "\"")
	newQuote = strings.TrimSuffix(newQuote, "\"")
	items := strings.Split(newQuote, ",")
	if len(items) < 32 {
		return nil, fmt.Errorf("invalid A stock quote line: %q", quoteLine)
	}
	res := new(SecurityQuote)
	res.Name = strings.TrimSpace(items[0])
	slog.Debug("parseSecQuoteOfAstock", "quote line", quoteLine, "items", items)

	var err error
	res.Current, err = strconv.ParseFloat(strings.TrimSpace(items[3]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Open, err = strconv.ParseFloat(strings.TrimSpace(items[1]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.YClose, err = strconv.ParseFloat(strings.TrimSpace(items[2]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.High, err = strconv.ParseFloat(strings.TrimSpace(items[4]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Low, err = strconv.ParseFloat(strings.TrimSpace(items[5]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Volume, err = strconv.ParseFloat(strings.TrimSpace(items[9]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.TurnOver, err = strconv.ParseInt(strings.TrimSpace(items[8]), 10, 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.TradeDate = strings.TrimSpace(items[30])
	res.Time = strings.TrimSpace(items[31])
	res.Timezone = types.TimezoneShanghai

	return res, nil
}

// parseSecQuoteOfHstock 从 H 股返回结果解析到结构化数据
// "TENCENT,腾讯控股,508.500,510.000,514.500,507.000,512.000,2.000,0.392,512.00000,512.50000,7662280393,14986877,0.000,0.000,542.266,345.980,2025/05/27,16:08";
// d                2open  3yclose  4high   5low   6current 7     8     9         10        11成交额   12成交量股 13    14    15      16      17         18
func parseSecQuoteOfHstock(quoteLine string) (*SecurityQuote, error) {
	// 将首尾的双引号去掉
	newQuote := strings.TrimPrefix(quoteLine, "\"")
	newQuote = strings.TrimSuffix(newQuote, "\"")
	items := strings.Split(newQuote, ",")
	if len(items) < 19 {
		return nil, fmt.Errorf("invalid HK stock quote line: %q", quoteLine)
	}
	res := new(SecurityQuote)
	res.Name = strings.TrimSpace(items[1])
	slog.Debug("parseSecQuote quote string", "quoteLine", quoteLine, "items", newQuote)

	var err error
	res.Current, err = strconv.ParseFloat(strings.TrimSpace(items[6]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Open, err = strconv.ParseFloat(strings.TrimSpace(items[2]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.YClose, err = strconv.ParseFloat(strings.TrimSpace(items[3]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.High, err = strconv.ParseFloat(strings.TrimSpace(items[4]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Low, err = strconv.ParseFloat(strings.TrimSpace(items[5]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Volume, err = strconv.ParseFloat(strings.TrimSpace(items[11]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.TurnOver, err = strconv.ParseInt(strings.TrimSpace(items[12]), 10, 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	// 港股日期是 2025/05/27，统一成和其他市场一样的格式。
	res.TradeDate = strings.ReplaceAll(strings.TrimSpace(items[17]), "/", "-")
	res.Time = strings.TrimSpace(items[18])
	res.Timezone = types.TimezoneHongKong

	return res, nil
}

// parseSecQuoteOfMstock 从美股返回结果解析到结构化数据
// "AMD,144.5500,4.44,2025-07-10 22:55:35,6.1400,143.0000,145.8200,141.8500,187.1100,76.4800,32285637,47105949,234373976387,1.37,105.510000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:55AM EDT,138.4100,0,1,2025,4641781226.0000,0.0000,0.0000,0.0000,0.0000,138.4100"
// 0name 1cur    2rate 3 date            4 change 5open   6 high   7 low    8        9       10 成交量 11 成交额 12 总市值     13   14         15    16   17  18   19 总股本   20  21    22    23 24  25               26yclose 27 28 29   30 成交额      31    32     33     34     35 yclose
// OTC 证券（如 TCEHY）只有前 31 列，没有第 35 列，昨收用第 26 列。
func parseSecQuoteOfMstock(quoteLine string) (*SecurityQuote, error) {
	// 将首尾的双引号去掉
	newQuote := strings.TrimPrefix(quoteLine, "\"")
	newQuote = strings.TrimSuffix(newQuote, "\"")
	items := strings.Split(newQuote, ",")
	if len(items) < 31 {
		return nil, fmt.Errorf("invalid US stock quote line: %q", quoteLine)
	}
	res := new(SecurityQuote)
	res.Name = strings.TrimSpace(items[0])
	slog.Debug("parseSecQuoteOfMstock quote string", "quoteLine", quoteLine, "items", newQuote)

	var err error
	res.Current, err = strconv.ParseFloat(strings.TrimSpace(items[1]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Open, err = strconv.ParseFloat(strings.TrimSpace(items[5]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	yclose := items[26]
	if len(items) > 35 {
		yclose = items[35]
	}
	res.YClose, err = strconv.ParseFloat(strings.TrimSpace(yclose), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.High, err = strconv.ParseFloat(strings.TrimSpace(items[6]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Low, err = strconv.ParseFloat(strings.TrimSpace(items[7]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.Volume, err = strconv.ParseFloat(strings.TrimSpace(items[30]), 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}
	res.TurnOver, err = strconv.ParseInt(strings.TrimSpace(items[10]), 10, 64)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}

	// 第 3 列是北京时间，换成美东时间，交易日期才和 K 线的日期一致。
	at, err := time.ParseInLocation(utils.StandardTimeLayout, strings.TrimSpace(items[3]), beijing)
	if err != nil {
		return nil, fmt.Errorf("invalid US stock quote date: %q", quoteLine)
	}
	at = at.In(newYork)
	res.TradeDate = at.Format(utils.LayoutYYMMDD)
	res.Time = at.Format("15:04:05")
	res.Timezone = types.TimezoneNewYork

	return res, nil
}

var (
	beijing    = time.FixedZone("CST", 8*60*60)
	newYork, _ = time.LoadLocation(types.TimezoneNewYork)
)

// QueryQuoteList 查询多个证券行情
// exCode SH600036 HK00700
// exCodes = {"$AMD", "SH600036", "HK09992"}
func QueryQuoteList(ctx context.Context, exCodes []string) ([]*SecurityQuote, error) {
	// 基金和其他证券放在同一次请求里：基金的交易所行情和净值行单独挑出来，其余行照常解析。
	funds, others := splitFundCodes(exCodes)
	keys := formatQuoteKeys(others)
	fundKeys := map[string]bool{}
	for _, exCode := range funds {
		for _, key := range fundQuoteKeys(exCode) {
			keys = append(keys, key)
			fundKeys[key] = true
		}
	}
	reqUrl := fmt.Sprintf("https://hq.sinajs.cn/list=%s", strings.Join(keys, ","))
	slog.DebugContext(ctx, "QueryQuoteList", "exCodes", strings.Join(exCodes, ", "), "req_url", reqUrl)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqUrl, defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	err = adjustRespBodyByEncode(resp)
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(funds) == 0 {
		return parseQuoteListBody(string(body))
	}

	fundLines := map[string]string{}
	var rest strings.Builder
	for _, line := range strings.Split(string(body), "\n") {
		if key, value, ok := quoteLineKey(line); ok && fundKeys[key] {
			fundLines[key] = value
			continue
		}
		rest.WriteString(line + "\n")
	}
	res, err := parseQuoteListBody(rest.String())
	if err != nil && len(fundLines) == 0 {
		return nil, err
	}
	for _, exCode := range funds {
		if quote := fundQuote(exCode, fundLines); quote != nil {
			res = append(res, quote)
		}
	}
	return inCodeOrder(res, exCodes), nil
}

// parseQuoteListBody 解析 QueryQuoteList 返回结果
// https://hq.sinajs.cn/list=sh688047,rt_hk09992,gb_amd body 为:
// var hq_str_sh688047=\"龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,\";\n
// var hq_str_rt_hk09992=\"POP MART,泡泡玛特,266.800,266.800,272.000,263.200,265.600,-1.200,-0.450,265.400,265.600,1385751264.400,5192605,103.455,0.000,283.400,36.101,2025/07/10,16:08:15,100|0,N|Y|Y,265.400|252.200|278.600,0|||0.000|0.000|0.000, |0,Y\";\n
// var hq_str_gb_amd=\"AMD,144.2896,4.25,2025-07-10 22:18:09,5.8796,143.0000,145.2101,141.8500,187.1100,76.4800,21723467,47105949,233951762734,1.37,105.330000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:18AM EDT,138.4100,0,1,2025,3112190954.0000,0.0000,0.0000,0.0000,0.0000,138.4100\";\n
func parseQuoteListBody(body string) ([]*SecurityQuote, error) {
	lines := strings.Split(body, "\n")
	res := make([]*SecurityQuote, 0, len(lines))
	invalid := ""
	for _, line := range lines {
		if line == "" {
			continue
		}
		// 不是行情行说明响应本身有问题，例如错误页。
		if !strings.HasPrefix(line, "var hq_str_") {
			invalid = line
			continue
		}
		exCode, formatLine := formatQuoteListLine(line)
		quote, err := parseSecQuote(exCode, formatLine)
		if err != nil {
			// 基金、退市证券等解析不了的行只跳过这一只，不影响其他证券。
			slog.Warn("parseQuoteListBody skip line", "code", exCode, "error", err)
			continue
		}
		res = append(res, quote)
	}
	if len(res) == 0 && invalid != "" {
		return nil, fmt.Errorf("invalid quote list body: %.80s", invalid)
	}

	return res, nil
}

// formatQuoteListLine 格式化单行结果，line 格式为
// var hq_str_sh688047=\"龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,\";
// 期望结果为：
// SH688047 龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,
func formatQuoteListLine(line string) (exCode string, res string) {
	newLine := strings.Replace(line, "var hq_str_", "", -1)
	newLine = strings.Replace(newLine, "rt_", "", -1)
	newLine = strings.Replace(newLine, ";", "", -1)

	toks := strings.Split(newLine, "=")
	if len(toks) != 2 {
		slog.Debug("invalid line", "line", line)
		return
	}
	formattedCode := toks[0]
	formattedCode = strings.Replace(formattedCode, "gb_", "$", -1)
	exCode = strings.ToUpper(formattedCode)

	// 去掉首尾双引号
	res = strings.TrimPrefix(toks[1], "\"")
	res = strings.TrimSuffix(res, "\"")

	return exCode, res
}
