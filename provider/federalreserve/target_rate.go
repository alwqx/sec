// Package federalreserve 采集美国联邦储备委员会（Federal Reserve Board）的官方数据。
package federalreserve

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	openMarketURL = "https://www.federalreserve.gov/monetarypolicy/openmarket.htm"
	calendarURL   = "https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm"
)

// 目标区间机制起点：此前为单一目标利率，不与区间比较。
const (
	EarliestDate   = "20081216"
	RangeStartDate = "2008-12-16"
)

type Client struct {
	CacheDir string
	Refresh  bool
	endpoint string
	calendar string
	history  string
}

func NewClient(cacheDir string) *Client {
	return &Client{CacheDir: cacheDir, endpoint: openMarketURL, calendar: calendarURL,
		history: "https://www.federalreserve.gov/monetarypolicy/fomchistorical%d.htm"}
}

type TargetRateRequest struct {
	Begin string `json:"begin"`
	End   string `json:"end"`
}

func (r *TargetRateRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("联邦基金目标利率查询参数不能为空")
	}
	for _, value := range []string{r.Begin, r.End} {
		date, err := time.Parse("20060102", value)
		if err != nil || date.Format("20060102") != value {
			return fmt.Errorf("无效日期 %q，请使用YYYYMMDD", value)
		}
	}
	if r.Begin < EarliestDate {
		return fmt.Errorf("目标区间最早支持%s，此前为单一目标利率", EarliestDate)
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始日期不能晚于截至日期")
	}
	if r.End > time.Now().UTC().Format("20060102") {
		return fmt.Errorf("截至日期不能晚于UTC今天")
	}
	return nil
}

// TargetRateEvent 记录目标区间调整；日期以美国官方原文为准，不由会议日期推断。
type TargetRateEvent struct {
	EffectiveDate          string        `json:"effective_date"`
	LowerPercent           float64       `json:"lower_percent"`
	UpperPercent           float64       `json:"upper_percent"`
	PreviousEffectiveDate  string        `json:"previous_effective_date,omitempty"`
	LowerChangeBasisPoints *float64      `json:"lower_change_basis_points"`
	UpperChangeBasisPoints *float64      `json:"upper_change_basis_points"`
	AnnouncedDate          *string       `json:"announced_date"`
	PublishedAt            *time.Time    `json:"published_at"`
	ReleaseStatus          string        `json:"release_status"`
	ReleaseNote            string        `json:"release_note"`
	ReleaseFetchFailed     bool          `json:"release_fetch_failed,omitempty"`
	StatementSourceURL     string        `json:"statement_source_url,omitempty"`
	StatementFetchedAt     *time.Time    `json:"statement_fetched_at"`
	StatementRaw           *StatementRaw `json:"statement_raw"`
	SourceURL              string        `json:"source_url"`
	FetchedAt              time.Time     `json:"fetched_at"`
	Raw                    RateTableRaw  `json:"raw"`
}

type RateTableRaw struct {
	Year     string `json:"year"`
	Date     string `json:"date"`
	Increase string `json:"increase_basis_points"`
	Decrease string `json:"decrease_basis_points"`
	Level    string `json:"level_percent"`
}

type StatementRaw struct {
	Date        string `json:"date"`
	ReleaseTime string `json:"release_time"`
	Decision    string `json:"decision"`
}

type TargetRateResult struct {
	Region      string             `json:"region"`
	Indicator   string             `json:"indicator"`
	Frequency   string             `json:"frequency"`
	DataVersion string             `json:"data_version"`
	Begin       string             `json:"begin"`
	End         string             `json:"end"`
	Unit        string             `json:"unit"`
	Source      string             `json:"source"`
	SourceURL   string             `json:"source_url"`
	FetchedAt   time.Time          `json:"fetched_at"`
	Baseline    *TargetRateEvent   `json:"baseline"`
	Current     *TargetRateEvent   `json:"current"`
	Events      []*TargetRateEvent `json:"events"`
}

var (
	yearPattern  = regexp.MustCompile(`^\d{4}$`)
	rangePattern = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*-\s*(\d+(?:\.\d+)?)$`)
)

func clean(text string) string { return strings.Join(strings.Fields(text), " ") }

func parseTable(body []byte) ([]*TargetRateEvent, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("解析美联储调整表失败: %w", err)
	}
	if !strings.Contains(doc.Text(), "FOMC's target federal funds rate or range") {
		return nil, fmt.Errorf("美联储响应缺少联邦基金目标利率调整表")
	}
	var events []*TargetRateEvent
	year := ""
	var parseErr error
	doc.Find("h4, table").Each(func(_ int, node *goquery.Selection) {
		if parseErr != nil {
			return
		}
		if goquery.NodeName(node) == "h4" {
			year = clean(node.Text())
			return
		}
		if !yearPattern.MatchString(year) || year < "2008" {
			return
		}
		var headers []string
		node.Find("thead th").Each(func(_ int, header *goquery.Selection) { headers = append(headers, clean(header.Text())) })
		if strings.Join(headers, "|") != "Date|Increase|Decrease|Level (%)" {
			parseErr = fmt.Errorf("美联储%s年调整表列结构变化", year)
			return
		}
		node.Find("tbody tr").Each(func(_ int, row *goquery.Selection) {
			if parseErr != nil {
				return
			}
			cells := row.Find("td")
			if cells.Length() != 4 {
				parseErr = fmt.Errorf("美联储%s年调整表记录缺少字段", year)
				return
			}
			rawDate := clean(cells.Eq(0).Text())
			dateCell := cells.Eq(0).Clone()
			dateCell.Find("a, sup").Remove()
			date, err := time.Parse("2006 January 2", year+" "+clean(dateCell.Text()))
			if err != nil {
				parseErr = fmt.Errorf("美联储返回无效生效日期 %s %s", year, rawDate)
				return
			}
			if date.Format("20060102") < EarliestDate {
				return
			}
			raw := RateTableRaw{year, rawDate, clean(cells.Eq(1).Text()), clean(cells.Eq(2).Text()), clean(cells.Eq(3).Text())}
			bounds := rangePattern.FindStringSubmatch(raw.Level)
			if bounds == nil {
				parseErr = fmt.Errorf("美联储%s不是有效目标区间: %s", date.Format("2006-01-02"), raw.Level)
				return
			}
			lower, _ := strconv.ParseFloat(bounds[1], 64)
			upper, _ := strconv.ParseFloat(bounds[2], 64)
			if !finite(lower) || !finite(upper) || lower > upper || lower < 0 || lower == upper {
				parseErr = fmt.Errorf("美联储返回无效目标区间: %s", raw.Level)
				return
			}
			events = append(events, &TargetRateEvent{EffectiveDate: date.Format("2006-01-02"), LowerPercent: lower, UpperPercent: upper, Raw: raw,
				ReleaseStatus: "unavailable", ReleaseNote: "尚未获得可核验的官方声明公布信息"})
		})
	})
	if parseErr != nil {
		return nil, parseErr
	}
	sort.Slice(events, func(i, j int) bool { return events[i].EffectiveDate < events[j].EffectiveDate })
	if len(events) == 0 || events[0].EffectiveDate != RangeStartDate {
		return nil, fmt.Errorf("美联储调整表缺少%s区间机制起点", RangeStartDate)
	}
	for i, event := range events {
		if i == 0 {
			// 初次由单一利率转为区间，保留官方75-100基点原文，不伪造同口径前值。
			continue
		}
		previous := events[i-1]
		if previous.EffectiveDate == event.EffectiveDate {
			return nil, fmt.Errorf("美联储返回重复生效日期: %s", event.EffectiveDate)
		}
		increase, err := strconv.ParseFloat(event.Raw.Increase, 64)
		if err != nil || !finite(increase) || increase < 0 {
			return nil, fmt.Errorf("美联储返回无效加息基点: %s", event.Raw.Increase)
		}
		decrease, err := strconv.ParseFloat(event.Raw.Decrease, 64)
		if err != nil || !finite(decrease) || decrease < 0 || (increase > 0 && decrease > 0) {
			return nil, fmt.Errorf("美联储返回无效降息基点: %s", event.Raw.Decrease)
		}
		lowerChange := math.Round((event.LowerPercent-previous.LowerPercent)*10000) / 100
		upperChange := math.Round((event.UpperPercent-previous.UpperPercent)*10000) / 100
		if increase-decrease == 0 || math.Abs(lowerChange-(increase-decrease)) > 0.001 || math.Abs(upperChange-(increase-decrease)) > 0.001 {
			return nil, fmt.Errorf("美联储%s区间变化与官方调整基点不一致", event.EffectiveDate)
		}
		event.PreviousEffectiveDate = previous.EffectiveDate
		event.LowerChangeBasisPoints, event.UpperChangeBasisPoints = &lowerChange, &upperChange
	}
	return events, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// TargetRate 按生效日期筛选实际调整事件，并保留区间前基准和截至日期有效的目标区间。
// 官方表是当前可见历史版本，不代表当时可得的数据快照，也不补造未调整会议。
func (c *Client) TargetRate(ctx context.Context, req *TargetRateRequest) (*TargetRateResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	body, fetched, err := c.fetch(ctx, c.endpoint, func(body []byte) error { _, err := parseTable(body); return err })
	if err != nil {
		return nil, err
	}
	events, err := parseTable(body)
	if err != nil {
		return nil, err
	}
	begin, _ := time.Parse("20060102", req.Begin)
	end, _ := time.Parse("20060102", req.End)
	result := &TargetRateResult{Region: "US", Indicator: "target-rate", Frequency: "event", DataVersion: "latest-available", Begin: req.Begin, End: req.End,
		Unit: "%", Source: "Federal Reserve Board", SourceURL: c.endpoint, FetchedAt: fetched, Events: []*TargetRateEvent{}}
	for _, event := range events {
		if event.EffectiveDate > end.Format("2006-01-02") {
			break
		}
		event.SourceURL, event.FetchedAt = c.endpoint, fetched
		result.Current = event
		if event.EffectiveDate < begin.Format("2006-01-02") {
			result.Baseline = event
		} else {
			result.Events = append(result.Events, event)
		}
	}
	selected := append([]*TargetRateEvent{}, result.Events...)
	if result.Baseline != nil {
		selected = append(selected, result.Baseline)
	}
	// 只补查选定事件及一个区间前基准；常规查询不会逐一抓取全部历史声明。
	resolver := newReleaseResolver(c)
	for _, event := range selected {
		if err := resolver.enrich(ctx, event); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			event.ReleaseNote = "公布信息不可用：" + err.Error()
			var fetchErr *FetchError
			event.ReleaseFetchFailed = errors.As(err, &fetchErr)
		}
	}
	return result, nil
}
