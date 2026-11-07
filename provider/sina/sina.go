package sina

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	SinaReferer = "https://finance.sina.com.cn"
)

// defaultHTTPHeaders 生成请求 sina 接口的默认 http.Header
func defaultHTTPHeaders() http.Header {
	headers := make(http.Header)
	headers.Add("Referer", SinaReferer)
	return headers
}

// ErrNotFound 表示新浪没有这只证券的行情，代码不存在或已退市。
var ErrNotFound = errors.New("没有这只证券的行情")

// hkCode 匹配带 HK 前缀的港股代码，如 hk00700、HK700。
var hkCode = regexp.MustCompile(`^(?i)hk(\d{1,5})$`)

// Search 根据关键字查询证券信息。没有匹配时返回空列表和 nil，请求失败时返回错误。
// 新浪搜索不认 hk00700 这样带前缀的港股代码，这时改用补足 5 位的数字搜索，只留下代码完全相同的港股。
func Search(ctx context.Context, key string) ([]*BasicSecurity, error) {
	m := hkCode.FindStringSubmatch(strings.TrimSpace(key))
	if m == nil {
		secs, err := search(ctx, key)
		if err != nil {
			return nil, err
		}
		return otcLast(key, secs), nil
	}
	code := strings.Repeat("0", 5-len(m[1])) + m[1]
	secs, err := search(ctx, code)
	if err != nil {
		return nil, err
	}
	var res []*BasicSecurity
	for _, sec := range secs {
		if sec.ExCode == "HK"+code {
			res = append(res, sec)
		}
	}
	return res, nil
}

// usOTC 匹配美股场外交易的外国公司代码：5 个字母，以 F（普通股）或 Y（ADR）结尾，如 TCEHY、TCTZF。
var usOTC = regexp.MustCompile(`^[A-Z]{4}[FY]$`)

// otcLast 把美股场外交易的外国公司代码排到其他结果后面，其余顺序不变。
// 新浪搜索「腾讯」时 TCEHY 排在港股 00700 前面，按第一个结果取数据的命令会拿到场外 ADR。
// 直接搜索场外代码本身时不调整。
func otcLast(key string, secs []*BasicSecurity) []*BasicSecurity {
	key = strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(key), "$"))
	isOTC := func(sec *BasicSecurity) bool {
		code := strings.TrimPrefix(sec.ExCode, "$")
		return sec.ExChange == types.ExChangeNasdaq && usOTC.MatchString(code) && code != key
	}
	res := make([]*BasicSecurity, 0, len(secs))
	for _, sec := range secs {
		if !isOTC(sec) {
			res = append(res, sec)
		}
	}
	for _, sec := range secs {
		if isOTC(sec) {
			res = append(res, sec)
		}
	}
	return res
}

func search(ctx context.Context, key string) ([]*BasicSecurity, error) {
	// 这个接口的参数写在路径里，& 和 = 也必须转义；空格用 %20，不用查询串的 +。
	reqUrl := "https://suggest3.sinajs.cn/suggest/type=11,12,15,21,22,23,24,25,26,31,33,41&key=" + strings.ReplaceAll(url.QueryEscape(key), "+", "%20")
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqUrl, defaultHTTPHeaders(), nil, 0)
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
	resBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	// 防火墙等可能用 200 返回其他页面，不能当成没有匹配。
	if !strings.Contains(string(resBytes), "var suggestvalue=") {
		return nil, errors.New("搜索接口返回了无法识别的内容")
	}
	return parseBasicSecurity(string(resBytes)), nil
}

// fullCode 匹配带市场前缀的完整代码：SH600519、sz000001、BJ920000、HK00700、$AAPL、$BRK.B。
var fullCode = regexp.MustCompile(`^(?i:(sh|sz|bj)(\d{6})|hk(\d{5})|\$([a-z][a-z0-9.\-]*))$`)

// Resolve 和 Search 一样查询证券，供按第一个结果取数据的命令使用。搜索接口失败时，
// 如果 key 是带市场前缀的完整代码，就直接按代码返回一个结果，不让一个搜索接口挡住所有命令；
// 这时没有证券名称，类型一律按股票。
func Resolve(ctx context.Context, key string) ([]*BasicSecurity, error) {
	secs, err := Search(ctx, key)
	if err == nil || ctx.Err() != nil {
		return secs, err
	}
	sec := securityFromCode(strings.TrimSpace(key))
	if sec == nil {
		return nil, err
	}
	slog.WarnContext(ctx, "搜索证券失败，按代码处理", "key", key, "error", err)
	return []*BasicSecurity{sec}, nil
}

// ResolvedKey 是 ResolveAll 里一个关键字的结果。
type ResolvedKey struct {
	Key string
	// Security 是搜到的第一个结果，没搜到或搜索失败时为 nil。
	Security *BasicSecurity
	// Err 是搜索接口的错误，这时 Security 为 nil，可以稍后重试。
	Err error
	// DuplicateOf 不为空时，这个关键字和前面的 DuplicateOf 搜到同一只证券，调用方应只保留前面那个。
	DuplicateOf string
}

// NotFound 表示搜索成功但没有结果。
func (r ResolvedKey) NotFound() bool { return r.Err == nil && r.Security == nil }

// ResolveAll 对每个关键字并发调用 Resolve，结果和 keys 一一对应、顺序相同，并按证券代码（不区分大小写）标出重复。
// ctx 被取消时仍返回全部结果，没完成的关键字 Err 是 ctx 的错误，调用方用 ctx.Err() 判断。
func ResolveAll(ctx context.Context, keys []string) []ResolvedKey {
	results := make([]ResolvedKey, len(keys))
	var wg sync.WaitGroup
	for i, key := range keys {
		// 先填 Key，即使以后改成提前返回，没完成的条目也知道是哪个关键字。
		results[i].Key = key
		wg.Add(1)
		go func() {
			defer wg.Done()
			secs, err := Resolve(ctx, key)
			switch {
			case err != nil:
				results[i].Err = err
			case len(secs) > 0:
				results[i].Security = secs[0]
			}
		}()
	}
	wg.Wait()

	first := map[string]string{} // 证券代码 -> 第一个搜到它的关键字
	for i, result := range results {
		if result.Security == nil {
			continue
		}
		code := strings.ToUpper(result.Security.ExCode)
		if key, ok := first[code]; ok {
			results[i].DuplicateOf = key
			continue
		}
		first[code] = result.Key
	}
	return results
}

// securityFromCode 把完整代码转成和搜索结果一样的格式，不是完整代码时返回 nil。
func securityFromCode(key string) *BasicSecurity {
	m := fullCode.FindStringSubmatch(key)
	switch {
	case m == nil:
		return nil
	case m[2] != "":
		market := strings.ToLower(m[1])
		return &BasicSecurity{Code: m[2], ExCode: strings.ToUpper(market) + m[2], ExChange: market, SecurityType: types.SecurityTypeStock}
	case m[3] != "":
		return &BasicSecurity{Code: m[3], ExCode: "HK" + m[3], ExChange: types.ExChangeHKex, SecurityType: types.SecurityTypeStock}
	default:
		return &BasicSecurity{Code: strings.ToLower(m[4]), ExCode: formatUSCode(m[4]), ExChange: types.ExChangeNasdaq, SecurityType: types.SecurityTypeStock}
	}
}

// Profile 根据证券代码获取证券的基本信息，exCode SH600036
func Profile(ctx context.Context, opts *types.InfoOptions) (*CorpProfile, error) {
	if opts == nil {
		return nil, errors.New("opts is nil")
	}

	// 新浪的公司资料页只有 A 股，港股只有行情部分，公司资料由 hkstock 包从东方财富补上。
	if types.IsHCode(opts.ExCode) {
		return hkQuoteProfile(ctx, opts.ExCode)
	}

	var (
		wg          sync.WaitGroup
		corp        *BasicCorp
		quote       *SecurityQuote
		partProfile *sinaPartProfile

		err1, err2 error
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		corp, err1 = QueryBasicCorp(ctx, opts.ExCode)
	}()
	go func() {
		defer wg.Done()
		quote, partProfile, err2 = Info(ctx, opts.ExCode)
	}()
	wg.Wait()

	if err1 != nil {
		slog.ErrorContext(ctx, "failed get corp info", "error", err1)
		return nil, err1
	}
	if err2 != nil {
		slog.ErrorContext(ctx, "failed get info", "error", err2)
		return nil, err2
	}

	metrics := calculateCoreMetrics(opts.ExCode, quote, partProfile)
	profile := &CorpProfile{
		ExCode:          corp.ExCode,
		Name:            corp.Name,
		HistoryName:     corp.HistoryName,
		ListingPrice:    corp.Price,
		ListingDate:     corp.Date,
		WebSite:         corp.WebSite,
		RegisterAddress: corp.RegisterAddress,
		BusinessAddress: corp.BusinessAddress,
		MainBusiness:    corp.MainBusiness,
		Current:         metrics.Price,
		PriceIsYClose:   metrics.PriceIsYClose,
		Category:        metrics.Category,
		MarketCap:       metrics.MarketCap,
		TradedMarketCap: metrics.TradedMarketCap,
		PB:              metrics.PB,
		PeTTM:           metrics.PeTTM,
	}

	if profile.HistoryName == "" {
		profile.HistoryName = quote.Name
	}
	return profile, nil
}

// QueryDividends 查询分红送转信息
func QueryDividends(ctx context.Context, code string) ([]Dividend, error) {
	pageURL := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/corp/go.php/vISSUE_ShareBonus/stockid/%s.phtml", code)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, pageURL, defaultHTTPHeaders(), nil, 0)
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

	// 解析页面内容
	return parseDividend(body)
}

// QueryBasicCorp 根据证券代码获取公司信息
func QueryBasicCorp(ctx context.Context, exCode string) (*BasicCorp, error) {
	corpUrl := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/%s.phtml", exCode)
	slog.DebugContext(ctx, "QueryBasicCorp", "corpUrl", corpUrl)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, corpUrl, defaultHTTPHeaders(), nil, 0)
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

	return parseBasicCorp(body)
}

// Info 请求证券信息
// TODO: 拆分成 2 个函数
// A 股
// var hq_str_sh688047="龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,";
// var hq_str_sh688047_i="A,lxzk,-0.8200,-1.1566,-0.5900,8.2671,94.6804,40100,27964.4729,27964.4729,0,CNY,-3.2944,-4.6378,60.0600,1,-6.9400,2.1959,-2.3813,133.21,67.89,0.2,龙芯中科,K|D|0|40100|4100,119.62|79.74,20240630|-119064971.81,700.7400|90.1790,|,,1/1,EQA,,0.00,110.610|119.620|99.680,半导体,龙芯中科,7,417392977.82";
// 港股
// var hq_str_hk00700="TENCENT,腾讯控股,508.500,510.000,514.500,507.000,512.000,2.000,0.392,512.00000,512.50000,7662280393,14986877,0.000,0.000,542.266,345.980,2025/05/27,16:08";
// var hq_str_hk00700_i="EQTY,MAIN,542.266,345.980,2.9127,0,0,9189794319,0,9189794319,0,195076015710.40,51819945047.20,5.69,1,腾讯控股,3.700,0.5,100,腾讯控股,50028.230,209573020603.860,216730058624.020,1216841671768.900,,,0.8788,122.528696,,413.391|473.070|518.000,4.756824,2.589747,51,HKD";
func Info(ctx context.Context, exCode string) (*SecurityQuote, *sinaPartProfile, error) {
	lowerKey := strings.ToLower(exCode)
	reqUrl := fmt.Sprintf("https://hq.sinajs.cn/list=%s,%s_i", lowerKey, lowerKey)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqUrl, defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, nil, err
	}

	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, nil, err
	}
	err = adjustRespBodyByEncode(resp)
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	// 正则抽取双引号("")中的内容
	// origin
	// var hq_str_sh688047="龙芯中科,124.000,124.780,124.960,125.990,123.480,124.950,124.960,1346625,167864719.000,4104,124.950,5000,124.940,1100,124.930,1200,124.710,500,124.700,2178,124.960,1700,124.980,2400,124.990,4889,125.000,200,125.020,2025-05-27,15:00:00,00,";
	// var hq_str_sh688047_i="A,lxzk,-1.5600,-1.7502,-0.3800,6.9603,66.4645,40100,27964.4729,27964.4729,0,CNY,-6.2535,-7.0182,60.0600,1,-5.2800,1.2496,-1.5128,168.88,83.5,0.2,龙芯中科,K|D|0|40100|4100,149.74|99.82,20250331|-151280445.14,718.7600|91.7230,|,,1/1,EQA,,0.00,132.280|129.790|120.400,半导体,龙芯中科,1,509214702.8";
	// 抽取后
	// "龙芯中科,124.000,124.780,124.960,125.990,123.480,124.950,124.960,1346625,167864719.000,4104,124.950,5000,124.940,1100,124.930,1200,124.710,500,124.700,2178,124.960,1700,124.980,2400,124.990,4889,125.000,200,125.020,2025-05-27,15:00:00,00,";
	// "A,lxzk,-1.5600,-1.7502,-0.3800,6.9603,66.4645,40100,27964.4729,27964.4729,0,CNY,-6.2535,-7.0182,60.0600,1,-5.2800,1.2496,-1.5128,168.88,83.5,0.2,龙芯中科,K|D|0|40100|4100,149.74|99.82,20250331|-151280445.14,718.7600|91.7230,|,,1/1,EQA,,0.00,132.280|129.790|120.400,半导体,龙芯中科,1,509214702.8";
	regstr := regexp.MustCompile(`"(.*)"`)
	lines := regstr.FindAll([]byte(body), -1)
	if len(lines) != 2 {
		slog.ErrorContext(ctx, "invalid body", "url", reqUrl, "body", body)
		return nil, nil, errors.New("invalid body, should have 2 lines but not")
	}
	// 代码不存在时新浪返回空字符串。
	if string(lines[0]) == `""` {
		return nil, nil, fmt.Errorf("%w: %s", ErrNotFound, exCode)
	}

	quote, err := parseSecQuote(exCode, string(lines[0]))
	if err != nil {
		return nil, nil, err
	}
	partProfile, err := parseInfoPartProfile(exCode, string(lines[1]))
	if err != nil {
		return nil, nil, err
	}

	return quote, partProfile, nil
}

// parseBasicSecurity 解析 sina 搜索结果字符串
// var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,";
func parseBasicSecurity(body string) []*BasicSecurity {
	// 去除首部多余字符串
	body1 := strings.ReplaceAll(body, `var suggestvalue="`, "")
	// 去除尾部多余字符串
	body2 := strings.ReplaceAll(body1, `";`, "")
	// 按照 ; 分隔成多行
	lines := strings.Split(body2, ";")

	res := make([]*BasicSecurity, 0, len(lines))
	for _, item := range lines {
		if item == "" {
			continue
		}

		// 深A  立讯精密,11,002475,sz002475,立讯精密,,立讯精密,99,1,ESG,,
		// 港股 腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG,
		// 港股 比亚迪电子,31,00285,00285,比亚迪电子,,比亚迪电子,99,1,ESG,,
		// 美股 AMD,41,amd,amd,AMD,,AMD,99,1,ESG,,
		// 1 5 7名称 2市场 3 4代码 8- 9在市 10- 11-
		ss := strings.Split(item, ",")
		// 只依赖前 9 列，后面的可选字段数量会随新浪接口更新变化。
		if len(ss) < 9 {
			slog.Warn("parseBasicSecurity invalid line ", "body", body, "item", item)
			continue
		}
		if ss[8] != "1" {
			continue
		}

		var (
			name     string = ss[4]
			exCode   string = strings.ToUpper(ss[3])
			code     string = ss[2]
			exChange string
			secType  types.SecurityType
		)

		switch ss[1] {
		case "11", "12", "15":
			if len(ss[3]) >= 2 {
				exChange = ss[3][:2]
			} else {
				slog.Warn("parseBasicSecurity invalid excode", "code", ss[0], "ss[3]", ss[3])
			}
			secType = types.SecurityTypeStock
		case "21", "22", "23", "24", "25", "26":
			secType = types.SecurityTypeFund
		case "31", "33":
			secType = types.SecurityTypeStock
			exChange = types.ExChangeHKex
			exCode = "HK" + ss[3]
		case "41":
			secType = types.SecurityTypeStock
			exChange = types.ExChangeNasdaq
			exCode = formatUSCode(ss[3])
		default:
			slog.Warn("can not recognize", "code", ss[0], "ss[2]", ss[2])
		}

		ssr := &BasicSecurity{
			Name:         name,
			Code:         code,
			ExCode:       exCode,
			ExChange:     exChange,
			SecurityType: secType,
		}

		res = append(res, ssr)
	}

	return res
}

// formatUSCode 格式化美国证券代码
func formatUSCode(in string) (out string) {
	out = in
	if !strings.Contains(in, "$") {
		out = "$" + out
	}
	return strings.ToUpper(out)
}

type sinaPartProfile struct {
	VPS      float64 // 每股净资产
	Cap      float64 // 总股本
	TradeCap float64 // 流通股本
	Profit   float64 // 净利润
	Category string  // 行业分类
	BShare   bool    // 是否 B 股
	HKType   string  // 港股证券类型：EQTY 股本证券，TRST 交易所买卖基金、房地产投资信托等，WRNT 权证
	Currency string  // 港股交易币种：HKD、CNY、USD
}

func parseInfoPartProfile(exCode, line string) (*sinaPartProfile, error) {
	if types.IsACode(exCode) {
		return parseInfoPartProfileOfAstock(line)
	}
	if types.IsHCode(exCode) {
		return parseInfoPartProfileOfHstock(line)
	}

	slog.Error("parseInfoPartProfile unsuported", "excode", exCode)
	return nil, fmt.Errorf("unsupported excode %s", exCode)
}

// parseInfoPartProfileOfAstock 解析 A 股 profile 数据
// "A,lxzk,-1.5600,-1.7502,-0.3800,6.9603,66.4645,40100,27964.4729,27964.4729,0,CNY,-6.2535,-7.0182,60.0600,1,-5.2800,1.2496,-1.5128,168.88,83.5,0.2,龙芯中科,K|D|0|40100|4100,149.74|99.82,20250331|-151280445.14,718.7600|91.7230,|,,1/1,EQA,,0.00,132.280|129.790|120.400,半导体,龙芯中科,1,509214702.8";
// 0  1    2       3       4       5      6       7     8          9          10 11 12      13      14      15  16    17     18      19     20   21  22      23               24                                  25              26 27 28 29 30 31  32                     33    34     35 36
func parseInfoPartProfileOfAstock(line string) (*sinaPartProfile, error) {
	items := strings.Split(strings.Trim(line, "\""), ",")
	if len(items) < 35 {
		return nil, fmt.Errorf("invalid items %s, num should >= 35 but get %d", line, len(items))
	}

	var err error
	partProfile := sinaPartProfile{BShare: items[0] == "B"}
	partProfile.VPS, err = strconv.ParseFloat(items[5], 64)
	if err != nil {
		return nil, err
	}
	partProfile.Cap, err = strconv.ParseFloat(items[7], 64)
	if err != nil {
		return nil, err
	}
	partProfile.TradeCap, err = strconv.ParseFloat(items[8], 64)
	if err != nil {
		return nil, err
	}
	// 13 是近四个季度净利润（亿元），18 只是最新报告期的累计值。
	partProfile.Profit, err = strconv.ParseFloat(items[13], 64)
	if err != nil {
		return nil, err
	}
	partProfile.Category = strings.TrimSpace(items[34])

	return &partProfile, nil
}

// parseInfoPartProfileOfHstock 解析 H 股 profile 数据
// "EQTY,MAIN,542.266,345.980,2.9127,0,0,9189794319,0,9189794319,0,195076015710.40,51819945047.20,5.69,1,腾讯控股,3.700,0.5,100,腾讯控股,50028.230,209573020603.860,216730058624.020,1216841671768.900,,,0.8788,122.528696,,413.391|473.070|518.000,4.756824,2.589747,51,HKD"
// 0     1    2       3       4      5 6 7总股本     8 9流通股本   10 11              12             13  14 15     16    17   18  19    20         21               22               23               24 25 26  27
func parseInfoPartProfileOfHstock(line string) (*sinaPartProfile, error) {
	items := strings.Split(strings.Trim(line, "\""), ",")
	if len(items) < 33 {
		return nil, fmt.Errorf("invalid items %s, num should >= 33 but get %d", line, len(items))
	}

	// 盈富基金等交易所买卖基金没有每股净资产，这一列为空，按 0 处理。
	parse := func(text string) (float64, error) {
		if text == "" {
			return 0, nil
		}
		return strconv.ParseFloat(text, 64)
	}
	var err error
	partProfile := sinaPartProfile{}
	partProfile.VPS, err = parse(items[27])
	if err != nil {
		return nil, err
	}
	partProfile.Cap, err = parse(items[7])
	if err != nil {
		return nil, err
	}
	partProfile.TradeCap, err = parse(items[9])
	if err != nil {
		return nil, err
	}
	partProfile.Profit, err = parse(items[12])
	if err != nil {
		return nil, err
	}
	partProfile.Category = strings.TrimSpace(items[32])
	partProfile.HKType = items[0]
	if len(items) > 33 {
		partProfile.Currency = items[33]
	}

	return &partProfile, nil
}

// adjustRespBodyByEncode 根据 header 中的编码调整 response.Body 的内容，避免乱码
func adjustRespBodyByEncode(resp *http.Response) error {
	if resp == nil {
		return nil
	}

	resBytes, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	encodHeader := strings.ToLower(resp.Header.Get("Content-Type"))
	// 未声明 GBK 编码时保留原始内容，避免 UTF-8 响应被清空。
	newBodyBytes := resBytes
	if strings.Contains(encodHeader, "charset=gbk") {
		newBodyBytes, err = simplifiedchinese.GBK.NewDecoder().Bytes(resBytes)
	} else if strings.Contains(encodHeader, "charset=gb18030") {
		newBodyBytes, err = simplifiedchinese.GB18030.NewDecoder().Bytes(resBytes)
	}
	if err != nil {
		return err
	}

	resp.Body = io.NopCloser(bytes.NewBuffer(newBodyBytes))
	return nil
}

// parseBasicCorp 解析 html 得到基本 corp 信息
func parseBasicCorp(body []byte) (*BasicCorp, error) {
	doc, err := goquery.NewDocumentFromReader(io.NopCloser(bytes.NewBuffer(body)))
	if err != nil {
		return nil, err
	}

	res := new(BasicCorp)
	ss := doc.Find("#comInfo1 td")
	ss.Each(func(i int, s *goquery.Selection) {
		// for debug/dev:
		// fmt.Printf("Review %d: %s\n", i, s.Text())
		// For each item found, get the title
		switch i {
		case 1:
			res.Name = s.Text()
		case 3:
			res.EnName = s.Text()
		case 4:
			res.ExChange = s.Text()
		case 7:
			res.Date = strings.TrimSpace(s.Text())
		case 9:
			str := strings.TrimSpace(s.Text())
			pf, err := strconv.ParseFloat(str, 64)
			if err == nil {
				res.Price = pf
			}
		case 35:
			res.WebSite = strings.TrimSpace(s.Text())
		case 41:
			res.HistoryName = strings.TrimSpace(s.Text())
		case 43:
			res.RegisterAddress = strings.TrimSpace(s.Text())
		case 45:
			res.BusinessAddress = strings.TrimSpace(s.Text())
		case 49:
			res.MainBusiness = strings.TrimSpace(s.Text())
		}
	})

	return res, nil
}

// parseDividend 解析 html 得到基本 dividend 信息
func parseDividend(body []byte) ([]Dividend, error) {
	doc, err := goquery.NewDocumentFromReader(io.NopCloser(bytes.NewBuffer(body)))
	if err != nil {
		return nil, err
	}

	res := make([]Dividend, 0)
	ss := doc.Find("#sharebonus_1 tr td")
	var (
		num  int
		d    Dividend
		errs []error
	)

	// 先统计分红送转总行数
	ss.Each(func(i int, s *goquery.Selection) {
		num += 1
	})

	ss.Each(func(i int, s *goquery.Selection) {
		// for debug/dev:
		// fmt.Printf("Review %d: %s\n", i, s.Text())
		mod := i % 9
		switch mod {
		case 0:
			if i > 0 {
				res = append(res, d)
				d = Dividend{}
			}
			d.PublicDate = s.Text()
		case 1:
			d.Shares, err = strconv.ParseFloat(s.Text(), 64)
		case 2:
			d.AddShares, err = strconv.ParseFloat(s.Text(), 64)
		case 3:
			d.Bonus, err = strconv.ParseFloat(s.Text(), 64)
		case 5:
			d.DividendedDate = s.Text()
		case 6:
			d.RecordDate = s.Text()
		}

		if err != nil {
			errs = append(errs, err)
		}

		if i == num-1 {
			res = append(res, d)
		}
	})

	if len(errs) != 0 {
		for i := range errs {
			slog.Error("parsed element error", "error", errs[i])
		}
		return nil, errs[0]
	}

	return res, nil
}
