package provider

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/types"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type SinaProvider struct{}

func Search(key string) []types.SinaSearchResult {
	reqUrl := fmt.Sprintf("https://suggest3.sinajs.cn/suggest/type=11,12,15,21,22,23,24,25,26,31,33,41&key=%s", key)
	var (
		resp *http.Response
		err  error
	)

	req, err := http.NewRequest(http.MethodGet, reqUrl, nil)
	if err != nil {
		slog.Error("new request %s error: %v", reqUrl, err)
		return nil
	}
	req.Header.Add("Referer", "https://finance.sina.com.cn")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("[Search] request %s error: %v", reqUrl, err)
		return nil
	}
	defer resp.Body.Close()

	var resBytes []byte
	resBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("[Search] request %s error: %v", reqUrl, err)
		return nil
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "charset=GBK") {
		resBytes, err = simplifiedchinese.GBK.NewDecoder().Bytes(resBytes)
		if err != nil {
			slog.Error("[Search] request %s error: %v", reqUrl, err)
			return nil
		}
	}

	return parseSinaSearchResults(string(resBytes))
}

// parseSinaSearchResults 解析 sina 搜索结果字符串
// var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,";
func parseSinaSearchResults(body string) []types.SinaSearchResult {
	body1 := strings.ReplaceAll(body, `var suggestvalue="`, "")
	body2 := strings.ReplaceAll(body1, `";`, "")
	lines := strings.Split(body2, ";")

	res := make([]types.SinaSearchResult, 0, len(lines))
	for _, item := range lines {
		// 腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG;
		// 1 5 7名称 2市场 3 4代码 8- 9在市 10-
		ss := strings.Split(item, ",")
		ssr := types.SinaSearchResult{
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
	coraUrl := fmt.Sprintf("https://vip.stock.finance.sina.com.cn/corp/go.php/vCI_CorpInfo/stockid/%s.phtml", key)
	// infoUrl = fmt.Sprintf("https://hq.sinajs.cn/list=%s,%s_i", key, key)

	var (
		resp *http.Response
		err  error
	)

	req, err := http.NewRequest(http.MethodGet, coraUrl, nil)
	if err != nil {
		slog.Error("new request %s error: %v", coraUrl, err)
		return nil
	}
	req.Header.Add("Referer", "https://finance.sina.com.cn")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("[Search] request %s error: %v", coraUrl, err)
		return nil
	}
	defer resp.Body.Close()

	var resBytes []byte
	resBytes, err = io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("[Search] request %s error: %v", coraUrl, err)
		return nil
	}

	encodHeader := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(encodHeader, "charset=gbk") {
		resBytes, err = simplifiedchinese.GBK.NewDecoder().Bytes(resBytes)
		// simplifiedchinese.GB18030.NewDecoder().Bytes(resBytes)
		if err != nil {
			slog.Error("[Search] request %s error: %v", coraUrl, err)
			return nil
		}
	}
	newBody := io.NopCloser(bytes.NewBuffer(resBytes))

	doc, err := goquery.NewDocumentFromReader(newBody)
	if err != nil {
		slog.Error("[Search] request %s error: %v", coraUrl, err)
		return nil
	}

	res := types.SinaSecurityProfile{}
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

	return nil
}
