package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/stretchr/testify/require"
)

func TestConfigAI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEEPSEEK_API_KEY", "")

	output, err := run(t, "ai", "list")
	require.NoError(t, err)
	require.Contains(t, output, "还没有模型配置")

	_, err = run(t, "ai", "add", "ds", "--model", "deepseek-chat")
	require.ErrorContains(t, err, "provider", "厂商必填")
	_, err = run(t, "ai", "add", "gw", "--provider", "custom", "--model", "x")
	require.ErrorContains(t, err, "必须填接口协议")
	_, err = run(t, "ai", "add", "Bad", "--provider", "deepseek", "--model", "m")
	require.ErrorContains(t, err, "配置名无效")

	output, err = run(t, "ai", "add", "ds", "--provider", "deepseek", "--model", "deepseek-chat")
	require.NoError(t, err)
	require.Contains(t, output, "ds 是 default")
	require.Contains(t, output, "DEEPSEEK_API_KEY")
	require.Contains(t, output, "sec config ai enable")
	output, err = run(t, "ai", "add", "local", "--provider", "ollama", "--model", "qwen3")
	require.NoError(t, err)
	require.NotContains(t, output, "是 default")
	_, err = run(t, "ai", "add", "ds", "--provider", "openai", "--model", "m")
	require.ErrorContains(t, err, "已存在")

	// 第一次启用时给隐私提示，再启用不重复。
	output, err = run(t, "ai", "enable")
	require.NoError(t, err)
	require.Contains(t, output, "AI 功能已启用，default 是 ds")
	require.Contains(t, output, "会发送到外部厂商的配置：ds（DeepSeek）")
	require.Contains(t, output, "数据不出本机：local")
	output, err = run(t, "ai", "enable")
	require.NoError(t, err)
	require.NotContains(t, output, "注意")

	t.Setenv("DEEPSEEK_API_KEY", "sk-abcdefghijkl1234")
	output, err = run(t, "ai", "list")
	require.NoError(t, err)
	require.Contains(t, output, "AI 功能：已启用")
	require.Contains(t, output, "ds *")
	require.Contains(t, output, "厂商环境变量 DEEPSEEK_API_KEY（末 4 位 1234）")
	require.NotContains(t, output, "abcdefghijkl", "不显示 Key")
	require.Contains(t, output, "未设置（本机模型可以不填）")

	output, err = run(t, "ai", "list", "--json")
	require.NoError(t, err)
	var listed struct {
		Enabled  bool            `json:"enabled"`
		Default  string          `json:"default"`
		Profiles []aiProfileView `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &listed))
	require.True(t, listed.Enabled)
	require.Equal(t, "ds", listed.Default)
	require.Equal(t, aiProfileView{Name: "ds", Default: true, Provider: "deepseek", ProviderName: "DeepSeek", API: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", Key: "厂商环境变量 DEEPSEEK_API_KEY（末 4 位 1234）"}, listed.Profiles[0])
	require.True(t, listed.Profiles[1].Local)

	// edit 只改给出的参数，空字符串清空。
	_, err = run(t, "ai", "edit", "ds")
	require.ErrorContains(t, err, "没有要修改的项")
	_, err = run(t, "ai", "edit", "ds", "--api", "anthropic")
	require.ErrorContains(t, err, "要填对应的接口地址", "校验失败时不保存")
	output, err = run(t, "ai", "edit", "ds", "--api", "anthropic", "--base-url", "https://api.deepseek.com/anthropic", "--api-key-env", "MY_KEY")
	require.NoError(t, err)
	require.Contains(t, output, "anthropic，https://api.deepseek.com/anthropic")
	cfg, err := config.LoadFile(t.Context(), filepath.Join(home, ".sec", "config.json"))
	require.NoError(t, err)
	require.Equal(t, config.AIProfile{Provider: "deepseek", API: "anthropic", BaseURL: "https://api.deepseek.com/anthropic", Model: "deepseek-chat", APIKeyEnv: "MY_KEY"}, cfg.AI.Profiles["ds"])
	_, err = run(t, "ai", "edit", "ds", "--api", "", "--base-url", "", "--api-key-env", "")
	require.NoError(t, err)
	cfg, err = config.LoadFile(t.Context(), filepath.Join(home, ".sec", "config.json"))
	require.NoError(t, err)
	require.Equal(t, config.AIProfile{Provider: "deepseek", Model: "deepseek-chat"}, cfg.AI.Profiles["ds"])
	_, err = run(t, "ai", "edit", "nosuch", "--model", "m")
	require.ErrorIs(t, err, config.ErrAIProfileNotFound)

	_, err = run(t, "ai", "remove", "ds")
	require.ErrorContains(t, err, "是 default，不能删除")
	output, err = run(t, "ai", "use", "local")
	require.NoError(t, err)
	require.Contains(t, output, "default 已改为 local")
	output, err = run(t, "ai", "remove", "ds")
	require.NoError(t, err)
	require.Contains(t, output, "已删除模型配置 ds")
	require.NotContains(t, output, "Key 也一并删除", "没有保存 Key 时不提")
	output, err = run(t, "ai", "remove", "local")
	require.NoError(t, err)
	require.Contains(t, output, "AI 功能已关闭")
	_, err = run(t, "ai", "enable")
	require.ErrorContains(t, err, "请先添加")
	output, err = run(t, "ai", "disable")
	require.NoError(t, err)
	require.Contains(t, output, "AI 功能已关闭")

	data, err := os.ReadFile(filepath.Join(home, ".sec", "config.json"))
	require.NoError(t, err)
	require.NotContains(t, string(data), `"ai"`, "没有配置时不写 ai 段")
}

func TestConfigAIRemoveStoredKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".sec"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".sec", "config.json"), []byte(`{"ai":{"default":"ds","profiles":{"ds":{"provider":"deepseek","model":"m","api_key":"enc:v1:AAAA"}}}}`), 0o600))
	output, err := run(t, "ai", "remove", "ds")
	require.NoError(t, err)
	require.Contains(t, output, "保存的 Key 也一并删除")
}

func writeConfig(t *testing.T, home, content string) string {
	t.Helper()
	path := filepath.Join(home, ".sec", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestConfigAIInvalidSection(t *testing.T) {
	// ai 段无效：所有子命令都报原因，不做任何修改。
	home := t.TempDir()
	t.Setenv("HOME", home)
	bad := `{"ai":{"enabled":true,"default":"ds","profiles":{"ds":{"provider":"deepseek","model":"m","base_url":"bad"}}}}`
	path := writeConfig(t, home, bad)
	for _, args := range [][]string{
		{"ai", "list"}, {"ai", "list", "--json"}, {"ai", "add", "x", "--provider", "openai", "--model", "m"},
		{"ai", "edit", "ds", "--model", "n"}, {"ai", "use", "ds"}, {"ai", "remove", "ds"}, {"ai", "enable"}, {"ai", "disable"},
	} {
		output, err := run(t, args...)
		require.ErrorIs(t, err, config.ErrInvalidAI, args)
		require.ErrorContains(t, err, "手动修正", args)
		require.NotContains(t, output, "还没有模型配置", args)
	}
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, bad, string(data), "文件不变")
}

func TestConfigAINotices(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeConfig(t, home, `{"ai":{"default":"ds","profiles":{"ds":{"provider":"deepseek","model":"m","api_key":"enc:v1:AAAA"}}}}`)

	// edit 不带参数时先报用法错误，不管配置存不存在。
	_, err := run(t, "ai", "edit", "nosuch")
	require.ErrorContains(t, err, "没有要修改的项")

	// 改地址或厂商时删除保存的 Key 并提示；只改模型时不提。
	output, err := run(t, "ai", "edit", "ds", "--model", "deepseek-reasoner")
	require.NoError(t, err)
	require.NotContains(t, output, "已删除")
	output, err = run(t, "ai", "edit", "ds", "--base-url", "https://llm.other.example/v1")
	require.NoError(t, err)
	require.Contains(t, output, "原来保存的 Key 不会发给新地址，已删除")
	cfg, err := config.LoadFile(t.Context(), path)
	require.NoError(t, err)
	require.Empty(t, cfg.AI.Profiles["ds"].APIKey)

	// 没启用过时删除最后一个配置，不说「已关闭」。
	output, err = run(t, "ai", "remove", "ds")
	require.NoError(t, err)
	require.NotContains(t, output, "AI 功能已关闭")

	// 启用后加入或切换到外部厂商的配置时提醒，本机模型不提醒。
	_, err = run(t, "ai", "add", "local", "--provider", "ollama", "--model", "qwen3")
	require.NoError(t, err)
	_, err = run(t, "ai", "enable")
	require.NoError(t, err)
	output, err = run(t, "ai", "add", "ds", "--provider", "deepseek", "--model", "m")
	require.NoError(t, err)
	require.Contains(t, output, "ds 不是本机模型")
	require.Contains(t, output, "发送给DeepSeek")
	output, err = run(t, "ai", "use", "ds")
	require.NoError(t, err)
	require.Contains(t, output, "ds 不是本机模型")
	output, err = run(t, "ai", "use", "local")
	require.NoError(t, err)
	require.NotContains(t, output, "不是本机模型")
}
