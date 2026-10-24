//go:build windows || darwin

package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestHandleQuote(t *testing.T) {
	defer gock.Off()

	body := `var hq_str_sh688047="龙芯中科,131.560,131.600,131.950,132.870,130.820,131.950,132.000,1972190,259739142.000,3237,131.950,1900,131.940,1000,131.930,6331,131.920,430,131.880,200,132.000,600,132.010,300,132.020,2373,132.040,2340,132.050,2025-07-10,15:00:01,00,";`
	gock.New("https://hq.sinajs.cn").Get("/list=sh688047").
		Reply(200).BodyString(body).
		Header.Add("content-type", "application/javascript; charset=gbk")

	srv := &desktopServer{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quote?code=SH688047", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "secret"})
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"ex_code":"SH688047"`)
}

func TestHandleQuoteRequiresToken(t *testing.T) {
	srv := &desktopServer{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quote?code=SH688047", nil)
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code)
}

func TestHandleSearchMissingKey(t *testing.T) {
	srv := &desktopServer{token: "secret", dev: true}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/search", nil)
	rr := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestWithRecover(t *testing.T) {
	srv := &desktopServer{token: "secret"}
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/quote", nil)
	rr := httptest.NewRecorder()

	require.NotPanics(t, func() {
		srv.withRecover(panicHandler).ServeHTTP(rr, req)
	})
	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Contains(t, rr.Body.String(), "服务暂时不可用，请稍后重试")
}

func TestCompanyAPIsRejectUnsupportedCodes(t *testing.T) {
	for _, endpoint := range []string{"company", "dividends"} {
		for _, code := range []string{"", "HK00700", "$AAPL", "SH600036/other"} {
			t.Run(endpoint+code, func(t *testing.T) {
				srv := &desktopServer{dev: true}
				rr := httptest.NewRecorder()
				srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/"+endpoint+"?code="+code, nil))
				require.Equal(t, http.StatusBadRequest, rr.Code)
			})
		}
		srv := &desktopServer{token: "secret"}
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/"+endpoint+"?code=SH600036", nil))
		require.Equal(t, http.StatusForbidden, rr.Code)
	}
}

func TestHandleCompany(t *testing.T) {
	defer gock.Off()
	gock.New("https://vip.stock.finance.sina.com.cn").
		Get("/corp/go.php/vCI_CorpInfo/stockid/600036.phtml").
		Reply(200).BodyString(`<table id="comInfo1"><tr><td>名称</td><td>招商银行</td><td>英文</td><td>CMB</td></tr></table>`).
		Header.Add("Content-Type", "text/html; charset=utf-8")
	srv := &desktopServer{dev: true}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/company?code=sh600036", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"name":"招商银行","en_name":"CMB","history_name":"","listing_date":"","listing_price":0,"website":"","register_address":"","business_address":"","main_business":""}`, rr.Body.String())
	require.True(t, gock.IsDone())
}

func TestHandleCompanyEmptyPage(t *testing.T) {
	defer gock.Off()
	gock.New("https://vip.stock.finance.sina.com.cn").Get("/corp/go.php/vCI_CorpInfo/stockid/600036.phtml").Reply(200).BodyString(`<html></html>`)
	srv := &desktopServer{dev: true}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/company?code=SH600036", nil))
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestHandleDividends(t *testing.T) {
	defer gock.Off()
	gock.New("https://vip.stock.finance.sina.com.cn").Get("/corp/go.php/vISSUE_ShareBonus/stockid/600036.phtml").Reply(200).
		BodyString(`<table id="sharebonus_1"><tr><td>2026-06-01</td><td>0</td><td>1</td><td>20</td><td>实施</td><td>2026-07-02</td><td>2026-07-01</td><td></td><td></td></tr></table>`)
	srv := &desktopServer{dev: true}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/dividends?code=SH600036", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"results":[{"public_date":"2026-06-01","record_date":"2026-07-01","dividend_date":"2026-07-02","shares":0,"add_shares":1,"bonus":20}]}`, rr.Body.String())
}

func TestHandleDividendsEmpty(t *testing.T) {
	defer gock.Off()
	gock.New("https://vip.stock.finance.sina.com.cn").Get("/corp/go.php/vISSUE_ShareBonus/stockid/600036.phtml").Reply(200).BodyString(`<html></html>`)
	srv := &desktopServer{dev: true}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/dividends?code=SH600036", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, `{"results":[]}`, rr.Body.String())
}
