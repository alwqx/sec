package federalreserve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/alwqx/sec/utils"
)

// FetchError 表示页面暂时无法下载，区别于已下载但内容无法匹配或解析；
// 页面不存在（404、410）属于后者。
type FetchError struct{ Err error }

func (e *FetchError) Error() string { return e.Err.Error() }
func (e *FetchError) Unwrap() error { return e.Err }

type cachedPage struct {
	SourceURL string    `json:"source_url"`
	FetchedAt time.Time `json:"fetched_at"`
	Body      string    `json:"body"`
}

func cachePath(dir, address string) string {
	key := sha256.Sum256([]byte(address))
	return filepath.Join(dir, hex.EncodeToString(key[:])+".json")
}

// fetch 仅缓存通过内容校验的官方原文；失败不会覆盖旧缓存，也不回退为陈旧结果。
func (c *Client) fetch(ctx context.Context, address string, validate func([]byte) error) ([]byte, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	path := cachePath(c.CacheDir, address)
	if c.CacheDir != "" && !c.Refresh {
		if data, err := os.ReadFile(path); err == nil {
			var cached cachedPage
			if json.Unmarshal(data, &cached) == nil && cached.SourceURL == address && time.Since(cached.FetchedAt) >= 0 && time.Since(cached.FetchedAt) < 6*time.Hour {
				body := []byte(cached.Body)
				if validate(body) == nil {
					return body, cached.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求美联储官方页面", "url", address)
	response, err := utils.MakeRequest(ctx, http.MethodGet, address, http.Header{"User-Agent": {utils.UserAgent()}}, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, &FetchError{fmt.Errorf("请求美联储失败: %w", err)}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("美联储返回HTTP %d: %s", response.StatusCode, address)
		if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
			return nil, time.Time{}, err
		}
		return nil, time.Time{}, &FetchError{err}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return nil, time.Time{}, &FetchError{err}
	}
	if len(body) > 8<<20 {
		return nil, time.Time{}, fmt.Errorf("美联储页面超过大小限制")
	}
	if err := validate(body); err != nil {
		return nil, time.Time{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return nil, time.Time{}, err
		}
		data, err := json.Marshal(cachedPage{address, fetched, string(body)})
		if err != nil {
			return nil, time.Time{}, err
		}
		file, err := os.CreateTemp(c.CacheDir, ".federalreserve-*")
		if err != nil {
			return nil, time.Time{}, err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return nil, time.Time{}, writeErr
		}
		if closeErr != nil {
			return nil, time.Time{}, closeErr
		}
		if err := os.Rename(file.Name(), path); err != nil {
			return nil, time.Time{}, err
		}
	}
	return body, fetched, nil
}
