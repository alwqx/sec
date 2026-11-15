package filings

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/edgar"
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
		{"AAPL", "--risks", "1", "--ai=x"}:      "--ai 只能和 --summary 一起用",
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
