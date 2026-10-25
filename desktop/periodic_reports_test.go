//go:build windows || darwin

package main

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestPeriodicReportKindsAndCurrentYear(t *testing.T) {
	year := time.Now().In(time.FixedZone("CST", 8*3600)).Year()
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	for _, tc := range []struct{ kind, title, category string }{
		{"halfyear", "半年度报告", "category_bndbg_szsh"},
		{"q1", "第一季度报告", "category_yjdbg_szsh"},
		{"q3", "第三季度报告全文", "category_sjdbg_szsh"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			defer gock.Off()
			gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").
				MatchParam("category", tc.category).MatchParam("searchkey", strconv.Itoa(year)).
				MatchParam("seDate", fmt.Sprintf("%d-01-01~%s", year, today)).
				Reply(200).JSON(map[string]any{"announcements": []map[string]any{
				{"announcementTitle": fmt.Sprintf("公司：%d年%s", year, tc.title), "adjunctUrl": "finalpage/2026-05-01/1.pdf"},
				{"announcementTitle": fmt.Sprintf("%d年%s摘要", year, tc.title), "adjunctUrl": "finalpage/2026-05-01/2.pdf"},
				{"announcementTitle": fmt.Sprintf("关于%d年%s的说明", year, tc.title), "adjunctUrl": "finalpage/2026-05-01/3.pdf"},
				{"announcementTitle": fmt.Sprintf("%d年年度报告", year), "adjunctUrl": "finalpage/2026-05-01/4.pdf"},
			}})
			rows, _, err := queryPeriodicReports(context.Background(), "600036,org", year, year, 1, tc.kind)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, year, rows[0].Year)
			require.Equal(t, tc.kind, rows[0].Type)
			_, err = periodicReportYear(strconv.Itoa(year), tc.kind)
			require.NoError(t, err)
			require.True(t, gock.IsDone())
		})
	}
	_, err := periodicReportYear(strconv.Itoa(year), "annual")
	require.Error(t, err)
	response := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/annual-reports?code=SH600036&year=2024&type=invalid", nil))
	require.Equal(t, 400, response.Code)
}

func TestPeriodicDownloadCurrentYear(t *testing.T) {
	defer gock.Off()
	year := strconv.Itoa(time.Now().In(time.FixedZone("CST", 8*3600)).Year())
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-05-01/1.pdf").Reply(200).BodyString("%PDF-1.7\nreport")
	s := &desktopServer{dev: true, downloadRoot: t.TempDir()}
	body := fmt.Sprintf(`{"code":"SH600036","year":%q,"type":"q1","url":"https://static.cninfo.com.cn/finalpage/2026-05-01/1.pdf"}`, year)
	request := httptest.NewRequest("POST", "/api/v1/annual-reports/download", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), year+"_q1_1.pdf")
	files, err := os.ReadDir(s.downloadRoot)
	require.NoError(t, err)
	require.Len(t, files, 1)
	// 中途失败不应发布半份报告，复用相同保存机制。
	_, err = s.saveDownload("failed.pdf", func(w io.Writer) error { _, _ = io.WriteString(w, "%PDF-"); return context.Canceled })
	require.Error(t, err)
	files, err = os.ReadDir(s.downloadRoot)
	require.NoError(t, err)
	require.Len(t, files, 1)
}
