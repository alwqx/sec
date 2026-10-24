//go:build windows || darwin

package main

import (
	"encoding/json"
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

func TestWorkspacePersistsAcrossServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	state := defaultWorkspace()
	state.Settings.RefreshSeconds = 30
	state.Settings.Theme = "light"
	state.Watchlist = []sina.BasicSecurity{{Name: "招商银行", Code: "600036", ExCode: "SH600036", ExChange: "sse", SecurityType: types.SecurityTypeStock}}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	srv := &desktopServer{token: "secret", workspace: workspaceStore{path: path}}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/workspace", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "token", Value: "secret"})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	restarted := &desktopServer{dev: true, workspace: workspaceStore{path: path}}
	rr = httptest.NewRecorder()
	restarted.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	require.JSONEq(t, string(data), rr.Body.String())
}

func TestWorkspaceRejectsInvalidWritesWithoutReplacingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	store := workspaceStore{path: path}
	require.NoError(t, store.save(defaultWorkspace()))
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	srv := &desktopServer{token: "secret", workspace: workspaceStore{path: path}}
	for _, tc := range []struct {
		name, body, origin, contentType string
		token                           bool
		status                          int
	}{
		{"未鉴权", string(original), "", "application/json", false, 403},
		{"跨站", string(original), "https://example.com", "application/json", true, 403},
		{"错误类型", string(original), "", "text/plain", true, 415},
		{"不合法 JSON", "{", "", "application/json", true, 400},
		{"刷新间隔无效", `{"watchlist":[],"settings":{"refresh_seconds":0,"theme":"dark"}}`, "", "application/json", true, 400},
		{"主题无效", `{"watchlist":[],"settings":{"refresh_seconds":5,"theme":"unknown"}}`, "", "application/json", true, 400},
		{"多对象", string(original) + ` {}`, "", "application/json", true, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/v1/workspace", strings.NewReader(tc.body))
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Content-Type", tc.contentType)
			if tc.token {
				req.AddCookie(&http.Cookie{Name: "token", Value: "secret"})
			}
			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, req)
			require.Equal(t, tc.status, rr.Code)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, data)
		})
	}
}

func TestWorkspaceMissingAndCorruptFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	store := workspaceStore{path: path}
	state, err := store.load()
	require.NoError(t, err)
	require.Equal(t, defaultWorkspace(), state)
	require.NoError(t, os.WriteFile(path, []byte(`broken`), 0600))
	_, err = store.load()
	require.Error(t, err)
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: path}}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil))
	require.Equal(t, http.StatusInternalServerError, rr.Code)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "broken", string(data))
}
