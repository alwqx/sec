package sina

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/version"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	SinaReferer = "https://finance.sina.com.cn"
)

// defaultHttpHeaders 生成请求 sina 接口的默认 http.Header
func defaultHttpHeaders() http.Header {
	headers := make(http.Header)
	headers.Add("Referer", SinaReferer)
	return headers
}

// Search 根据关键字查询证券信息
func Search(key string) []BasicSecurity {
	reqUrl := fmt.Sprintf("https://suggest3.sinajs.cn/suggest/type=11,12,15,21,22,23,24,25,26,31,33,41&key=%s", key)
	resp, err := makeRequest(http.MethodGet, reqUrl, defaultHttpHeaders(), nil)
	if err != nil {
		return nil
	}
	err = adjustRespBodyByEncode(resp)
	defer resp.Body.Close()
	if err != nil {
		slog.Error("[Search] request %s error: %v", reqUrl, err)
		return nil
	}

	var resBytes []byte
	resBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("[Search] request %s error: %v", reqUrl, err)
		return nil
	}

	return parseBasicSecurity(string(resBytes))
}

// MultiSearch 根据关键字查询多个证券信息
// 最多支持 8 条证券信息查询
func MultiSearch(keys []string) []BasicSecurity {
	num := len(keys)
	if num == 0 {
		return nil
	}
	if num > 8 {
		slog.Debug("MultiSearch: sec num>8, choose 8 keys to search")
		keys = keys[:8]
	}

	res := make([]BasicSecurity, 0, num)
	ch := make(chan BasicSecurity, num)
	for _, key := range keys {
		go func(code string) {
			secs := Search(code)
			if len(secs) >= 1 {
				slog.Debug("MultiSearch", "secs", secs)
				ch <- secs[0]
			}
		}(key)
	}

	for range num {
		res = append(res, <-ch)
	}

	return res
}

// QuerySecQuote 查询证券行情
func QuerySecQuote(exCode string) (*SecurityQuote, error) {
	lowerKey := strings.ToLower(exCode)
	reqUrl := fmt.Sprintf("https://hq.sinajs.cn/list=%s", lowerKey)
	resp, err := makeRequest(http.MethodGet, reqUrl, defaultHttpHeaders(), nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
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
		slog.Error("request %s get invalid body %s", reqUrl, body)
	}
	quote := parseSecQuote(string(lines[0]))

	return quote, nil
}

// Profile 根据证券代码获取证券的基本信息，exCode SH600036
func Profile(opts *types.InfoOptions) *CorpProfile {
	if opts == nil {
		return nil
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
		corp, err1 = QueryBasicCorp(opts.ExCode)
	}()
	go func() {
		defer wg.Done()
		quote, partProfile, err2 = Info(opts.ExCode)
	}()
	wg.Wait()

	if err1 != nil {
		slog.Error(fmt.Sprintf("corp info error: %v", err1))
	}
	if err2 != nil {
		slog.Error(fmt.Sprintf("info error: %v", err2))
	}

	profile := &CorpProfile{
		ExCode:          corp.ExCode,
		Name:            corp.Name,
		HistoryName:     corp.HistoryName,
		ListingPrice:    corp.Price,
		ListingDate:     corp.Date,
		WebSite:         corp.WebSite,
		RegisterAddress: corp.WebSite,
		BusinessAddress: corp.BusinessAddress,
		MainBusiness:    corp.MainBussiness,
		Current:         quote.Current,
		Category:        partProfile.Categray,
		MarketCap:       quote.Current * float64(partProfile.Cap) * 10000.0,
		TradedMarketCap: quote.Current * float64(partProfile.TradeCap) * 10000.0,
	}

	if profile.HistoryName == "" {
		profile.HistoryName = quote.Name
	}
	if partProfile.VPS != 0 {
		profile.PB = quote.Current / partProfile.VPS
	}
	if partProfile.Profit > 0 {
		profile.PeTTM = quote.Current * float64(partProfile.Cap) / partProfile.Profit / 10000.0
	}

	return profile
}

// QueryDividends 查询分红送转信息
func QueryDividends(code string) ([]Dividend, error) {
	pageURL := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/corp/go.php/vISSUE_ShareBonus/stockid/%s.phtml", code)
	resp, err := makeRequest(http.MethodGet, pageURL, defaultHttpHeaders(), nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
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
func QueryBasicCorp(exCode string) (*BasicCorp, error) {
	coraUrl := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/%s.phtml", exCode)
	resp, err := makeRequest(http.MethodGet, coraUrl, defaultHttpHeaders(), nil)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
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
// var hq_str_sh688047="龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,";
// var hq_str_sh688047_i="A,lxzk,-0.8200,-1.1566,-0.5900,8.2671,94.6804,40100,27964.4729,27964.4729,0,CNY,-3.2944,-4.6378,60.0600,1,-6.9400,2.1959,-2.3813,133.21,67.89,0.2,龙芯中科,K|D|0|40100|4100,119.62|79.74,20240630|-119064971.81,700.7400|90.1790,|,,1/1,EQA,,0.00,110.610|119.620|99.680,半导体,龙芯中科,7,417392977.82";
func Info(exCode string) (*SecurityQuote, *sinaPartProfile, error) {
	lowerKey := strings.ToLower(exCode)
	reqUrl := fmt.Sprintf("https://hq.sinajs.cn/list=%s,%s_i", lowerKey, lowerKey)
	resp, err := makeRequest(http.MethodGet, reqUrl, defaultHttpHeaders(), nil)
	if err != nil {
		return nil, nil, err
	}

	defer resp.Body.Close()
	err = adjustRespBodyByEncode(resp)
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	regstr := regexp.MustCompile(`"(.*)"`)
	lines := regstr.FindAll([]byte(body), -1)
	if len(lines) != 2 {
		slog.Error("request %s get invalid body %s", reqUrl, body)
		return nil, nil, errors.New("invalid body, should have 2 lines but not")
	}

	quote := parseSecQuote(string(lines[0]))
	partProfile, err := parseInfoPartProfile(string(lines[1]))
	if err != nil {
		return nil, nil, err
	}

	return quote, partProfile, nil
}

// parseBasicSecurity 解析 sina 搜索结果字符串
// var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,";
func parseBasicSecurity(body string) []BasicSecurity {
	body1 := strings.ReplaceAll(body, `var suggestvalue="`, "")
	body2 := strings.ReplaceAll(body1, `";`, "")
	lines := strings.Split(body2, ";")

	res := make([]BasicSecurity, 0, len(lines))
	for _, item := range lines {
		// 腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG;
		// 1 5 7名称 2市场 3 4代码 8- 9在市 10-
		ss := strings.Split(item, ",")
		if len(ss) != 11 {
			slog.Debug("parseBasicSecurity", "body invalid", body)
			slog.Warn("parseBasicSecurity", "line of body invalid", item)
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
				slog.Warn("invalid code %s of %s", ss[0], ss[3])
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
			slog.Warn("can not recganize code: %s %s", ss[0], ss[2])
		}

		ssr := BasicSecurity{
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

// 原始行：var hq_str_sh688047="龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,";
// 经过正则抽取后的内容："龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,"
func parseSecQuote(quote string) *SecurityQuote {
	// 将首尾的冒号去掉
	newQuote := strings.TrimPrefix(quote, "\"")
	newQuote = strings.TrimSuffix(newQuote, "\"")
	items := strings.Split(newQuote, ",")
	res := new(SecurityQuote)
	res.Name = strings.TrimSpace(items[0])
	slog.Debug("parseSecQuote", "quote string", quote, "items", items)
	var err error
	res.Current, err = strconv.ParseFloat(strings.TrimSpace(items[3]), 64)
	if err != nil {
		slog.Error(err.Error())
	}
	res.Open, err = strconv.ParseFloat(strings.TrimSpace(items[1]), 64)
	if err != nil {
		slog.Error(err.Error())
	}
	res.YClose, err = strconv.ParseFloat(strings.TrimSpace(items[2]), 64)
	if err != nil {
		slog.Error(err.Error())
	}
	res.High, err = strconv.ParseFloat(strings.TrimSpace(items[4]), 64)
	if err != nil {
		slog.Error(err.Error())
	}
	res.Low, err = strconv.ParseFloat(strings.TrimSpace(items[5]), 64)
	if err != nil {
		slog.Error(err.Error())
	}
	res.Volume, err = strconv.ParseFloat(strings.TrimSpace(items[9]), 64)
	if err != nil {
		slog.Error(err.Error())
	}
	res.TurnOver, err = strconv.ParseInt(strings.TrimSpace(items[8]), 10, 64)
	if err != nil {
		slog.Error(err.Error())
	}
	res.TradeDate = strings.TrimSpace(items[30])
	res.Time = strings.TrimSpace(items[31])

	return res
}

type sinaPartProfile struct {
	VPS      float64 // 每股净资产
	Cap      float64 // 总股本
	TradeCap float64 // 流通股本
	Profit   float64 // 净利润
	Categray string  // 行业分类
}

func parseInfoPartProfile(line string) (*sinaPartProfile, error) {
	items := strings.Split(line, ",")
	var (
		err error
	)

	partProfile := sinaPartProfile{}
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
	partProfile.Profit, err = strconv.ParseFloat(items[18], 64)
	if err != nil {
		return nil, err
	}
	partProfile.Categray = strings.TrimSpace(items[34])

	return &partProfile, nil
}

// adjustRespBodyByEncode 根据 header 中的编码调整 response.Body 的内容，避免乱码
func adjustRespBodyByEncode(resp *http.Response) error {
	if resp == nil {
		return nil
	}

	resBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	encodHeader := strings.ToLower(resp.Header.Get("Content-Type"))
	var newBodyBytes []byte
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

func makeRequest(method, reqURL string, headers http.Header, body io.Reader) (*http.Response, error) {
	var (
		resp *http.Response
		err  error
	)

	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}
	if headers != nil {
		req.Header = headers
	}

	req.Header.Set("User-Agent", fmt.Sprintf("sec/%s (%s %s) Go/%s", version.Version, runtime.GOARCH, runtime.GOOS, runtime.Version()))

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	return resp, nil
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
			pf, err := strconv.ParseFloat(str, 32)
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
			res.MainBussiness = strings.TrimSpace(s.Text())
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

	return res, nil
}
