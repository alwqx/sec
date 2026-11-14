package edgar

import (
	"context"
	"strings"
	"testing"

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
	require.ErrorContains(t, err, "需要 10-K，这份申报是 8-K")
	require.True(t, gock.IsDone())
}

func TestPageFurniture(t *testing.T) {
	for _, line := range []string{"12", "Page 5", "5 of 120", "Table of Contents", "PART II", "Parts II and III", "Item 1A", "Item 1B, 1C", "Apple Inc. | 2026 Form 10-K | 7"} {
		require.True(t, pageFurniture(line), line)
	}
	for _, line := range []string{"2026 Change 2025", "(In millions)", "Item 7. Management’s Discussion", "Competition", "This Annual Report on Form 10-K contains forward-looking statements within the meaning of the Private Securities Litigation Reform Act."} {
		require.False(t, pageFurniture(line), line)
	}
}
