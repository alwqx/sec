// Package forex 定义汇率采集的共同口径与经过校验的原始响应缓存。
package forex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

var Beijing = time.FixedZone("CST", 8*3600)

var MarketPairs = []string{"USD/CNY", "USD/CNH", "USD/HKD", "HKD/CNY"}
var OnshorePairs = []string{"USD/CNY", "HKD/CNY"}
var FREDPairs = []string{"USD/CNY", "USD/HKD"}

type Request struct {
	Pairs      []string
	Begin, End string // YYYYMMDD，均包含在查询内。
	At         string // 在岸参考汇率的当地观测时点。
}

func ValidatePairs(pairs, allowed []string) error {
	if len(pairs) == 0 {
		return fmt.Errorf("请至少指定一个货币对")
	}
	seen := map[string]bool{}
	for _, pair := range pairs {
		valid := false
		for _, candidate := range allowed {
			valid = valid || pair == candidate
		}
		if !valid {
			return fmt.Errorf("不支持货币对 %s，可选：%s", pair, strings.Join(allowed, "、"))
		}
		if seen[pair] {
			return fmt.Errorf("货币对重复：%s", pair)
		}
		seen[pair] = true
	}
	return nil
}

func (r *Request) Validate(allowed []string, earliest string) error {
	if r == nil {
		return fmt.Errorf("汇率请求不能为空")
	}
	if err := ValidatePairs(r.Pairs, allowed); err != nil {
		return err
	}
	for _, date := range []string{r.Begin, r.End} {
		d, err := time.Parse("20060102", date)
		if err != nil || d.Format("20060102") != date {
			return fmt.Errorf("无效汇率日期 %q，请使用 YYYYMMDD", date)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始日期不能晚于结束日期")
	}
	if earliest != "" && r.Begin < earliest {
		return fmt.Errorf("该汇率来源支持自 %s 起查询", earliest)
	}
	if r.End > time.Now().In(Beijing).Format("20060102") {
		return fmt.Errorf("截至日期不能晚于北京时间今天")
	}
	return nil
}

// Observation 的 Value 单位始终为每一单位基础货币兑换的计价货币。
// 日线没有已核实的时区和收盘时刻时，只保留来源交易日期。
type Observation struct {
	Date          string            `json:"date"`
	Pair          string            `json:"pair"`
	BaseCurrency  string            `json:"base_currency"`
	QuoteCurrency string            `json:"quote_currency"`
	Market        string            `json:"market"`
	Value         *float64          `json:"value"`
	Bid           *float64          `json:"bid,omitempty"`
	Ask           *float64          `json:"ask,omitempty"`
	Open          *float64          `json:"open,omitempty"`
	High          *float64          `json:"high,omitempty"`
	Low           *float64          `json:"low,omitempty"`
	Close         *float64          `json:"close,omitempty"`
	ObservedAt    string            `json:"observed_at,omitempty"`
	Source        string            `json:"source"`
	FetchedAt     time.Time         `json:"fetched_at"`
	RawValues     map[string]string `json:"raw_values"`
	Note          string            `json:"note,omitempty"`
}

type Result struct {
	Provider         string               `json:"provider"`
	PriceType        string               `json:"price_type"`
	Frequency        string               `json:"frequency"`
	Timezone         string               `json:"timezone"`
	ObservationTime  string               `json:"observation_time,omitempty"`
	DateBasis        string               `json:"date_basis"`
	Begin            string               `json:"begin,omitempty"`
	End              string               `json:"end,omitempty"`
	DataVersion      string               `json:"data_version"`
	Observations     []Observation        `json:"observations"`
	SnapshotCoverage map[string][2]string `json:"snapshot_coverage,omitempty"`
	Notes            []string             `json:"notes"`
}

func NewObservation(pair, date, source string, fetched time.Time) Observation {
	parts := strings.Split(pair, "/")
	market := "foreign-exchange"
	if strings.Contains(pair, "CNY") {
		market = "onshore"
	}
	if strings.Contains(pair, "CNH") {
		market = "offshore"
	}
	return Observation{Pair: pair, Date: date, BaseCurrency: parts[0], QuoteCurrency: parts[1], Market: market, Source: source, FetchedAt: fetched, RawValues: map[string]string{}}
}

func Number(raw string) (*float64, error) {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "", ".", "-", "---", "/", "N/A":
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) || v <= 0 {
		return nil, fmt.Errorf("汇率数值无效：%q", raw)
	}
	return &v, nil
}

type Cache struct {
	CacheDir string
	Refresh  bool
}
type snapshot struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	Body      []byte    `json:"body"`
}

// Snapshot 只缓存经过调用方语义校验的响应，失败刷新不会覆盖旧缓存。
func (c *Cache) Snapshot(ctx context.Context, address string, ttl time.Duration, headers http.Header, validate func([]byte) error) ([]byte, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	key := sha256.Sum256([]byte(address))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var old snapshot
			if json.Unmarshal(raw, &old) == nil && old.URL == address && time.Since(old.FetchedAt) >= 0 && time.Since(old.FetchedAt) < ttl && validate(old.Body) == nil {
				return old.Body, old.FetchedAt, nil
			}
		}
	}
	if headers == nil {
		headers = http.Header{}
	}
	if headers.Get("User-Agent") == "" {
		headers.Set("User-Agent", utils.UserAgent())
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, address, headers, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("请求汇率数据失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("汇率来源返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 16<<20 {
		return nil, time.Time{}, fmt.Errorf("汇率响应超过16MiB")
	}
	if err := validate(raw); err != nil {
		return nil, time.Time{}, err
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return nil, time.Time{}, err
		}
		body, err := json.Marshal(snapshot{address, fetched, raw})
		if err != nil {
			return nil, time.Time{}, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".fx-*")
		if err != nil {
			return nil, time.Time{}, err
		}
		defer os.Remove(f.Name())
		_, writeErr := f.Write(body)
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
	return raw, fetched, nil
}
