package provider

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

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

	resp, err = http.DefaultClient.Get(reqUrl)
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
			ssr.ExChange = ss[3][:2]
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

	fmt.Println(res)

	return res
}

func formatUSCode(in string) (out string) {
	out = in
	if !strings.Contains(in, "$") {
		out = "$" + out
	}
	return
}
