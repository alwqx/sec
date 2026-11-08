//go:build windows || darwin

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
