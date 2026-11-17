package edgar

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// tenK 生成一份 10-K 主文件：目录、带页眉页脚的 Item 1A、只有一句引用的 Item 7。
func tenK(risk, mdna string) string {
	return `<html><body><p>FORM 10-K</p>
<table><tr><td>Item 1A.</td><td>Risk Factors</td><td>5</td></tr><tr><td>Item 1B.</td><td>Unresolved Staff Comments</td><td>20</td></tr>
<tr><td>Item 7.</td><td>Management’s Discussion and Analysis of Financial Condition and Results of Operations</td><td>24</td></tr><tr><td>Item 7A.</td><td>Market Risk</td><td>30</td></tr></table>
<p>PART I</p><p>Item 1. Business</p><p>The Company designs products.</p>
<p>ITEM 1A. RISK FACTORS</p><p><b>Macroeconomic Risks</b></p><p>` + risk + `</p>
<p>Acme Inc. | 2026 Form 10-K | 5</p><p>12</p><p>Table of Contents</p><p>PART I</p><p>Item 1A</p>
<p><i>Competition could reduce demand.</i></p><p>` + risk + `</p>
<p>Item 1B. Unresolved Staff Comments</p><p>None.</p>
<p>Item 7. Management’s Discussion and Analysis of Financial Condition and Results of Operations</p><p>` + mdna + `</p>
<table><tr><td></td><td>2026</td><td>Change</td><td>2025</td></tr></table>
<p>Item 7A. Quantitative and Qualitative Disclosures About Market Risk</p><p>Interest rate risk.</p>
<p>Item 8. Financial Statements</p></body></html>`
}

func mock10K(form, body string) {
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex(form, [2]string{docFolder + "acme-10k.htm", form}))
	gock.New("https://www.sec.gov").Get(docFolder + "acme-10k.htm").Reply(200).BodyString(body)
}

func TestRead10K(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	risk := strings.Repeat("Adverse economic conditions could harm demand. ", 20)
	mdna := strings.Repeat("Net sales increased 6% due to higher iPhone sales. ", 40)
	mock10K("10-K", tenK(risk, mdna))
	doc, err := client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, "10-K", doc.Form)
	require.Len(t, doc.Sections, 2)
	require.Equal(t, "Item 1A", doc.Sections[0].Label)
	require.Equal(t, "https://www.sec.gov"+docFolder+"acme-10k.htm", doc.Sections[0].URL)
	require.Equal(t, "ITEM 1A. RISK FACTORS\nMacroeconomic Risks\n"+strings.TrimSpace(risk)+"\nCompetition could reduce demand.\n"+strings.TrimSpace(risk), doc.Sections[0].Text,
		"取正文里的章节而不是目录；去掉页眉页脚、页码和只有编号的 Item 页眉")
	require.Equal(t, "Item 7", doc.Sections[1].Label)
	require.True(t, strings.HasPrefix(doc.Sections[1].Text, "Item 7. Management’s Discussion"))
	require.True(t, strings.HasSuffix(doc.Sections[1].Text, "2026 Change 2025"), "MD&A 表格的表头不当成页眉删掉")
	require.NotContains(t, doc.Sections[1].Text, "Interest rate risk", "Item 7A 不在 Item 7 里")
	require.Empty(t, doc.Notes)
}

func TestRead10KShortSections(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	risk := strings.Repeat("Adverse economic conditions could harm demand. ", 20)
	// 银行等公司的 MD&A 在年报其他部分，Item 7 只有一句引用；过渡期年报 10-KT 也能读。
	mock10K("10-KT", tenK(risk, "Management’s discussion and analysis appears on pages 40-120 of this Annual Report."))
	doc, err := client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Len(t, doc.Sections, 1)
	require.Equal(t, "Item 1A", doc.Sections[0].Label)
	require.Len(t, doc.Notes, 1)
	require.Contains(t, doc.Notes[0], "Item 7 管理层讨论与分析正文只有")
	require.Contains(t, doc.Notes[0], "银行等公司常见")

	// 只补充第三部分的 10-K/A 没有可摘要的章节。
	mock10K("10-K/A", `<p>Amendment No. 1</p><p>PART III</p><p>Item 10. Directors, Executive Officers and Corporate Governance</p><p>Names.</p>`)
	_, err = client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorIs(t, err, ErrDocument)
	require.ErrorContains(t, err, "没有可摘要的 Item 1A 或 Item 7")

	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("8-K", [2]string{docFolder + "a.htm", "8-K"}))
	_, err = client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorIs(t, err, ErrUnsupportedForm)
	require.ErrorIs(t, err, ErrDocument, "Desktop 按 ErrDocument 返回 422")
	require.EqualError(t, err, "这份申报是 8-K，不是 10-K 或 10-KT")
	require.True(t, gock.IsDone())
}

func TestPageFurniture(t *testing.T) {
	for _, line := range []string{"Table of Contents", "PART II", "Parts II and III", "Item 1A", "Item 1A.", "Item 1B, 1C", "Apple Inc. | 2026 Form 10-K | 7", "160 JPMorgan Chase & Co./2025 Form 10-K"} {
		require.True(t, pageFurniture(line), line)
	}
	for _, line := range []string{"2026 Change 2025", "(In millions)", "Item 7. Management’s Discussion", "Competition", "394",
		"See Note 5 in Part II, Item 8 of this Form 10-K.", "1 See Note 2 of this Form 10-K", "This Annual Report on Form 10-K contains forward-looking statements."} {
		require.False(t, pageFurniture(line), line)
	}
}

// section 用纯文字的行测试章节查找，item 用小写编号。
func section(text, item, title string) string {
	lines := strings.Split(text, "\n")
	scanned := scanTenK(lines)
	start, end := findSection(lines, scanned, item, title)
	return sectionText(lines, scanned, start, end)
}

func TestFindSection(t *testing.T) {
	body := strings.Repeat("Demand could decline. ", 5)
	for name, c := range map[string]struct {
		text, item, title, want string
	}{
		"编号和标题分成两行": {
			text: "Item 1A. Risk Factors 5\nItem 1B. Unresolved Staff Comments 20\nItem 1A.\nRisk Factors\n" + body + "\nItem 1B.\nUnresolved Staff Comments\nNone.",
			item: "1a", title: "risk factors", want: "Item 1A.\nRisk Factors\n" + body,
		},
		"下一个标题和正文在同一段": {
			text: "Item 1A. Risk Factors\n" + body + "\nItem 1B. Unresolved Staff Comments. " + strings.Repeat("We have no unresolved comments from the staff. ", 5),
			item: "1a", title: "risk factors", want: "Item 1A. Risk Factors\n" + body,
		},
		"以 Item 开头的长句不是标题": {
			text: "Item 7. Management’s Discussion\n" + body + "\nItem 8 of this Form 10-K contains the consolidated financial statements, which should be read together with this discussion; the notes describe the accounting policies and estimates used in more detail.\nMore discussion.\nItem 7A. Market Risk",
			item: "7", title: "management", want: "Item 7. Management’s Discussion\n" + body + "\nItem 8 of this Form 10-K contains the consolidated financial statements, which should be read together with this discussion; the notes describe the accounting policies and estimates used in more detail.\nMore discussion.",
		},
		"到 SIGNATURES 为止": {
			text: "Item 7. Management’s Discussion\n" + body + "\nSIGNATURES\nPursuant to the requirements of the Act.\nEXHIBIT INDEX",
			item: "7", title: "management", want: "Item 7. Management’s Discussion\n" + body,
		},
		"Items 复数标题": {
			text: "Items 7 and 7A. Management’s Discussion and Market Risk\n" + body + "\nItems 8 and 9. Financial Statements\nBalance sheet.",
			item: "7", title: "management", want: "Items 7 and 7A. Management’s Discussion and Market Risk\n" + body,
		},
		"Items 7A and 8 结束 Item 7": {
			text: "Item 7. Management’s Discussion\n" + body + "\nItems 7A and 8. Market Risk and Financial Statements\nBalance sheet.",
			item: "7", title: "management", want: "Item 7. Management’s Discussion\n" + body,
		},
		"单独一行的数字挨着页眉页脚才是页码": {
			text: "Item 7. Management’s Discussion\nNet sales\n394\n" + body + "\n31\nTable of Contents\nMore.\nSee Note 5 in Part II, Item 8 of this Form 10-K.\nItem 7A. Market Risk",
			item: "7", title: "management", want: "Item 7. Management’s Discussion\nNet sales\n394\n" + body + "\nMore.\nSee Note 5 in Part II, Item 8 of this Form 10-K.",
		},
		"连续递增的单独数字是页码": {
			text: "Item 7. Management’s Discussion\n" + body + "\n31\nMore.\n32\nEven more.\n34\nItem 7A. Market Risk",
			item: "7", title: "management", want: "Item 7. Management’s Discussion\n" + body + "\nMore.\nEven more.",
		},
		"每页重复完整页眉": {
			text: strings.Repeat("Item 1A. Risk Factors\n"+body+"\n", 3) + "Item 1B. Unresolved Staff Comments",
			item: "1a", title: "risk factors", want: strings.TrimSuffix(strings.Repeat("Item 1A. Risk Factors\n"+body+"\n", 3), "\n"),
		},
		"只有编号的页眉不截断章节": {
			text: "Item 1A. Risk Factors\n" + body + "\nItem 1A\n" + body + "\nItem 1B. Unresolved Staff Comments",
			item: "1a", title: "risk factors", want: "Item 1A. Risk Factors\n" + body + "\n" + body,
		},
	} {
		require.Equal(t, c.want, section(c.text, c.item, c.title), name)
	}
	require.Empty(t, section("Item 1. Business\nText.", "1a", "risk factors"))
}

func TestReadFiling(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	risk := strings.Repeat("Adverse economic conditions could harm demand. ", 40)
	mock10K("10-K/A", tenK(risk, risk))
	doc, err := client.ReadFiling(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Equal(t, "10-K/A", doc.Form)
	require.Equal(t, []string{"Item 1A", "Item 7"}, []string{doc.Sections[0].Label, doc.Sections[1].Label})

	mock10K("8-K", `<p>Item 2.02 Results of Operations.</p><p>Revenue grew.</p>`)
	doc, err = client.ReadFiling(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Equal(t, "Item 2.02", doc.Sections[0].Label)

	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("S-1", [2]string{docFolder + "a.htm", "S-1"}))
	_, err = client.ReadFiling(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorIs(t, err, ErrUnsupportedForm)
	require.EqualError(t, err, "这份申报是 S-1，不是 8-K、10-K 或 10-KT")
	require.True(t, gock.IsDone(), "每份申报只读一次目录页和主文件")
}

func TestReadTenKBankMDNA(t *testing.T) {
	risk := strings.Repeat("Adverse economic conditions could harm demand. ", 40)
	mdna := strings.Repeat("Net interest income increased 3% driven by higher deposit margins. ", 60)
	// 摩根大通：Item 7 只引用年报，MD&A 在年报正文里，每页有同名页眉，页码在页脚开头或结尾；目录行以冒号结尾。
	jpm := `<p>ITEM 1A. RISK FACTORS</p><p>` + risk + `</p>
<p>Item 7. Management’s Discussion and Analysis of Financial Condition and Results of Operations.</p><p>Management’s discussion and analysis appears on pages 46–160.</p>
<p>Item 8. Financial Statements and Supplementary Data.</p><p>The financial statements appear on pages 161–300.</p>
<p><b>Management’s discussion and analysis:</b></p><p>Introduction 46</p><p><b>Audited financial statements:</b></p>
<p><b>Management’s discussion and analysis</b></p><p>` + mdna + `</p><p>46 JPMorgan Chase & Co./2025 Form 10-K</p>
<p><b>Management’s discussion and analysis</b></p><p>` + mdna + `</p><p>JPMorgan Chase & Co./2025 Form 10-K 47</p>
<p><b>Management’s report on internal control over financial reporting</b></p><p>Management is responsible.</p>`
	lines, err := htmlLines([]byte(jpm))
	require.NoError(t, err)
	doc, err := read10K(&FilingDocument{}, lines, nil)
	require.NoError(t, err)
	require.Len(t, doc.Sections, 2)
	require.Equal(t, "Item 7", doc.Sections[1].Label)
	require.Equal(t, "Management’s discussion and analysis\n"+strings.TrimSpace(mdna)+"\n"+strings.TrimSpace(mdna), doc.Sections[1].Text,
		"跳过目录，去掉每页的同名页眉和页脚，到管理层内部控制报告为止")
	require.Contains(t, strings.Join(doc.Notes, ""), "按「Management’s discussion and analysis」标题")

	// 美国银行：Item 7 后面是公司名，标准标题在下一行。
	bac := `<p>ITEM 1A. RISK FACTORS</p><p>` + risk + `</p><p>Item 7. Bank of America Corporation and Subsidiaries</p>
<p>Management's Discussion and Analysis of Financial Condition and Results of Operations</p><p>` + mdna + `</p><p>Item 8. Financial Statements and Supplementary Data</p><p>Balance sheet.</p>`
	lines, err = htmlLines([]byte(bac))
	require.NoError(t, err)
	doc, err = read10K(&FilingDocument{}, lines, nil)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(doc.Sections[1].Text, "Item 7. Bank of America Corporation and Subsidiaries\nManagement's Discussion"), doc.Sections[1].Text[:80])
	require.NotContains(t, doc.Sections[1].Text, "Balance sheet")
}

func TestReadTenKLongMDNA(t *testing.T) {
	risk := strings.Repeat("Adverse economic conditions could harm demand. ", 40)
	line := strings.Repeat("Revenues rose. ", 60)
	var mdna strings.Builder
	for mdna.Len() < maxMDNARunes*3 {
		mdna.WriteString("<p>" + line + "</p>")
	}
	lines, err := htmlLines([]byte(`<p>ITEM 1A. RISK FACTORS</p><p>` + risk + `</p><p>Item 7. Management’s Discussion and Analysis</p>` + mdna.String() + `<p>Item 8. Financial Statements</p>`))
	require.NoError(t, err)
	doc, err := read10K(&FilingDocument{}, lines, nil)
	require.NoError(t, err)
	n := utf8.RuneCountInString(doc.Sections[1].Text)
	require.LessOrEqual(t, n, maxMDNARunes)
	require.Greater(t, n, maxMDNARunes-len(line)-2, "按整行截取")
	require.True(t, strings.HasSuffix(doc.Sections[1].Text, strings.TrimSpace(line)), "不截断一行")
	require.Contains(t, strings.Join(doc.Notes, ""), "只摘要开头的")
	require.NoError(t, checkTenKSize(doc), "截取后不超过摘要的字数上限")
}

func TestMDNAByTitleEdges(t *testing.T) {
	risk := strings.Repeat("Adverse economic conditions could harm demand. ", 40)
	mdna := strings.Repeat("Net interest income increased 3% driven by higher deposit margins. ", 60)
	// 目录里的 MD&A 标题没有冒号、也加粗，但后面紧跟审计报告的目录行，那一段最短；正文里的那段最长。
	html := `<p>ITEM 1A. RISK FACTORS</p><p>` + risk + `</p>
<p><b>Management’s discussion and analysis 46</b></p><p>Report of Independent Registered Public Accounting Firm 162</p>
<p><b>MANAGEMENT’S DISCUSSION AND ANALYSIS</b></p><p>` + mdna + `</p>
<p><b>Report of Independent Registered Public Accounting Firm</b></p><p>Opinion.</p>`
	lines, err := htmlLines([]byte(html))
	require.NoError(t, err)
	doc, err := read10K(&FilingDocument{}, lines, nil)
	require.NoError(t, err)
	require.Equal(t, "MANAGEMENT’S DISCUSSION AND ANALYSIS\n"+strings.TrimSpace(mdna), doc.Sections[1].Text)
	require.Contains(t, strings.Join(doc.Notes, ""), "10-K 正文里没有 Item 7 标题", "没有 Item 7 标题时不说成引用")
}

func TestTruncateLines(t *testing.T) {
	require.Equal(t, "ab\ncd", truncateLines("ab\ncd\nef", 6), "按整行截")
	require.Equal(t, "title\n"+strings.Repeat("x", 14), truncateLines("title\n"+strings.Repeat("x", 100), 20), "后面是很长的一行时截这一行的开头")
	require.Equal(t, "abcd", truncateLines("abcdefgh", 4))
	require.Equal(t, "ab\ncd", truncateLines("ab\ncd", 10))
}

func TestRead10KAnnualExhibit(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	body := func(s string) string { return "<p>" + strings.Repeat(s+" ", 40) + "</p>" }
	// 富国银行：Item 1A 和 Item 7 只引用年报，风险因素和 MD&A（Financial Review）在附件 EX-13 里，每页有「(continued)」页眉。
	main := `<p>ITEM 1A. RISK FACTORS</p><p>Information in response to this Item 1A can be found in the 2025 Annual Report under “Financial Review – Risk Factors.”</p>
<p>ITEM 7. MANAGEMENT’S DISCUSSION AND ANALYSIS</p><p>Information in response to this Item 7 can be found in the 2025 Annual Report under “Financial Review.”</p>
<p>ITEM 8. FINANCIAL STATEMENTS</p><p>See the Annual Report.</p>`
	// 年报开头的目录也有「Financial Review」标题，下一行是带页码的目录行，不从这里开始。
	annual := `<p><b>Exhibit 13</b></p><p><b>Financial Review</b></p><p>2 Overview 114 4 Equity Securities</p>` + body("62 Risk Factors 134 14 Fair Value Measurements") +
		`<p><b>Financial Review</b></p>` + body("Net income rose to $21.3 billion in 2025 on higher fee income.") +
		`<p><b>Financial Review (continued)</b></p>` + body("Deposits grew across segments.") + `
<p><b>Risk Factors</b></p>` + body("An investment in the Company involves risk.") + `
<p><b>REGULATORY RISKS</b></p><p><b>Current and future legislation could require us to change certain business practices.</b> Rules are changing.</p>` + body("Regulators may act.") + `
<p><b>Risk Factors (continued)</b></p><p><b>We face significant competition in financial services.</b> Competitors are many.</p>` + body("Fintechs grow.") + `
<p><b>Controls and Procedures</b></p><p><b>Disclosure Controls and Procedures</b></p>` + body("Controls are effective.") + `
<p><b>Management’s Report on Internal Control Over Financial Reporting</b></p><p>Management is responsible.</p>`
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("10-K", [2]string{docFolder + "wfc-10k.htm", "10-K"}, [2]string{docFolder + "wfc-ex13.htm", "EX-13"}))
	gock.New("https://www.sec.gov").Get(docFolder + "wfc-10k.htm").Reply(200).BodyString(main)
	gock.New("https://www.sec.gov").Get(docFolder + "wfc-ex13.htm").Reply(200).BodyString(annual)
	doc, err := client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.True(t, gock.IsDone(), "年报附件只下载一次")
	require.Len(t, doc.Sections, 2)
	ex13 := "https://www.sec.gov" + docFolder + "wfc-ex13.htm"
	require.Equal(t, ex13, doc.Sections[0].URL, "引用链接到附件")
	require.Equal(t, ex13, doc.Sections[1].URL)
	require.True(t, strings.HasPrefix(doc.Sections[0].Text, "Risk Factors\nAn investment"), doc.Sections[0].Text[:40])
	require.NotContains(t, doc.Sections[0].Text, "(continued)", "(continued) 页眉不放进正文，也不结束章节")
	require.NotContains(t, doc.Sections[0].Text, "Controls are effective", "到下一个同样式的章标题为止")
	require.True(t, strings.HasPrefix(doc.Sections[1].Text, "Financial Review\nNet income rose"), "跳过目录上的标题："+doc.Sections[1].Text[:40])
	require.Contains(t, doc.Sections[1].Text, "Deposits grew")
	require.NotContains(t, doc.Sections[1].Text, "An investment in the Company", "MD&A 到风险因素的标题为止，不重复摘要")
	require.Contains(t, strings.Join(doc.Notes, ""), "Item 1A 只是引用年报，按「Risk Factors」标题在年报附件 Exhibit 13 里取出风险因素")
	require.Contains(t, strings.Join(doc.Notes, ""), "Item 7 只是引用年报，按「Financial Review」标题在年报附件 Exhibit 13 里取出管理层讨论与分析")
	var titles []string
	for _, r := range doc.RiskFactors {
		titles = append(titles, r.Category+" | "+r.Title)
	}
	require.Equal(t, []string{"REGULATORY RISKS | Current and future legislation could require us to change certain business practices.",
		"REGULATORY RISKS | We face significant competition in financial services."}, titles)

	// 主文件里找得到两节时不下载年报附件（没有为附件注册响应，下载会报错）。
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("10-K", [2]string{docFolder + "acme-10k.htm", "10-K"}, [2]string{docFolder + "wfc-ex13.htm", "EX-13"}))
	gock.New("https://www.sec.gov").Get(docFolder + "acme-10k.htm").Reply(200).BodyString(tenK(strings.Repeat("Adverse economic conditions could harm demand. ", 20), strings.Repeat("Net sales increased 6% due to higher iPhone sales. ", 40)))
	doc, err = client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Len(t, doc.Sections, 2)
	require.True(t, gock.IsDone())

	// 风险因素在主文件里、只有 Item 7 引用年报时：风险因素对比不为 Item 7 下载附件；摘要下载附件，附件超过大小时
	// 照常返回风险因素，在说明里写明附件无法读取。
	partial := tenK(strings.Repeat("Adverse economic conditions could harm demand. ", 20), "See the Annual Report.")
	index := filingIndex("10-K", [2]string{docFolder + "acme-10k.htm", "10-K"}, [2]string{docFolder + "wfc-ex13.htm", "EX-13"})
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(index)
	gock.New("https://www.sec.gov").Get(docFolder + "acme-10k.htm").Reply(200).BodyString(partial)
	doc, err = client.readTenK(context.Background(), "https://www.sec.gov"+docIndex, "1a")
	require.NoError(t, err)
	require.Len(t, doc.Sections, 1)
	require.True(t, gock.IsDone(), "没有下载附件")
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(index)
	gock.New("https://www.sec.gov").Get(docFolder + "acme-10k.htm").Reply(200).BodyString(partial)
	gock.New("https://www.sec.gov").Get(docFolder + "wfc-ex13.htm").Reply(200).BodyString(strings.Repeat("a", MaxDocumentBytes+1))
	doc, err = client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Equal(t, "Item 1A", doc.Sections[0].Label)
	require.Contains(t, strings.Join(doc.Notes, ""), "年报附件 Exhibit 13 无法读取")
	require.True(t, gock.IsDone())

	// 附件下载失败可能是暂时的，报错，不返回缺了一节的结果。
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(index)
	gock.New("https://www.sec.gov").Get(docFolder + "acme-10k.htm").Reply(200).BodyString(partial)
	gock.New("https://www.sec.gov").Get(docFolder + "wfc-ex13.htm").Reply(404)
	_, err = client.Read10K(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorIs(t, err, ErrDownload)
}
