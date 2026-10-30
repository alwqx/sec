// Package chinamoney 采集中国外汇交易中心发布的人民币汇率。
package chinamoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/forex"
)

const fixingURL = "https://www.chinamoney.com.cn/ags/ms/cm-u-bk-ccpr/CcprHisNew"
const referenceURL = "https://www.chinamoney.com.cn/ags/ms/cm-u-bk-fx/RefRateHis"

type Client struct{ forex.Cache }

func NewClient(cacheDir string) *Client { return &Client{Cache: forex.Cache{CacheDir: cacheDir}} }

type fixingEnvelope struct {
	Head struct {
		Code string `json:"rep_code"`
	} `json:"head"`
	Data struct {
		Pairs []string `json:"searchlist"`
		Total int      `json:"total"`
		Pages int      `json:"pageTotal"`
		Page  int      `json:"pageNum"`
		Begin string   `json:"startDate"`
		End   string   `json:"endDate"`
	} `json:"data"`
	Records []struct {
		Date   string   `json:"date"`
		Values []string `json:"values"`
	} `json:"records"`
}

func parseFixing(raw []byte, pairs []string, begin, end string, page int) (fixingEnvelope, []forex.Observation, error) {
	var env fixingEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, nil, fmt.Errorf("中间价响应不是有效 JSON：%w", err)
	}
	if env.Head.Code != "200" || env.Data.Begin != begin || env.Data.End != end || env.Data.Page != page || env.Data.Total < 0 || env.Data.Pages < 0 || env.Records == nil || len(env.Data.Pairs) != len(pairs) {
		return env, nil, fmt.Errorf("中间价响应元数据无效")
	}
	// 上游按自己的列顺序返回 searchlist，不能要求与请求顺序相同。
	// 列数相同且返回币种各不重复、均属于请求集合时，才允许按该表头映射。
	if err := forex.ValidatePairs(env.Data.Pairs, pairs); err != nil {
		return env, nil, fmt.Errorf("中间价返回货币对集合不符：%w", err)
	}
	if env.Data.Total == 0 {
		if len(env.Records) != 0 {
			return env, nil, fmt.Errorf("中间价总数与记录不符")
		}
	} else if env.Data.Pages < page || len(env.Records) == 0 || len(env.Records) > 10 {
		return env, nil, fmt.Errorf("中间价分页无效")
	}
	items := []forex.Observation{}
	seen := map[string]bool{}
	for _, row := range env.Records {
		date, err := time.Parse("2006-01-02", row.Date)
		if err != nil || row.Date < begin || row.Date > end || seen[row.Date] || len(row.Values) != len(pairs) {
			return env, nil, fmt.Errorf("中间价日期、列数或重复记录无效")
		}
		seen[row.Date] = true
		for i, pair := range env.Data.Pairs {
			value, err := forex.Number(row.Values[i])
			if err != nil {
				return env, nil, err
			}
			item := forex.NewObservation(pair, date.Format("2006-01-02"), "", time.Time{})
			item.Value = value
			item.RawValues["rate"] = row.Values[i]
			if value == nil {
				item.Note = "来源未提供该日中间价"
			}
			items = append(items, item)
		}
	}
	return env, items, nil
}

// Fixing 返回公布日对应的人民币中间价，不将其作为市场成交价。
func (c *Client) Fixing(ctx context.Context, req *forex.Request) (*forex.Result, error) {
	if err := req.Validate(forex.OnshorePairs, "20060104"); err != nil {
		return nil, err
	}
	result := newResult(req, "central-parity", "", "publication-date")
	result.Notes = []string{"中国人民银行授权中国外汇交易中心公布的人民币中间价；单位为每1单位基础货币兑换的计价货币。", "公布日与市场成交时点不同；没有公布的日期不补值，不将中间价拼接为在岸市场价。"}
	seen := map[string]bool{}
	for _, window := range monthWindows(req.Begin, req.End) {
		total, pages, collected := -1, 0, 0
		for page := 1; page <= 100; page++ {
			// 使用官网前端相同的货币对写法与每页10条参数；部分编码形式会被上游拒绝。
			address := fmt.Sprintf("%s?startDate=%s&endDate=%s&currency=%s&pageNum=%d&pageSize=10", fixingURL, window[0], window[1], strings.Join(req.Pairs, ","), page)
			var env fixingEnvelope
			var items []forex.Observation
			_, fetched, err := c.Snapshot(ctx, address, 6*time.Hour, headers(), func(raw []byte) error {
				var err error
				env, items, err = parseFixing(raw, req.Pairs, window[0], window[1], page)
				return err
			})
			if err != nil {
				return nil, err
			}
			if total < 0 {
				total, pages = env.Data.Total, env.Data.Pages
			} else if total != env.Data.Total || pages != env.Data.Pages {
				return nil, fmt.Errorf("中间价分页总数发生变化，请刷新重试")
			}
			collected += len(env.Records)
			for _, item := range items {
				key := item.Pair + item.Date
				if seen[key] {
					return nil, fmt.Errorf("中间价分页重复记录：%s", key)
				}
				seen[key] = true
				item.Source, item.FetchedAt = address, fetched
				result.Observations = append(result.Observations, item)
			}
			if total == 0 || page >= pages {
				if collected != total {
					return nil, fmt.Errorf("中间价分页记录不完整")
				}
				break
			}
			if page == 100 {
				return nil, fmt.Errorf("中间价分页超过上限")
			}
		}
	}
	sortRates(result.Observations)
	return result, nil
}

type referenceEnvelope struct {
	Head struct {
		Code string `json:"rep_code"`
	} `json:"head"`
	Data struct {
		Begin   string `json:"startDateTool"`
		End     string `json:"endDateTool"`
		Message string `json:"message"`
		Flag    string `json:"flag"`
	} `json:"data"`
	Records []map[string]string `json:"records"`
}

func ValidateReference(req *forex.Request) error {
	if err := req.Validate(forex.OnshorePairs, "20150824"); err != nil {
		return err
	}
	switch req.At {
	case "10:00", "11:00", "14:00", "15:00", "16:00":
		return nil
	}
	return fmt.Errorf("在岸参考时点可选 10:00、11:00、14:00、15:00、16:00（北京时间）")
}

func parseReference(raw []byte, pair, begin, end, at string) ([]forex.Observation, error) {
	var env referenceEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("在岸参考汇率响应无效：%w", err)
	}
	if env.Head.Code != "200" || env.Data.Begin != begin || env.Data.End != end || env.Data.Message != "" || (env.Data.Flag != "0" && env.Data.Flag != "1") || env.Records == nil {
		return nil, fmt.Errorf("在岸参考汇率响应元数据无效：%s", env.Data.Message)
	}
	b, _ := time.Parse("02 Jan 2006", begin)
	e, _ := time.Parse("02 Jan 2006", end)
	key := "rateOf" + at[:2] + "hour"
	items := []forex.Observation{}
	seen := map[string]bool{}
	for _, row := range env.Records {
		date, err := time.Parse("2006-01-02", row["dealDate"])
		text, exists := row[key]
		if err != nil || date.Before(b) || date.After(e) || row["ccyPair"] != pair || seen[row["dealDate"]] || !exists {
			return nil, fmt.Errorf("在岸参考汇率日期、货币对或时点字段无效")
		}
		seen[row["dealDate"]] = true
		value, err := forex.Number(text)
		if err != nil {
			return nil, err
		}
		item := forex.NewObservation(pair, row["dealDate"], "", time.Time{})
		item.Value = value
		item.RawValues = row
		if value != nil {
			item.ObservedAt = row["dealDate"] + "T" + at + ":00+08:00"
		} else {
			item.Note = "来源未提供该时点参考汇率"
		}
		items = append(items, item)
	}
	return items, nil
}

// Reference 保留固定日内时点，按月查询，防止上游查询区间限制静默截断。
func (c *Client) Reference(ctx context.Context, req *forex.Request) (*forex.Result, error) {
	if err := ValidateReference(req); err != nil {
		return nil, err
	}
	result := newResult(req, "onshore-reference", req.At, "observation-date")
	result.Notes = []string{"参考汇率按对应时点前30秒的境内银行间成交价加权平均，无成交时采用买卖报价计算；不是人民币中间价，也不是16:30收盘价。", "历史按月查询；休市日不补值，来源列出的空值保留为 null。"}
	for _, window := range monthWindows(req.Begin, req.End) {
		b, _ := time.Parse("2006-01-02", window[0])
		e, _ := time.Parse("2006-01-02", window[1])
		begin, end := b.Format("02 Jan 2006"), e.Format("02 Jan 2006")
		for _, pair := range req.Pairs {
			query := url.Values{"lang": {"cn"}, "startDateTool": {begin}, "endDateTool": {end}, "currencyCode": {strings.ReplaceAll(pair, "/", ".")}}
			address := referenceURL + "?" + query.Encode()
			var items []forex.Observation
			_, fetched, err := c.Snapshot(ctx, address, 6*time.Hour, headers(), func(raw []byte) error {
				var err error
				items, err = parseReference(raw, pair, begin, end, req.At)
				return err
			})
			if err != nil {
				return nil, err
			}
			for _, item := range items {
				item.Source, item.FetchedAt = address, fetched
				result.Observations = append(result.Observations, item)
			}
		}
	}
	sortRates(result.Observations)
	return result, nil
}

func headers() http.Header {
	return http.Header{"Referer": {"https://www.chinamoney.com.cn/chinese/bkccpr/"}}
}
func newResult(req *forex.Request, kind, at, basis string) *forex.Result {
	return &forex.Result{Provider: "CFETS", PriceType: kind, Frequency: "daily", Timezone: "Asia/Shanghai", ObservationTime: at, DateBasis: basis, Begin: req.Begin, End: req.End, DataVersion: "current-visible-release", Observations: []forex.Observation{}}
}
func sortRates(items []forex.Observation) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Date == items[j].Date {
			return items[i].Pair < items[j].Pair
		}
		return items[i].Date < items[j].Date
	})
}
func monthWindows(begin, end string) [][2]string {
	b, _ := time.Parse("20060102", begin)
	e, _ := time.Parse("20060102", end)
	out := [][2]string{}
	for !b.After(e) {
		last := time.Date(b.Year(), b.Month()+1, 0, 0, 0, 0, 0, time.UTC)
		if last.After(e) {
			last = e
		}
		out = append(out, [2]string{b.Format("2006-01-02"), last.Format("2006-01-02")})
		b = last.AddDate(0, 0, 1)
	}
	return out
}
