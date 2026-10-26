// Package nbs 采集中国国家统计局（National Bureau of Statistics）发布库数据。
package nbs

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

	"github.com/alwqx/sec/utils"
)

const baseURL = "https://data.stats.gov.cn/dg/website/publicrelease/web/external"

// Client 使用新版官方发布库。CacheDir 为空时不写磁盘；Refresh 跳过已有缓存。
type Client struct {
	CacheDir string
	Refresh  bool
	baseURL  string
}

type cacheEntry struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Body      json.RawMessage `json:"body"`
}

// NewClient 创建采集客户端，缓存有效期为六小时。
func NewClient(cacheDir string) *Client { return &Client{CacheDir: cacheDir, baseURL: baseURL} }

func (c *Client) request(ctx context.Context, path string, params url.Values, body any, dst any) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	method := http.MethodGet
	endpoint := c.baseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	var payload []byte
	var err error
	if body != nil {
		method = http.MethodPost
		payload, err = json.Marshal(body)
		if err != nil {
			return time.Time{}, err
		}
	}
	key := sha256.Sum256(append([]byte(endpoint+"\n"), payload...))
	cachePath := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, readErr := os.ReadFile(cachePath); readErr == nil {
			var entry cacheEntry
			if json.Unmarshal(raw, &entry) == nil && time.Since(entry.FetchedAt) >= 0 && time.Since(entry.FetchedAt) < 6*time.Hour {
				if json.Unmarshal(entry.Body, dst) == nil {
					return entry.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求国家统计局数据", "path", path)
	headers := http.Header{}
	headers.Set("User-Agent", utils.UserAgent())
	headers.Set("Referer", "https://data.stats.gov.cn/")
	headers.Set("Content-Type", "application/json")
	resp, err := utils.MakeRequest(ctx, method, endpoint, headers, bytes.NewReader(payload), 30*time.Second)
	if err != nil {
		return time.Time{}, fmt.Errorf("请求国家统计局失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("国家统计局返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return time.Time{}, fmt.Errorf("读取国家统计局响应失败: %w", err)
	}
	var envelope struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return time.Time{}, fmt.Errorf("国家统计局响应不是有效 JSON: %w", err)
	}
	if !envelope.Success {
		return time.Time{}, fmt.Errorf("国家统计局查询失败: %s", envelope.Message)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return time.Time{}, fmt.Errorf("解析国家统计局响应失败: %w", err)
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := c.saveCache(cachePath, cacheEntry{fetched, raw}); err != nil {
			return time.Time{}, err
		}
	}
	return fetched, nil
}

func (c *Client) saveCache(path string, entry cacheEntry) error {
	if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
		return fmt.Errorf("创建宏观数据缓存目录失败: %w", err)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(c.CacheDir, ".nbs-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
