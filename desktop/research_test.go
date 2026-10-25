//go:build windows || darwin

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestResearchEndpointsValidateParameters(t *testing.T) {
	srv := &desktopServer{dev: true}
	for _, path := range []string{
		"/api/v1/ipo?page=0", "/api/v1/ipo?page=no", "/api/v1/ipo?page=10001",
		"/api/v1/announcements?page=-1", "/api/v1/announcements?code=HK00700",
		"/api/v1/financials?code=SH600036&report=unknown", "/api/v1/financials?code=SH600036&period=unknown",
		"/api/v1/history?code=$AAPL", "/api/v1/history?code=SH600036other",
	} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
	for _, endpoint := range []string{"ipo", "announcements", "financials", "history"} {
		rr := httptest.NewRecorder()
		(&desktopServer{token: "secret"}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/"+endpoint, nil))
		require.Equal(t, http.StatusForbidden, rr.Code)
	}
}

func TestFinancialsMapping(t *testing.T) {
	defer gock.Off()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").
		MatchParam("filter", regexp.QuoteMeta(`(SECURITY_CODE="600036")(DATE_TYPE_CODE="001")`)).
		Reply(200).BodyString(`{"success":true,"result":{"data":[{"REPORT_DATE":"2025-12-31 00:00:00","TOTAL_ASSETS":12000000000}]}}`)
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/financials?code=SH600036&period=annual", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `"date":"2025-12-31"`)
	require.Contains(t, rr.Body.String(), `"label":"资产总计"`)
	require.Contains(t, rr.Body.String(), `"TOTAL_ASSETS":12000000000`)
	require.True(t, gock.IsDone())
}

func TestAnnouncementsSanitizeTitlesNamesAndLinks(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").Reply(200).
		BodyString(`{"totalAnnouncement":3,"totalpages":2,"announcements":[{"announcementId":"a1","announcementTitle":"<em>年度</em>报告 &amp; 摘要","announcementTime":1767225600000,"adjunctUrl":"finalpage/2026-01-01/a1.PDF","secCode":"600036","secName":"<em>招商银行</em>"},{"announcementId":"a2","announcementTitle":"已失效","invalidationFlag":1}]}`)
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/announcements?key=%E6%8B%9B%E5%95%86%E9%93%B6%E8%A1%8C", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `年度报告`)
	require.Contains(t, rr.Body.String(), `"name":"招商银行"`)
	require.NotContains(t, rr.Body.String(), `\u003cem\u003e`)
	require.NotContains(t, rr.Body.String(), `<em>`)
	require.NotContains(t, rr.Body.String(), `已失效`)
	require.Contains(t, rr.Body.String(), `https://static.cninfo.com.cn/finalpage/2026-01-01/a1.PDF`)
	require.Contains(t, rr.Body.String(), `"has_more":true`)
	require.True(t, gock.IsDone())
	for _, unsafe := range []string{"javascript:alert(1)", "https://evil.example/a.pdf", "//evil.example/a.pdf"} {
		require.Contains(t, announcementURL(&cninfo.Announcement{ID: "a1", AdjunctURL: unsafe}), "https://www.cninfo.com.cn/new/disclosure/detail?")
	}
}

func TestHistoryMapping(t *testing.T) {
	for _, tc := range []struct {
		code, symbol, secid string
		market, volume      int
	}{
		{"SH600036", "600036", "1.600036", 1, 10000},
		{"SZ000001", "000001", "0.000001", 0, 10000},
		{"HK00700", "00700", "116.00700", 116, 100},
	} {
		t.Run(tc.code, func(t *testing.T) {
			defer gock.Off()
			gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").MatchParam("secid", tc.secid).Reply(200).
				BodyString(fmt.Sprintf(`{"data":{"code":%q,"market":%d,"klines":["2026-10-21,40,41,42,39,100,410000,5,2,1,1"]}}`, tc.symbol, tc.market))
			rr := httptest.NewRecorder()
			(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/history?code="+tc.code, nil))
			require.Equal(t, http.StatusOK, rr.Code)
			require.JSONEq(t, fmt.Sprintf(`{"results":[{"date":"2026-10-21","open":40,"close":41,"high":42,"low":39,"volume":%d,"turnover":410000}]}`, tc.volume), rr.Body.String())
			require.True(t, gock.IsDone())
		})
	}
}

func TestDisclosureURLValidation(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://evil.example/", "https://www.cninfo.com.cn@evil.example/", "https://www.cninfo.com.cn:8443/", "http://www.cninfo.com.cn/"} {
		require.Error(t, validateDisclosureURL(raw))
	}
	require.NoError(t, validateDisclosureURL("https://static.cninfo.com.cn/finalpage/2026-01-01/test.PDF"))
	require.NoError(t, validateDisclosureURL("https://www.cninfo.com.cn/new/disclosure/detail?announcementId=1"))
}

func TestHistoryFilters(t *testing.T) {
	for _, code := range []string{"SH600036", "SZ000001", "HK00700"} {
		for index, fq := range []string{"bfq", "qfq", "hfq"} {
			t.Run(code+fq, func(t *testing.T) {
				defer gock.Off()
				gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").
					MatchParam("beg", "20240101").MatchParam("end", "20241231").MatchParam("fqt", fmt.Sprint(index)).
					Reply(200).BodyString(`{"data":{"code":"600036","market":1,"klines":[]}}`)
				response := httptest.NewRecorder()
				(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/history?code="+code+"&begin=2024-01-01&end=2024-12-31&fq="+fq, nil))
				require.Equal(t, 200, response.Code, response.Body.String())
				require.JSONEq(t, `{"results":[]}`, response.Body.String())
				require.True(t, gock.IsDone())
			})
		}
	}
}

func TestAnnouncementCategoryFilters(t *testing.T) {
	for name, category := range map[string]string{
		"annual": cninfo.CategoryAnnual, "halfyear": cninfo.CategoryHalfYear,
		"q1": cninfo.CategoryQ1, "q3": cninfo.CategoryQ3,
	} {
		t.Run(name, func(t *testing.T) {
			defer gock.Off()
			gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").
				MatchParam("category", category).MatchParam("searchkey", "报告").MatchParam("pageNum", "2").
				Reply(200).BodyString(`{"totalAnnouncement":45,"hasMore":true,"announcements":[]}`)
			response := httptest.NewRecorder()
			(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/announcements?page=2&key=报告&type="+name, nil))
			require.Equal(t, 200, response.Code, response.Body.String())
			require.JSONEq(t, `{"results":[],"total":45,"has_more":true}`, response.Body.String())
			require.True(t, gock.IsDone())
		})
	}
	// 类别应先于证券身份查询校验，不允许未知分类落回全部公告。
	response := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/announcements?code=SH600036&type=unknown", nil))
	require.Equal(t, 400, response.Code)
}

func TestHistoryRejectsInvalidFilters(t *testing.T) {
	for _, query := range []string{
		"begin=2024-02-30", "end=20240101", "begin=2024-02-01&end=2024-01-01",
		"begin=1989-12-31&end=1990-01-01", "end=2999-01-01",
		"begin=2010-01-01&end=2015-01-02", "fq=unknown",
	} {
		response := httptest.NewRecorder()
		(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/history?code=SH600036&"+query, nil))
		require.Equal(t, 400, response.Code, query)
	}
}

func TestHistoryDoesNotTruncateRange(t *testing.T) {
	defer gock.Off()
	lines := make([]string, 0, 70)
	for day := 0; day < 70; day++ {
		date := time.Date(2024, 1, 1+day, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		lines = append(lines, date+",40,41,42,39,100,410000,5,2,1,1")
	}
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(200).
		JSON(map[string]any{"data": map[string]any{"code": "600036", "market": 1, "klines": lines}})
	response := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/history?code=SH600036&begin=2024-01-01&end=2024-03-31", nil))
	require.Equal(t, 200, response.Code)
	var data struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &data))
	require.Len(t, data.Results, 70)
	require.Equal(t, "2024-01-01", data.Results[0]["date"])
	require.Equal(t, 10000.0, data.Results[0]["volume"])
}
