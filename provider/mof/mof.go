// Package mof 采集中国财政部（Ministry of Finance）全国财政收支发布稿。
package mof

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

const topicURL = "https://www.mof.gov.cn/zhengwuxinxi/redianzhuanti/quanguocaizhengshouzhiqingkuang/"

// Client 缓存经语义校验的原始页面；空目录禁用缓存。
type Client struct {
	CacheDir string
	Refresh  bool
	topicURL string
}

func NewClient(cacheDir string) *Client { return &Client{CacheDir: cacheDir, topicURL: topicURL} }

type cachedPage struct {
	Source      string    `json:"source"`
	FetchedAt   time.Time `json:"fetched_at"`
	ContentType string    `json:"content_type"`
	Body        []byte    `json:"body"`
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
	local, _ := url.Parse(c.topicURL)
	if u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || (u.Host != "www.mof.gov.cn" && u.Host != "gks.mof.gov.cn" && u.Host != local.Host) {
		return "", fmt.Errorf("非财政部官方链接: %s", href)
	}
	if u.Host == "www.mof.gov.cn" || u.Host == "gks.mof.gov.cn" {
		u.Scheme = "https"
	}
	u.Fragment = ""
	return u.String(), nil
}

func decode(page cachedPage) (*goquery.Document, error) {
	r, err := charset.NewReader(bytes.NewReader(page.Body), page.ContentType)
	if err != nil {
		return nil, err
	}
	return goquery.NewDocumentFromReader(r)
}

func (c *Client) get(ctx context.Context, address string, validate func(*goquery.Document) error) (*goquery.Document, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	address, err := c.resolve(c.topicURL, address)
	if err != nil {
		return nil, time.Time{}, err
	}
	hash := sha256.Sum256([]byte(address))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(hash[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var page cachedPage
			if json.Unmarshal(raw, &page) == nil && page.Source == address && time.Since(page.FetchedAt) >= 0 && time.Since(page.FetchedAt) < 6*time.Hour {
				if doc, err := decode(page); err == nil && (validate == nil || validate(doc) == nil) {
					return doc, page.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求财政部数据", "url", address)
	headers := http.Header{}
	headers.Set("User-Agent", utils.UserAgent())
	resp, err := utils.MakeRequest(ctx, http.MethodGet, address, headers, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("请求财政部失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("财政部返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 8<<20 {
		return nil, time.Time{}, fmt.Errorf("财政部页面超过8MiB")
	}
	page := cachedPage{Source: address, FetchedAt: time.Now().UTC(), ContentType: resp.Header.Get("Content-Type"), Body: raw}
	doc, err := decode(page)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("解码财政部页面失败: %w", err)
	}
	if validate != nil {
		if err := validate(doc); err != nil {
			return nil, time.Time{}, err
		}
	}
	if c.CacheDir != "" {
		if err := c.save(path, page); err != nil {
			return nil, time.Time{}, err
		}
	}
	return doc, page.FetchedAt, nil
}
func (c *Client) save(path string, page cachedPage) error {
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(page)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(c.CacheDir, ".mof-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
