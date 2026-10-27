// Package chinabond 采集中央国债登记结算有限责任公司（中债）的官方收益率曲线。
package chinabond

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/alwqx/sec/utils"
)

const (
	CoverageStart = "20060301"
	curveID       = "2c9081e50a2f9606010a3068cae70001"
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
		Note:         "中债国债到期收益率曲线，单位为百分数（1.5表示1.5%）；日期为北京时间所属日。缺失期限为null，不补值；整天未返回不区分休市、尚未发布或上游缺口。接口未提供逐日公布时间和历史修订版本，published_at为null；数据为抓取时可见版本。",
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

type rawSeries struct {
	ID   string              `json:"ycDefId"`
	Name string              `json:"ycDefName"`
	Data [][]json.RawMessage `json:"seriesData"`
}

// parseTreasury 按曲线身份和期限匹配，不依赖上游数组顺序。
func parseTreasury(raw []byte, from, to, source string, fetchedAt time.Time) ([]TreasuryObservation, error) {
	var series []rawSeries
	if err := json.Unmarshal(raw, &series); err != nil {
		return nil, fmt.Errorf("中债响应不是有效曲线数组: %w", err)
	}
	if series == nil {
		return nil, fmt.Errorf("中债响应为空或结构异常")
	}
	rows := map[string]*TreasuryObservation{}
	seen := map[string]bool{}
	for _, s := range series {
		tenor := ""
		for _, key := range tenors {
			years := key[:len(key)-1]
			if s.ID == curveID+years+".0" && s.Name == curveName+"(到期)("+key+")" {
				tenor = key
				break
			}
		}
		if tenor == "" || seen[tenor] {
			return nil, fmt.Errorf("中债返回未知或重复曲线: %s (%s)", s.Name, s.ID)
		}
		seen[tenor] = true
		if s.Data == nil {
			return nil, fmt.Errorf("中债%s期限缺少数据数组", tenor)
		}
		dates := map[string]bool{}
		for _, point := range s.Data {
			if len(point) != 2 {
				return nil, fmt.Errorf("中债%s期限数据点格式异常", tenor)
			}
			var millis int64
			if err := json.Unmarshal(point[0], &millis); err != nil || string(point[0]) == "null" {
				return nil, fmt.Errorf("中债时间戳无效")
			}
			timestamp := time.UnixMilli(millis).In(beijing)
			date := timestamp.Format("20060102")
			if date < from || date > to || timestamp.Format("15:04:05.000") != "00:00:00.000" {
				return nil, fmt.Errorf("中债所属日期超出查询范围或时间戳异常: %s", timestamp)
			}
			if dates[date] {
				return nil, fmt.Errorf("中债%s期限日期重复: %s", tenor, date)
			}
			dates[date] = true
			var value *float64
			if err := json.Unmarshal(point[1], &value); err != nil {
				return nil, fmt.Errorf("中债%s期限收益率无效: %w", tenor, err)
			}
			row := rows[date]
			if row == nil {
				row = &TreasuryObservation{Date: date, Yields: map[string]*float64{}, MissingTenors: []string{}, Source: source, FetchedAt: fetchedAt}
				rows[date] = row
			}
			row.Yields[tenor] = value
		}
	}
	result := make([]TreasuryObservation, 0, len(rows))
	for _, row := range rows {
		row.Status = "complete"
		for _, key := range tenors {
			if row.Yields[key] == nil {
				row.Yields[key] = nil
				row.MissingTenors = append(row.MissingTenors, key)
				row.Status = "missing-tenors"
			}
		}
		result = append(result, *row)
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
	query := url.Values{
		"bjlx": {"no"}, "dcq": {"1,1y;2,2y;5,5y;10,10y;"}, "startTime": {formatDate(from)}, "endTime": {formatDate(to)},
		"qxlx": {"0,"}, "yqqxN": {"N"}, "yqqxK": {"K"}, "par": {"day"}, "ycDefIds": {curveID}, "locale": {"CN"},
	}
	source := c.origin + "/cbweb-mn/yc/queryYz?" + query.Encode()
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
	slog.DebugContext(ctx, "请求中债国债收益率曲线", "url", source)
	resp, err := utils.MakeRequest(ctx, http.MethodPost, source, nil, nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("中债返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 16<<20 {
		return nil, fmt.Errorf("中债响应超过大小限制")
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
