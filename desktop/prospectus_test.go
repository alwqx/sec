//go:build windows || darwin

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestProspectusQueryFiltersAndPagination(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").
		MatchParam("stock", "300750,org").MatchParam("category", "category_sf_szsh").
		MatchParam("pageNum", "2").MatchParam("pageSize", "30").MatchParam("seDate", "2018-01-01~2018-12-31").
		Reply(200).JSON(map[string]any{"hasMore": true, "announcements": []map[string]any{
		{"announcementId": "1", "announcementTitle": "<em>招股</em>说明书 &amp; 附件", "adjunctUrl": "finalpage/2018-06-01/123.pdf", "announcementTime": 1527782400000},
		{"announcementId": "2", "announcementTitle": "发行公告", "adjunctUrl": "http://static.cninfo.com.cn/finalpage/2018-06-01/124.PDF"},
		{"announcementId": "3", "announcementTitle": "重复", "adjunctUrl": "finalpage/2018-06-01/123.pdf"},
		{"announcementTitle": "取消", "existFlag": 1, "adjunctUrl": "finalpage/2018-06-01/125.pdf"},
		{"announcementTitle": "失效", "invalidationFlag": 1, "adjunctUrl": "finalpage/2018-06-01/126.pdf"},
		{"announcementTitle": "外部", "adjunctUrl": "https://example.com/file.pdf"},
	}})
	rows, more, err := queryProspectuses(context.Background(), "300750,org", "2018-01-01", "2018-12-31", 2)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "招股说明书 & 附件", rows[0].Title)
	require.Equal(t, "2018-06-01", rows[0].Date)
	require.Equal(t, "https://static.cninfo.com.cn/finalpage/2018-06-01/124.PDF", rows[1].URL)
	require.True(t, more)
	require.True(t, gock.IsDone())
}

func TestProspectusQueryFailureAndEmpty(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").Reply(200).JSON(map[string]any{"announcements": []any{}})
	rows, more, err := queryProspectuses(context.Background(), "300750,org", "1990-01-01", "2024-01-01", 1)
	require.NoError(t, err)
	require.NotNil(t, rows)
	require.Empty(t, rows)
	require.False(t, more)
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").Reply(500)
	_, _, err = queryProspectuses(context.Background(), "300750,org", "1990-01-01", "2024-01-01", 1)
	require.Error(t, err)
}

func TestProspectusValidationAndAuth(t *testing.T) {
	for _, query := range []string{"code=HK00700", "code=../300750", "code=300750&page=0", "code=300750&begin=2024-02-30", "code=300750&begin=2024-02-01&end=2024-01-01", "code=300750&end=2999-01-01"} {
		response := httptest.NewRecorder()
		(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/ipo/prospectuses?"+query, nil))
		require.Equal(t, 400, response.Code, query)
	}
	for _, endpoint := range []string{"/api/v1/ipo/prospectuses?code=300750", "/api/v1/ipo/prospectuses/download"} {
		method := "GET"
		if strings.HasSuffix(endpoint, "download") {
			method = "POST"
		}
		response := httptest.NewRecorder()
		(&desktopServer{token: "secret"}).Handler().ServeHTTP(response, httptest.NewRequest(method, endpoint, nil))
		require.Equal(t, 403, response.Code)
	}
	require.Equal(t, "300750", normalizeProspectusCode(" sz300750 "))
}

func TestProspectusDownloadPublication(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, downloadRoot: t.TempDir()}
	url := "https://static.cninfo.com.cn/finalpage/2018-06-01/123.pdf"
	for index, content := range []string{"%PDF-1.7\ncomplete", "%PDF-1.7\ncomplete", "<html>not a PDF</html>"} {
		gock.New("https://static.cninfo.com.cn").Get("/finalpage/2018-06-01/123.pdf").Reply(200).BodyString(content)
		body, err := json.Marshal(map[string]string{"code": "SZ300750", "url": url})
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ipo/prospectuses/download", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		if index == 2 {
			require.Equal(t, 502, response.Code)
			continue
		}
		require.Equal(t, 200, response.Code, response.Body.String())
		var saved savedDownload
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
		data, err := os.ReadFile(saved.Path)
		require.NoError(t, err)
		require.Equal(t, content, string(data))
		if index == 1 {
			require.Equal(t, "300750_ipo_123_2.pdf", saved.Filename)
		}
	}
	files, err := os.ReadDir(s.downloadRoot)
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.True(t, gock.IsDone())
}

func TestProspectusDownloadRejectsUnsafeRequests(t *testing.T) {
	s := &desktopServer{token: "secret", downloadRoot: t.TempDir()}
	for _, tc := range []struct {
		body, origin string
		status       int
	}{
		{`{"code":"300750","url":"https://example.com/file.pdf"}`, "", 400},
		{`{"code":"../300750","url":"https://static.cninfo.com.cn/finalpage/2018-06-01/123.pdf"}`, "", 400},
		{`{"code":"300750","url":"https://static.cninfo.com.cn/finalpage/2018-06-01/123.pdf"}`, "https://external.example", 403},
	} {
		request := httptest.NewRequest("POST", "/api/v1/ipo/prospectuses/download", strings.NewReader(tc.body))
		request.AddCookie(&http.Cookie{Name: "token", Value: "secret"})
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", tc.origin)
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		require.Equal(t, tc.status, response.Code)
	}
	files, err := os.ReadDir(s.downloadRoot)
	require.NoError(t, err)
	require.Empty(t, files)
}
