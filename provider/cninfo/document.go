package cninfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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
		text, err := reader.Page(n).GetPlainText(nil)
		if err != nil {
			return nil, fmt.Errorf("%w：第 %d 页提取失败", ErrDocument, n)
		}
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
