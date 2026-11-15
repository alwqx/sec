package edgar

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestHTMLLinesStyle(t *testing.T) {
	lines, err := htmlLines([]byte(`<p><span style="font-weight:700">Business Risks</span></p>
<p><span style="font-style: italic; font-weight: bold">The Company depends on <span>suppliers</span>.</span> Body text.</p>
<p><b>Bold <span style="font-weight:400">then plain</span></b> text.</p>
<p><b><i>Run-in heading without period</i></b>. Body.</p>
<p style="text-decoration:underline">LEGAL RISKS</p>
<p>Plain <b>bold later</b>.</p>
<p><span style="font: bold italic 10pt Times">Shorthand font heading.</span></p>
<p><b><span style="font: 10pt Times">Shorthand resets bold</span></b></p>`))
	require.NoError(t, err)
	require.Equal(t, []docLine{
		{text: "Business Risks", lead: "Business Risks", bold: true, emphasized: true},
		{text: "The Company depends on suppliers. Body text.", lead: "The Company depends on suppliers.", bold: true, italic: true},
		{text: "Bold then plain text.", lead: "Bold", bold: true},
		{text: "Run-in heading without period. Body.", lead: "Run-in heading without period", bold: true, italic: true},
		{text: "LEGAL RISKS", emphasized: true},
		{text: "Plain bold later."},
		{text: "Shorthand font heading.", lead: "Shorthand font heading.", bold: true, italic: true, emphasized: true},
		{text: "Shorthand resets bold"},
	}, lines, "font-weight:400 和 font 简写关闭外层的加粗；只取行首连续同样式的文字；整行带下划线也算强调")
}

// riskTenK 生成一份带风险因素小标题的 10-K：斜体引言、加粗类别、粗斜体小标题、带下划线的类别、
// 和正文写在同一段的小标题（句号不在加粗范围内）、不是句子的分项标题和页眉页脚。
func riskTenK(titles ...string) string {
	body := strings.Repeat("Supply constraints could increase component costs and delay shipments. ", 10)
	var risks strings.Builder
	// 每条正文不同，对比时不会因为正文相同配成改写。
	for _, title := range titles {
		risks.WriteString(`<p><span style="font-weight:700;font-style:italic">` + title + `</span></p><p>` + body + strings.Repeat(title+" ", 10) + `</p>`)
	}
	return `<html><body><p>Item 1A. Risk Factors</p><p>5</p>
<p>ITEM 1A. RISK FACTORS</p>
<p><i>You should carefully consider the risks described below before investing.</i></p>
<p><span style="font-weight:700">Business Risks</span></p>` + risks.String() + `
<p>Acme Inc. | 2026 Form 10-K | 7</p>
<p style="text-decoration:underline">LEGAL RISKS</p>
<p><b><i>The Company faces legal proceedings worldwide</i></b>. Lawsuits are costly.</p>
<p><b><i>Patent litigation</i></b></p><p>` + body + `</p>
<p>Item 1B. Unresolved Staff Comments</p><p>None.</p>
<p>Item 7. Management’s Discussion and Analysis</p><p>` + strings.Repeat("Net sales increased 6%. ", 80) + `</p>
<p>Item 8. Financial Statements</p></body></html>`
}

func readRisks(t *testing.T, html string) []RiskFactor {
	t.Helper()
	lines, err := htmlLines([]byte(html))
	require.NoError(t, err)
	doc, err := read10K(&FilingDocument{}, lines)
	require.NoError(t, err)
	return doc.RiskFactors
}

func TestRiskFactors(t *testing.T) {
	risks := readRisks(t, riskTenK("The Company depends on component suppliers in Asia.", `Tariffs could start a "trade war."`))
	var titles, categories []string
	for _, r := range risks {
		titles, categories = append(titles, r.Title), append(categories, r.Category)
	}
	require.Equal(t, []string{"The Company depends on component suppliers in Asia.", `Tariffs could start a "trade war."`, "The Company faces legal proceedings worldwide."}, titles,
		"跳过目录和样式不同的斜体引言；句号在引号里也是句子；和正文写在同一段的小标题补上句号；不是句子的分项标题不算")
	require.Equal(t, []string{"Business Risks", "Business Risks", "LEGAL RISKS"}, categories, "加粗和带下划线的短行是类别")
	require.True(t, strings.HasPrefix(risks[0].Text, "Supply constraints"))
	require.NotContains(t, risks[1].Text, "Form 10-K", "正文不含页眉页脚")
	require.True(t, strings.HasPrefix(risks[2].Text, "Lawsuits are costly.\nPatent litigation\nSupply constraints"), "同一段里小标题后面的文字和分项标题都是正文")
	require.NotContains(t, risks[2].Text, "Unresolved", "正文到 Item 1A 结束为止")

	// 「Item 1A.」和「Risk Factors」分成两行时，第二行不是类别。
	split := strings.Replace(riskTenK("The Company depends on component suppliers in Asia."), "<p>ITEM 1A. RISK FACTORS</p>", "<p><b>Item 1A.</b></p><p><b>Risk Factors</b></p>", 1)
	split = strings.Replace(split, `<p><span style="font-weight:700">Business Risks</span></p>`, "", 1)
	risks = readRisks(t, split)
	require.Equal(t, "The Company depends on component suppliers in Asia.", risks[0].Title)
	require.Empty(t, risks[0].Category)

	// 只有纯文本、没有样式时识别不出小标题。
	doc, err := read10K(&FilingDocument{}, plainLines("Item 1A. Risk Factors\n"+strings.Repeat("The Company depends on suppliers. ", 60)+"\nItem 7. Management’s Discussion\n"+strings.Repeat("Sales rose. ", 200)))
	require.NoError(t, err)
	require.Empty(t, doc.RiskFactors)
}

func TestCompareRiskFactors(t *testing.T) {
	body := func(s string) string { return strings.Repeat(s+" ", 5) }
	old := []RiskFactor{
		{Title: "The Company depends on component suppliers.", Text: body("supply chain")},
		{Title: "The Company faces intense competition.", Text: body("competition prices margins")},
		{Title: "Our stock price may be volatile.", Text: body("stock volatility")},
		{Title: "Losses of confidential information could harm the Company’s reputation and financial condition.", Text: body("hackers breach personal data systems")},
		{Title: "We must manage obligations under a state agreement.", Text: body("state agreement jobs")},
	}
	new := []RiskFactor{
		{Title: "Artificial intelligence may harm the Company.", Text: body("ai models outputs")},
		{Title: "THE COMPANY DEPENDS ON COMPONENT SUPPLIERS", Text: body("supply chain changed")},
		{Title: "The Company faces intense competition in global markets.", Text: body("competition prices margins")},
		{Title: "The Company experiences cybersecurity incidents.", Text: body("hackers breach personal data systems")},
		{Title: "Our stock price may be volatile.", Text: body("stock volatility")},
	}
	c := CompareRiskFactors(old, new)
	require.Equal(t, []RiskFactor{new[0]}, c.Added)
	require.Equal(t, []RiskFactor{old[4]}, c.Removed)
	require.Equal(t, []RiskFactor{new[1], new[4]}, c.Kept, "不计大小写和末尾句号，正文改了也算保留")
	require.Len(t, c.Revised, 2)
	require.Equal(t, old[1].Title, c.Revised[0].Old.Title, "小标题相似度够高")
	require.Equal(t, 1.0, c.Revised[0].TextSimilarity)
	require.Equal(t, old[3].Title, c.Revised[1].Old.Title, "小标题改写得多，正文相同")
	require.Less(t, c.Revised[1].TitleSimilarity, minTitleSimilarity)

	// 套话不影响小标题相似度。
	require.Equal(t, 1.0, dice(titleWords("Defects could materially adversely affect the Company’s business."), titleWords("Defects.")))
	empty := CompareRiskFactors(nil, nil)
	require.NotNil(t, empty.Added, "JSON 输出 [] 而不是 null")
}

const prevFolder = "/Archives/edgar/data/320193/000032019325000079/"

func TestRiskChanges(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	mock10K("10-K", riskTenK("The Company depends on component suppliers in Asia.", "Artificial intelligence features may not succeed."))
	// 列表里最新的是本年的修订版和本年 10-K，上一年的修订版只补充第三部分，要跳过。
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"cik":"320193","name":"Apple Inc.","filings":{"recent":{
"accessionNumber":["0000320193-27-000001","0000320193-26-000024","0000320193-26-000002","0000320193-25-000079"],
"filingDate":["2027-01-20","2026-11-02","2026-01-20","2025-10-31"],"reportDate":["2026-10-30","2026-10-30","2025-09-27","2025-09-27"],
"form":["10-K/A","10-K","10-K/A","10-K"],"primaryDocument":["a.htm","acme-10k.htm","b.htm","aapl-20250927.htm"]},"files":[]}}`)
	gock.New("https://www.sec.gov").Get(prevFolder + "0000320193-25-000079-index.htm").Reply(200).BodyString(
		strings.ReplaceAll(filingIndex("10-K", [2]string{prevFolder + "aapl-20250927.htm", "10-K"}), "2026-", "2025-"))
	gock.New("https://www.sec.gov").Get(prevFolder + "aapl-20250927.htm").Reply(200).BodyString(riskTenK("The Company depends on component suppliers in Asia.", "Climate change could disrupt operations."))

	changes, err := client.RiskChanges(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, "0000320193-26-000024", changes.Current.Accession)
	require.Equal(t, "0000320193-25-000079", changes.Previous.Accession)
	require.Equal(t, 3, changes.Current.Count)
	require.Equal(t, []string{"Artificial intelligence features may not succeed."}, []string{changes.Added[0].Title})
	require.Equal(t, "Climate change could disrupt operations.", changes.Removed[0].Title)
	require.Len(t, changes.Kept, 2)
	require.NotEmpty(t, changes.Notes)

	// 没有上一份 10-K。
	mock10K("10-K", riskTenK("The Company depends on component suppliers in Asia."))
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"cik":"320193","filings":{"recent":{
"accessionNumber":["0000320193-26-000024"],"filingDate":["2026-11-02"],"reportDate":["2026-10-30"],"form":["10-K"],"primaryDocument":["acme-10k.htm"]},"files":[]}}`)
	_, err = (&Client{Email: "test@example.com", CacheDir: t.TempDir()}).RiskChanges(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorIs(t, err, ErrDocument)
	require.ErrorContains(t, err, "没有找到 2026-11-02 之前的上一份 10-K")

	// 读申报列表失败是下载失败。
	mock10K("10-K", riskTenK("The Company depends on component suppliers in Asia."))
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(500)
	_, err = (&Client{Email: "test@example.com", CacheDir: t.TempDir()}).RiskChanges(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorIs(t, err, ErrDownload)

	// 识别不出小标题时不去找上一年。
	mock10K("10-K", `<p>Item 1A. Risk Factors</p><p>`+strings.Repeat("Adverse economic conditions could harm demand. ", 40)+
		`</p><p>Item 7. Management’s Discussion and Analysis</p><p>`+strings.Repeat("Net sales increased 6%. ", 80)+`</p>`)
	_, err = (&Client{Email: "test@example.com", CacheDir: t.TempDir()}).RiskChanges(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorContains(t, err, "没有识别出风险因素小标题")
}

func TestRiskFactorsIgnoreSummaryLimit(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	// MD&A 很长时摘要超过字数上限，风险因素对比不受影响。
	long := strings.Replace(riskTenK("The Company depends on component suppliers in Asia."), "Net sales increased 6%. ", strings.Repeat("Net sales increased 6%. ", MaxDocumentRunes/20), 1)
	mock10K("10-K", long)
	_, err := client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorContains(t, err, "超过 400000 字")
	mock10K("10-K", long)
	doc, err := client.readTenK(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Len(t, doc.RiskFactors, 2)
}
