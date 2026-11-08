//go:build windows || darwin

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func filingsRequest(t *testing.T, srv *desktopServer, query string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/filings?"+query, nil))
	return rr
}

const testSubmissions = `{"cik":"320193","name":"Apple Inc.","tickers":["AAPL"],"exchanges":["Nasdaq"],"sicDescription":"Electronic Computers","fiscalYearEnd":"0926",
"filings":{"recent":{"accessionNumber":["0000320193-26-000026","0000320193-26-000024","0001140361-26-041201"],"filingDate":["2026-11-03","2026-11-02","2026-10-29"],
"reportDate":["2026-09-26","2026-11-02",""],"form":["10-K","8-K","4"],"size":[1,1,1],"primaryDocument":["aapl-20260926.htm","aapl-20261102.htm","form4.xml"],
"primaryDocDescription":["10-K","8-K","FORM 4"],"items":["","2.02,9.01",""]},"files":[]}}`

// 邮箱来自共享配置；按类别筛选并分页。
func TestFilingsEndpoint(t *testing.T) {
	defer gock.Off()
	t.Cleanup(func() { filingsCache.entries = map[string]filingsEntry{} })
	t.Setenv("HOME", t.TempDir())
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	require.NoError(t, srv.saveConfig(context.Background(), config.Config{SECEmail: "me@example.org"}))
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").MatchHeader("User-Agent", `me@example\.org$`).
		Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").MatchHeader("User-Agent", `me@example\.org$`).Times(2).Reply(200).BodyString(testSubmissions)

	rr := filingsRequest(t, srv, "code=$AAPL")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Company *edgar.Profile `json:"company"`
		Total   int            `json:"total"`
		Results []edgar.Filing `json:"results"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "Apple Inc.", data.Company.Name)
	require.Equal(t, 2, data.Total, "默认不含 Form 4")
	require.Equal(t, "重大事项：经营业绩", data.Results[1].Description)

	rr = filingsRequest(t, srv, "code=$AAPL&type=insider")
	require.Equal(t, http.StatusOK, rr.Code)
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, 1, data.Total)
	require.Equal(t, "内部人交易", data.Results[0].Description)
	require.True(t, gock.IsDone())

	// 5 分钟内翻页或重复查询走缓存，不再请求 SEC。
	rr = filingsRequest(t, srv, "code=$AAPL&page=2")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Empty(t, data.Results)
	require.Equal(t, 2, data.Total)
}

// 共享配置文件写坏时，工作台照常打开，用默认邮箱；在设置里保存可以修好。
func TestWorkspaceWithInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(dir, "workspace.json")}}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"sec_edgar_email":"me"}`), 0600))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.Contains(t, rr.Body.String(), config.DefaultSECEmail)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspace", strings.NewReader(`{"watchlist":[],"settings":{"refresh_seconds":5,"theme":"dark","sec_edgar_email":"fixed@example.org"}}`))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	cfg, err := config.LoadFile(context.Background(), filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	require.Equal(t, "fixed@example.org", cfg.SECEmail)
}

// ai 段无效时照常保存工作台和邮箱，ai 段原样保留。
func TestWorkspaceWithInvalidAIConfig(t *testing.T) {
	dir := t.TempDir()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(dir, "workspace.json")}}
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":"me@example.org","ai":{"enabled":"yes","profiles":{"x":{"api_key":"enc:v1:keep"}}}}`), 0600))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspace", strings.NewReader(`{"watchlist":[],"settings":{"refresh_seconds":5,"theme":"dark","sec_edgar_email":"new@example.org"}}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	cfg, err := config.LoadFile(context.Background(), path)
	require.ErrorIs(t, err, config.ErrInvalidAI, "ai 段仍然无效，没有被覆盖")
	require.Equal(t, "new@example.org", cfg.SECEmail)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "enc:v1:keep")
}

func TestFilingsEndpointErrors(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	require.Equal(t, http.StatusBadRequest, filingsRequest(t, srv, "code=SH600036").Code)
	require.Equal(t, http.StatusBadRequest, filingsRequest(t, srv, "code=$AAPL&type=x").Code)
	rr := filingsRequest(t, srv, "code=$AAPL&page=0")
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Contains(t, rr.Body.String(), "页码必须在 1 到 10000 之间")
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	require.Equal(t, http.StatusNotFound, filingsRequest(t, srv, "code=$ZZZZ").Code)
}
