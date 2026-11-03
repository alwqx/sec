//go:build windows || darwin

package main

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestFinancialChartsValidation(t *testing.T) {
	rr := macroRequest(t, "/api/v1/financials/charts?code=HK00700")
	require.Equal(t, http.StatusBadRequest, rr.Code)
	rr = macroRequest(t, "/api/v1/financials/charts?code=SH600519&freq=month")
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestFinancialChartsUpstreamFailure(t *testing.T) {
	defer gock.Off()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").Reply(500)
	rr := macroRequest(t, "/api/v1/financials/charts?code=SH600519")
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "财报数据获取失败")
}

func TestFinancialChartsNoData(t *testing.T) {
	defer gock.Off()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").Reply(200).BodyString(`{"success":true,"code":0,"result":{"data":[{"BOARD_NAME":"白酒Ⅱ"}]}}`)
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbDateAjaxNew").Reply(200).BodyString(`{"data":[]}`)
	rr := macroRequest(t, "/api/v1/financials/charts?code=SH600519")
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestFinancialChartsCache(t *testing.T) {
	defer gock.Off()
	financialChartsMu.Lock()
	financialChartsCache = map[string]cachedCharts{}
	financialChartsMu.Unlock()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").Reply(200).BodyString(`{"success":true,"code":0,"result":{"data":[{"BOARD_NAME":"白酒Ⅱ"}]}}`)
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbDateAjaxNew").Reply(200).BodyString(`{"data":[{"REPORT_DATE":"2025-12-31 00:00:00"}]}`)
	income := `{"data":[{"REPORT_DATE":"2025-12-31 00:00:00","TOTAL_OPERATE_INCOME":100,"TOTAL_OPERATE_COST":60,"OPERATE_COST":50,"OPERATE_PROFIT":40,"TOTAL_PROFIT":40,"NETPROFIT":30,"PARENT_NETPROFIT":30}]}`
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbAjaxNew").Reply(200).BodyString(income)
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/zcfzbAjaxNew").Reply(200).BodyString(`{"data":[]}`)
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/xjllbAjaxNew").Reply(200).BodyString(`{"data":[]}`)
	first := macroRequest(t, "/api/v1/financials/charts?code=sh600519")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.True(t, gock.IsDone())
	second := macroRequest(t, "/api/v1/financials/charts?code=SH600519")
	require.Equal(t, http.StatusOK, second.Code, "第二次直接用缓存，不再请求上游")
	require.Equal(t, first.Body.String(), second.Body.String())
	// 不同频率分开缓存。
	other := macroRequest(t, "/api/v1/financials/charts?code=SH600519&freq=quarter")
	require.Equal(t, http.StatusBadGateway, other.Code)
}
