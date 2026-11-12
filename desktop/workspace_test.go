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
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
)

func TestWorkspacePersistsAcrossServers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	state := defaultWorkspace()
	state.Settings.RefreshSeconds = 30
	state.Settings.Theme = "light"
	// 左侧菜单的隐藏状态存在工作区里，换端口重启后仍然保留（localStorage 做不到）。
	state.Settings.SidebarHidden = true
	state.Watchlist = []sina.BasicSecurity{{Name: "招商银行", Code: "600036", ExCode: "SH600036", ExChange: "sse", SecurityType: types.SecurityTypeStock}}
	data, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
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
	state.Settings.SECEmail = config.DefaultSECEmail
	expected, err := json.Marshal(state)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), rr.Body.String(), "没有共享配置时返回默认 SEC 联系邮箱")
}

// SEC 联系邮箱保存到和 CLI 共用的 config.json，不写入 workspace.json；留空表示不修改。
func TestWorkspaceSECEmailSharedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: path}}
	put := func(body string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/workspace", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		return rr.Code
	}
	require.Equal(t, http.StatusOK, put(`{"watchlist":[],"settings":{"refresh_seconds":5,"theme":"dark","sec_edgar_email":"me@example.org"}}`))
	cfg, err := config.LoadFile(context.Background(), filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	require.Equal(t, "me@example.org", cfg.SECEmail)
	workspace, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(workspace), "sec_edgar_email")

	require.Equal(t, http.StatusOK, put(`{"watchlist":[],"settings":{"refresh_seconds":15,"theme":"dark"}}`))
	cfg, err = config.LoadFile(context.Background(), filepath.Join(dir, "config.json"))
	require.NoError(t, err)
	require.Equal(t, "me@example.org", cfg.SECEmail, "留空不修改")

	require.Equal(t, http.StatusBadRequest, put(`{"watchlist":[],"settings":{"refresh_seconds":5,"theme":"dark","sec_edgar_email":"bad"}}`))
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

// 旧的工作区文件没有 sidebar_hidden，读出来按展开处理。
func TestWorkspaceWithoutSidebarSetting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workspace.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"watchlist":[],"settings":{"refresh_seconds":5,"theme":"dark"}}`), 0600))
	state, err := (&workspaceStore{path: path}).load()
	require.NoError(t, err)
	require.False(t, state.Settings.SidebarHidden)
}
