//go:build windows || darwin

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
)

func TestDesktopSharesWatchlistAndSettingsCannotOverwriteIt(t *testing.T) {
	s := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	store, err := s.sharedWatchlist()
	require.NoError(t, err)
	sec := sina.BasicSecurity{Code: "600036", ExCode: "SH600036", Name: "招商银行", SecurityType: types.SecurityTypeStock}
	// 共享存储模拟 CLI 独立进程写入，Desktop 查询应立即看到。
	_, _, err = store.Add(context.Background(), sec)
	require.NoError(t, err)
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/workspace", nil))
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), "SH600036")
	// 空的过期快照不得通过保存设置删掉 CLI 自选。
	request := httptest.NewRequest("PUT", "/api/v1/workspace", strings.NewReader(`{"watchlist":[],"settings":{"refresh_seconds":15,"theme":"light"}}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	s.Handler().ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "SH600036")
	prefs, err := os.ReadFile(s.workspace.path)
	require.NoError(t, err)
	require.NotContains(t, string(prefs), "watchlist")
	// Desktop 增加的证券可经公共存储读取，删除也即时共享。
	for _, action := range []string{"add", "remove"} {
		body := `{"action":"remove","code":"SH600036"}`
		if action == "add" {
			body = `{"action":"add","security":{"code":"510300","ex_code":"SH510300","name":"ETF","security_type":"fund"}}`
		}
		request = httptest.NewRequest("POST", "/api/v1/watchlist", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response = httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	items, err := store.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "SH510300", items[0].ExCode)
	require.Equal(t, types.SecurityTypeFund, items[0].SecurityType)
	_, _, err = store.Remove(context.Background(), "SH510300")
	require.NoError(t, err)
	response = httptest.NewRecorder()
	s.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/watchlist", nil))
	require.JSONEq(t, `{"results":[]}`, response.Body.String())
}

func TestWatchlistMutationsRejectCrossSiteAndInvalidRequests(t *testing.T) {
	s := &desktopServer{token: "secret", workspace: workspaceStore{path: filepath.Join(t.TempDir(), "workspace.json")}}
	for _, tc := range []struct {
		body, origin string
		token        bool
		status       int
	}{
		{`{"action":"remove","code":"SH600036"}`, "", false, 403},
		{`{"action":"remove","code":"SH600036"}`, "https://evil.example", true, 403},
		{`{"action":"replace"}`, "", true, 400},
		{`{"action":"add","security":{"code":"600036","ex_code":"SZ000001","name":"wrong"}}`, "", true, 400},
		{`{"action":"remove","code":"../../file"}`, "", true, 400},
	} {
		request := httptest.NewRequest("POST", "/api/v1/watchlist", strings.NewReader(tc.body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", tc.origin)
		if tc.token {
			request.AddCookie(&http.Cookie{Name: "token", Value: "secret"})
		}
		response := httptest.NewRecorder()
		s.Handler().ServeHTTP(response, request)
		require.Equal(t, tc.status, response.Code, response.Body.String())
	}
}
