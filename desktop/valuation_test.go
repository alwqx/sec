//go:build windows || darwin

package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func mockValuationUpstream(valuation, performance string) {
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_VALUEANALYSIS_DET").Reply(200).BodyString(valuation)
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_LICO_FN_CPD").Reply(200).BodyString(performance)
}

const (
	valuationBody   = `{"success":true,"code":0,"result":{"data":[{"SECURITY_NAME_ABBR":"招商银行","BOARD_NAME":"银行Ⅱ","TRADE_DATE":"2026-10-30 00:00:00","CLOSE_PRICE":40,"TOTAL_MARKET_CAP":4e11,"TOTAL_SHARES":1e10,"PE_TTM":-8,"PB_MRQ":0.8,"PS_TTM":null}]}}`
	performanceBody = `{"success":true,"code":0,"result":{"data":[{"REPORTDATE":"2025-12-31 00:00:00","BASIC_EPS":5,"BPS":50,"WEIGHTAVG_ROE":10,"TOTAL_OPERATE_INCOME":100,"PARENT_NETPROFIT":50}]}}`
	emptyBody       = `{"success":false,"code":9201,"message":"返回数据为空","result":null}`
)

func TestValuation(t *testing.T) {
	defer gock.Off()
	mockValuationUpstream(valuationBody, performanceBody)
	rr := macroRequest(t, "/api/v1/valuation?code=sh600036&wacc=9")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "SH600036", data["code"])
	require.Equal(t, 40.0, data["price"])
	pe := data["pe"].(map[string]any)
	require.Nil(t, pe["current"], "亏损时当前 PE 为 null")
	require.Equal(t, -8.0, pe["raw"])
	require.Nil(t, data["ps"].(map[string]any)["current"], "缺失值是 null，不是 0")
	require.Equal(t, 50.0, data["bvps"])
	dcf := data["dcf"].(map[string]any)
	require.Equal(t, false, dcf["applicable"])
	require.Equal(t, 9.0, dcf["params"].(map[string]any)["wacc"])
}

func TestValuationNoData(t *testing.T) {
	defer gock.Off()
	mockValuationUpstream(emptyBody, emptyBody)
	rr := macroRequest(t, "/api/v1/valuation?code=SH000001")
	require.Equal(t, http.StatusNotFound, rr.Code)
	require.Contains(t, rr.Body.String(), "暂无估值数据")
}

func TestValuationUpstreamFailure(t *testing.T) {
	defer gock.Off()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").Times(2).Reply(500)
	rr := macroRequest(t, "/api/v1/valuation?code=SH600036")
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "估值数据获取失败")
}

func TestValuationRejectsInvalidParams(t *testing.T) {
	for _, query := range []string{
		"code=HK00700", "code=600036", "code=SH600036&wacc=abc", "code=SH600036&wacc=NaN", "code=SH600036&growth=NaN", "code=SH600036&wacc=3", "code=SH600036&wacc=0", "code=SH600036&growth=200", "code=SH600036&margin=95", "code=SH600036&terminal=7.5",
	} {
		rr := macroRequest(t, "/api/v1/valuation?"+query)
		require.Equal(t, http.StatusBadRequest, rr.Code, query)
	}
}

func TestValuationPeerFailureKeepsResult(t *testing.T) {
	defer gock.Off()
	mockValuationUpstream(`{"success":true,"code":0,"result":{"data":[{"SECURITY_NAME_ABBR":"招商银行","BOARD_CODE":"016029","BOARD_NAME":"银行Ⅱ","TRADE_DATE":"2026-10-30 00:00:00","CLOSE_PRICE":40,"TOTAL_MARKET_CAP":4e11,"TOTAL_SHARES":1e10,"PE_TTM":8,"PB_MRQ":0.8,"PS_TTM":3}]}}`, performanceBody)
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("filter", "BOARD_CODE").Reply(500)
	rr := macroRequest(t, "/api/v1/valuation?code=SH600036")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Nil(t, data["peers"])
	require.Contains(t, data["peers_note"], "未计入行业规则")
}
