package filings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// riskDoc 生成一份 10-K 主文件，Item 1A 里每个标题是一条粗斜体的风险因素小标题。
func riskDoc(titles ...string) string {
	var b strings.Builder
	b.WriteString(`<p>ITEM 1A. RISK FACTORS</p><p><b>Business Risks</b></p>`)
	for _, title := range titles {
		b.WriteString(`<p><b><i>` + title + `</i></b></p><p>` + strings.Repeat(title+" Costs could rise. ", 40) + `</p>`)
	}
	b.WriteString(`<p>Item 7. Management’s Discussion and Analysis</p><p>` + strings.Repeat("Net sales increased 6%. ", 80) + `</p>`)
	return b.String()
}

func mockTenK(folder, accession, date, body string) {
	gock.New("https://www.sec.gov").Get(folder + accession + "-index.htm").Reply(200).BodyString(`<div id="formName"><strong>Form 10-K</strong></div>
<div class="infoHead">Filing Date</div><div class="info">` + date + `</div><div class="infoHead">Period of Report</div><div class="info">` + date[:4] + `-09-26</div>
<table class="tableFile" summary="Document Format Files"><tr><td>1</td><td></td><td><a href="` + folder + `main.htm">x</a></td><td>10-K</td><td>1</td></tr></table>`)
	gock.New("https://www.sec.gov").Get(folder + "main.htm").Reply(200).BodyString(body)
}

func TestRisks(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	const current, previous = "/Archives/edgar/data/320193/000032019326000026/", "/Archives/edgar/data/320193/000032019325000079/"
	mockTenK(current, "0000320193-26-000026", "2026-11-03", riskDoc("The Company depends on component suppliers.", "Artificial intelligence features may not succeed."))
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"cik":"320193","filings":{"recent":{
"accessionNumber":["0000320193-26-000026","0000320193-25-000079"],"filingDate":["2026-11-03","2025-10-31"],"reportDate":["2026-09-26","2025-09-26"],
"form":["10-K","10-K"],"primaryDocument":["main.htm","main.htm"]},"files":[]}}`)
	mockTenK(previous, "0000320193-25-000079", "2025-10-31", riskDoc("The Company depends on component suppliers.", "Climate change could disrupt operations."))
	out, err := run(t, "--risks", "https://www.sec.gov"+current+"main.htm")
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	for _, text := range []string{"【风险因素变化】10-K 0000320193-26-000026（报告期 2026-09-26）对比上一年 10-K 0000320193-25-000079（报告期 2025-09-26）",
		"上一年 2 条，今年 2 条：新增 1 条，删除 1 条，改写 0 条，保留 1 条。", "新增（1）：\n  1. [Business Risks] Artificial intelligence features may not succeed.",
		"删除（1）：\n  1. [Business Risks] Climate change could disrupt operations.", "改写（0）：\n  无", "保留的 1 条小标题相同", "不代表公司面临的风险本身变了"} {
		require.Contains(t, out, text)
	}

	mockTenK(current, "0000320193-26-000026", "2026-11-03", riskDoc("The Company depends on component suppliers."))
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"cik":"320193","filings":{"recent":{
"accessionNumber":["0000320193-26-000026","0000320193-25-000079"],"filingDate":["2026-11-03","2025-10-31"],"reportDate":["2026-09-26","2025-09-26"],
"form":["10-K","10-K"],"primaryDocument":["main.htm","main.htm"]},"files":[]}}`)
	mockTenK(previous, "0000320193-25-000079", "2025-10-31", riskDoc("The Company depends on component suppliers."))
	out, err = run(t, "--risks", "https://www.sec.gov"+current+"main.htm", "--json")
	require.NoError(t, err)
	var changes edgar.RiskChanges
	require.NoError(t, json.Unmarshal([]byte(out), &changes))
	require.Len(t, changes.Kept, 1)
	require.NotContains(t, out, "Costs could rise", "JSON 不输出正文")
}

func TestRisksArgs(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	for args, want := range map[[4]string]string{
		{"AAPL", "--risks", "1", "--summary=1"}: "--summary 和 --risks 不能一起用",
		{"AAPL", "--risks", "1", "-o=x"}:        "--risks 不能和 --output 一起用",
		{"AAPL", "--risks=", "", ""}:            "--risks 需要列表序号",
		{"--risks", "https://evil.test/a.htm"}:  "--risks 需要从 1 开始的列表序号",
		{"AAPL", "--risks", cliTenKURL, ""}:     "--risks 用链接时不需要股票代码",
		{"AAPL", "--risks", "1", "--ai=x"}:      "--ai 只能和 --summary 或 --risks --explain 一起用",
		{"AAPL", "--explain", "", ""}:           "--explain 只能和 --risks 一起用",
		{"AAPL", "--risks=", "--explain", ""}:   "--risks 需要列表序号",
		{"--risks", "1", "", ""}:                "请提供股票代码",
	} {
		var list []string
		for _, a := range args {
			if a != "" {
				list = append(list, a)
			}
		}
		_, err := run(t, list...)
		require.ErrorContains(t, err, want, list)
	}
	mockSEC()
	_, err := run(t, "AAPL", "--risks", "2")
	require.ErrorContains(t, err, "第 2 条是 8-K，风险因素对比只支持 10-K")
}

func fakeRiskExplainer(t *testing.T, changes *edgar.RiskChanges) *llm.Fake {
	t.Helper()
	fake := &llm.Fake{Replies: []llm.FakeReply{llm.FakeText(`{"sections":[{"text":"今年新增人工智能风险，ESG 风险不再单独列出。","refs":["新增 1","删除 1"]}]}`)}}
	old := openRiskExplainer
	openRiskExplainer = func(cmd *cobra.Command) (*llm.RiskExplainer, *config.Config, error) {
		return &llm.RiskExplainer{Client: fake, Model: llm.Model{Profile: "test", Name: "fake"}, Read: func(ctx context.Context, url string) (*edgar.RiskChanges, error) {
			require.Equal(t, cliTenKURL, url)
			return changes, ctx.Err()
		}}, &config.Config{SECEmail: "sec_dev@example.com"}, nil
	}
	t.Cleanup(func() { openRiskExplainer = old })
	return fake
}

func TestRisksExplain(t *testing.T) {
	changes := &edgar.RiskChanges{
		Current:  edgar.RiskFiling{Accession: "0000320193-26-000026", Form: "10-K", ReportDate: "2026-09-26", URL: cliTenKURL, Count: 1},
		Previous: edgar.RiskFiling{Accession: "0000320193-25-000079", Form: "10-K", ReportDate: "2025-09-27", Count: 1},
		RiskComparison: edgar.RiskComparison{Added: []edgar.RiskFactor{{Title: "AI may harm the Company.", Text: "AI outputs may be wrong."}},
			Removed: []edgar.RiskFactor{{Title: "ESG scrutiny may result in costs.", Text: "ESG."}}, Revised: []edgar.RiskRevision{}, Kept: []edgar.RiskFactor{}},
		Notes: []string{"对比说明。"},
	}
	fakeRiskExplainer(t, changes)
	out, err := run(t, "--risks", cliTenKURL, "--explain", "--ai", "test")
	require.NoError(t, err)
	for _, text := range []string{"新增（1）：\n  1. AI may harm the Company.", "【AI 解读】模型 fake · 配置 test", "今年新增人工智能风险，ESG 风险不再单独列出。（新增 1、删除 1）",
		"核对：", "对比说明。", "改写只看两年正文里不同的句子", "不构成投资建议"} {
		require.Contains(t, out, text)
	}

	// 申报内容的问题和不加 --explain 时一样直接报错。
	old := openRiskExplainer
	openRiskExplainer = func(cmd *cobra.Command) (*llm.RiskExplainer, *config.Config, error) {
		return &llm.RiskExplainer{Client: &llm.Fake{}, Read: func(context.Context, string) (*edgar.RiskChanges, error) {
			return nil, fmt.Errorf("%w：没有找到上一份 10-K", edgar.ErrDocument)
		}}, &config.Config{}, nil
	}
	_, err = run(t, "--risks", cliTenKURL, "--explain")
	openRiskExplainer = old
	require.EqualError(t, err, "SEC 申报原文无法读取：没有找到上一份 10-K")

	fake := fakeRiskExplainer(t, changes)
	out, err = run(t, "--risks", cliTenKURL, "--explain", "--json")
	require.NoError(t, err)
	var result llm.RiskExplanation
	require.NoError(t, json.Unmarshal([]byte(out), &result), "JSON 模式只输出解读对象")
	require.Equal(t, "0000320193-26-000026", result.Changes.Current.Accession)
	require.Len(t, fake.Requests, 1)
}

func TestRisksExplainRejectsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_, err := run(t, "AAPL", "--risks", "1", "--explain")
	require.ErrorIs(t, err, llm.ErrNoProfile)
	require.Empty(t, gock.GetUnmatchedRequests(), "模型配置有问题时不请求 EDGAR")
}

func TestRisksExplainReusesClient(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	old := openRiskExplainer
	openRiskExplainer = func(cmd *cobra.Command) (*llm.RiskExplainer, *config.Config, error) {
		return &llm.RiskExplainer{Client: &llm.Fake{}}, &config.Config{SECEmail: "sec_dev@example.com"}, nil
	}
	t.Cleanup(func() { openRiskExplainer = old })
	// 申报列表只 mock 一次：找上一年 10-K 时复用列表的客户端，不再下载。列表里只有一份 10-K。
	mockSEC()
	mockTenK("/Archives/edgar/data/320193/000032019326000026/", "0000320193-26-000026", "2026-11-03", riskDoc("The Company depends on component suppliers."))
	_, err := run(t, "AAPL", "--risks", "1", "--explain")
	require.EqualError(t, err, "SEC 申报原文无法读取：没有找到 2026-11-03 之前的上一份 10-K")
}
