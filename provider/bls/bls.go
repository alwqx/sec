// Package bls 采集美国劳工统计局（Bureau of Labor Statistics）的官方数据。
package bls

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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const apiURL = "https://api.bls.gov/publicAPI/v1/timeseries/data/"

type Client struct {
	CacheDir string
	Refresh  bool
	endpoint string
}

func NewClient(cacheDir string) *Client { return &Client{CacheDir: cacheDir, endpoint: apiURL} }

type Footnote struct {
	Code string `json:"code,omitempty"`
	Text string `json:"text,omitempty"`
}
type rawPoint struct {
	Year      string     `json:"year"`
	Period    string     `json:"period"`
	Value     string     `json:"value"`
	Latest    string     `json:"latest"`
	Footnotes []Footnote `json:"footnotes"`
}
type rawResponse struct {
	Status   string   `json:"status"`
	Messages []string `json:"message"`
	Results  struct {
		Series []struct {
			ID   string     `json:"seriesID"`
			Data []rawPoint `json:"data"`
		} `json:"series"`
	} `json:"Results"`
}

type cachedResponse struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Body      json.RawMessage `json:"body"`
}

func decodeResponse(raw []byte) (*rawResponse, error) {
	var response rawResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("BLS返回非数据JSON: %w", err)
	}
	if response.Status != "REQUEST_SUCCEEDED" {
		return nil, fmt.Errorf("BLS查询失败: %s %s", response.Status, strings.Join(response.Messages, "; "))
	}
	// 无历史数据的说明正常保留；缩短年份、配额等其他消息不能当成完整查询。
	for _, message := range response.Messages {
		if !strings.HasPrefix(message, "No Data Available for Series ") {
			return nil, fmt.Errorf("BLS未完整处理请求: %s", message)
		}
	}
	if response.Results.Series == nil {
		return nil, fmt.Errorf("BLS响应缺少系列数组")
	}
	return &response, nil
}

func decodeCPIBatch(raw []byte, ids []string, first, last int) (*rawResponse, error) {
	response, err := decodeResponse(raw)
	if err != nil {
		return nil, err
	}
	values := map[string]map[string]*CPIValue{}
	for _, id := range ids {
		values[id] = map[string]*CPIValue{}
	}
	if err := mergeCPI(response, first, last, time.Time{}, values); err != nil {
		return nil, err
	}
	return response, nil
}

// query 每批最多十个自然年，符合无需注册的v1接口限制。
func (c *Client) query(ctx context.Context, ids []string, first, last int) (*rawResponse, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	if first > last || last-first >= 10 {
		return nil, time.Time{}, fmt.Errorf("BLS单批查询最多十个自然年")
	}
	payload, err := json.Marshal(map[string]any{"seriesid": ids, "startyear": strconv.Itoa(first), "endyear": strconv.Itoa(last)})
	if err != nil {
		return nil, time.Time{}, err
	}
	key := sha256.Sum256(append([]byte(c.endpoint+"\n"), payload...))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var cache cachedResponse
			if json.Unmarshal(raw, &cache) == nil && time.Since(cache.FetchedAt) >= 0 && time.Since(cache.FetchedAt) < 6*time.Hour {
				if response, err := decodeCPIBatch(cache.Body, ids, first, last); err == nil {
					return response, cache.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求BLS数据", "start_year", first, "end_year", last, "series", ids)
	headers := http.Header{"Content-Type": {"application/json"}, "User-Agent": {utils.UserAgent()}}
	resp, err := utils.MakeRequest(ctx, http.MethodPost, c.endpoint, headers, bytes.NewReader(payload), 30*time.Second)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("请求BLS失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("BLS返回HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 16<<20 {
		return nil, time.Time{}, fmt.Errorf("BLS响应超过大小限制")
	}
	response, err := decodeCPIBatch(raw, ids, first, last)
	if err != nil {
		return nil, time.Time{}, err
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return nil, time.Time{}, err
		}
		data, err := json.Marshal(cachedResponse{fetched, raw})
		if err != nil {
			return nil, time.Time{}, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".bls-*")
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
	return response, fetched, nil
}
