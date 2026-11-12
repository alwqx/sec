package cninfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alwqx/sec/utils"
	"github.com/ledongthuc/pdf"
)

// 公告摘要只处理有限大小的文本 PDF；扫描件不做 OCR，超限不静默截断。
const (
	MaxDocumentBytes = 20 * 1024 * 1024
	MaxDocumentPages = 200
	MaxDocumentRunes = 400000
)

var (
	documentPath = regexp.MustCompile(`^/finalpage/(\d{4}-\d{2}-\d{2})/(\d+)\.[pP][dD][fF]$`)
	ErrDocument  = errors.New("公告 PDF 无法读取")
)

// DocumentPage 的页码是 PDF 的物理页码，从 1 开始，不是正文印刷页码。
type DocumentPage struct {
	Number int    `json:"page"`
	Text   string `json:"text"`
}

type Document struct {
	ID    string         `json:"id"`
	URL   string         `json:"url"`
	AsOf  string         `json:"as_of"` // CDN 路径中的披露日期，不是报告期
	Pages []DocumentPage `json:"pages"`
}

// DocumentURL 严格限制巨潮 CDN 文件，兼容上游提供的 HTTP 地址并升级为 HTTPS。
// 不接受详情页、任意域名、查询参数、端口、编码路径或非数字文档 ID。
func DocumentURL(raw string) (*Document, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != "static.cninfo.com.cn" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" {
		return nil, fmt.Errorf("%w：请使用巨潮资讯的 PDF 原文链接", ErrDocument)
	}
	m := documentPath.FindStringSubmatch(u.Path)
	if m == nil {
		return nil, fmt.Errorf("%w：无效的巨潮资讯 PDF 路径", ErrDocument)
	}
	if _, err := time.Parse("2006-01-02", m[1]); err != nil {
		return nil, fmt.Errorf("%w：PDF 路径中的日期无效", ErrDocument)
	}
	u.Scheme = "https"
	return &Document{ID: m[2], URL: u.String(), AsOf: m[1]}, nil
}

// ReadDocument 下载并按页提取文本。重定向也检查来源，防止服务端请求离开巨潮 CDN。
func ReadDocument(ctx context.Context, raw string) (*Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	doc, err := DocumentURL(raw)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("%w：重定向次数过多", ErrDocument)
		}
		if req.URL.Scheme != "https" {
			return fmt.Errorf("%w：重定向必须使用 HTTPS", ErrDocument)
		}
		_, err := DocumentURL(req.URL.String())
		return err
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, doc.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", utils.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w：下载失败：%w", ErrDocument, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w：下载返回 HTTP %d", ErrDocument, resp.StatusCode)
	}
	if resp.ContentLength > MaxDocumentBytes {
		return nil, fmt.Errorf("%w：文件超过 20 MB", ErrDocument)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w：下载未完成：%w", ErrDocument, err)
	}
	if len(data) > MaxDocumentBytes {
		return nil, fmt.Errorf("%w：文件超过 20 MB", ErrDocument)
	}
	doc.Pages, err = extractDocumentPages(ctx, data)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

type documentReader struct {
	ctx context.Context
	*bytes.Reader
}

func (r documentReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.ReadAt(p, off)
}

func extractDocumentPages(ctx context.Context, data []byte) (pages []DocumentPage, err error) {
	// 第三方解析器遇到损坏文件时可能 panic，不能让 CLI 或 Desktop 随之退出。
	defer func() {
		if recover() != nil {
			pages, err = nil, fmt.Errorf("%w：PDF 损坏或编码不支持", ErrDocument)
		}
		if ctx.Err() != nil {
			pages, err = nil, ctx.Err()
		}
	}()
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, fmt.Errorf("%w：返回内容不是 PDF", ErrDocument)
	}
	reader, err := pdf.NewReader(documentReader{ctx, bytes.NewReader(data)}, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w：PDF 损坏、加密或编码不支持", ErrDocument)
	}
	count := reader.NumPage()
	if count < 1 || count > MaxDocumentPages {
		return nil, fmt.Errorf("%w：页数须在 1 至 %d 页之间", ErrDocument, MaxDocumentPages)
	}
	total := 0
	for n := 1; n <= count; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text := pageText(reader.Page(n))
		text = strings.TrimSpace(strings.Map(func(r rune) rune {
			if r == '\n' || r == '\t' || !unicode.IsControl(r) {
				return r
			}
			return -1
		}, text))
		total += utf8.RuneCountInString(text)
		if total > MaxDocumentRunes {
			return nil, fmt.Errorf("%w：文本超过 %d 字", ErrDocument, MaxDocumentRunes)
		}
		pages = append(pages, DocumentPage{Number: n, Text: text})
	}
	if total == 0 {
		return nil, fmt.Errorf("%w：未提取到文本，可能是扫描件；暂不支持 OCR", ErrDocument)
	}
	return pages, nil
}

// textMatrix 是 PDF 的 3×3 变换矩阵 [a b 0; c d 0; e f 1]，只存前两列。
type textMatrix [6]float64

var identityMatrix = textMatrix{1, 0, 0, 1, 0, 0}

func (m textMatrix) mul(n textMatrix) textMatrix {
	return textMatrix{
		m[0]*n[0] + m[1]*n[2], m[0]*n[1] + m[1]*n[3],
		m[2]*n[0] + m[3]*n[2], m[2]*n[1] + m[3]*n[3],
		m[4]*n[0] + m[5]*n[2] + n[4], m[4]*n[1] + m[5]*n[3] + n[5],
	}
}

func translate(x, y float64) textMatrix { return textMatrix{1, 0, 0, 1, x, y} }

// pageText 按文字在页面上的位置拼接文本。巨潮的 PDF 常把一行拆成很多文本对象（一个数字也会拆开），
// 库自带的 GetPlainText 在每个文本对象前加换行，会把 2026 拆成 202 和 6；GetTextByRow 又不跟踪 Td 等移动。
// 这里自己跟踪文本矩阵：同一行紧挨着的片段直接相连，间距大（如表格的相邻单元格）时加空格，换行或回退时换行，
// 避免把不同单元格的数字拼成一个数。字宽没有可靠来源，按中文全角 1 em、其他 0.5 em 估算。
func pageText(page pdf.Page) string {
	var out strings.Builder
	fonts := map[string]pdf.TextEncoding{}
	var enc pdf.TextEncoding
	var size, leading float64
	ctm, tm, tlm := identityMatrix, identityMatrix, identityMatrix
	var saved []textMatrix
	started := false
	var endX, lastY, lastEm float64
	show := func(raw string) {
		text := raw
		if enc != nil {
			text = enc.Decode(raw)
		}
		if text == "" {
			return
		}
		trm := textMatrix{size, 0, 0, size, 0, 0}.mul(tm).mul(ctm)
		x, y := trm[4], trm[5]
		em := math.Hypot(trm[0], trm[1])
		if em == 0 {
			em = 1
		}
		if started {
			switch {
			case math.Abs(y-lastY) > 0.5*math.Max(em, lastEm) || x < endX-0.5*em:
				out.WriteByte('\n')
			case x-endX > 0.3*em:
				out.WriteByte(' ')
			}
		}
		out.WriteString(text)
		width := 0.0
		for _, r := range text {
			if unicode.Is(unicode.Han, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef) {
				width++
			} else {
				width += 0.5
			}
		}
		endX, lastY, lastEm, started = x+width*em, y, em, true
		// 同一文本对象里没有用 Td 定位的后续文字接在估算的宽度之后。
		tm = translate(width*size, 0).mul(tm)
	}
	nextLine := func() {
		tlm = translate(0, -leading).mul(tlm)
		tm = tlm
	}
	pdf.Interpret(page.V.Key("Contents"), func(stk *pdf.Stack, op string) {
		args := make([]pdf.Value, stk.Len())
		for i := len(args) - 1; i >= 0; i-- {
			args[i] = stk.Pop()
		}
		num := func(i int) float64 { return args[i].Float64() }
		switch {
		case op == "q":
			saved = append(saved, ctm)
		case op == "Q" && len(saved) > 0:
			ctm, saved = saved[len(saved)-1], saved[:len(saved)-1]
		case op == "cm" && len(args) == 6:
			ctm = textMatrix{num(0), num(1), num(2), num(3), num(4), num(5)}.mul(ctm)
		case op == "BT":
			tm, tlm = identityMatrix, identityMatrix
		case op == "Tf" && len(args) == 2:
			name := args[0].Name()
			if _, ok := fonts[name]; !ok {
				font := page.Font(name)
				fonts[name] = font.Encoder()
			}
			enc, size = fonts[name], num(1)
		case op == "TL" && len(args) == 1:
			leading = num(0)
		case op == "Tm" && len(args) == 6:
			tlm = textMatrix{num(0), num(1), num(2), num(3), num(4), num(5)}
			tm = tlm
		case (op == "Td" || op == "TD") && len(args) == 2:
			if op == "TD" {
				leading = -num(1)
			}
			tlm = translate(num(0), num(1)).mul(tlm)
			tm = tlm
		case op == "T*":
			nextLine()
		case op == "Tj" && len(args) == 1:
			show(args[0].RawString())
		case (op == "'" && len(args) == 1) || (op == "\"" && len(args) == 3):
			nextLine()
			show(args[len(args)-1].RawString())
		case op == "TJ" && len(args) == 1:
			for i := 0; i < args[0].Len(); i++ {
				if item := args[0].Index(i); item.Kind() == pdf.String {
					show(item.RawString())
				} else {
					// 数字是以千分之一 em 为单位的反向位移，较大的负值相当于空格。
					tm = translate(-item.Float64()/1000*size, 0).mul(tm)
				}
			}
		}
	})
	return out.String()
}
