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
	// 最近报表改用 IFRS 时用 IFRS 数据，不返回陈旧的历史 US GAAP 报表。
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0001046179.json").Reply(200).BodyString(`{"facts":{
"us-gaap":{"NetIncomeLoss":{"units":{"USD":[{"start":"2020-04-01","end":"2021-03-31","val":10,"form":"20-F","filed":"2021-06-01"}]}}},
"ifrs-full":{"ProfitLoss":{"units":{"JPY":[{"start":"2024-04-01","end":"2025-03-31","val":20,"form":"20-F","filed":"2025-06-01"}]}}}
}}`)
	rr := usRequest(t, srv, "/api/v1/us/financials?code=$TSM&statement=income")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		Report edgar.Report `json:"report"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, edgar.IFRS, body.Report.Standard)
	require.Equal(t, "JPY", body.Report.Currency)
	require.Len(t, body.Report.Periods, 1)
	require.Equal(t, "2025-03-31", body.Report.Periods[0].End)
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

// 美股估值：SEC XBRL 财务数据 + 新浪不复权日线。
func TestUSValuation(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(usTickers)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"name":"Apple Inc.","sicDescription":"Electronic Computers","sic":"3571","filings":{"recent":{}}}`)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000320193.json").Reply(200).BodyString(`{"entityName":"Apple Inc.","facts":{
"us-gaap":{
 "Revenues":{"units":{"USD":[
  {"start":"2025-01-01","end":"2025-03-31","val":100,"fp":"Q1","form":"10-Q","filed":"2025-04-30"},
  {"start":"2025-04-01","end":"2025-06-30","val":100,"fp":"Q2","form":"10-Q","filed":"2025-07-30"},
  {"start":"2025-07-01","end":"2025-09-30","val":100,"fp":"Q3","form":"10-Q","filed":"2025-10-30"},
  {"start":"2025-01-01","end":"2025-09-30","val":300,"fp":"Q3","form":"10-Q","filed":"2025-10-30"},
  {"start":"2025-01-01","end":"2025-12-31","val":400,"fp":"FY","form":"10-K","filed":"2026-01-30"}]}},
 "NetIncomeLoss":{"units":{"USD":[
  {"start":"2025-01-01","end":"2025-03-31","val":10,"fp":"Q1","form":"10-Q","filed":"2025-04-30"},
  {"start":"2025-04-01","end":"2025-06-30","val":10,"fp":"Q2","form":"10-Q","filed":"2025-07-30"},
  {"start":"2025-07-01","end":"2025-09-30","val":10,"fp":"Q3","form":"10-Q","filed":"2025-10-30"},
  {"start":"2025-01-01","end":"2025-09-30","val":30,"fp":"Q3","form":"10-Q","filed":"2025-10-30"},
  {"start":"2025-01-01","end":"2025-12-31","val":40,"fp":"FY","form":"10-K","filed":"2026-01-30"}]}},
 "StockholdersEquity":{"units":{"USD":[{"end":"2025-12-31","val":1000,"fp":"FY","form":"10-K","filed":"2026-01-30"}]}}},
"dei":{"EntityCommonStockSharesOutstanding":{"units":{"shares":[{"end":"2026-01-20","val":100,"fp":"FY","form":"10-K","filed":"2026-01-30","accn":"a"}]}}}
}}`)
	gock.New("https://stock.finance.sina.com.cn").Get("/usstock/api/jsonp.php/var.t=/US_MinKService.getDailyK").MatchParam("symbol", "aapl").Reply(200).
		BodyString(`var t=([{"d":"2026-03-02","o":"40","h":"40","l":"40","c":"40","v":"1"},{"d":"2026-03-03","o":"40","h":"40","l":"40","c":"40","v":"1"}]);`)
	rr := usRequest(t, srv, "/api/v1/valuation?code=$aapl")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Code      string  `json:"code"`
		Currency  string  `json:"currency"`
		MarketCap float64 `json:"market_cap"`
		PE        struct {
			Current float64 `json:"current"`
		} `json:"pe"`
		Dividends any    `json:"dividends"`
		Note      string `json:"dividends_note"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "$AAPL", data.Code)
	require.Equal(t, "USD", data.Currency)
	require.Equal(t, 4000.0, data.MarketCap)
	require.Equal(t, 100.0, data.PE.Current)
	require.Nil(t, data.Dividends)
	require.Contains(t, data.Note, "可能不分红")
}

func TestUSValuationErrors(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(usTickers)
	require.Equal(t, http.StatusNotFound, usRequest(t, srv, "/api/v1/valuation?code=$ZZZZ").Code)

	// 按 IFRS 申报的外国公司暂不支持。网易等按 US GAAP 提交 20-F 的外国公司同样不支持，见 valuation.TestForeignReason。
	gock.New("https://data.sec.gov").Get("/submissions/CIK0001046179.json").Reply(404)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0001046179.json").Reply(200).BodyString(`{"facts":{"ifrs-full":{"Revenue":{"units":{}}}}}`)
	gock.New("https://stock.finance.sina.com.cn").Get("/usstock/api/jsonp.php/var.t=/US_MinKService.getDailyK").Reply(200).BodyString(`var t=(null);`)
	rr := usRequest(t, srv, "/api/v1/valuation?code=$TSM")
	require.Equal(t, http.StatusUnprocessableEntity, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), "IFRS")
}

// 美股分红表和 CLI 的 sec info -d 相同；日线读不到时不按拆股调整并标明；SEC 没有的代码返回 404。
func TestUSDividends(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(usTickers)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000320193.json").Reply(200).BodyString(`{"entityName":"Apple Inc.","facts":{"us-gaap":{
"CommonStockDividendsPerShareDeclared":{"units":{"USD/shares":[{"start":"2024-09-29","end":"2025-09-27","val":1.02,"form":"10-K","filed":"2025-10-31"}]}},
"PaymentsOfDividends":{"units":{"USD":[{"start":"2024-09-29","end":"2025-09-27","val":15421000000,"form":"10-K","filed":"2025-10-31"}]}}}}}`)
	gock.New("https://stock.finance.sina.com.cn").Get("/usstock/api/jsonp.php/var.t=/US_MinKService.getDailyK").Reply(502)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_aapl").Reply(502)
	rr := usRequest(t, srv, "/api/v1/us/dividends?code=$AAPL")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		Name            string `json:"name"`
		SplitsUnchecked bool   `json:"splits_unchecked"`
		Years           []struct {
			FiscalYear int      `json:"fiscal_year"`
			PerShare   *float64 `json:"per_share"`
			Paid       *float64 `json:"paid"`
		} `json:"years"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Equal(t, "Apple Inc.", body.Name)
	require.True(t, body.SplitsUnchecked)
	require.Len(t, body.Years, 1)
	require.Equal(t, 2025, body.Years[0].FiscalYear)
	require.Equal(t, 1.02, *body.Years[0].PerShare)
	require.Equal(t, 15421000000.0, *body.Years[0].Paid)

	require.Equal(t, http.StatusBadRequest, usRequest(t, srv, "/api/v1/us/dividends?code=SH600036").Code)
	require.Equal(t, http.StatusNotFound, usRequest(t, srv, "/api/v1/us/dividends?code=$ZZZZ").Code)

	// 没有 XBRL 财务数据的公司也是 404，不是上游失败。
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0001046179.json").Reply(200).BodyString(`{"facts":{}}`)
	gock.New("https://stock.finance.sina.com.cn").Get("/usstock/api/jsonp.php/var.t=/US_MinKService.getDailyK").Reply(502)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_tsm").Reply(502)
	require.Equal(t, http.StatusNotFound, usRequest(t, srv, "/api/v1/us/dividends?code=$TSM").Code)
}

// 拆股之后还没有申报时，用新浪的当前股数核对日线跳变：拆股前年报的每股股息按 1 拆 4 调整。
func TestUSDividendsSplitWithCurrentShares(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(usTickers)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000320193.json").Reply(200).BodyString(`{"entityName":"Apple Inc.","facts":{"us-gaap":{
"CommonStockDividendsPerShareDeclared":{"units":{"USD/shares":[{"start":"2024-09-29","end":"2025-09-27","val":4.00,"form":"10-K","filed":"2025-10-31"}]}}},
"dei":{"EntityCommonStockSharesOutstanding":{"units":{"shares":[{"end":"2025-10-17","val":1000,"form":"10-K","filed":"2025-10-31"}]}}}}}`)
	gock.New("https://stock.finance.sina.com.cn").Get("/usstock/api/jsonp.php/var.t=/US_MinKService.getDailyK").
		Reply(200).BodyString(`var t=([{"d":"2026-03-02","o":"400","h":"400","l":"400","c":"400","v":"1"},{"d":"2026-03-03","o":"100","h":"100","l":"100","c":"100","v":"1"}]);`)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_aapl").
		Reply(200).BodyString(`var hq_str_gb_aapl="苹果,100.0000,-1.90,2026-11-04 09:14:47,-6.4200,325.5800,334.5000,325.3300,351.7300,242.8900,63160026,45152763,400000,8.30,39.990000,0.00,0.00,0.00,0.00,4000,63,329.800";`)
	rr := usRequest(t, srv, "/api/v1/us/dividends?code=$AAPL")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var body struct {
		Splits []struct {
			Date   string  `json:"date"`
			Factor float64 `json:"factor"`
		} `json:"splits"`
		Years []struct {
			PerShare *float64 `json:"per_share"`
		} `json:"years"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	require.Len(t, body.Splits, 1)
	require.Equal(t, "2026-03-03", body.Splits[0].Date)
	require.Equal(t, 4.0, body.Splits[0].Factor)
	require.Equal(t, 1.0, *body.Years[0].PerShare)
}
