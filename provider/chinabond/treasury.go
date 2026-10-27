// Package chinabond 采集中央国债登记结算有限责任公司（中债）的官方收益率曲线。
package chinabond

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	CoverageStart = "20060301"
	curveID       = "1"
	curveName     = "中债国债收益率曲线"
)

var beijing = time.FixedZone("CST", 8*60*60)
var tenors = []string{"1y", "2y", "5y", "10y"}

// Client 按年批量查询四个期限；CacheDir 为空时不读写磁盘缓存。
type Client struct {
	CacheDir string
	Refresh  bool
	origin   string
}

func NewClient(cacheDir string) *Client {
	return &Client{CacheDir: cacheDir, origin: "https://yield.chinabond.com.cn"}
}

// TreasuryRequest 使用北京时间的收益率所属日期，包含起止日。
type TreasuryRequest struct{ Begin, End string }

func (r TreasuryRequest) Validate() error {
	for _, date := range []string{r.Begin, r.End} {
		if _, err := time.Parse("20060102", date); err != nil || len(date) != 8 {
			return fmt.Errorf("无效日期 %q，请使用 YYYYMMDD", date)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始日期不能晚于截至日期")
	}
	if r.Begin < CoverageStart {
		return fmt.Errorf("国债收益率曲线查询支持自20060301起")
	}
	if r.End > time.Now().In(beijing).Format("20060102") {
		return fmt.Errorf("截至日期不能晚于北京时间今天")
	}
	return nil
}

// TreasuryObservation 仅含上游实际返回的日期；缺失收益率为 nil，真实零值保留。
type TreasuryObservation struct {
	Original      *ChinaBondItem      `json:"original"`
	Date          string              `json:"date"`
	Yields        map[string]*float64 `json:"yields"`
	MissingTenors []string            `json:"missing_tenors"`
	Status        string              `json:"status"`
	Source        string              `json:"source"`
	FetchedAt     time.Time           `json:"fetched_at"`
}

type TreasuryResult struct {
	Region        string                `json:"region"`
	Currency      string                `json:"currency"`
	Indicator     string                `json:"indicator"`
	CurveID       string                `json:"curve_id"`
	CurveName     string                `json:"curve_name"`
	CurveType     string                `json:"curve_type"`
	Unit          string                `json:"unit"`
	Frequency     string                `json:"frequency"`
	Begin         string                `json:"begin"`
	End           string                `json:"end"`
	CoverageStart string                `json:"coverage_start"`
	PublishedAt   *time.Time            `json:"published_at"`
	Observations  []TreasuryObservation `json:"observations"`
	Note          string                `json:"note"`
}

// Treasury 采集到期收益率曲线，不混入即期、远期或其他信用类别曲线。
func (c *Client) Treasury(ctx context.Context, req *TreasuryRequest) (*TreasuryResult, error) {
	if req == nil {
		return nil, fmt.Errorf("国债收益率查询请求不能为空")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	result := &TreasuryResult{
		Region: "CN", Currency: "CNY", Indicator: "treasury-yield-curve",
		CurveID: curveID, CurveName: curveName, CurveType: "yield-to-maturity",
		Unit: "%", Frequency: "daily", Begin: req.Begin, End: req.End, CoverageStart: CoverageStart,
		Observations: []TreasuryObservation{},
		Note:         "中债国债到期收益率曲线，单位为百分数（1.5表示1.5%）；日期为北京时间所属日。缺失期限为null，不补值；整天未返回不区分休市、尚未发布或上游缺口。historyQuery源值保留两位小数；original保留其他期限源字段（源null统一为空字符串）。接口未提供逐日公布时间和历史修订版本，published_at为null；数据为抓取时可见版本。",
	}
	begin, _ := time.Parse("20060102", req.Begin)
	end, _ := time.Parse("20060102", req.End)
	for year := begin.Year(); year <= end.Year(); year++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		from, to := fmt.Sprintf("%d0101", year), fmt.Sprintf("%d1231", year)
		if from < CoverageStart {
			from = CoverageStart
		}
		if today := time.Now().In(beijing).Format("20060102"); to > today {
			to = today
		}
		rows, err := c.fetchYear(ctx, year, from, to)
		if err != nil {
			return nil, fmt.Errorf("采集%d年中债国债收益率失败: %w", year, err)
		}
		for _, row := range rows {
			if row.Date >= req.Begin && row.Date <= req.End {
				result.Observations = append(result.Observations, row)
			}
		}
	}
	return result, nil
}

// parseTreasury 按PR #66的日期行结构解析，不再拼接期限序列或转换时间戳。
func parseTreasury(raw []byte, from, to, source string, fetchedAt time.Time) ([]TreasuryObservation, error) {
	response, err := decodeChinaBond(raw)
	if err != nil {
		return nil, err
	}
	result := make([]TreasuryObservation, 0, len(response.HeList))
	seen := map[string]bool{}
	for _, item := range response.HeList {
		if item == nil {
			return nil, fmt.Errorf("中债返回空数据行")
		}
		date, err := time.Parse("2006-01-02", item.Date)
		if err != nil {
			return nil, fmt.Errorf("中债所属日期无效: %s", item.Date)
		}
		day := date.Format("20060102")
		if day < from || day > to || seen[day] {
			return nil, fmt.Errorf("中债所属日期越界或重复: %s", item.Date)
		}
		seen[day] = true
		row := TreasuryObservation{Original: item, Date: day, Yields: map[string]*float64{}, MissingTenors: []string{}, Status: "complete", Source: source, FetchedAt: fetchedAt}
		values := map[string]string{"1y": item.OneYear, "2y": item.TwoYear, "5y": item.FiveYear, "10y": item.TenYear}
		for _, tenor := range tenors {
			text := strings.TrimSpace(values[tenor])
			if text == "" {
				row.Yields[tenor] = nil
				row.MissingTenors = append(row.MissingTenors, tenor)
				row.Status = "missing-tenors"
				continue
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("中债%s期限收益率无效: %s", tenor, text)
			}
			row.Yields[tenor] = &value
		}
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result, nil
}

type cachedResponse struct {
	Source    string          `json:"source"`
	FetchedAt time.Time       `json:"fetched_at"`
	Body      json.RawMessage `json:"body"`
}

func (c *Client) fetchYear(ctx context.Context, year int, from, to string) ([]TreasuryObservation, error) {
	formatDate := func(date string) string { d, _ := time.Parse("20060102", date); return d.Format("2006-01-02") }
	source := chinaBondURL(c.origin, &GetChinaBondReq{Start: formatDate(from), End: formatDate(to)})
	path := filepath.Join(c.CacheDir, "treasury-"+strconv.Itoa(year)+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var cache cachedResponse
			if json.Unmarshal(raw, &cache) == nil && cache.Source == source && time.Since(cache.FetchedAt) >= 0 && time.Since(cache.FetchedAt) < 6*time.Hour {
				if rows, err := parseTreasury(cache.Body, from, to, source, cache.FetchedAt); err == nil {
					return rows, nil
				}
			}
		}
	}
	raw, err := requestChinaBond(ctx, source)
	if err != nil {
		return nil, err
	}
	fetchedAt := time.Now().UTC()
	rows, err := parseTreasury(raw, from, to, source, fetchedAt)
	if err != nil {
		return nil, err
	}
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return nil, err
		}
		data, err := json.Marshal(cachedResponse{source, fetchedAt, raw})
		if err != nil {
			return nil, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".treasury-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if err := os.Rename(f.Name(), path); err != nil {
			return nil, err
		}
	}
	return rows, nil
}
