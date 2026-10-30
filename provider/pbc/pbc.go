// Package pbc 采集中国人民银行（People's Bank of China）的官方统计数据和政策公告。
package pbc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/utils"
	"golang.org/x/net/html/charset"
)

const statisticsPath = "/diaochatongjisi/116219/116319/index.html"

// Client 按官方目录发现统计表和政策公告链接；CacheDir 为空时禁用缓存。
type Client struct {
	CacheDir string
	Refresh  bool
	origin   string
}

func NewClient(cacheDir string) *Client {
	return &Client{CacheDir: cacheDir, origin: "https://www.pbc.gov.cn"}
}

type cachedPage struct {
	FetchedAt   time.Time `json:"fetched_at"`
	Body        []byte    `json:"body"`
	ContentType string    `json:"content_type"`
}

func (c *Client) resolve(base, href string) (string, error) {
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(href)
	if err != nil {
		return "", err
	}
	u = b.ResolveReference(u)
	origin, _ := url.Parse(c.origin)
	if u.Host != origin.Host || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("非人民银行官方链接: %s", href)
	}
	u.Scheme = origin.Scheme
	u.Fragment = ""
	return u.String(), nil
}

func decodePage(page cachedPage) (*goquery.Document, error) {
	reader, err := charset.NewReader(bytes.NewReader(page.Body), page.ContentType)
	if err != nil {
		return nil, fmt.Errorf("解码人民银行页面失败: %w", err)
	}
	return goquery.NewDocumentFromReader(reader)
}

func (c *Client) get(ctx context.Context, address string) (*goquery.Document, time.Time, error) {
	return c.getValidated(ctx, address, nil)
}

// getValidated 在命中和写入缓存前校验指标语义，异常刷新不覆盖有效响应。
func (c *Client) getValidated(ctx context.Context, address string, validate func(*goquery.Document) error) (*goquery.Document, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	address, err := c.resolve(c.origin, address)
	if err != nil {
		return nil, time.Time{}, err
	}
	key := sha256.Sum256([]byte(address))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var page cachedPage
			if json.Unmarshal(raw, &page) == nil && time.Since(page.FetchedAt) >= 0 && time.Since(page.FetchedAt) < 6*time.Hour {
				if doc, err := decodePage(page); err == nil && (validate == nil || validate(doc) == nil) {
					return doc, page.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求人民银行数据", "url", address)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, address, nil, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("请求人民银行失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("人民银行返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 16<<20 {
		return nil, time.Time{}, fmt.Errorf("人民银行响应超过大小限制")
	}
	page := cachedPage{FetchedAt: time.Now().UTC(), Body: raw, ContentType: resp.Header.Get("Content-Type")}
	doc, err := decodePage(page)
	if err != nil {
		return nil, time.Time{}, err
	}
	if doc.Find("a[href],table tr").Length() == 0 {
		return nil, time.Time{}, fmt.Errorf("人民银行返回非数据页面")
	}
	if validate != nil {
		if err := validate(doc); err != nil {
			return nil, time.Time{}, err
		}
	}
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return nil, time.Time{}, err
		}
		data, err := json.Marshal(page)
		if err != nil {
			return nil, time.Time{}, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".pbc-*")
		if err != nil {
			return nil, time.Time{}, err
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return nil, time.Time{}, writeErr
		}
		if closeErr != nil {
			return nil, time.Time{}, closeErr
		}
		if err := os.Rename(f.Name(), path); err != nil {
			return nil, time.Time{}, err
		}
	}
	return doc, page.FetchedAt, nil
}

func compact(s string) string {
	var result []rune
	for _, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' && r != '\u00a0' && r != '\u3000' {
			result = append(result, r)
		}
	}
	return string(result)
}

func (c *Client) link(doc *goquery.Document, base, text string) (string, error) {
	result := ""
	var resultErr error
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if compact(a.Text()) != text {
			return
		}
		href, _ := a.Attr("href")
		address, err := c.resolve(base, href)
		if err != nil {
			resultErr = err
			return
		}
		if result != "" && result != address {
			resultErr = fmt.Errorf("人民银行目录链接重复: %s", text)
			return
		}
		result = address
	})
	if resultErr != nil {
		return "", resultErr
	}
	if result == "" {
		return "", fmt.Errorf("人民银行目录缺少 %s", text)
	}
	return result, nil
}
