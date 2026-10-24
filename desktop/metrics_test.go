//go:build windows || darwin

package main

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func metricsBody(vps, cap, floatCap, profit string) string {
	quote := make([]string, 33)
	for i := range quote {
		quote[i] = "0"
	}
	quote[0], quote[1], quote[2], quote[3], quote[4], quote[5] = "招商银行", "10", "10", "10", "10", "10"
	quote[30], quote[31] = "2026-10-23", "15:00:00"
	profile := make([]string, 35)
	for i := range profile {
		profile[i] = "0"
	}
	profile[5], profile[7], profile[8], profile[18], profile[34] = vps, cap, floatCap, profit, "银行"
	return fmt.Sprintf("var hq_str_sh600036=\"%s\";\nvar hq_str_sh600036_i=\"%s\";", strings.Join(quote, ","), strings.Join(profile, ","))
}

func TestMetricsEndpoint(t *testing.T) {
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036,sh600036_i").Reply(200).BodyString(metricsBody("5", "10000", "5000", "2"))
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?code=sh600036", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.JSONEq(t, `{"code":"SH600036","industry":"银行","pe_ttm":5,"pb":2,"market_cap":1000000000,"float_market_cap":500000000,"currency":"CNY","trade_date":"2026-10-23","time":"15:00:00"}`, rr.Body.String())
	require.True(t, gock.IsDone()) // 不请求公司资料页面，核心指标单独加载。
}

func TestMetricsUnknownValues(t *testing.T) {
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036,sh600036_i").Reply(200).BodyString(metricsBody("0", "0", "0", "-1"))
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?code=SH600036", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"code":"SH600036","industry":"银行","pe_ttm":null,"pb":null,"market_cap":null,"float_market_cap":null,"currency":"CNY","trade_date":"2026-10-23","time":"15:00:00"}`, rr.Body.String())
	require.Nil(t, validMetric(math.Inf(1), false))
	require.Nil(t, validMetric(math.NaN(), true))
}

func TestMetricsValidationAndFailure(t *testing.T) {
	for _, code := range []string{"", "600036", "HK00700", "$AAPL", "SH600036other"} {
		rr := httptest.NewRecorder()
		(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?code="+code, nil))
		require.Equal(t, http.StatusBadRequest, rr.Code)
	}
	rr := httptest.NewRecorder()
	(&desktopServer{token: "secret"}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?code=SH600036", nil))
	require.Equal(t, http.StatusForbidden, rr.Code)
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036,sh600036_i").Reply(200).BodyString("invalid")
	rr = httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metrics?code=SH600036", nil))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.True(t, gock.IsDone())
}
