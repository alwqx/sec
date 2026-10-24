//go:build windows || darwin

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func metalRequest(t *testing.T, query, body string) metalResponse {
	t.Helper()
	defer gock.Off()
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").MatchParam("instid", "Au99.99").Reply(200).BodyString(body)
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metal"+query, nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var response metalResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	require.True(t, gock.IsDone())
	return response
}

func TestMetalHistoryAndLatest(t *testing.T) {
	// 上游记录无序；区间首日的昨收来自区间外的前一交易日；最新行情独立于历史区间。
	response := metalRequest(t, "?begin=2016-12-20&end=2016-12-21", `{"time":[["2016-12-30",265,266,264,267],["2016-12-21",261,260,259,262],["2016-12-19",261,262,260,263],["2016-12-20",262,264,261,265]]}`)
	require.Equal(t, "Au99.99", response.Name)
	require.Equal(t, "元/克", response.Unit)
	require.Equal(t, "2016-12-30", response.Latest.Date)
	require.Len(t, response.Results, 2)
	require.Equal(t, "2016-12-20", response.Results[0].Date)
	require.Equal(t, "2016-12-21", response.Results[1].Date)
	require.InDelta(t, 262, *response.Results[0].YClose, 1e-9)
	require.InDelta(t, 2, *response.Results[0].Change, 1e-9)
	require.InDelta(t, 2.0/262*100, *response.Results[0].ChangeRate, 1e-9)
	require.InDelta(t, -4, *response.Results[1].Change, 1e-9)
}

func TestMetalUnknownChange(t *testing.T) {
	for _, body := range []string{
		`{"time":[["2016-12-19",261,262,260,263]]}`,
		`{"time":[["2016-12-18",0,0,0,0],["2016-12-19",261,262,260,263]]}`,
	} {
		response := metalRequest(t, "?begin=2016-12-19&end=2016-12-19", body)
		require.Len(t, response.Results, 1)
		require.Nil(t, response.Results[0].YClose)
		require.Nil(t, response.Results[0].Change)
		require.Nil(t, response.Results[0].ChangeRate)
	}
}

func TestMetalEmptyHistoryKeepsLatest(t *testing.T) {
	response := metalRequest(t, "?begin=2010-01-01&end=2010-01-10", `{"time":[["2016-12-19",261,262,260,263]]}`)
	require.NotNil(t, response.Latest)
	require.Empty(t, response.Results)
	response = metalRequest(t, "", `{"time":[]}`)
	require.Nil(t, response.Latest)
	require.NotNil(t, response.Results)
	require.Empty(t, response.Results)
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	require.Equal(t, today, response.End)
}

func TestMetalDefaultRangeRelativeToEnd(t *testing.T) {
	response := metalRequest(t, "?end=2016-12-31", `{"time":[]}`)
	require.Equal(t, "2016-12-01", response.Begin)
	require.Equal(t, "2016-12-31", response.End)
}

func TestMetalRequiresAuthAndValidDates(t *testing.T) {
	rr := httptest.NewRecorder()
	(&desktopServer{token: "secret"}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metal", nil))
	require.Equal(t, http.StatusForbidden, rr.Code)
	for _, query := range []string{
		"?begin=20160101", "?end=invalid", "?begin=2026-02-30", "?begin=2017-01-01&end=2016-01-01",
	} {
		rr := httptest.NewRecorder()
		(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metal"+query, nil))
		require.Equal(t, http.StatusBadRequest, rr.Code, query)
	}
}

func TestMetalUpstreamFailure(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").Reply(500).BodyString("接口暂时不可用")
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/metal", nil))
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "黄金行情获取失败")
	require.True(t, gock.IsDone())
}
