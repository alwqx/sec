// Package fred 通过圣路易斯联储FRED采集官方经济统计数据。
package fred

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const pceCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=PCEPI,PCEPILFE"

type Client struct {
	CacheDir string
	Refresh  bool
	endpoint string
}

func NewClient(cacheDir string) *Client { return &Client{CacheDir: cacheDir, endpoint: pceCSVURL} }

type PCERequest struct{ Indicator, Begin, End string }

func (r *PCERequest) Validate() error {
	if r == nil {
		return fmt.Errorf("PCE查询请求不能为空")
	}
	if r.Indicator != "pce" && r.Indicator != "core-pce" {
		return fmt.Errorf("不支持的PCE指标: %s", r.Indicator)
	}
	for _, period := range []string{r.Begin, r.End} {
		if _, err := time.Parse("200601", period); err != nil || len(period) != 6 {
			return fmt.Errorf("无效月份 %q，请使用YYYYMM", period)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "195901" {
		return fmt.Errorf("PCE价格指数查询支持自195901起")
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

// PCEValue 保留季调价格指数原值；不包含消费支出金额或实际消费量。
type PCEValue struct {
	Period   string   `json:"period"`
	Index    *float64 `json:"index"`
	RawIndex string   `json:"raw_index"`
}

type PCEObservation struct {
	PCEValue
	YoY     *float64 `json:"yoy_percent"`
	MoM     *float64 `json:"mom_percent"`
	YoYBase PCEValue `json:"yoy_base"`
	MoMBase PCEValue `json:"mom_base"`
	Note    string   `json:"note,omitempty"`
}

type PCEResult struct {
	Region             string           `json:"region"`
	Indicator          string           `json:"indicator"`
	SeriesID           string           `json:"series_id"`
	BEAAccountCode     string           `json:"bea_account_code"`
	Frequency          string           `json:"frequency"`
	Measure            string           `json:"measure"`
	IndexBase          string           `json:"index_base"`
	SeasonallyAdjusted bool             `json:"seasonally_adjusted"`
	Begin              string           `json:"begin"`
	End                string           `json:"end"`
	Producer           string           `json:"producer"`
	Distributor        string           `json:"distributor"`
	Source             string           `json:"source"`
	SeriesSource       string           `json:"series_source"`
	OriginalSource     string           `json:"original_source"`
	FetchedAt          time.Time        `json:"fetched_at"`
	DataVersion        string           `json:"data_version"`
	PublishedAt        *time.Time       `json:"published_at"`
	RevisionID         *string          `json:"revision_id"`
	Observations       []PCEObservation `json:"observations"`
	Note               string           `json:"note"`
}

// PCE 从同一份两序列CSV快照计算季调价格指数同比和月环比，严格按日历基期取值。
func (c *Client) PCE(ctx context.Context, req *PCERequest) (*PCEResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	values, fetched, err := c.pceValues(ctx)
	if err != nil {
		return nil, err
	}
	id, code := "PCEPI", "DPCERG"
	if req.Indicator == "core-pce" {
		id, code = "PCEPILFE", "DPCCRG"
	}
	result := &PCEResult{Region: "US", Indicator: req.Indicator, SeriesID: id, BEAAccountCode: code, Frequency: "monthly", Measure: "chain-type-price-index", IndexBase: "2017=100", SeasonallyAdjusted: true, Begin: req.Begin, End: req.End, Producer: "BEA", Distributor: "FRED", Source: pceCSVURL, SeriesSource: "https://fred.stlouisfed.org/series/" + id, OriginalSource: "https://www.bea.gov/data/personal-consumption-expenditures-price-index", FetchedAt: fetched, DataVersion: "latest-available", Observations: []PCEObservation{}, Note: "PCE链式价格指数，2017=100，月度季调；不是消费金额或实际消费量。同比和月环比均由同一季调指数序列计算，未年化，不跨缺口。历史值可能修订或调整参考年；此为抓取时可见版本，CSV不提供实际公布时间、修订编号或历史版本档案，相关字段为null。"}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	value := func(period string) PCEValue {
		if point, ok := values[id][period]; ok {
			return point
		}
		return PCEValue{Period: period}
	}
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		point := value(month.Format("200601"))
		item := PCEObservation{PCEValue: point, YoYBase: value(month.AddDate(-1, 0, 0).Format("200601")), MoMBase: value(month.AddDate(0, -1, 0).Format("200601"))}
		item.YoY = pceChange(point.Index, item.YoYBase.Index)
		item.MoM = pceChange(point.Index, item.MoMBase.Index)
		notes := []string{}
		if point.Index == nil {
			notes = append(notes, "当月指数缺失")
		} else {
			if item.YoY == nil {
				notes = append(notes, "同比基期缺失或为零")
			}
			if item.MoM == nil {
				notes = append(notes, "环比基期缺失或为零")
			}
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

func pceChange(current, base *float64) *float64 {
	if current == nil || base == nil || *base == 0 {
		return nil
	}
	n := (*current / *base - 1) * 100
	return &n
}

// parsePCE 通过列名匹配固定价格指数，禁止误读PCE支出额、其他频率或变换后的增长率。
func parsePCE(raw string) (map[string]map[string]PCEValue, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED CSV表头失败: %w", err)
	}
	if len(header) != 3 {
		return nil, fmt.Errorf("FRED PCE表头不完整")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != "PCEPI" && name != "PCEPILFE" {
			return nil, fmt.Errorf("FRED返回未知PCE列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("FRED返回重复列: %s", name)
		}
		columns[name] = i
	}
	values := map[string]map[string]PCEValue{"PCEPI": {}, "PCEPILFE": {}}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED CSV失败: %w", err)
		}
		date, err := time.Parse("2006-01-02", row[columns["observation_date"]])
		if err != nil || date.Day() != 1 || date.Format("2006-01-02") != row[columns["observation_date"]] {
			return nil, fmt.Errorf("FRED返回无效月度日期: %s", row[columns["observation_date"]])
		}
		period := date.Format("200601")
		if _, ok := values["PCEPI"][period]; ok {
			return nil, fmt.Errorf("FRED返回重复PCE月份: %s", period)
		}
		for _, id := range []string{"PCEPI", "PCEPILFE"} {
			text := row[columns[id]]
			point := PCEValue{Period: period, RawIndex: text}
			if trimmed := strings.TrimSpace(text); trimmed != "" && trimmed != "." {
				n, err := strconv.ParseFloat(trimmed, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
					return nil, fmt.Errorf("FRED返回无效PCE指数: %s %s %q", id, period, text)
				}
				point.Index = &n
			}
			values[id][period] = point
		}
	}
	if len(values["PCEPI"]) == 0 {
		return nil, fmt.Errorf("FRED未返回PCE历史数据")
	}
	return values, nil
}

type pceCache struct {
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
	CSV       string    `json:"csv"`
}

func (c *Client) pceValues(ctx context.Context) (map[string]map[string]PCEValue, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	path := filepath.Join(c.CacheDir, "pce-prices.json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var cache pceCache
			if json.Unmarshal(raw, &cache) == nil && cache.Source == c.endpoint && time.Since(cache.FetchedAt) >= 0 && time.Since(cache.FetchedAt) < 6*time.Hour {
				if values, err := parsePCE(cache.CSV); err == nil {
					return values, cache.FetchedAt, nil
				}
			}
		}
	}
	slog.DebugContext(ctx, "请求FRED PCE价格指数", "url", c.endpoint)
	// FRED公开CSV下载对sec/Go标识会重置连接，使用兼容的通用下载客户端标识。
	headers := http.Header{"User-Agent": {"curl"}}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, c.endpoint, headers, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("FRED返回HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 8<<20 {
		return nil, time.Time{}, fmt.Errorf("FRED响应超过大小限制")
	}
	values, err := parsePCE(string(raw))
	if err != nil {
		return nil, time.Time{}, err
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return nil, time.Time{}, err
		}
		data, err := json.Marshal(pceCache{c.endpoint, fetched, string(raw)})
		if err != nil {
			return nil, time.Time{}, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".fred-pce-*")
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
	return values, fetched, nil
}
