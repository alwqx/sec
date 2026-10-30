// Package mofcom 采集商务部数据中心转发的海关货物贸易数据。
package mofcom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
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

	"github.com/alwqx/sec/utils"
)

const (
	monthlyURL = "https://data.mofcom.gov.cn/datamofcom/front/totalmonth/query"
	partnerURL = "https://data.mofcom.gov.cn/datamofcom/front/totalbycountry/query"
	listURL    = "https://fdi.mofcom.gov.cn/cipa-api/statistics/web/list"
	articleURL = "https://fdi.mofcom.gov.cn/cipa-api/statistics/web/get"
)

// Client 缓存已通过语义校验的公开响应；CacheDir 为空时禁用磁盘缓存。
type Client struct {
	CacheDir string
	Refresh  bool
}

// fdiHTTPClient 兼容商务部 fdi 站点仅接受的 TLS 1.2 RSA-GCM 密码套件。
// 该兼容设置只用于 fdi.mofcom.gov.cn；数据中心接口仍使用系统默认 TLS 配置。
func newFDIHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

var fdiHTTPClient = newFDIHTTPClient()

func NewClient(cacheDir string) *Client { return &Client{CacheDir: cacheDir} }

type cacheEntry struct {
	URL       string    `json:"url"`
	Body      string    `json:"body"`
	FetchedAt time.Time `json:"fetched_at"`
}

func (c *Client) request(ctx context.Context, method, address, referer string, form url.Values, body any, validate func([]byte) error) ([]byte, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, time.Time{}, err
		}
	} else if len(form) > 0 {
		payload = []byte(form.Encode())
	}
	key := sha256.Sum256(append(append([]byte(address+"\n"+method+"\n"), payload...), []byte(referer)...))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, readErr := os.ReadFile(path); readErr == nil {
			var cached cacheEntry
			if json.Unmarshal(raw, &cached) == nil && cached.URL == address && time.Since(cached.FetchedAt) >= 0 && time.Since(cached.FetchedAt) < 6*time.Hour {
				if validate == nil || validate([]byte(cached.Body)) == nil {
					return []byte(cached.Body), cached.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求商务部海关统计数据", "url", address)
	headers := http.Header{"Referer": {referer}}
	if body != nil {
		headers.Set("Content-Type", "application/json;charset=UTF-8")
		headers.Set("Origin", "https://fdi.mofcom.gov.cn")
	} else if len(form) > 0 {
		headers.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	}
	var resp *http.Response
	if parsed, parseErr := url.Parse(address); parseErr == nil && parsed.Hostname() == "fdi.mofcom.gov.cn" {
		req, requestErr := http.NewRequestWithContext(ctx, method, address, bytesReader(payload))
		if requestErr != nil {
			return nil, time.Time{}, requestErr
		}
		req.Header.Set("User-Agent", utils.UserAgent())
		for key, values := range headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		resp, err = fdiHTTPClient.Do(req)
	} else {
		resp, err = utils.MakeRequest(ctx, method, address, headers, bytesReader(payload), 30*time.Second)
	}
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("请求商务部海关统计数据失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("商务部返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 16<<20 {
		return nil, time.Time{}, fmt.Errorf("商务部海关统计响应超过16MiB")
	}
	if validate != nil {
		if err := validate(raw); err != nil {
			return nil, time.Time{}, err
		}
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := c.save(path, cacheEntry{URL: address, Body: string(raw), FetchedAt: fetched}); err != nil {
			return nil, time.Time{}, err
		}
	}
	return raw, fetched, nil
}

func bytesReader(raw []byte) io.Reader {
	if len(raw) == 0 {
		return nil
	}
	return bytes.NewReader(raw)
}

func (c *Client) save(path string, entry cacheEntry) error {
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(c.CacheDir, ".mofcom-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
