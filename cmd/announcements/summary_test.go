package announcements

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/cninfo"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const cliSummaryURL = "https://static.cninfo.com.cn/finalpage/2026-11-01/123456.pdf"

func fakeSummarizer(t *testing.T) *llm.Fake {
	t.Helper()
	fake := &llm.Fake{Replies: []llm.FakeReply{llm.FakeText(`{"sections":[{"text":"收入 1200 万元。","pages":[1]}]}`)}}
	old := openSummarizer
	openSummarizer = func(cmd *cobra.Command) (*llm.AnnouncementSummarizer, error) {
		return &llm.AnnouncementSummarizer{Client: fake, Model: llm.Model{Profile: "test", Name: "fake"}, Read: func(ctx context.Context, url string) (*cninfo.Document, error) {
			require.Equal(t, cliSummaryURL, url)
			return &cninfo.Document{Pages: []cninfo.DocumentPage{{Number: 1, Text: "收入 1200 万元。"}}}, ctx.Err()
		}}, nil
	}
	t.Cleanup(func() { openSummarizer = old })
	return fake
}

func runSummaryCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := NewAnnouncementsCLI()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestSummaryDirectLink(t *testing.T) {
	fake := fakeSummarizer(t)
	out, err := runSummaryCLI(t, "--summary", cliSummaryURL, "--ai", "test", "--json")
	require.NoError(t, err)
	var result llm.AnnouncementSummary
	require.NoError(t, json.Unmarshal([]byte(out), &result), "JSON 模式只输出摘要对象")
	require.Equal(t, "123456", result.DocumentID)
	require.Contains(t, result.Text, "PDF 第 1 页")
	fake.Replies = []llm.FakeReply{llm.FakeText(`{"sections":[{"text":"收入 1200 万元。","pages":[1]}]}`)}
	out, err = runSummaryCLI(t, "--summary", cliSummaryURL)
	require.NoError(t, err)
	for _, text := range []string{"【AI 公告摘要】", "披露日期 2026-11-01", "模型 fake", "配置 test", cliSummaryURL, "不构成投资建议"} {
		require.Contains(t, out, text)
	}
}

func mockSummaryList() {
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").Reply(200).BodyString(`{"totalAnnouncement":3,"totalpages":1,"announcements":[null,{"announcementId":"0","announcementTitle":"失效","invalidationFlag":1},{"announcementId":"123456","announcementTitle":"公司公告","adjunctUrl":"finalpage/2026-11-01/123456.pdf"}]}`)
}

func TestSummaryByVisibleRow(t *testing.T) {
	defer gock.Off()
	fakeSummarizer(t)
	mockSummaryList()
	out, err := runSummaryCLI(t, "--latest", "--summary", "1", "--json")
	require.NoError(t, err)
	require.Contains(t, out, `"document_id": "123456"`)
	mockSummaryList()
	_, err = runSummaryCLI(t, "--latest", "--summary", "2")
	require.ErrorContains(t, err, "本页 1 条")
	mockSummaryList()
	out, err = runSummaryCLI(t, "--latest")
	require.NoError(t, err)
	require.Contains(t, out, "序号")
	require.Contains(t, out, "公司公告")
	require.NotContains(t, out, "失效")
	require.True(t, gock.IsDone())
}

func TestSummaryRowWithoutPDF(t *testing.T) {
	defer gock.Off()
	fake := fakeSummarizer(t)
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").Reply(200).BodyString(`{"totalAnnouncement":1,"totalpages":1,"announcements":[{"announcementId":"1","announcementTitle":"无附件","secCode":"600036","orgId":"x"}]}`)
	_, err := runSummaryCLI(t, "--latest", "--summary", "1")
	require.ErrorContains(t, err, "第 1 条公告没有 PDF 原文")
	require.Empty(t, fake.Requests, "不下载、不请求模型")
}

func TestSummaryUnknownStockFails(t *testing.T) {
	defer gock.Off()
	fakeSummarizer(t)
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).BodyString(`var suggestvalue="";`)
	_, err := runSummaryCLI(t, "BADCODE", "--summary", "1")
	require.ErrorContains(t, err, "未找到证券", "摘要模式找不到证券时返回错误，不以 0 退出")
}

func TestSummaryRejectsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	for _, args := range [][]string{
		{"--ai", "x"}, {"--summary="}, {"--summary", "0"}, {"--summary", "-1"},
		{"--summary", "https://evil.test/file.pdf"}, {"--summary", "1"}, {"600036", "--latest", "--summary", "1"},
	} {
		_, err := runSummaryCLI(t, args...)
		require.Error(t, err, args)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_, err := runSummaryCLI(t, "--summary", cliSummaryURL)
	require.ErrorIs(t, err, llm.ErrNoProfile)
	path := filepath.Join(home, ".sec", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{"enabled":"yes"}}`), 0o600))
	_, err = runSummaryCLI(t, "--summary", cliSummaryURL)
	require.ErrorIs(t, err, config.ErrInvalidAI)
	require.Empty(t, gock.GetUnmatchedRequests(), "无效参数和配置不触发上游请求")
}

func TestSummaryModelErrorHidesKey(t *testing.T) {
	fake := fakeSummarizer(t)
	fake.Replies = []llm.FakeReply{{Err: &llm.APIError{Status: 401, Message: "secret-key"}}}
	_, err := runSummaryCLI(t, "--summary", cliSummaryURL)
	require.ErrorContains(t, err, "HTTP 401")
	require.NotContains(t, err.Error(), "secret-key")
}
