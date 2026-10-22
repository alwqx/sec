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
	require.Contains(t, rr.Body.String(), "internal error")
}
