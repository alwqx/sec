package aicli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestError(t *testing.T) {
	err := Error("公告摘要", fmt.Errorf("分块：%w", &llm.APIError{Provider: "DeepSeek", Status: 401, Message: "invalid key sk-secret"}))
	require.EqualError(t, err, "公告摘要失败：模型接口返回 HTTP 401，请检查模型配置或稍后重试")
	cause := errors.New("offline")
	err = Error("申报摘要", cause)
	require.EqualError(t, err, "申报摘要失败：offline")
	require.ErrorIs(t, err, cause)
}

func TestPrintSummaryFooter(t *testing.T) {
	var out bytes.Buffer
	PrintSummaryFooter(&out, llm.Check{Numbers: 2, Unsourced: []llm.NumberRef{{Text: "12%"}}}, []string{"说明一", "说明二"}, "免责声明")
	text := out.String()
	for _, want := range []string{"核对：核对 2 个数字，1 个未找到来源", "核对：以下数字未在数据中找到：12%", "说明一\n说明二\n免责声明\n"} {
		require.Contains(t, text, want)
	}
}

func command() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("ai", "", "")
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	return cmd
}

func TestOpenConfigErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_, err := Open(command(), "filings")
	require.ErrorIs(t, err, llm.ErrNoProfile)

	path := filepath.Join(home, ".sec", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{"enabled":"yes"}}`), 0o600))
	_, err = Open(command(), "filings")
	require.ErrorIs(t, err, config.ErrInvalidAI)
	require.ErrorContains(t, err, "请先手动修正 ai 段")

	require.NoError(t, os.WriteFile(path, []byte(`not json`), 0o600))
	_, err = Open(command(), "filings")
	require.ErrorIs(t, err, config.ErrInvalid)
	require.ErrorContains(t, err, "读不到模型配置")
}
