package provider

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/version"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	SinaReferer = "https://finance.sina.com.cn"
)

type SinaProvider struct{}

func Search(key string) []types.BasicSecurity {
	reqUrl := fmt.Sprintf("https://suggest3.sinajs.cn/suggest/type=11,12,15,21,22,23,24,25,26,31,33,41&key=%s", key)
	headers := make(http.Header)
	headers.Add("Referer", "https://finance.sina.com.cn")

	resp, err := makeRequest(http.MethodGet, reqUrl, headers, nil)
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

	return parseBasicSecuritys(string(resBytes))
}

// parseBasicSecuritys 解析 sina 搜索结果字符串
// var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,";
func parseBasicSecuritys(body string) []types.BasicSecurity {
	body1 := strings.ReplaceAll(body, `var suggestvalue="`, "")
	body2 := strings.ReplaceAll(body1, `";`, "")
	lines := strings.Split(body2, ";")

	res := make([]types.BasicSecurity, 0, len(lines))
	for _, item := range lines {
		// 腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG;
		// 1 5 7名称 2市场 3 4代码 8- 9在市 10-
		ss := strings.Split(item, ",")
		ssr := types.BasicSecurity{
			Name: ss[0],
			Code: ss[2],
		}

		switch ss[1] {
		case "11", "12", "15":
			if len(ss[3]) >= 2 {
				ssr.ExChange = ss[3][:2]
			} else {
				slog.Warn("invalid code %s of %s", ss[0], ss[3])
			}
			ssr.ExCode = strings.ToUpper(ss[3])
			ssr.SecurityType = types.SecurityTypeStock
		case "21", "22", "23", "24", "25", "26":
			ssr.SecurityType = types.SecurityTypeFund
		case "31", "33":
			ssr.SecurityType = types.SecurityTypeStock
			ssr.ExChange = types.ExChangeHKex
			ssr.ExCode = "HK" + ss[3]
		case "41":
			ssr.SecurityType = types.SecurityTypeStock
			ssr.ExChange = types.ExChangeNasdaq
			ssr.ExCode = formatUSCode(ss[3])
		default:
			slog.Warn("can not recganize code: %s %s", ss[0], ss[2])
		}

		res = append(res, ssr)
	}

	return res
}

func formatUSCode(in string) (out string) {
	out = in
	if !strings.Contains(in, "$") {
		out = "$" + out
	}
	return
}

// Profile 获取证券的基本信息
func Profile(key string) []types.SinaSecurityProfile {
	// infoUrl = fmt.Sprintf("https://hq.sinajs.cn/list=%s,%s_i", key, key)

	corp, err := CorpInfo(key)
	if err != nil {
		slog.Error(fmt.Sprintf("corp info error: %v", err))
	}
	fmt.Println(*corp)
	return nil
}

// CorpInfo 请求公司信息
func CorpInfo(key string) (*types.BasicCorp, error) {
	coraUrl := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/%s.phtml", key)
	headers := make(http.Header)
	headers.Set("Referer", SinaReferer)

	resp, err := makeRequest(http.MethodGet, coraUrl, headers, nil)
	if err != nil {
		slog.Error("new request %s error: %v", coraUrl, err)
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

	return parseCorpInfo(body)
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

// parseCorpInfo 解析 html 得到基本 corp 信息
func parseCorpInfo(body []byte) (*types.BasicCorp, error) {
	doc, err := goquery.NewDocumentFromReader(io.NopCloser(bytes.NewBuffer(body)))
	if err != nil {
		return nil, err
	}

	res := new(types.BasicCorp)
	ss := doc.Find("#comInfo1 td")
	ss.Each(func(i int, s *goquery.Selection) {
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
		case 43:
			res.RegisterAddress = strings.TrimSpace(s.Text())
		case 45:
			res.WorkAddress = strings.TrimSpace(s.Text())
		case 49:
			res.MainBussiness = strings.TrimSpace(s.Text())
		}
		// fmt.Printf("Review %d: %s\n", i, s.Text())
	})

	return res, nil
}
