// Package nyfed 采集纽约联邦储备银行（Federal Reserve Bank of New York）的官方数据。
package nyfed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/alwqx/sec/utils"
)

const apiURL = "https://markets.newyorkfed.org/api/rates/unsecured/effr/search.json"
const earliest = "20000703"

type Client struct {
	CacheDir string
	Refresh  bool
	endpoint string
}

func NewClient(cacheDir string) *Client { return &Client{CacheDir: cacheDir, endpoint: apiURL} }

type EFFRRequest struct {
	Begin string `json:"begin"`
	End   string `json:"end"`
}

func (r *EFFRRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("EFFR查询参数不能为空")
	}
	for _, value := range []string{r.Begin, r.End} {
		parsed, err := time.Parse("20060102", value)
		if err != nil || parsed.Format("20060102") != value {
			return fmt.Errorf("无效日期 %q，请使用YYYYMMDD", value)
		}
	}
	if r.Begin < earliest {
		return fmt.Errorf("EFFR接口最早支持20000703")
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始日期不能晚于截至日期")
	}
	if r.End > time.Now().UTC().Format("20060102") {
		return fmt.Errorf("截至日期不能晚于UTC今天")
	}
	return nil
}

// EFFRObservation 的所属日期对应隔夜交易日；API未提供逐条实际公布时间。
type EFFRObservation struct {
	EffectiveDate     string     `json:"effective_date"`
	PublishedAt       *time.Time `json:"published_at"`
	RatePercent       float64    `json:"rate_percent"`
	PreviousDate      string     `json:"previous_date,omitempty"`
	ChangeBasisPoints *float64   `json:"change_basis_points"`
	RevisionIndicator string     `json:"revision_indicator"`
	Methodology       string     `json:"methodology"`
	FetchedAt         time.Time  `json:"fetched_at"`
	// Raw 保留分位数、成交量、目标区间、脚注及历史日内区间等所有官方字段。
	Raw json.RawMessage `json:"raw"`
}

type EFFRResult struct {
	Region       string            `json:"region"`
	Frequency    string            `json:"frequency"`
	DataVersion  string            `json:"data_version"`
	Begin        string            `json:"begin"`
	End          string            `json:"end"`
	SeriesID     string            `json:"series_id"`
	Unit         string            `json:"unit"`
	Source       string            `json:"source"`
	SourceURL    string            `json:"source_url"`
	Observations []EFFRObservation `json:"observations"`
}

type rawRate struct {
	EffectiveDate     string   `json:"effectiveDate"`
	Type              string   `json:"type"`
	PercentRate       *float64 `json:"percentRate"`
	RevisionIndicator string   `json:"revisionIndicator"`
}

type point struct {
	rate    rawRate
	raw     json.RawMessage
	fetched time.Time
}
type cachedResponse struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Body      json.RawMessage `json:"body"`
}

func parseResponse(body []byte, begin, end time.Time) ([]point, error) {
	var response struct {
		Rates []json.RawMessage `json:"refRates"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("纽约联储返回无效JSON: %w", err)
	}
	if response.Rates == nil {
		return nil, fmt.Errorf("纽约联储响应缺少refRates数组")
	}
	points := make([]point, 0, len(response.Rates))
	seen := map[string]bool{}
	for _, raw := range response.Rates {
		var rate rawRate
		if err := json.Unmarshal(raw, &rate); err != nil {
			return nil, fmt.Errorf("纽约联储返回无效EFFR记录: %w", err)
		}
		date, err := time.Parse("2006-01-02", rate.EffectiveDate)
		if err != nil || date.Format("2006-01-02") != rate.EffectiveDate || date.Before(begin) || date.After(end) || date.Format("20060102") < earliest {
			return nil, fmt.Errorf("纽约联储返回无效或越界所属日期: %s", rate.EffectiveDate)
		}
		if rate.Type != "EFFR" || rate.PercentRate == nil || math.IsNaN(*rate.PercentRate) || math.IsInf(*rate.PercentRate, 0) {
			return nil, fmt.Errorf("纽约联储返回无效EFFR利率: %s", rate.EffectiveDate)
		}
		if seen[rate.EffectiveDate] {
			return nil, fmt.Errorf("纽约联储返回重复所属日期: %s", rate.EffectiveDate)
		}
		seen[rate.EffectiveDate] = true
		points = append(points, point{rate: rate, raw: raw})
	}
	sort.Slice(points, func(i, j int) bool { return points[i].rate.EffectiveDate < points[j].rate.EffectiveDate })
	return points, nil
}

// EFFR 按所属日期筛选，仅返回官方实际观测，不补周末、节假日或未发布日期。
func (c *Client) EFFR(ctx context.Context, req *EFFRRequest) (*EFFRResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	begin, _ := time.Parse("20060102", req.Begin)
	end, _ := time.Parse("20060102", req.End)
	first, _ := time.Parse("20060102", earliest)
	contextBegin := begin.AddDate(0, 0, -31)
	if contextBegin.Before(first) {
		contextBegin = first
	}
	points, err := c.query(ctx, contextBegin, end)
	if err != nil {
		return nil, err
	}
	// 如31天上下文仍没有前值，向前继续查询，避免把缺口后的首值误称为日变化。
	for contextBegin.After(first) && (len(points) == 0 || points[0].rate.EffectiveDate >= begin.Format("2006-01-02")) {
		contextEnd := contextBegin.AddDate(0, 0, -1)
		previousBegin := contextBegin.AddDate(-1, 0, 0)
		if previousBegin.Before(first) {
			previousBegin = first
		}
		previous, err := c.query(ctx, previousBegin, contextEnd)
		if err != nil {
			return nil, err
		}
		points = append(previous, points...)
		contextBegin = previousBegin
	}
	result := &EFFRResult{Region: "US", Frequency: "daily", DataVersion: "latest-available", Begin: req.Begin, End: req.End, SeriesID: "EFFR", Unit: "%", Source: "纽约联邦储备银行", SourceURL: apiURL, Observations: []EFFRObservation{}}
	for i, p := range points {
		if p.rate.EffectiveDate < begin.Format("2006-01-02") {
			continue
		}
		method := "FR2420交易量加权中位数"
		if p.rate.EffectiveDate < "2016-03-01" {
			method = "经纪商数据交易量加权均值（旧口径）"
		}
		item := EFFRObservation{EffectiveDate: p.rate.EffectiveDate, RatePercent: *p.rate.PercentRate, RevisionIndicator: p.rate.RevisionIndicator, Methodology: method, FetchedAt: p.fetched, Raw: p.raw}
		if i > 0 {
			item.PreviousDate = points[i-1].rate.EffectiveDate
			// 2016-03-01切换来源及统计方法，跨界值不作为可比变化。
			if !(item.PreviousDate < "2016-03-01" && item.EffectiveDate >= "2016-03-01") {
				change := math.Round((*p.rate.PercentRate-*points[i-1].rate.PercentRate)*100*1e8) / 1e8
				item.ChangeBasisPoints = &change
			}
		}
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

func (c *Client) query(ctx context.Context, begin, end time.Time) ([]point, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, err
	}
	values := endpoint.Query()
	values.Set("startDate", begin.Format("2006-01-02"))
	values.Set("endDate", end.Format("2006-01-02"))
	endpoint.RawQuery = values.Encode()
	address := endpoint.String()
	key := sha256.Sum256([]byte(address))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	stamp := func(points []point, fetched time.Time) []point {
		for i := range points {
			points[i].fetched = fetched
		}
		return points
	}
	if c.CacheDir != "" && !c.Refresh {
		if body, err := os.ReadFile(path); err == nil {
			var cache cachedResponse
			if json.Unmarshal(body, &cache) == nil && time.Since(cache.FetchedAt) >= 0 && time.Since(cache.FetchedAt) < 6*time.Hour {
				if points, err := parseResponse(cache.Body, begin, end); err == nil {
					return stamp(points, cache.FetchedAt), nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求纽约联储EFFR", "begin", begin.Format("2006-01-02"), "end", end.Format("2006-01-02"))
	resp, err := utils.MakeRequest(ctx, http.MethodGet, address, http.Header{"User-Agent": {utils.UserAgent()}}, nil, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("请求纽约联储失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("纽约联储返回HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 16<<20 {
		return nil, fmt.Errorf("纽约联储响应超过大小限制")
	}
	points, err := parseResponse(body, begin, end)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return nil, err
		}
		data, err := json.Marshal(cachedResponse{fetched, body})
		if err != nil {
			return nil, err
		}
		file, err := os.CreateTemp(c.CacheDir, ".nyfed-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.Rename(file.Name(), path); err != nil {
			return nil, err
		}
	}
	return stamp(points, fetched), nil
}
