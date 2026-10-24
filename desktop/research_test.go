//go:build windows || darwin

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

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

func TestAnnouncementsSanitizeTitlesAndLinks(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").Reply(200).
		BodyString(`{"totalAnnouncement":3,"totalpages":2,"announcements":[{"announcementId":"a1","announcementTitle":"<em>年度</em>报告 &amp; 摘要","announcementTime":1767225600000,"adjunctUrl":"finalpage/2026-01-01/a1.PDF","secCode":"600036","secName":"招商银行"},{"announcementId":"a2","announcementTitle":"已失效","invalidationFlag":1}]}`)
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/announcements", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.Contains(t, rr.Body.String(), `年度报告`)
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
