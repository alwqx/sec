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
	doc, err := read10K(&FilingDocument{}, lines, nil)
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
	doc, err := read10K(&FilingDocument{}, plainLines("Item 1A. Risk Factors\n"+strings.Repeat("The Company depends on suppliers. ", 60)+"\nItem 7. Management’s Discussion\n"+strings.Repeat("Sales rose. ", 200)), nil)
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
	// 风险因素很长时摘要超过字数上限（MD&A 有自己的上限，见 TestReadTenKLongMDNA），风险因素对比不受影响。
	sentence := "Supply constraints could increase component costs and delay shipments. "
	long := strings.Replace(riskTenK("The Company depends on component suppliers in Asia."), sentence, strings.Repeat(sentence, MaxDocumentRunes/len(sentence)+1), 1)
	mock10K("10-K", long)
	_, err := client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorContains(t, err, "超过 400000 字")
	mock10K("10-K", long)
	doc, err := client.readTenK(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Len(t, doc.RiskFactors, 2)
}

func TestRiskFilingURL(t *testing.T) {
	doc := &FilingDocument{URL: "main.htm", Sections: []DocumentSection{{Label: "Item 1A", URL: "ex13.htm"}, {Label: "Item 7", URL: "ex13.htm"}}}
	require.Equal(t, "ex13.htm", riskFiling(doc).URL, "链接到风险因素所在的文件")
	require.Equal(t, "main.htm", riskFiling(&FilingDocument{URL: "main.htm"}).URL)
}

func TestRiskFactorsByTitle(t *testing.T) {
	body := func(s string) string { return "<p>" + strings.Repeat(s+" ", 40) + "</p>" }
	bi := func(s string) string {
		return `<p><span style="font-weight:700;font-style:italic">` + s + `</span></p>`
	}
	// 花旗：正文没有 Item 标题，只有交叉索引表和目录；风险因素是全大写加粗的一章，类别也是全大写加粗，
	// 小标题粗斜体，有一条在分栏处断成两行；下一章 SUSTAINABILITY 的样式和章标题相同。
	citi := `<p><b>1A. Risk Factors 49–62</b></p><p><b>RISK FACTORS 49</b></p><p><b>SUSTAINABILITY 62</b></p>
<p><b>RISK FACTORS</b></p>` + body("The following discussion presents the material risks.") + `
<p><b>MARKET-RELATED RISKS</b></p>` + bi("Changes to Interest Rates Could Adversely Affect Citi’s Results.") + body("Rates moved sharply.") + `
<p><b>STRATEGIC RISKS</b></p>` + bi("Citi Faces Potential Disruptions from an Evolving") + bi("Business Environment, Including Emerging Technologies.") + body("Competitors invest heavily.") +
		bi("The Development and Use of AI Present Risks to Citi’s Businesses.") + body("AI models may be wrong.") + `
<p><b>SUSTAINABILITY</b></p>` + body("Citi publishes a sustainability report.") + `
<p><b>MANAGEMENT’S DISCUSSION AND ANALYSIS</b></p>` + body("Revenues increased 3% driven by Services and Markets in every quarter of the year.") +
		body("Expenses declined.") + `<p><b>Report of Independent Registered Public Accounting Firm</b></p><p>Opinion.</p>`
	lines, err := htmlLines([]byte(citi))
	require.NoError(t, err)
	doc, err := read10K(&FilingDocument{}, lines, nil)
	require.NoError(t, err)
	require.Equal(t, "Item 1A", doc.Sections[0].Label)
	require.True(t, strings.HasPrefix(doc.Sections[0].Text, "RISK FACTORS\nThe following discussion"), "跳过交叉索引表和目录里的标题行")
	require.NotContains(t, doc.Sections[0].Text, "sustainability report", "到下一个同样式的章标题为止")
	require.Contains(t, strings.Join(doc.Notes, ""), "10-K 正文里没有 Item 1A 标题，按「RISK FACTORS」标题在年报正文里")
	var got [][2]string
	for _, r := range doc.RiskFactors {
		got = append(got, [2]string{r.Category, r.Title})
	}
	require.Equal(t, [][2]string{
		{"MARKET-RELATED RISKS", "Changes to Interest Rates Could Adversely Affect Citi’s Results."},
		{"STRATEGIC RISKS", "Citi Faces Potential Disruptions from an Evolving Business Environment, Including Emerging Technologies."},
		{"STRATEGIC RISKS", "The Development and Use of AI Present Risks to Citi’s Businesses."},
	}, got, "全大写的类别不结束章节；断成两行的小标题接起来，第一行不当成类别")
	require.True(t, strings.HasPrefix(doc.RiskFactors[1].Text, "Competitors invest heavily."), doc.RiskFactors[1].Text)

	// 摩根大通、美国银行：类别和小标题都只加粗，类别不超过 4 个词，不当成断开的小标题。
	b := func(s string) string { return "<p><b>" + s + "</b></p>" }
	risks := readRisks(t, `<p>ITEM 1A. RISK FACTORS</p>`+b("Regulatory, Compliance and Legal")+b("We are subject to extensive regulation in many jurisdictions.")+
		body("Regulators may impose fines.")+body("Rules change often.")+b("Market")+b("We may be adversely affected by the financial markets.")+body("Markets fall.")+
		`<p>Item 7. Management’s Discussion and Analysis</p>`+body("Net income rose 6% on higher net interest income across all segments of the firm.")+body("More.")+body("Even more.")+`<p>Item 8. Financial Statements</p>`)
	require.Len(t, risks, 2)
	require.Equal(t, "Regulatory, Compliance and Legal", risks[0].Category)
	require.Equal(t, "We are subject to extensive regulation in many jurisdictions.", risks[0].Title)
	require.Equal(t, "Market", risks[1].Category)

	// 每页重复的「RISK FACTORS」页眉和只有编号的 Item 1A 页眉不结束章节。
	paged := strings.Replace(citi, `<p><b>STRATEGIC RISKS</b></p>`, `<p><b>RISK FACTORS</b></p><p>Item 1A</p><p><b>STRATEGIC RISKS</b></p>`, 1)
	paged = strings.Replace(paged, body("Competitors invest heavily."), body("Competitors invest heavily.")+`<p><b>RISK FACTORS</b></p>`+body("More about competitors."), 1)
	lines, err = htmlLines([]byte(paged))
	require.NoError(t, err)
	doc, err = read10K(&FilingDocument{}, lines, nil)
	require.NoError(t, err)
	require.Len(t, doc.RiskFactors, 3, "页眉之后的风险因素都在")
	require.Contains(t, doc.RiskFactors[1].Text, "More about competitors.")
	require.NotContains(t, doc.Sections[0].Text, "RISK FACTORS\nItem 1A", "同名页眉和 Item 页眉不放进正文")

	// 较小的申报公司写明不适用时，不按标题去找别的部分。
	small := `<p>ITEM 1. BUSINESS</p><p><b>RISK FACTORS</b></p>` + body("Forward-looking statements involve risks.") +
		`<p><b>OTHER MATTERS</b></p><p>ITEM 1A. RISK FACTORS</p><p>Not applicable.</p><p>Item 7. Management’s Discussion and Analysis</p>` + body("Net income rose 6% on higher net interest income across all segments of the firm.") + `<p>Item 8. Financial Statements</p>`
	lines, err = htmlLines([]byte(small))
	require.NoError(t, err)
	doc, err = read10K(&FilingDocument{}, lines, nil)
	require.NoError(t, err)
	require.Equal(t, "Item 7", doc.Sections[0].Label)
	require.Contains(t, strings.Join(doc.Notes, ""), "可能不适用")

	// 带 Risks 的长类别和小标题样式相同，也不当成断开的小标题。
	risks = readRisks(t, `<p>ITEM 1A. RISK FACTORS</p>`+b("Risks Related to Our Business and Industry")+b("We depend on a small number of customers.")+
		body("Customers may leave.")+body("Contracts are short.")+
		`<p>Item 7. Management’s Discussion and Analysis</p>`+body("Net income rose 6% on higher net interest income across all segments of the firm.")+`<p>Item 8. Financial Statements</p>`)
	require.Len(t, risks, 1)
	require.Equal(t, "Risks Related to Our Business and Industry", risks[0].Category)
	require.Equal(t, "We depend on a small number of customers.", risks[0].Title)
}
