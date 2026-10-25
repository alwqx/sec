//go:build windows || darwin

package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestConfiguredDownloadDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	for _, value := range []string{"", "~/Downloads"} {
		directory, err := downloadDirectory(value)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, "Downloads"), directory)
	}
	_, err = downloadDirectory("relative/path")
	require.Error(t, err)
	root := t.TempDir()
	store := workspaceStore{path: filepath.Join(root, "workspace.json")}
	state := defaultWorkspace()
	state.Settings.DownloadDirectory = filepath.Join(root, "custom downloads", "reports")
	require.True(t, validateWorkspace(state))
	require.NoError(t, store.save(state))
	// 重启后重新读取已保存的目录，并自动创建尚不存在的子目录。
	s := &desktopServer{workspace: workspaceStore{path: store.path}}
	saved, err := s.saveDownload("report.json", func(w io.Writer) error { _, err := io.WriteString(w, "[]"); return err })
	require.NoError(t, err)
	require.Equal(t, filepath.Join(state.Settings.DownloadDirectory, "report.json"), saved.Path)
	state.Settings.DownloadDirectory = ""
	require.NoError(t, store.save(state))
	directory, err := s.downloadDirectory()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(home, "Downloads"), directory)
}

func TestDownloadPublication(t *testing.T) {
	s := &desktopServer{downloadRoot: t.TempDir()}
	write := func(w io.Writer) error { _, err := io.WriteString(w, "complete"); return err }
	first, err := s.saveDownload("report.csv", write)
	require.NoError(t, err)
	second, err := s.saveDownload("report.csv", write)
	require.NoError(t, err)
	require.Equal(t, "report_2.csv", second.Filename)
	body, err := os.ReadFile(first.Path)
	require.NoError(t, err)
	require.Equal(t, "complete", string(body))
	_, err = s.saveDownload("failed.pdf", func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return errors.New("interrupted")
	})
	require.Error(t, err)
	files, err := os.ReadDir(s.downloadRoot)
	require.NoError(t, err)
	require.Len(t, files, 2)
}

func TestFinancialExportRawValues(t *testing.T) {
	items := []*eastmoney.FinancialReportItem{{SecurityCode: "600036", ReportDate: "2025-12-31", Fields: map[string]interface{}{"ASSETS": 123456789.0, "NAME": "=formula", "LOSS": -123.0, "EMPTY": nil}}, {Fields: map[string]interface{}{"EXTRA": "a,b\nc"}}}
	var output bytes.Buffer
	require.NoError(t, writeFinancialExport(&output, items, eastmoney.ReportBalance, "csv"))
	require.True(t, strings.HasPrefix(output.String(), "\ufeff"))
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(output.String(), "\ufeff")))
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"ASSETS", "EMPTY", "EXTRA", "LOSS", "NAME"}, rows[1])
	require.Equal(t, []string{"1.23456789e+08", "", "", "-123", "'=formula"}, rows[2])
	require.Equal(t, "a,b\nc", rows[3][2])
	output.Reset()
	require.NoError(t, writeFinancialExport(&output, items, eastmoney.ReportBalance, "json"))
	var records []map[string]interface{}
	require.NoError(t, json.Unmarshal(output.Bytes(), &records))
	require.Equal(t, 123456789.0, records[0]["fields"].(map[string]interface{})["ASSETS"])
	require.Equal(t, "600036", records[0]["security_code"])
}

func TestAnnualReportYearAndFiltering(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").
		MatchParam("searchkey", "2024").MatchParam("seDate", "2025-01-01~"+time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")).
		Reply(200).JSON(map[string]interface{}{"hasMore": true, "announcements": []map[string]interface{}{
		{"announcementId": "1", "announcementTitle": "公司：<em>2024</em>年年度报告", "adjunctUrl": "finalpage/2025-03-01/123.PDF"},
		{"announcementTitle": "2024年年度报告摘要", "adjunctUrl": "finalpage/2025-03-01/124.pdf"},
		{"announcementTitle": "2023年年度报告", "adjunctUrl": "finalpage/2025-03-01/125.pdf"},
		{"announcementTitle": "2024年年度报告（修订版）", "adjunctUrl": "finalpage/2025-03-01/126.pdf"},
		{"announcementTitle": "关于2024年年度报告的说明", "adjunctUrl": "finalpage/2025-03-01/127.pdf"},
	}})
	results, more, err := queryAnnualReports(context.Background(), "600036,org", 2024, 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, 2024, results[0].Year)
	require.Equal(t, "https://static.cninfo.com.cn/finalpage/2025-03-01/123.PDF", results[0].URL)
	require.True(t, more)
	require.True(t, gock.IsDone())
}

func TestAnnualPDFValidationAndContent(t *testing.T) {
	defer gock.Off()
	url := "https://static.cninfo.com.cn/finalpage/2025-03-01/123.pdf"
	for _, invalid := range []string{"http://static.cninfo.com.cn/finalpage/2025-03-01/123.pdf", url + "?redirect=1", "https://example.com/file.pdf", "https://static.cninfo.com.cn/../../file.pdf"} {
		_, err := validatedPDFURL(invalid)
		require.Error(t, err)
	}
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2025-03-01/123.pdf").Reply(200).BodyString("%PDF-1.7\nbody\n%%EOF")
	var output bytes.Buffer
	require.NoError(t, copyAnnualPDF(context.Background(), url, &output))
	require.Contains(t, output.String(), "%PDF-1.7")
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2025-03-01/123.pdf").Reply(200).BodyString("<html>error</html>")
	output.Reset()
	require.Error(t, copyAnnualPDF(context.Background(), url, &output))
	require.Empty(t, output.String())
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2025-03-01/123.pdf").Reply(302).SetHeader("Location", "https://example.com/file.pdf")
	require.Error(t, copyAnnualPDF(context.Background(), url, &output))
}

func TestDownloadRequestOriginAndAuth(t *testing.T) {
	s := &desktopServer{token: "secret", downloadRoot: t.TempDir()}
	request := httptest.NewRequest("POST", "/api/v1/financials/export", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	require.Equal(t, 403, response.Code)
	request.Header.Set("Origin", "https://external.example")
	response = httptest.NewRecorder()
	var value financialExportRequest
	require.False(t, s.readDownloadRequest(response, request, &value))
	require.Equal(t, 403, response.Code)
}

func TestFinancialExportEndpoint(t *testing.T) {
	defer gock.Off()
	s := &desktopServer{dev: true, downloadRoot: t.TempDir()}
	for _, fixture := range []struct {
		body   string
		status int
	}{
		{`{"success":true,"result":{"data":[{"SECURITY_CODE":"600036","REPORT_DATE":"2024-12-31","TOTAL_ASSETS":123456789}]}}`, 200},
		{`{"success":true,"result":{"data":[]}}`, 404},
		{`{"success":false,"message":"unavailable"}`, 502},
	} {
		gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").
			MatchParam("reportName", string(eastmoney.ReportBalance)).
			MatchParam("filter", regexp.QuoteMeta(`(SECURITY_CODE="600036")(DATE_TYPE_CODE="001")`)).Reply(200).BodyString(fixture.body)
		request := httptest.NewRequest("POST", "/api/v1/financials/export", strings.NewReader(`{"code":"SH600036","report":"balance","period":"annual","format":"json"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		require.Equal(t, fixture.status, response.Code, response.Body.String())
		if response.Code == 200 {
			var saved savedDownload
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &saved))
			body, err := os.ReadFile(saved.Path)
			require.NoError(t, err)
			require.Contains(t, string(body), "123456789")
		}
	}
	files, err := os.ReadDir(s.downloadRoot)
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.True(t, gock.IsDone())
}
