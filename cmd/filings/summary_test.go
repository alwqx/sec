package filings

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const cliFilingURL = "https://www.sec.gov/Archives/edgar/data/320193/000032019326000024/aapl-20261102.htm"

func fakeSummarizer(t *testing.T) *llm.Fake {
	t.Helper()
	fake := &llm.Fake{Replies: []llm.FakeReply{llm.FakeText(`{"sections":[{"text":"公司发布季度业绩新闻稿。","refs":["Item 2.02"]}]}`)}}
	old := openSummarizer
	openSummarizer = func(cmd *cobra.Command) (*llm.FilingSummarizer, *config.Config, error) {
		return &llm.FilingSummarizer{Client: fake, Model: llm.Model{Profile: "test", Name: "fake"}, Read: func(ctx context.Context, url string) (*edgar.FilingDocument, error) {
			require.Equal(t, cliFilingURL, url)
			ref, err := edgar.ParseFilingURL(url)
			require.NoError(t, err)
			return &edgar.FilingDocument{FilingRef: *ref, Form: "8-K", FilingDate: "2026-11-02", ReportDate: "2026-11-02", URL: url,
				Sections: []edgar.DocumentSection{{Label: "Item 2.02", URL: url, Text: "Item 2.02 Apple issued a press release."}}}, ctx.Err()
		}}, &config.Config{SECEmail: "sec_dev@example.com"}, nil
	}
	t.Cleanup(func() { openSummarizer = old })
	return fake
}

func TestSummaryDirectLink(t *testing.T) {
	fake := fakeSummarizer(t)
	out, err := run(t, "--summary", cliFilingURL, "--ai", "test", "--json")
	require.NoError(t, err)
	var result llm.FilingSummary
	require.NoError(t, json.Unmarshal([]byte(out), &result), "JSON 模式只输出摘要对象")
	require.Equal(t, "0000320193-26-000024", result.DocumentID)
	require.Contains(t, result.Text, "（Item 2.02）")
	fake.Replies = []llm.FakeReply{llm.FakeText(`{"sections":[{"text":"公司发布季度业绩新闻稿。","refs":["Item 2.02"]}]}`)}
	out, err = run(t, "--summary", cliFilingURL)
	require.NoError(t, err)
	for _, text := range []string{"【AI 申报摘要】8-K 0000320193-26-000024", "申报日 2026-11-02", "模型 fake", "配置 test", "Item 2.02：" + cliFilingURL, "不构成投资建议"} {
		require.Contains(t, out, text)
	}
}

func TestSummaryByRow(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	fakeSummarizer(t)
	mockSEC()
	out, err := run(t, "AAPL", "--summary", "2", "--json")
	require.NoError(t, err)
	require.Contains(t, out, `"document_id": "0000320193-26-000024"`)
	mockSEC()
	_, err = run(t, "AAPL", "--summary", "1")
	require.ErrorContains(t, err, "第 1 条是 10-K，目前只支持摘要 8-K")
	mockSEC()
	_, err = run(t, "AAPL", "--summary", "3")
	require.ErrorContains(t, err, "共 2 条")
}

func TestSummaryListShowsRowNumbers(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSEC()
	out, err := run(t, "AAPL")
	require.NoError(t, err)
	require.Contains(t, out, "序号")
	require.Contains(t, out, "--summary <序号>")
}

func TestSummaryRejectsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, args := range [][]string{
		{"AAPL", "--ai", "x"}, {"AAPL", "--summary="}, {"AAPL", "--summary", "0"}, {"AAPL", "--summary", "-1"},
		{"--summary", "https://evil.test/a.htm"}, {"--summary", "1"}, {"AAPL", "--summary", "1", "-o", t.TempDir()}, {},
	} {
		_, err := run(t, args...)
		require.Error(t, err, args)
	}
	for _, args := range [][]string{} {
		_, err := run(t, args...)
		require.ErrorContains(t, err, "不需要股票代码和 --form、-n、-b、-e", args)
	}
	_, err := run(t, "--summary", cliFilingURL)
	require.ErrorIs(t, err, llm.ErrNoProfile)
	path := filepath.Join(home, ".sec", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{"enabled":"yes"}}`), 0o600))
	_, err = run(t, "AAPL", "--summary", "1")
	require.ErrorIs(t, err, config.ErrInvalidAI)
	require.Empty(t, gock.GetUnmatchedRequests(), "无效参数和配置不触发上游请求")
}

func TestSummaryModelErrorHidesKey(t *testing.T) {
	fake := fakeSummarizer(t)
	fake.Replies = []llm.FakeReply{{Err: &llm.APIError{Status: 401, Message: "secret-key"}}}
	_, err := run(t, "--summary", cliFilingURL)
	require.ErrorContains(t, err, "HTTP 401")
	require.NotContains(t, err.Error(), "secret-key")
}
