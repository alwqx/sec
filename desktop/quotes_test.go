//go:build windows || darwin

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const sinaLine = `var hq_str_%s="%s,10.000,9.800,10.200,10.300,9.900,10.190,10.200,1000,10200.000,100,10.190,200,10.180,300,10.170,400,10.160,500,10.150,100,10.200,200,10.210,300,10.220,400,10.230,500,10.240,2026-10-30,15:00:00,00,";`

func mockSina(path, body string) {
	gock.New("https://hq.sinajs.cn").Get(path).Reply(200).SetHeader("Content-Type", "application/javascript; charset=gbk").BodyString(body)
}

func TestQuotes(t *testing.T) {
	defer gock.Off()
	mockSina("/list=sh600036,sz000001", fmt.Sprintf(sinaLine, "sh600036", "A")+"\n"+fmt.Sprintf(sinaLine, "sz000001", "B")+"\n")
	rr := macroRequest(t, "/api/v1/quotes?codes=sh600036,SZ000001")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Results []struct {
			ExCode string `json:"ex_code"`
		} `json:"results"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Len(t, data.Results, 2)
	require.Equal(t, "SH600036", data.Results[0].ExCode)
	require.Empty(t, data.Missing)
}

const sinaUSLine = `var hq_str_gb_amd="AMD,144.5500,4.44,2025-07-10 22:55:35,6.1400,143.0000,145.8200,141.8500,187.1100,76.4800,32285637,47105949,234373976387,1.37,105.510000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:55AM EDT,138.4100,0,1,2025,4641781226.0000,0.0000,0.0000,0.0000,0.0000,138.4100";`

func quotesResponse(t *testing.T, path string) (int, []string, []string) {
	t.Helper()
	rr := macroRequest(t, path)
	var data struct {
		Results []struct {
			ExCode string `json:"ex_code"`
		} `json:"results"`
		Missing []string `json:"missing"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data), rr.Body.String())
	codes := []string{}
	for _, item := range data.Results {
		codes = append(codes, item.ExCode)
	}
	return rr.Code, codes, data.Missing
}

// 美股代码以 $ 开头，不能让整个请求失败。
func TestQuotesAcceptsUSCodes(t *testing.T) {
	defer gock.Off()
	mockSina("/list=sh600036,gb_amd", fmt.Sprintf(sinaLine, "sh600036", "A")+"\n"+sinaUSLine+"\n")
	status, codes, missing := quotesResponse(t, "/api/v1/quotes?codes=SH600036,%24AMD")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{"SH600036", "$AMD"}, codes)
	require.Empty(t, missing)
}

// 解析不了的证券只标记自己，不发额外请求。
func TestQuotesMarksUnavailableCodesWithoutExtraRequests(t *testing.T) {
	defer gock.Off()
	gock.CleanUnmatchedRequest()
	mockSina("/list=sh600036,sh999999", fmt.Sprintf(sinaLine, "sh600036", "A")+"\n"+`var hq_str_sh999999="";`+"\n")
	status, codes, missing := quotesResponse(t, "/api/v1/quotes?codes=SH600036,SH999999")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, []string{"SH600036"}, codes)
	require.Equal(t, []string{"SH999999"}, missing)
	require.True(t, gock.IsDone())
	require.False(t, gock.HasUnmatchedRequest(), "不逐只重试")
}

func TestQuotesAllUnavailableIsNotAnError(t *testing.T) {
	defer gock.Off()
	mockSina("/list=sh999999", `var hq_str_sh999999="";`+"\n")
	status, codes, missing := quotesResponse(t, "/api/v1/quotes?codes=SH999999")
	require.Equal(t, http.StatusOK, status)
	require.Empty(t, codes)
	require.Equal(t, []string{"SH999999"}, missing)
}

func TestQuotesUpstreamFailure(t *testing.T) {
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(500)
	rr := macroRequest(t, "/api/v1/quotes?codes=SH600036")
	require.Equal(t, http.StatusBadGateway, rr.Code)
}

// 超过一批的自选分批查询，一批失败时另一批仍有结果。
func TestQuotesSplitsIntoBatches(t *testing.T) {
	defer gock.Off()
	codes := make([]string, 0, 101)
	for i := 0; i < 101; i++ {
		codes = append(codes, fmt.Sprintf("SH%06d", 600000+i))
	}
	first := make([]string, 0, 100)
	for i := 0; i < 100; i++ {
		first = append(first, fmt.Sprintf(sinaLine, fmt.Sprintf("sh%06d", 600000+i), "A"))
	}
	mockSina("/list=sh600000,.*sh600099$", strings.Join(first, "\n")+"\n")
	gock.New("https://hq.sinajs.cn").Get("/list=sh600100$").Reply(500)
	status, found, missing := quotesResponse(t, "/api/v1/quotes?codes="+strings.Join(codes, ","))
	require.Equal(t, http.StatusOK, status)
	require.Len(t, found, 100)
	require.Equal(t, []string{"SH600100"}, missing)
}

func TestQuotesRejectsInvalidCodes(t *testing.T) {
	for _, query := range []string{"", "codes=", "codes=SH600036,%3Cscript%3E", "codes=" + strings.Repeat("SH600036,", 1001)} {
		rr := macroRequest(t, "/api/v1/quotes?"+query)
		require.Equal(t, http.StatusBadRequest, rr.Code, query)
	}
}

func historyLines(days int) string {
	lines := make([]string, 0, days)
	for day := 1; day <= days; day++ {
		close := float64(day)
		lines = append(lines, fmt.Sprintf(`"2026-01-%02d,%.2f,%.2f,%.2f,%.2f,100,1000,0,0,0,0"`, day, close, close, close, close))
	}
	return strings.Join(lines, ",")
}

// 均线窗口使用查询起点之前的数据，预热数据不进入结果。
func TestHistoryOverlaysUseWarmup(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").MatchParam("beg", "20251213").Reply(200).BodyString(`{"data":{"code":"600036","market":1,"name":"A","klines":[` + historyLines(10) + `]}}`)
	rr := macroRequest(t, "/api/v1/history?code=SH600036&begin=2026-01-03&end=2026-01-10&ma=3&boll=3,2")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Results []struct {
			Date string `json:"date"`
		} `json:"results"`
		Overlays []struct {
			Label  string     `json:"label"`
			Kind   string     `json:"kind"`
			Values []*float64 `json:"values"`
		} `json:"overlays"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Len(t, data.Results, 8)
	require.Equal(t, "2026-01-03", data.Results[0].Date)
	require.Len(t, data.Overlays, 4)
	require.Equal(t, "MA3", data.Overlays[0].Label)
	require.Len(t, data.Overlays[0].Values, 8)
	require.NotNil(t, data.Overlays[0].Values[0])
	require.Equal(t, 2.0, *data.Overlays[0].Values[0], "1、2、3 日的均值，窗口使用了查询起点之前的数据")
	require.Equal(t, "BOLL上轨", data.Overlays[1].Label)
	require.Equal(t, "boll", data.Overlays[1].Kind)
}

func TestHistoryOverlayWindowTooShortIsNull(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(200).BodyString(`{"data":{"code":"600036","market":1,"name":"A","klines":[` + historyLines(3) + `]}}`)
	rr := macroRequest(t, "/api/v1/history?code=SH600036&begin=2026-01-01&end=2026-01-03&ma=2,5")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Overlays []struct {
			Values []*float64 `json:"values"`
		} `json:"overlays"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Nil(t, data.Overlays[0].Values[0], "窗口不足是空值，不是0")
	require.Equal(t, 1.5, *data.Overlays[0].Values[1])
	for _, value := range data.Overlays[1].Values {
		require.Nil(t, value)
	}
}

func TestHistoryWithoutOverlaysKeepsResponse(t *testing.T) {
	defer gock.Off()
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").MatchParam("beg", "20260101").Reply(200).BodyString(`{"data":{"code":"600036","market":1,"name":"A","klines":[` + historyLines(2) + `]}}`)
	rr := macroRequest(t, "/api/v1/history?code=SH600036&begin=2026-01-01&end=2026-01-02")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotContains(t, rr.Body.String(), "overlays")
}

func TestHistoryRejectsInvalidOverlays(t *testing.T) {
	for _, query := range []string{"ma=0", "ma=abc", "ma=251", "ma=1,2,3,4,5,6,7", "boll=20", "boll=20,0", "boll=20,11", "boll=0,2"} {
		rr := macroRequest(t, "/api/v1/history?code=SH600036&begin=2026-01-01&end=2026-01-02&"+query)
		require.Equal(t, http.StatusBadRequest, rr.Code, query)
	}
}
