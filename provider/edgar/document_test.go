package edgar

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const (
	docFolder = "/Archives/edgar/data/320193/000032019326000024/"
	docIndex  = docFolder + "0000320193-26-000024-index.htm"
)

func TestParseFilingURL(t *testing.T) {
	for _, raw := range []string{
		"https://www.sec.gov" + docFolder + "aapl-20261102.htm",
		"https://www.sec.gov" + docIndex,
	} {
		ref, err := ParseFilingURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, FilingRef{CIK: 320193, Accession: "0000320193-26-000024", IndexURL: "https://www.sec.gov" + docIndex}, *ref)
	}
	for _, raw := range []string{
		"http://www.sec.gov" + docIndex, "https://evil.test" + docIndex, "https://www.sec.gov" + docIndex + "?x=1",
		"https://www.sec.gov/Archives/edgar/data/320193/aapl.htm", "https://www.sec.gov/Archives/edgar/data/320193/0000320193/a.htm",
		"https://www.sec.gov" + docFolder, "https://www.sec.gov" + docFolder + "sub/a.htm", "https://www.sec.gov/Archives/edgar/data/0/000032019326000024/a.htm",
	} {
		_, err := ParseFilingURL(raw)
		require.ErrorIs(t, err, ErrDocument, raw)
	}
	// 前导零和浏览器里的内联 XBRL 查看器地址指向同一份申报。
	for _, raw := range []string{
		"https://www.sec.gov/Archives/edgar/data/0000320193/000032019326000024/aapl-20261102.htm",
		"https://www.sec.gov/ix?doc=" + docFolder + "aapl-20261102.htm",
	} {
		ref, err := ParseFilingURL(raw)
		require.NoError(t, err, raw)
		require.Equal(t, "https://www.sec.gov"+docIndex, ref.IndexURL, raw)
	}
	for _, raw := range []string{
		"https://www.sec.gov/ix?doc=" + docFolder + "a.htm&x=1", "https://www.sec.gov/ix?doc=https://evil.test/a.htm", "http://www.sec.gov/ix?doc=" + docFolder + "a.htm",
	} {
		_, err := ParseFilingURL(raw)
		require.ErrorIs(t, err, ErrDocument, raw)
	}
}

// filingIndex 按 SEC 申报目录页的格式生成页面：rows 是 {链接, 类型}。
func filingIndex(form string, rows ...[2]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div id="formName"><strong>Form %s</strong> - Current report</div><div class="formGrouping">
<div class="infoHead">Filing Date</div><div class="info">2026-11-02</div><div class="infoHead">Accepted</div><div class="info">2026-11-02 16:30:23</div></div>
<div class="formGrouping"><div class="infoHead">Period of Report</div><div class="info">2026-10-30</div></div>
<table class="tableFile" summary="Document Format Files"><tr><th>Seq</th><th>Description</th><th>Document</th><th>Type</th><th>Size</th></tr>`, form)
	for i, r := range rows {
		fmt.Fprintf(&b, `<tr><td>%d</td><td></td><td><a href="%s">x</a></td><td>%s</td><td>1</td></tr>`, i+1, r[0], r[1])
	}
	b.WriteString(`</table><table class="tableFile" summary="Data Files"><tr><td>3</td><td></td><td><a href="` + docFolder + `ex99.htm">x</a></td><td>EX-99.9</td><td>1</td></tr></table>`)
	return b.String()
}

const primary8K = `<html><head><title>8-K</title></head><body>
<div style="display: none"><ix:header><ix:hidden>dei:EntityCentralIndexKey 0000320193</ix:hidden></ix:header></div>
<p>FORM 8-K</p><p>Date of Report: October 30, 2026</p>
<p><b>Item&#160;2.02</b> Results of Operations and Financial Condition.</p>
<p>On November 2, 2026, the Company reported revenue of $1<span>14</span>.5 billion. See Exhibit
99.1.</p>
<table><tr><td><p>Item 5.02</p></td><td><p>Departure of Directors.</p></td></tr></table>
<p>Jane Doe resigned as a director.</p>
<p>ITEM 2.02. (continued)</p><p>Guidance unchanged.</p>
<p>Item 9.01 Financial Statements and Exhibits.</p><p>99.1 Press release</p>
<p>SIGNATURE</p><p>Item 8.01 should not appear after signature.</p>
</body></html>`

func TestRead8K(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("8-K",
		[2]string{"/ix?doc=" + docFolder + "aapl-20261102.htm", "8-K"},
		[2]string{docFolder + "ex991.htm", "EX-99.1"},
		[2]string{docFolder + "ex992.pdf", "EX-99.2"},
		[2]string{docFolder + "ex101.htm", "EX-10.1"},
		[2]string{"https://evil.test/ex993.htm", "EX-99.3"},
		[2]string{"/Archives/edgar/data/320193/000032019326000099/ex994.htm", "EX-99.4"},
		[2]string{"/Archives/edgar/data/1234/000032019326000024/ex995.htm", "EX-99.5"},
		[2]string{docFolder + "g1.jpg", "GRAPHIC"},
	))
	gock.New("https://www.sec.gov").Get(docFolder + "aapl-20261102.htm").Reply(200).BodyString(primary8K)
	gock.New("https://www.sec.gov").Get(docFolder + "ex991.htm").Reply(200).BodyString(`<p>Exhibit 99.1</p><table><tr><td><p>Net</p><p>sales</p></td><td>$</td><td>114,477</td></tr></table>`)
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/1234/000032019326000024/ex995.htm").Reply(200).BodyString(`<p>Co-registrant exhibit</p>`)

	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	// 链接里的 CIK 带前导零，目录页链接不带，也能对上同一个申报目录。
	doc, err := client.Read8K(context.Background(), "https://www.sec.gov/Archives/edgar/data/0000320193/000032019326000024/ex991.htm")
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, "8-K", doc.Form)
	require.Equal(t, "2026-11-02", doc.FilingDate)
	require.Equal(t, "2026-10-30", doc.ReportDate)
	require.Equal(t, "0000320193-26-000024", doc.Accession)
	require.Equal(t, "https://www.sec.gov"+docFolder+"aapl-20261102.htm", doc.URL, "内联 XBRL 查看器链接还原为文件地址")
	require.Equal(t, []DocumentSection{
		{Label: "Item 2.02", URL: doc.URL, Text: "Item 2.02 Results of Operations and Financial Condition.\nOn November 2, 2026, the Company reported revenue of $114.5 billion. See Exhibit 99.1.\nITEM 2.02. (continued)\nGuidance unchanged."},
		{Label: "Item 5.02", URL: doc.URL, Text: "Item 5.02 Departure of Directors.\nJane Doe resigned as a director."},
		{Label: "Exhibit 99.1", URL: "https://www.sec.gov" + docFolder + "ex991.htm", Text: "Exhibit 99.1\nNet sales $ 114,477"},
		{Label: "Exhibit 99.5", URL: "https://www.sec.gov/Archives/edgar/data/1234/000032019326000024/ex995.htm", Text: "Co-registrant exhibit"},
	}, doc.Sections, "跳过封面、隐藏的 XBRL、Item 9.01 和签名；同一 Item 合并；表格一行保持在一行；共同申报人 CIK 下的同一申报目录也算")
	require.Equal(t, []string{"Exhibit 99.2 不是 HTML 或文本文件，未参与摘要。"}, doc.Notes, "其他申报目录和其他域名的链接直接忽略")
}

func TestRead8KRejects(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	link := "https://www.sec.gov" + docIndex

	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("10-K", [2]string{docFolder + "a.htm", "10-K"}))
	_, err := client.Read8K(context.Background(), link)
	require.ErrorContains(t, err, "需要 8-K，这份申报是 10-K")
	require.ErrorIs(t, err, ErrUnsupportedForm)

	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("8-K", [2]string{docFolder + "a.pdf", "8-K"}))
	_, err = client.Read8K(context.Background(), link)
	require.ErrorContains(t, err, "没有 HTML 或文本格式的 8-K 主文件")

	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("8-K/A", [2]string{docFolder + "a.htm", "8-K/A"}))
	gock.New("https://www.sec.gov").Get(docFolder + "a.htm").Reply(200).BodyString("<p>Amendment without items.</p>")
	_, err = client.Read8K(context.Background(), link)
	require.ErrorContains(t, err, "没有找到 Item 段落")

	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString("<html>not an index</html>")
	_, err = client.Read8K(context.Background(), link)
	require.ErrorContains(t, err, "没有表格类型或申报日期")

	gock.New("https://www.sec.gov").Get(docIndex).Reply(404)
	_, err = client.Read8K(context.Background(), link)
	require.ErrorIs(t, err, ErrDownload, "下载失败可以稍后重试，和内容问题分开")
	require.ErrorIs(t, err, ErrDocument)
	require.True(t, gock.IsDone())
}

func TestRead8KPlainTextAndLimits(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("8-K",
		[2]string{docFolder + "a.txt", "8-K"}, [2]string{docFolder + "b.htm", "EX-99.1"}, [2]string{docFolder + "c.htm", "EX-99.2"}))
	// 旧申报的纯文本主文件保留原有换行。
	gock.New("https://www.sec.gov").Get(docFolder + "a.txt").Reply(200).BodyString("COVER\r\nItem 1.01  Entry into a Material Agreement\r\nThe Company signed\r\na contract.\r\n")
	gock.New("https://www.sec.gov").Get(docFolder + "b.htm").Reply(200).BodyString("<p>" + strings.Repeat("a ", MaxDocumentRunes/2) + "</p>")
	gock.New("https://www.sec.gov").Get(docFolder + "c.htm").Reply(200).BodyString(`<p style="DISPLAY: NONE">hidden</p>`)
	doc, err := client.Read8K(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.Equal(t, []DocumentSection{{Label: "Item 1.01", URL: "https://www.sec.gov" + docFolder + "a.txt", Text: "Item 1.01 Entry into a Material Agreement\nThe Company signed\na contract."}}, doc.Sections)
	require.Equal(t, []string{"Exhibit 99.1 太长，加上后超过 400000 字，未参与摘要。", "Exhibit 99.2 没有可提取的文字，未参与摘要。"}, doc.Notes)
}

func TestRead8KExhibits(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	rows := [][2]string{{docFolder + "a.htm", "8-K"}, {docFolder + "p.pdf", "EX-99.1"}}
	for _, name := range []string{"b", "c", "d", "e", "f", "g"} {
		rows = append(rows, [2]string{docFolder + name + ".htm", "EX-99"})
	}
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("8-K", rows...))
	gock.New("https://www.sec.gov").Get(docFolder + "a.htm").Reply(200).BodyString("<p>Item 8.01 Other Events.</p>")
	for _, name := range []string{"b", "c", "d", "e", "f"} {
		gock.New("https://www.sec.gov").Get(docFolder + name + ".htm").Reply(200).BodyString("<p>exhibit " + name + "</p>")
	}
	doc, err := client.Read8K(context.Background(), "https://www.sec.gov"+docIndex)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	var labels []string
	for _, s := range doc.Sections {
		labels = append(labels, s.Label)
	}
	require.Equal(t, []string{"Item 8.01", "Exhibit 99", "Exhibit 99 (c.htm)", "Exhibit 99 (d.htm)", "Exhibit 99 (e.htm)", "Exhibit 99 (f.htm)"}, labels,
		"跳过的 PDF 不占附件名额；类型相同的附件用文件名区分")
	require.Equal(t, []string{"Exhibit 99.1 不是 HTML 或文本文件，未参与摘要。", "附件超过 5 个，Exhibit 99 (g.htm) 未参与摘要。"}, doc.Notes)
}

func TestRead8KTooLarge(t *testing.T) {
	defer gock.Off()
	client := &Client{Email: "test@example.com", CacheDir: t.TempDir()}
	gock.New("https://www.sec.gov").Get(docIndex).Reply(200).BodyString(filingIndex("8-K", [2]string{docFolder + "a.htm", "8-K"}))
	gock.New("https://www.sec.gov").Get(docFolder + "a.htm").Reply(200).BodyString(strings.Repeat("a", MaxDocumentBytes+1))
	_, err := client.Read8K(context.Background(), "https://www.sec.gov"+docIndex)
	require.ErrorIs(t, err, ErrDocument)
	require.ErrorContains(t, err, "a.htm 超过 20 MB")
	require.NotErrorIs(t, err, ErrDownload, "文件太大是内容问题，重试也不会成功")
}
