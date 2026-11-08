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
	"github.com/stretchr/testify/require"
)

func aiSettingsRequest(t *testing.T, srv *desktopServer) aiSettings {
	t.Helper()
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/ai", nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NotContains(t, rr.Body.String(), "enc:v1:", "不返回加密的 Key")
	var got aiSettings
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	return got
}

func TestAISettingsEndpoint(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(dir, "workspace.json")}}

	got := aiSettingsRequest(t, srv)
	require.False(t, got.Enabled)
	require.NotNil(t, got.Profiles, "没有配置时是空数组，不是 null")
	require.Empty(t, got.Profiles)

	secret, err := srv.aiSecret()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "secret.key"), secret.Path(), "测试时不读写真实的 ~/.sec")
	var ai config.AIConfig
	require.NoError(t, ai.Add("local", config.AIProfile{Provider: "ollama", Model: "qwen3"}))
	require.NoError(t, ai.Add("ds", config.AIProfile{Provider: "deepseek", Model: "deepseek-chat"}))
	_, err = ai.SetKey(secret, "ds", "sk-test-abcdef123456")
	require.NoError(t, err)
	require.NoError(t, ai.SetEnabled(true))
	require.NoError(t, srv.saveConfig(context.Background(), config.Config{SECEmail: "me@example.org", AI: ai}))

	got = aiSettingsRequest(t, srv)
	require.True(t, got.Enabled)
	require.Equal(t, "local", got.Default)
	require.Equal(t, []string{"ds", "local"}, []string{got.Profiles[0].Name, got.Profiles[1].Name})
	require.Equal(t, "加密保存（末 4 位 3456）", got.Profiles[0].Key)
	require.Equal(t, "DeepSeek", got.Profiles[0].ProviderName)
	require.True(t, got.Profiles[1].Local)
	require.True(t, got.Profiles[1].Default)
	require.Empty(t, got.Invalid)

	// ai 段无效：按没有配置显示，并给出原因。
	path, err := srv.configPath()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":"me@example.org","ai":{"enabled":"yes"}}`), 0o600))
	got = aiSettingsRequest(t, srv)
	require.False(t, got.Enabled)
	require.Empty(t, got.Profiles)
	require.Contains(t, got.Invalid, "ai 段无效")

	// 邮箱也无效：只显示 ai 段的问题。
	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":"me","ai":{"enabled":"yes"}}`), 0o600))
	got = aiSettingsRequest(t, srv)
	require.Contains(t, got.Invalid, "ai 段无效")
	require.NotContains(t, got.Invalid, "SEC 联系邮箱")

	// 整个文件不是 JSON：不能显示成没有配置。
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{"profiles":{}},}`), 0o600))
	got = aiSettingsRequest(t, srv)
	require.Empty(t, got.Profiles)
	require.Contains(t, got.Invalid, "不是有效的 JSON 对象")
	require.Contains(t, got.Invalid, "读不到模型配置")

	// 只有邮箱无效：不影响模型配置，也不提示。
	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":"me"}`), 0o600))
	require.Empty(t, aiSettingsRequest(t, srv).Invalid)
}

func aiChange(t *testing.T, srv *desktopServer, method, path, body string) (int, aiSettings, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	var got aiSettings
	if rr.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	}
	return rr.Code, got, rr.Body.String()
}

func TestAIProfileChanges(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	dir := t.TempDir()
	srv := &desktopServer{dev: true, workspace: workspaceStore{path: filepath.Join(dir, "workspace.json")}}

	code, got, _ := aiChange(t, srv, http.MethodPost, "/api/v1/ai/profiles", `{"name":"ds","provider":"deepseek","model":"deepseek-chat"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "ds", got.Default, "第一个配置是 default")
	require.False(t, got.Enabled)
	require.Contains(t, got.Notice, "DEEPSEEK_API_KEY")
	require.NotEmpty(t, got.Providers)

	code, _, body := aiChange(t, srv, http.MethodPost, "/api/v1/ai/profiles", `{"name":"ds","provider":"deepseek","model":"m"}`)
	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, body, "已存在")
	code, _, body = aiChange(t, srv, http.MethodPost, "/api/v1/ai/profiles", `{"name":"gw","provider":"custom","model":"m"}`)
	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, body, "必须填接口协议")
	code, _, _ = aiChange(t, srv, http.MethodPost, "/api/v1/ai/profiles", `{"name":"x","provider":"deepseek","model":"m","api_key":"sk-plain"}`)
	require.Equal(t, http.StatusBadRequest, code, "不接受随配置提交的 Key")

	code, got, _ = aiChange(t, srv, http.MethodPost, "/api/v1/ai/profiles", `{"name":"local","provider":"ollama","model":"qwen3"}`)
	require.Equal(t, http.StatusOK, code)
	require.Empty(t, got.Notice, "本机模型没有 Key 提示")

	// 编辑：保存的 Key 在地址不变时保留，换地址时删除。
	secret, err := srv.aiSecret()
	require.NoError(t, err)
	require.NoError(t, srv.updateConfig(context.Background(), func(c *config.Config) error {
		_, err := c.AI.SetKey(secret, "ds", "sk-test-abcdef123456")
		return err
	}))
	code, got, _ = aiChange(t, srv, http.MethodPut, "/api/v1/ai/profiles/ds", `{"provider":"deepseek","model":"deepseek-reasoner"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "deepseek-reasoner", got.Profiles[0].Model)
	require.Contains(t, got.Profiles[0].Key, "加密保存")
	require.Empty(t, got.Notice)
	code, got, _ = aiChange(t, srv, http.MethodPut, "/api/v1/ai/profiles/ds", `{"provider":"deepseek","api":"anthropic","base_url":"https://api.deepseek.com/anthropic","model":"deepseek-reasoner"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "anthropic", got.Profiles[0].API)
	require.Contains(t, got.Notice, "已删除")
	require.Equal(t, "未设置", got.Profiles[0].Key)
	code, _, _ = aiChange(t, srv, http.MethodPut, "/api/v1/ai/profiles/nosuch", `{"provider":"deepseek","model":"m"}`)
	require.Equal(t, http.StatusBadRequest, code)

	// 设为默认、启用、删除。
	code, _, body = aiChange(t, srv, http.MethodDelete, "/api/v1/ai/profiles/ds", "")
	require.Equal(t, http.StatusBadRequest, code)
	require.Contains(t, body, "默认配置")
	require.NotContains(t, body, "edit")
	code, got, _ = aiChange(t, srv, http.MethodPut, "/api/v1/ai/default", `{"name":"local"}`)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, "local", got.Default)
	code, got, _ = aiChange(t, srv, http.MethodPut, "/api/v1/ai/enabled", `{"enabled":true}`)
	require.Equal(t, http.StatusOK, code)
	require.True(t, got.Enabled)
	code, got, _ = aiChange(t, srv, http.MethodDelete, "/api/v1/ai/profiles/ds", "")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, got.Profiles, 1)
	require.NoError(t, srv.updateConfig(context.Background(), func(c *config.Config) error {
		_, err := c.AI.SetKey(secret, "local", "sk-local-abcdef123456")
		return err
	}))
	code, got, _ = aiChange(t, srv, http.MethodDelete, "/api/v1/ai/profiles/local", "")
	require.Equal(t, http.StatusOK, code)
	require.False(t, got.Enabled)
	require.Contains(t, got.Notice, "AI 功能已关闭")
	require.Contains(t, got.Notice, "Key 也一并删除", "删除最后一个配置时两条提示都有")
	code, _, _ = aiChange(t, srv, http.MethodPut, "/api/v1/ai/enabled", `{"enabled":true}`)
	require.Equal(t, http.StatusBadRequest, code, "没有配置时不能启用")

	// 其他配置保留。
	cfg, err := srv.loadConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, config.DefaultSECEmail, cfg.SECEmail)
}

func TestAIProfileChangesRefused(t *testing.T) {
	dir := t.TempDir()
	srv := &desktopServer{workspace: workspaceStore{path: filepath.Join(dir, "workspace.json")}}
	path, err := srv.configPath()
	require.NoError(t, err)

	// 不是 JSON 或跨站：不修改。
	req := httptest.NewRequest(http.MethodPut, "/api/v1/ai/enabled", strings.NewReader(`{"enabled":true}`))
	req.AddCookie(&http.Cookie{Name: "token", Value: srv.token})
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnsupportedMediaType, rr.Code)
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/ai/profiles/ds", nil)
	req.AddCookie(&http.Cookie{Name: "token", Value: srv.token})
	req.Header.Set("Origin", "https://evil.example")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	require.Equal(t, http.StatusForbidden, rr.Code)

	// 整个文件不是 JSON、ai 段无效：不覆盖文件。
	srv.dev = true
	for _, bad := range []string{`{"ai":{"profiles":{"ds":{}}},}`, `{"ai":{"enabled":"yes"}}`} {
		require.NoError(t, os.WriteFile(path, []byte(bad), 0o600))
		code, _, body := aiChange(t, srv, http.MethodPost, "/api/v1/ai/profiles", `{"name":"ds","provider":"deepseek","model":"m"}`)
		require.Equal(t, http.StatusConflict, code, body)
		code, _, body = aiChange(t, srv, http.MethodPut, "/api/v1/ai/enabled", `{"enabled":false}`)
		require.Equal(t, http.StatusConflict, code, "关闭也不能报成功：%s", body)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, bad, string(data), "文件没有被改写")
	}
}
