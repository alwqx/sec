package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		Enabled  bool                   `json:"enabled"`
		Default  string                 `json:"default"`
		Profiles []config.AIProfileView `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &listed))
	require.True(t, listed.Enabled)
	require.Equal(t, "ds", listed.Default)
	require.Equal(t, config.AIProfileView{Name: "ds", Default: true, Provider: "deepseek", ProviderName: "DeepSeek", API: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", Key: "厂商环境变量 DEEPSEEK_API_KEY（末 4 位 1234）"}, listed.Profiles[0])
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

// runIn 和 run 一样，stdin 用 input（不是终端，set-key 读一行）。
func runIn(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	cmd := NewConfigCLI()
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(input))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestConfigAISetKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DEEPSEEK_API_KEY", "")
	_, err := run(t, "ai", "add", "ds", "--provider", "deepseek", "--model", "m", "--api-key-env", "MY_KEY")
	require.NoError(t, err)

	_, err = runIn(t, "sk-abcdefghijkl1234\n", "ai", "set-key", "nosuch")
	require.ErrorIs(t, err, config.ErrAIProfileNotFound, "名字写错时不读 Key")
	_, err = runIn(t, "", "ai", "set-key", "ds")
	require.ErrorContains(t, err, "没有读到 Key")
	_, err = runIn(t, "sk-abcdefghijkl1234\n", "ai", "set-key", "ds", "sk-extra")
	require.Error(t, err, "不接受命令行参数里的 Key")

	_, err = runIn(t, "sk-abcdefghijkl1234\nsecond line\n", "ai", "set-key", "ds")
	require.ErrorContains(t, err, "多行内容", "传错了多行内容时不保存")
	_, err = runIn(t, strings.Repeat("k", maxKeyLength+10), "ai", "set-key", "ds")
	require.ErrorContains(t, err, "超过 4096 字节", "太长时不截断保存")

	output, err := runIn(t, "  sk-abcdefghijkl1234\r\n\n  \n", "ai", "set-key", "ds")
	require.NoError(t, err, "后面的空行可以有")
	require.Contains(t, output, "已保存 ds 的 Key：加密保存（末 4 位 1234）")
	require.Contains(t, output, "已不再从环境变量 MY_KEY 读取 Key")
	require.Contains(t, output, "已生成加密密钥", "第一次生成密钥时提醒")
	require.Contains(t, output, "不要和 config.json 一起同步或备份")
	require.NotContains(t, output, "abcdefghijkl")

	data, err := os.ReadFile(filepath.Join(home, ".sec", "config.json"))
	require.NoError(t, err)
	require.NotContains(t, string(data), "sk-abcdefghijkl1234", "文件里只有密文")
	require.Contains(t, string(data), `"api_key": "enc:v1:`)
	require.NotContains(t, string(data), "MY_KEY")
	info, err := os.Stat(filepath.Join(home, ".sec", "secret.key"))
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	output, err = run(t, "ai", "list")
	require.NoError(t, err)
	require.Contains(t, output, "加密保存（末 4 位 1234）")

	output, err = runIn(t, "sk-zyxwvutsrqpo9876", "ai", "set-key", "ds")
	require.NoError(t, err, "没有换行的最后一行也能读")
	require.Contains(t, output, "末 4 位 9876")
	require.NotContains(t, output, "已生成加密密钥", "已有密钥时不重复提醒")

	// 密钥损坏后被替换：同样提醒。
	require.NoError(t, os.WriteFile(filepath.Join(home, ".sec", "secret.key"), []byte("short"), 0o600))
	output, err = runIn(t, "sk-zyxwvutsrqpo9876", "ai", "set-key", "ds")
	require.NoError(t, err)
	require.Contains(t, output, "已生成加密密钥", "替换了损坏的密钥也提醒")

	// unset-key 后说明现在用的是什么 Key。
	t.Setenv("DEEPSEEK_API_KEY", "sk-provider-55555555")
	output, err = run(t, "ai", "unset-key", "ds")
	require.NoError(t, err)
	require.Contains(t, output, "已删除 ds 保存的 Key")
	require.Contains(t, output, "现在的 Key：厂商环境变量 DEEPSEEK_API_KEY（末 4 位 5555）")
	output, err = run(t, "ai", "unset-key", "ds")
	require.NoError(t, err)
	require.Contains(t, output, "ds 没有保存的 Key")
	_, err = run(t, "ai", "unset-key", "nosuch")
	require.ErrorIs(t, err, config.ErrAIProfileNotFound)

	// 配置文件根本不是 JSON：找不到配置时把读取的问题一起说明。
	writeConfig(t, home, `{not json`)
	_, err = runIn(t, "sk-abcdefghijkl1234\n", "ai", "set-key", "ds")
	require.ErrorIs(t, err, config.ErrAIProfileNotFound)
	require.ErrorContains(t, err, "读取配置时有问题")

	// ai 段无效时同样先报原因。
	writeConfig(t, home, `{"ai":{"default":"x"}}`)
	_, err = runIn(t, "sk-abcdefghijkl1234\n", "ai", "set-key", "ds")
	require.ErrorIs(t, err, config.ErrInvalidAI)
	_, err = run(t, "ai", "unset-key", "ds")
	require.ErrorIs(t, err, config.ErrInvalidAI)
}
