//go:build windows || darwin

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func usServer(t *testing.T) *desktopServer {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Cleanup(func() { usFactsCache.entries = map[int]usFactsEntry{} })
	return &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
}

func usRequest(t *testing.T, srv *desktopServer, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

const usTickers = `{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."},"1":{"cik_str":1046179,"ticker":"TSM","title":"TSMC"}}`

// 行情失败时照常返回公司资料，并说明原因。
func TestUSCompany(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(usTickers)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"name":"Apple Inc.","category":"Large accelerated filer","filings":{"recent":{}}}`)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_aapl").Reply(502)
	rr := usRequest(t, srv, "/api/v1/us/company?code=$AAPL")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Info struct {
			Company map[string]any `json:"company"`
			Quote   map[string]any `json:"quote"`
		} `json:"info"`
		Notes []string `json:"notes"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "Large accelerated filer", data.Info.Company["category"])
	require.Nil(t, data.Info.Quote)
	require.Contains(t, data.Notes[0], "行情获取失败")
	require.Equal(t, http.StatusBadRequest, usRequest(t, srv, "/api/v1/us/company?code=SH600036").Code)
}

func TestUSFinancials(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(usTickers)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000320193.json").Reply(200).
		BodyString(`{"entityName":"Apple Inc.","facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"start":"2024-09-29","end":"2025-09-27","val":112,"fp":"FY","form":"10-K","filed":"2025-10-31"}]}}}}}`)
	rr := usRequest(t, srv, "/api/v1/us/financials?code=$AAPL&statement=income")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Name   string `json:"name"`
		Report struct {
			Periods []struct {
				End    string             `json:"end"`
				Values map[string]float64 `json:"values"`
			} `json:"periods"`
		} `json:"report"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "Apple Inc.", data.Name)
	require.Equal(t, 112.0, data.Report.Periods[0].Values["net_income"])

	// 10 分钟内切换报表用缓存，不再请求 SEC。
	rr = usRequest(t, srv, "/api/v1/us/financials?code=$AAPL&statement=balance&period=quarter")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.True(t, gock.IsDone())

	require.Equal(t, http.StatusBadRequest, usRequest(t, srv, "/api/v1/us/financials?code=$AAPL&statement=x").Code)
	require.Equal(t, http.StatusBadRequest, usRequest(t, srv, "/api/v1/us/financials?code=$AAPL&statement=income&period=week").Code)
}

func TestUSFinancialsIFRS(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(usTickers)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0001046179.json").Reply(200).BodyString(`{"facts":{"ifrs-full":{"Revenue":{"units":{}}}}}`)
	rr := usRequest(t, srv, "/api/v1/us/financials?code=$TSM&statement=income")
	require.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	require.Contains(t, rr.Body.String(), "IFRS")

	// 同一公司仍保留历史 US GAAP 数据时，最近报表改用 IFRS 也应返回 422，而非陈旧报表。
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0001046179.json").Reply(200).BodyString(`{"facts":{
"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"end":"2021-03-31","val":10,"filed":"2026-06-01"}]}}},
"ifrs-full":{"ProfitLoss":{"units":{"JPY":[{"end":"2025-03-31","val":20,"filed":"2025-06-01"}]}}}
}}`)
	rr = usRequest(t, srv, "/api/v1/us/financials?code=$TSM&statement=income")
	require.Equal(t, http.StatusUnprocessableEntity, rr.Code)
	require.Contains(t, rr.Body.String(), "IFRS")
	require.True(t, gock.IsDone())
}

func TestUSFactsCacheEviction(t *testing.T) {
	usServer(t)
	now := time.Now()
	expired := &edgar.Facts{Name: "expired"}
	usFactsCache.entries[1] = usFactsEntry{now.Add(-usFactsTTL), expired}
	usFactsCache.entries[2] = usFactsEntry{now.Add(-time.Minute), &edgar.Facts{Name: "fresh"}}
	cacheUSFacts(3, &edgar.Facts{Name: "new"}, now)
	require.NotContains(t, usFactsCache.entries, 1)
	require.Contains(t, usFactsCache.entries, 2)
	require.Contains(t, usFactsCache.entries, 3)

	usFactsCache.entries = map[int]usFactsEntry{}
	for i := 0; i < usFactsMaxEntries+1; i++ {
		cacheUSFacts(i, &edgar.Facts{CIK: i}, now.Add(time.Duration(i)*time.Second))
	}
	require.Len(t, usFactsCache.entries, usFactsMaxEntries)
	require.NotContains(t, usFactsCache.entries, 0, "淘汰最早写入的公司")
	cacheUSFacts(1, &edgar.Facts{Name: "refreshed"}, now.Add(time.Minute))
	require.Len(t, usFactsCache.entries, usFactsMaxEntries, "更新已有公司不淘汰其他公司")
	require.Equal(t, "refreshed", usFactsCache.entries[1].facts.Name)
}
