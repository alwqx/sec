package edgar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

const (
	// MaxDocumentBytes 是单个申报文件的大小上限。
	MaxDocumentBytes = 20 * 1024 * 1024
	// MaxDocumentRunes 是参与摘要的文本总字数上限：主文件超过时报错，附件超过时跳过并说明。
	MaxDocumentRunes = 400000
	// maxExhibits 是读取的 EX-99 附件数上限。
	maxExhibits = 5
)

// ErrDocument 表示申报原文无法读取或不能摘要。
var ErrDocument = errors.New("SEC 申报原文无法读取")

// ErrDownload 表示从 SEC 读取申报目录或文件失败（网络错误、HTTP 错误），稍后重试可能成功；其他 ErrDocument
// 是申报内容的问题（不是 8-K、没有 Item、超过大小），重试也不会成功。它也是 ErrDocument。
var ErrDownload = fmt.Errorf("%w：从 SEC 下载失败", ErrDocument)

// ErrUnsupportedForm 表示申报类型不是读取的那一种（比如用 Read8K 读 10-K），不能摘要。返回的错误也是 ErrDocument。
var ErrUnsupportedForm = errors.New("申报类型不支持")

// unsupportedForm 是 ErrUnsupportedForm 的具体错误，说明实际和需要的类型，不带层层前缀，Desktop 直接显示。
type unsupportedForm struct{ got, want string }

func (e unsupportedForm) Error() string {
	return fmt.Sprintf("这份申报是 %s，不是 %s", e.got, e.want)
}

func (e unsupportedForm) Is(target error) bool {
	return target == ErrUnsupportedForm || target == ErrDocument
}

var (
	filingPath   = regexp.MustCompile(`^/Archives/edgar/data/(\d+)/(\d{18})/[^/]+$`)
	itemHeading  = regexp.MustCompile(`(?i)^item\s*(\d{1,2}\.\d{2})\b`)
	signatures   = regexp.MustCompile(`(?i)^signatures?$`)
	exhibitType  = regexp.MustCompile(`(?i)^EX-(99(?:\.\d+)*)$`)
	textDocument = map[string]bool{".htm": true, ".html": true, ".txt": true}
	// blockTags 前后换行，其余标签（span、font 等）里的文字直接相连，不把一个数字拆开。
	blockTags = map[string]bool{
		"p": true, "div": true, "br": true, "tr": true, "li": true, "ul": true, "ol": true, "table": true, "hr": true,
		"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "center": true, "pre": true, "blockquote": true,
	}
)

// FilingRef 是从申报文件地址解析出的申报：同一申报目录下的任何文件都指向同一份申报。
type FilingRef struct {
	CIK       int
	Accession string // 0000320193-26-000024
	IndexURL  string // 申报目录页，列出主文件和附件
}

// ParseFilingURL 解析 SEC 申报目录下的文件地址，比如 sec filings 列出的主文件或目录页链接，
// 也接受浏览器里内联 XBRL 查看器的地址 https://www.sec.gov/ix?doc=/Archives/...。
// 链接里的 CIK 可能带前导零，目录页地址统一用不带前导零的 CIK，同一份申报的不同写法得到同一个 FilingRef。
func ParseFilingURL(raw string) (*FilingRef, error) {
	if u, err := url.Parse(raw); err == nil && u.Scheme == "https" && u.Host == "www.sec.gov" && u.Path == "/ix" && u.Fragment == "" {
		if doc := u.Query().Get("doc"); len(u.Query()) == 1 && strings.HasPrefix(doc, "/Archives/") {
			raw = "https://www.sec.gov" + doc
		}
	}
	if !IsDocumentURL(raw) {
		return nil, fmt.Errorf("%w：请使用 www.sec.gov/Archives/edgar/data/ 下的申报文件链接", ErrDocument)
	}
	u, _ := url.Parse(raw)
	m := filingPath.FindStringSubmatch(u.Path)
	if m == nil {
		return nil, fmt.Errorf("%w：链接不在某份申报的目录下", ErrDocument)
	}
	cik, err := strconv.Atoi(m[1])
	if err != nil || cik <= 0 {
		return nil, fmt.Errorf("%w：链接里的 CIK 无效", ErrDocument)
	}
	accession := m[2][:10] + "-" + m[2][10:12] + "-" + m[2][12:]
	return &FilingRef{CIK: cik, Accession: accession, IndexURL: archivesURL + strconv.Itoa(cik) + "/" + m[2] + "/" + accession + "-index.htm"}, nil
}

// DocumentSection 是申报里的一节：主文件的一个 Item（如 Item 2.02），或一个附件（如 Exhibit 99.1）。
type DocumentSection struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Text  string `json:"text"`
}

// FilingDocument 是一份申报中参与摘要的文本。
type FilingDocument struct {
	FilingRef
	Form       string
	FilingDate string
	ReportDate string // Period of Report，8-K 是最早事项的发生日
	URL        string // 主文件
	Sections   []DocumentSection
	Notes      []string // 没有读取的附件等说明
}

// indexDocument 是申报目录页文件表里的一行。
type indexDocument struct {
	Type, URL string
}

// readPrimary 读取申报目录页和主文件的文字，是 Read8K、Read10K 共用的部分。forms 是可以读取的基本类型（不含 /A），
// 第一个用在错误说明里。返回申报信息、目录页的文件表和主文件文字。
func (c *Client) readPrimary(ctx context.Context, raw string, forms ...string) (*FilingDocument, []indexDocument, string, error) {
	ref, err := ParseFilingURL(raw)
	if err != nil {
		return nil, nil, "", err
	}
	body, err := c.get(ctx, ref.IndexURL)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w（申报目录）：%w", ErrDownload, err)
	}
	doc := &FilingDocument{FilingRef: *ref}
	docs, err := parseFilingIndex(body, ref, doc)
	if err != nil {
		return nil, nil, "", err
	}
	if base, _ := strings.CutSuffix(doc.Form, "/A"); !slices.Contains(forms, base) {
		return nil, nil, "", unsupportedForm{got: doc.Form, want: forms[0]}
	}
	for _, d := range docs {
		if strings.EqualFold(d.Type, doc.Form) {
			doc.URL = d.URL
			break
		}
	}
	if doc.URL == "" || !textDocument[strings.ToLower(path.Ext(doc.URL))] {
		return nil, nil, "", fmt.Errorf("%w：申报目录里没有 HTML 或文本格式的 %s 主文件", ErrDocument, forms[0])
	}
	text, err := c.documentText(ctx, doc.URL)
	if err != nil {
		return nil, nil, "", err
	}
	return doc, docs, text, nil
}

// Read8K 读取一份 8-K（含 8-K/A）：主文件按 Item 切分（不含封面、签名和 Item 9.01 附件清单），
// EX-99 附件（新闻稿、演示材料等）各为一节。raw 是该申报目录下任意文件的地址。
func (c *Client) Read8K(ctx context.Context, raw string) (*FilingDocument, error) {
	doc, docs, text, err := c.readPrimary(ctx, raw, "8-K")
	if err != nil {
		return nil, err
	}
	var exhibits []indexDocument
	for _, d := range docs {
		if exhibitType.MatchString(d.Type) {
			exhibits = append(exhibits, d)
		}
	}
	doc.Sections = splitItems(text, doc.URL)
	if len(doc.Sections) == 0 {
		return nil, fmt.Errorf("%w：8-K 正文里没有找到 Item 段落", ErrDocument)
	}
	total := 0
	for _, s := range doc.Sections {
		total += utf8.RuneCountInString(s.Text)
	}
	if total > MaxDocumentRunes {
		return nil, fmt.Errorf("%w：8-K 正文超过 %d 字", ErrDocument, MaxDocumentRunes)
	}
	read := 0
	labels := map[string]bool{}
	for _, ex := range exhibits {
		label := "Exhibit " + exhibitType.FindStringSubmatch(ex.Type)[1]
		// 两个附件都标成 EX-99（或重复的 EX-99.1）时用文件名区分，引用和核对不能混在一起。
		if labels[label] {
			label += " (" + path.Base(ex.URL) + ")"
		}
		labels[label] = true
		switch {
		case !textDocument[strings.ToLower(path.Ext(ex.URL))]:
			doc.Notes = append(doc.Notes, fmt.Sprintf("%s 不是 HTML 或文本文件，未参与摘要。", label))
			continue
		case read >= maxExhibits:
			doc.Notes = append(doc.Notes, fmt.Sprintf("附件超过 %d 个，%s 未参与摘要。", maxExhibits, label))
			continue
		}
		read++
		text, err := c.documentText(ctx, ex.URL)
		if err != nil {
			return nil, err
		}
		n := utf8.RuneCountInString(text)
		switch {
		case n == 0:
			doc.Notes = append(doc.Notes, fmt.Sprintf("%s 没有可提取的文字，未参与摘要。", label))
		case total+n > MaxDocumentRunes:
			doc.Notes = append(doc.Notes, fmt.Sprintf("%s 太长，加上后超过 %d 字，未参与摘要。", label, MaxDocumentRunes))
		default:
			total += n
			doc.Sections = append(doc.Sections, DocumentSection{Label: label, URL: ex.URL, Text: text})
		}
	}
	return doc, nil
}

// parseFilingIndex 从申报目录页读取表格类型、申报日、报告期和文件表，文件地址限定在本申报目录下。
func parseFilingIndex(body []byte, ref *FilingRef, doc *FilingDocument) ([]indexDocument, error) {
	page, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w：申报目录页格式无效：%w", ErrDocument, err)
	}
	doc.Form = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(page.Find("#formName strong").First().Text()), "Form "))
	page.Find(".infoHead").Each(func(_ int, head *goquery.Selection) {
		value := strings.TrimSpace(head.NextFiltered(".info").Text())
		switch strings.TrimSpace(head.Text()) {
		case "Filing Date":
			doc.FilingDate = value
		case "Period of Report":
			doc.ReportDate = value
		}
	})
	if doc.Form == "" || doc.FilingDate == "" {
		return nil, fmt.Errorf("%w：申报目录页里没有表格类型或申报日期", ErrDocument)
	}
	accession := strings.ReplaceAll(ref.Accession, "-", "")
	var docs []indexDocument
	page.Find(`table.tableFile[summary="Document Format Files"] tr`).Each(func(_ int, row *goquery.Selection) {
		cells := row.Find("td")
		if cells.Length() < 4 {
			return
		}
		href, _ := cells.Eq(2).Find("a").Attr("href")
		// 内联 XBRL 文件的链接是查看器地址 /ix?doc=/Archives/...。
		href = strings.TrimPrefix(href, "/ix?doc=")
		link := "https://www.sec.gov" + href
		// 按申报编号判断是否在本申报目录下，不比较 CIK：共同申报人的申报在各自的 CIK 下都有同一个目录。
		if !strings.HasPrefix(href, "/") || !IsDocumentURL(link) {
			return
		}
		if m := filingPath.FindStringSubmatch(href); m == nil || m[2] != accession {
			return
		}
		docs = append(docs, indexDocument{Type: strings.TrimSpace(cells.Eq(3).Text()), URL: link})
	})
	return docs, nil
}

// documentText 下载申报文件并转成文字：HTML 按块换行，纯文本保留原有换行。
func (c *Client) documentText(ctx context.Context, link string) (string, error) {
	body, err := c.getLimit(ctx, link, MaxDocumentBytes)
	if errors.Is(err, errTooLarge) {
		return "", fmt.Errorf("%w：%s 超过 20 MB", ErrDocument, path.Base(link))
	}
	if err != nil {
		return "", fmt.Errorf("%w（%s）：%w", ErrDownload, path.Base(link), err)
	}
	if strings.EqualFold(path.Ext(link), ".txt") {
		return cleanLines(string(body)), nil
	}
	return htmlText(body)
}

// htmlText 提取 HTML 的可见文字。跳过脚本、样式、内联 XBRL 的隐藏数据（ix:header、display:none）；
// 块级标签前后换行，表格单元格之间加空格，单元格里的块级标签也只加空格，一行表格保持在一行；其余标签里的文字直接相连。
func htmlText(body []byte) (string, error) {
	root, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w：HTML 格式无效：%w", ErrDocument, err)
	}
	var b strings.Builder
	var walk func(n *html.Node, inCell bool)
	walk = func(n *html.Node, inCell bool) {
		if n.Type == html.TextNode {
			// 源码里的换行只是排版，不是段落。
			b.WriteString(strings.NewReplacer("\r", " ", "\n", " ").Replace(n.Data))
			return
		}
		separator := byte(0)
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "head", "title", "ix:header":
				return
			}
			for _, a := range n.Attr {
				if a.Key == "style" && strings.Contains(strings.ReplaceAll(strings.ToLower(a.Val), " ", ""), "display:none") {
					return
				}
			}
			switch {
			case n.Data == "td" || n.Data == "th":
				separator, inCell = ' ', true
			case blockTags[n.Data] && inCell:
				separator = ' '
			case blockTags[n.Data]:
				separator = '\n'
			}
		}
		if separator != 0 {
			b.WriteByte(separator)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inCell)
		}
		if separator != 0 {
			b.WriteByte(separator)
		}
	}
	walk(root, false)
	return cleanLines(b.String()), nil
}

// cleanLines 合并每行内的连续空白（strings.Fields 也把不换行空格当作空白），去掉空行。
func cleanLines(text string) string {
	var lines []string
	for line := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// splitItems 按行首的 Item 编号切分 8-K 正文：第一个 Item 之前是封面，SIGNATURE 之后是签名，都不参与摘要；
// Item 9.01 只是附件清单，附件另外读取。同一个 Item 出现多次时合并。
func splitItems(text, link string) []DocumentSection {
	var sections []DocumentSection
	current := -1
	for line := range strings.SplitSeq(text, "\n") {
		if signatures.MatchString(line) {
			break
		}
		if m := itemHeading.FindStringSubmatch(line); m != nil {
			current = -1
			if m[1] == "9.01" {
				continue
			}
			label := "Item " + m[1]
			for i, s := range sections {
				if s.Label == label {
					current = i
				}
			}
			if current == -1 {
				sections = append(sections, DocumentSection{Label: label, URL: link})
				current = len(sections) - 1
			}
		}
		if current >= 0 {
			if sections[current].Text != "" {
				sections[current].Text += "\n"
			}
			sections[current].Text += line
		}
	}
	return sections
}
