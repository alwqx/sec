package cmd

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func runText(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewCLI()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// 文本模式也要报出没查到的证券，并以非 0 退出，不能只记日志。
func TestQuoteTextReportsPartialFailures(t *testing.T) {
	defer gock.Off()
	// 关键字并发搜索，按关键字匹配 mock。
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/.*key=600036$").Reply(200).BodyString(suggestBody)
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/.*key=nosuch$").Reply(200).BodyString(`var suggestvalue="";`)
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/.*key=broken$").Reply(500)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(cmbA)
	_, err := runText(t, "quote", "600036,nosuch,broken")
	require.ErrorContains(t, err, "nosuch 未找到证券")
	require.ErrorContains(t, err, "broken 搜索失败")
	require.NotContains(t, err.Error(), "600036")
	require.True(t, gock.IsDone(), "查到的证券照常请求行情")
}

func TestQuoteTextAllFound(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(cmbA)
	_, err := runText(t, "quote", "600036")
	require.NoError(t, err)
}

func TestQuoteTextMissingQuote(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(`var hq_str_sh600036="";`)
	_, err := runText(t, "quote", "600036")
	require.ErrorContains(t, err, "SH600036 没有行情数据")
}

const reportRow = `{"success":true,"result":{"data":[{"REPORT_DATE":"2025-12-31 00:00:00","SECURITY_CODE":"600036","DATE_TYPE_CODE":"001"}]}}`

func mockReports(statuses ...int) {
	for _, status := range statuses {
		request := gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get")
		if status == 200 {
			request.Reply(200).BodyString(reportRow)
		} else {
			request.Reply(status)
		}
	}
}

// 不带 --type 时某一类报表失败：文本先显示拿到的报表，再报错。
func TestFinancialsTextReportsPartialFailures(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	mockReports(200, 500, 200)
	output, err := runText(t, "financials", "600036")
	require.ErrorContains(t, err, "部分财报没有获取到")
	require.Contains(t, output, "资产负债表", "拿到的报表照常显示")
}

func TestFinancialsExportRefusesPartialReports(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	mockReports(200, 500)
	path := filepath.Join(t.TempDir(), "report.json")
	_, err := runText(t, "financials", "600036", "-o", path)
	require.ErrorContains(t, err, "失败")
	_, statErr := os.Stat(path)
	require.True(t, os.IsNotExist(statErr), "不能写出少一类报表的文件")
}

func TestFinancialsNotFoundIsAnError(t *testing.T) {
	for _, args := range [][]string{{"financials", "nosuch"}, {"financials", "nosuch", "-o", filepath.Join(t.TempDir(), "x.json")}} {
		mockSearch(`var suggestvalue="";`)
		_, err := runText(t, args...)
		require.ErrorContains(t, err, "未找到证券: nosuch", args)
		gock.Off()
	}
}

func TestFinancialsCancelIsNotAFailure(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	ctx, cancel := context.WithCancel(context.Background())
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").Reply(200).BodyString(reportRow)
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").AddMatcher(func(*http.Request, *gock.Request) (bool, error) {
		cancel()
		return true, nil
	}).Reply(500)
	root := NewCLI()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"financials", "600036"})
	require.NoError(t, root.ExecuteContext(ctx), "按 Ctrl-C 取消不是报表失败")
}
