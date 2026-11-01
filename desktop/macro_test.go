//go:build windows || darwin

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func macroRequest(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	srv := &desktopServer{token: "secret"}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: "secret"})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	return rr
}

const macroTreasuryFeed = `<feed xmlns="http://www.w3.org/2005/Atom"><entry><content><m:properties xmlns:m="m" xmlns:d="d"><d:NEW_DATE>2026-01-02T00:00:00</d:NEW_DATE><d:BC_1MONTH m:null="true"/><d:BC_10YEAR>4.15</d:BC_10YEAR></m:properties></content></entry></feed>`

func mockMacroTreasury(status int, body string) {
	gock.New("https://home.treasury.gov").Get("/resource-center/data-chart-center/interest-rates/pages/xml").MatchParam("field_tdr_date_value", "2026").Reply(status).BodyString(body)
}

func TestMacroCatalog(t *testing.T) {
	rr := macroRequest(t, "/api/v1/macro/catalog")
	require.Equal(t, http.StatusOK, rr.Code)
	var catalog struct {
		Categories []struct{ ID, Title string } `json:"categories"`
		Indicators []struct {
			Region, Command, Period string
		} `json:"indicators"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &catalog))
	require.Len(t, catalog.Categories, 10)
	require.Len(t, catalog.Indicators, 61)
}

func TestMacroData(t *testing.T) {
	defer gock.Off()
	mockMacroTreasury(200, macroTreasuryFeed)
	rr := macroRequest(t, "/api/v1/macro/data?region=us&command=treasury&begin=20260101&end=20260131")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var dataset map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &dataset))
	require.NotContains(t, dataset, "original")
	require.Contains(t, dataset, "series")
	require.Contains(t, string(dataset["observations"]), "us.treasury.10y.yield")
}

func TestMacroDataErrors(t *testing.T) {
	defer gock.Off()
	for path, status := range map[string]int{
		"/api/v1/macro/data?region=uk&command=cpi":                     http.StatusBadRequest,
		"/api/v1/macro/data?region=us&command=trade&component=--json":  http.StatusBadRequest,
		"/api/v1/macro/data?region=us&command=treasury&begin=2026":     http.StatusBadRequest,
		"/api/v1/macro/data?region=us&command=treasury&begin=19800101": http.StatusBadGateway,
	} {
		rr := macroRequest(t, path)
		require.Equal(t, status, rr.Code, path)
		require.Contains(t, rr.Body.String(), "error")
	}
	mockMacroTreasury(503, "unavailable")
	rr := macroRequest(t, "/api/v1/macro/data?region=us&command=treasury&begin=20260101&end=20260131")
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "503")
}

func TestMacroRequiresToken(t *testing.T) {
	srv := &desktopServer{token: "secret"}
	for _, path := range []string{"/api/v1/macro/catalog", "/api/v1/macro/data?region=us&command=treasury"} {
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusForbidden, rr.Code, path)
	}
}
