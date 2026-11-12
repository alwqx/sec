package cninfo

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/alwqx/sec/internal/testpdf"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const documentTestURL = "https://static.cninfo.com.cn/finalpage/2026-11-01/123456.pdf"

func TestDocumentURL(t *testing.T) {
	doc, err := DocumentURL(strings.Replace(documentTestURL, "https:", "http:", 1))
	require.NoError(t, err)
	require.Equal(t, documentTestURL, doc.URL)
	require.Equal(t, "123456", doc.ID)
	require.Equal(t, "2026-11-01", doc.AsOf)
	upper := strings.TrimSuffix(documentTestURL, ".pdf") + ".PDF"
	doc, err = DocumentURL(upper)
	require.NoError(t, err)
	require.Equal(t, upper, doc.URL, "保留大小写，避免请求不同 CDN 对象")
	for _, raw := range []string{
		"", "file:///etc/passwd", "https://example.com/finalpage/2026-11-01/123456.pdf",
		"https://static.cninfo.com.cn.evil.test/finalpage/2026-11-01/123456.pdf",
		strings.Replace(documentTestURL, ".cn/", ".cn:443/", 1),
		strings.Replace(documentTestURL, "/finalpage/", "/%66inalpage/", 1),
		strings.Replace(documentTestURL, "11-01", "02-31", 1),
		strings.Replace(documentTestURL, "123456", "../123456", 1),
		documentTestURL + "?", documentTestURL + "?target=http://localhost", documentTestURL + "#page=1",
		strings.Replace(documentTestURL, "https://", "https://user@", 1),
	} {
		_, err := DocumentURL(raw)
		require.ErrorIs(t, err, ErrDocument, raw)
	}
}

func TestReadDocument(t *testing.T) {
	defer gock.Off()
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-11-01/123456.pdf").Reply(200).
		Body(bytes.NewReader(testpdf.Build("公司公告：收入 1,200 万元。", "", "风险提示：报告期为 2025 年。")))
	doc, err := ReadDocument(context.Background(), documentTestURL)
	require.NoError(t, err)
	require.Len(t, doc.Pages, 3)
	require.Equal(t, DocumentPage{Number: 1, Text: "公司公告：收入 1,200 万元。"}, doc.Pages[0])
	require.Equal(t, DocumentPage{Number: 2}, doc.Pages[1])
	require.Equal(t, DocumentPage{Number: 3, Text: "风险提示：报告期为 2025 年。"}, doc.Pages[2])
	require.True(t, gock.IsDone())
}

func TestDocumentFailures(t *testing.T) {
	defer gock.Off()
	for _, body := range [][]byte{[]byte("<html>error</html>"), []byte("%PDF-1.7\nbroken"), testpdf.Build("", "")} {
		gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-11-01/123456.pdf").Reply(200).Body(bytes.NewReader(body))
		_, err := ReadDocument(context.Background(), documentTestURL)
		require.ErrorIs(t, err, ErrDocument)
	}
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-11-01/123456.pdf").Reply(404)
	_, err := ReadDocument(context.Background(), documentTestURL)
	require.ErrorContains(t, err, "404")
	gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-11-01/123456.pdf").Reply(200).
		Body(bytes.NewReader(bytes.Repeat([]byte("x"), MaxDocumentBytes+1)))
	_, err = ReadDocument(context.Background(), documentTestURL)
	require.ErrorContains(t, err, "20 MB")
	for _, target := range []string{"http://127.0.0.1/private.pdf", "https://example.com/file.pdf", strings.Replace(documentTestURL, "https:", "http:", 1)} {
		gock.New("https://static.cninfo.com.cn").Get("/finalpage/2026-11-01/123456.pdf").Reply(http.StatusFound).SetHeader("Location", target)
		_, err = ReadDocument(context.Background(), documentTestURL)
		require.ErrorIs(t, err, ErrDocument)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ReadDocument(ctx, documentTestURL)
	require.ErrorIs(t, err, context.Canceled)
}

func TestDocumentExtractionLimits(t *testing.T) {
	_, err := extractDocumentPages(context.Background(), testpdf.Build(make([]string, MaxDocumentPages+1)...))
	require.ErrorContains(t, err, "页数")
	_, err = extractDocumentPages(context.Background(), testpdf.Build(strings.Repeat("文", MaxDocumentRunes+1)))
	require.ErrorContains(t, err, "文本超过")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = extractDocumentPages(ctx, testpdf.Build("文本"))
	require.ErrorIs(t, err, context.Canceled)
}

func TestExtractJoinsFragmentsByPosition(t *testing.T) {
	// 字号 12：数字估算宽 6pt、中文 12pt。202 和 6 分在两个文本对象里但紧挨着，应拼回 2026；
	// 表格同一行相距较远的两个单元格加空格，不能拼成一个数字；下一行另起一行。
	data := testpdf.BuildFragments([]testpdf.Fragment{
		{X: 50, Y: 700, Text: "202"}, {X: 68, Y: 700, Text: "6"}, {X: 74, Y: 700, Text: "年总资产"},
		{X: 122, Y: 700, Text: "27,"}, {X: 140, Y: 700, Text: "859"}, {X: 158, Y: 700, Text: ".52"}, {X: 176, Y: 700, Text: "亿元"},
		{X: 50, Y: 680, Text: "1,200"}, {X: 200, Y: 680, Text: "18.07"},
		{X: 50, Y: 660, Text: "下一行"},
	})
	pages, err := extractDocumentPages(context.Background(), data)
	require.NoError(t, err)
	require.Equal(t, "2026年总资产27,859.52亿元\n1,200 18.07\n下一行", pages[0].Text)
}
