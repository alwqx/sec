// Package fred 通过圣路易斯联储FRED采集美国经济统计数据。
package fred

import (
	"context"
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

type Client struct {
	CacheDir                    string
	Refresh                     bool
	endpoint                    string
	claimsEndpoint              string
	realPCEEndpoint             string
	incomeEndpoint              string
	productionEndpoint          string
	moneyEndpoint               string
	gdpEndpoint                 string
	tradeEndpoint               string
	realTradeEndpoint           string
	housingInvestmentEndpoint   string
	equipmentInvestmentEndpoint string
	housingSalesEndpoint        string
	housePriceEndpoint          string
	enterpriseEndpoints         map[string]string
	creditEndpoints             map[string]string
}

func NewClient(cacheDir string) *Client {
	return &Client{CacheDir: cacheDir, endpoint: pceCSVURL, claimsEndpoint: claimsCSVURL, realPCEEndpoint: realPCECSVURL,
		incomeEndpoint:              realIncomeCSVURL,
		productionEndpoint:          productionCSVURL,
		moneyEndpoint:               moneyCSVURL,
		gdpEndpoint:                 gdpCSVURL,
		tradeEndpoint:               tradeCSVURL,
		realTradeEndpoint:           realTradeCSVURL,
		housingInvestmentEndpoint:   housingInvestmentCSVURL,
		equipmentInvestmentEndpoint: equipmentInvestmentCSVURL,
		housingSalesEndpoint:        housingSalesCSVURL,
		housePriceEndpoint:          housePriceCSVURL,
		enterpriseEndpoints:         map[string]string{"profit": enterpriseCSVURL("CPROFIT"), "inventory": enterpriseCSVURL("BUSINV"), "new-orders": enterpriseCSVURL("AMTMNO")},
		creditEndpoints:             map[string]string{"bank": creditCSVURL("LOANINV"), "standards": creditCSVURL("DRTSCILM"), "debt-service": creditCSVURL("TDSP")}}
}

type csvCache struct {
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
	CSV       string    `json:"csv"`
}

// csvSnapshot 只缓存经过指标解析校验的完整快照，不回退过期数据。
func (c *Client) csvSnapshot(ctx context.Context, endpoint, filename string, validate func(string) error) (string, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return "", time.Time{}, err
	}
	path := filepath.Join(c.CacheDir, filename)
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var cache csvCache
			if json.Unmarshal(raw, &cache) == nil && cache.Source == endpoint && time.Since(cache.FetchedAt) >= 0 && time.Since(cache.FetchedAt) < 6*time.Hour {
				if err := validate(cache.CSV); err == nil {
					return cache.CSV, cache.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求FRED CSV数据", "url", endpoint)
	// FRED公开下载对默认sec/Go标识会重置连接，使用其接受的通用下载客户端标识。
	resp, err := utils.MakeRequest(ctx, http.MethodGet, endpoint, http.Header{"User-Agent": {"curl"}}, nil, 30*time.Second)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("FRED返回HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return "", time.Time{}, err
	}
	if len(raw) > 8<<20 {
		return "", time.Time{}, fmt.Errorf("FRED响应超过大小限制")
	}
	if err := validate(string(raw)); err != nil {
		return "", time.Time{}, err
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return "", time.Time{}, err
		}
		data, err := json.Marshal(csvCache{endpoint, fetched, string(raw)})
		if err != nil {
			return "", time.Time{}, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".fred-csv-*")
		if err != nil {
			return "", time.Time{}, err
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return "", time.Time{}, writeErr
		}
		if closeErr != nil {
			return "", time.Time{}, closeErr
		}
		if err := os.Rename(f.Name(), path); err != nil {
			return "", time.Time{}, err
		}
	}
	return string(raw), fetched, nil
}
