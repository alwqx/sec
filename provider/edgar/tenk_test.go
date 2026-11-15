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
	doc, err := read10K(&FilingDocument{}, lines)
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
	doc, err = read10K(&FilingDocument{}, lines)
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
	doc, err := read10K(&FilingDocument{}, lines)
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
	doc, err := read10K(&FilingDocument{}, lines)
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
