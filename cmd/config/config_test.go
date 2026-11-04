package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewConfigCLI()
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestConfigCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	output, err := run(t, "get", "sec-edgar-email")
	require.NoError(t, err)
	require.Equal(t, "sec_dev@example.com\n", output, "没有配置文件时是默认值")

	output, err = run(t, "set", "sec-edgar-email", "me@example.org")
	require.NoError(t, err)
	require.Contains(t, output, "me@example.org")
	data, err := os.ReadFile(filepath.Join(home, ".sec", "config.json"))
	require.NoError(t, err)
	require.Contains(t, string(data), `"sec_edgar_email": "me@example.org"`)

	output, err = run(t)
	require.NoError(t, err)
	require.Contains(t, output, "sec-edgar-email = me@example.org")

	_, err = run(t, "set", "sec-edgar-email", "bad")
	require.ErrorContains(t, err, "SEC 联系邮箱无效")
	_, err = run(t, "set", "theme", "dark")
	require.ErrorContains(t, err, "没有配置项 theme")

	// 配置文件写坏后，查看时用默认值，修改时直接覆盖。
	require.NoError(t, os.WriteFile(filepath.Join(home, ".sec", "config.json"), []byte(`{"sec_edgar_email":"me"}`), 0600))
	output, err = run(t, "get", "sec-edgar-email")
	require.NoError(t, err)
	require.Equal(t, "sec_dev@example.com\n", output)
	_, err = run(t, "set", "sec-edgar-email", "fixed@example.org")
	require.NoError(t, err)
	output, err = run(t, "get", "sec-edgar-email")
	require.NoError(t, err)
	require.Equal(t, "fixed@example.org\n", output)
}
