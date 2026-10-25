//go:build windows || darwin

package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestAnnualReportRangeFiltering(t *testing.T) {
	defer gock.Off()
	items := []map[string]any{}
	for _, year := range []int{2021, 2022, 2023, 2024, 2025} {
		items = append(items, map[string]any{"announcementTitle": fmt.Sprintf("公司：%d年年度报告", year), "adjunctUrl": fmt.Sprintf("finalpage/2026-03-01/%d.pdf", year)})
	}
	items = append(items, map[string]any{"announcementTitle": "2023年年度报告摘要", "adjunctUrl": "finalpage/2026-03-01/summary.pdf"})
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").
		MatchParam("category", "category_ndbg_szsh").MatchParam("pageNum", "2").
		Reply(200).JSON(map[string]any{"announcements": items, "hasMore": true})
	rows, more, err := queryAnnualReportRange(context.Background(), "600036,org", 2022, 2024, 2)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	require.Equal(t, 2022, rows[0].Year)
	require.Equal(t, 2024, rows[2].Year)
	require.True(t, more)
	require.True(t, gock.IsDone())
}

func TestAnnualRangeValidation(t *testing.T) {
	for _, query := range []string{"start_year=2024&end_year=2022", "start_year=2022", "end_year=2024", "start_year=1989&end_year=2024", "start_year=2022&end_year=2999", "start_year=bad&end_year=2024"} {
		response := httptest.NewRecorder()
		(&desktopServer{dev: true}).Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/annual-reports?code=SH600036&"+query, nil))
		require.Equal(t, 400, response.Code, query)
	}
}
