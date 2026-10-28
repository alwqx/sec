package fred

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const claimsCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=ICSA,CCSA"

type ClaimsRequest struct{ Indicator, Begin, End string }

func (r *ClaimsRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("失业金申领查询请求不能为空")
	}
	if r.Indicator != "initial-claims" && r.Indicator != "continued-claims" {
		return fmt.Errorf("不支持的失业金申领指标: %s", r.Indicator)
	}
	for _, date := range []string{r.Begin, r.End} {
		if _, err := time.Parse("20060102", date); err != nil || len(date) != 8 {
			return fmt.Errorf("无效日期 %q，请使用YYYYMMDD", date)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始日期不能晚于截至日期")
	}
	if r.Begin < "19670107" {
		return fmt.Errorf("失业金周度数据支持自19670107起")
	}
	if r.End > time.Now().UTC().Format("20060102") {
		return fmt.Errorf("截至日期不能晚于当前UTC日期")
	}
	return nil
}

type ClaimsValue struct {
	WeekEnding string   `json:"week_ending"`
	Count      *float64 `json:"count"`
	RawValue   string   `json:"raw_value"`
}

type ClaimsObservation struct {
	ClaimsValue
	WeeklyChange    *float64      `json:"weekly_change"`
	PreviousWeek    ClaimsValue   `json:"previous_week"`
	FourWeekAverage *float64      `json:"four_week_average"`
	FourWeekWindow  []ClaimsValue `json:"four_week_window"`
	Note            string        `json:"note,omitempty"`
}

type ClaimsResult struct {
	Region              string              `json:"region"`
	Indicator           string              `json:"indicator"`
	SeriesID            string              `json:"series_id"`
	Measure             string              `json:"measure"`
	Coverage            string              `json:"coverage"`
	Unit                string              `json:"unit"`
	Frequency           string              `json:"frequency"`
	WeekEndingDay       string              `json:"week_ending_day"`
	SeasonallyAdjusted  bool                `json:"seasonally_adjusted"`
	Begin               string              `json:"begin"`
	End                 string              `json:"end"`
	Producer            string              `json:"producer"`
	Distributor         string              `json:"distributor"`
	Source              string              `json:"source"`
	SeriesSource        string              `json:"series_source"`
	OriginalSource      string              `json:"original_source"`
	FetchedAt           time.Time           `json:"fetched_at"`
	DataVersion         string              `json:"data_version"`
	PublishedAt         *time.Time          `json:"published_at"`
	RevisionID          *string             `json:"revision_id"`
	LatestAvailableWeek string              `json:"latest_available_week"`
	Observations        []ClaimsObservation `json:"observations"`
	Note                string              `json:"note"`
}

// Claims 按实际周六所属周查询两条季调序列，不按同次发布将续请日期前移。
func (c *Client) Claims(ctx context.Context, req *ClaimsRequest) (*ClaimsResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var values map[string]map[string]ClaimsValue
	_, fetched, err := c.csvSnapshot(ctx, c.claimsEndpoint, "jobless-claims.json", func(raw string) error { var err error; values, err = parseClaims(raw); return err })
	if err != nil {
		return nil, err
	}
	id, measure, unit := "ICSA", "initial-claims", "件"
	if req.Indicator == "continued-claims" {
		id, measure, unit = "CCSA", "insured-unemployment", "人"
	}
	result := &ClaimsResult{Region: "US", Indicator: req.Indicator, SeriesID: id, Measure: measure, Coverage: "regular state unemployment insurance programs", Unit: unit, Frequency: "weekly", WeekEndingDay: "Saturday", SeasonallyAdjusted: true, Begin: req.Begin, End: req.End, Producer: "U.S. Department of Labor, Employment and Training Administration", Distributor: "FRED", Source: claimsCSVURL, SeriesSource: "https://fred.stlouisfed.org/series/" + id, OriginalSource: "https://oui.doleta.gov/unemploy/claims.asp", FetchedAt: fetched, DataVersion: "latest-available", Observations: []ClaimsObservation{}, Note: "美国常规州失业保险项目季调周度数据，所属周截至周六；初请是资格申请，续请是参保失业人数，不代表全部失业人口。初请与续请可能有不同的最新所属周，不前移续请日期。周变化按严格前一周相减；四周均值为当周及前三个连续日历周的算术平均计算值，任一缺失即为null。历史为当前可见修订值，CSV没有初值/修订标记、逐期实际公布时间或完整历史版本，published_at/revision_id=null。"}
	point := func(week time.Time) ClaimsValue {
		if v, ok := values[id][week.Format("20060102")]; ok {
			return v
		}
		return ClaimsValue{WeekEnding: week.Format("2006-01-02")}
	}
	latest := ""
	for date, v := range values[id] {
		if v.Count != nil && date > latest {
			latest = date
			result.LatestAvailableWeek = v.WeekEnding
		}
	}
	begin, _ := time.Parse("20060102", req.Begin)
	end, _ := time.Parse("20060102", req.End)
	for begin.Weekday() != time.Saturday {
		begin = begin.AddDate(0, 0, 1)
	}
	for week := begin; !week.After(end); week = week.AddDate(0, 0, 7) {
		item := ClaimsObservation{ClaimsValue: point(week), PreviousWeek: point(week.AddDate(0, 0, -7)), FourWeekWindow: []ClaimsValue{}}
		if item.Count != nil && item.PreviousWeek.Count != nil {
			n := *item.Count - *item.PreviousWeek.Count
			item.WeeklyChange = &n
		}
		sum, complete := 0.0, true
		for i := 0; i < 4; i++ {
			v := point(week.AddDate(0, 0, -7*i))
			item.FourWeekWindow = append(item.FourWeekWindow, v)
			if v.Count == nil {
				complete = false
			} else {
				sum += *v.Count
			}
		}
		if complete {
			average := sum / 4
			item.FourWeekAverage = &average
		}
		notes := []string{}
		if item.Count == nil {
			notes = append(notes, "当周数据缺失或尚未发布")
		} else if item.WeeklyChange == nil {
			notes = append(notes, "前一周数据缺失")
		}
		if item.FourWeekAverage == nil {
			notes = append(notes, "连续四周数据不完整")
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

// parseClaims 按固定系列列名及周六日期校验完整CSV，保留不同系列的空单元格。
func parseClaims(raw string) (map[string]map[string]ClaimsValue, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED申领CSV表头失败: %w", err)
	}
	if len(header) != 3 {
		return nil, fmt.Errorf("FRED申领CSV表头不完整")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != "ICSA" && name != "CCSA" {
			return nil, fmt.Errorf("FRED返回未知申领列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("FRED返回重复列: %s", name)
		}
		columns[name] = i
	}
	values := map[string]map[string]ClaimsValue{"ICSA": {}, "CCSA": {}}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED申领CSV失败: %w", err)
		}
		dateText := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", dateText)
		if err != nil || date.Format("2006-01-02") != dateText || date.Weekday() != time.Saturday || dateText < "1967-01-07" {
			return nil, fmt.Errorf("FRED返回无效周六日期: %s", dateText)
		}
		key := date.Format("20060102")
		if _, ok := values["ICSA"][key]; ok {
			return nil, fmt.Errorf("FRED返回重复所属周: %s", dateText)
		}
		for _, id := range []string{"ICSA", "CCSA"} {
			text := row[columns[id]]
			point := ClaimsValue{WeekEnding: dateText, RawValue: text}
			if trimmed := strings.TrimSpace(text); trimmed != "" && trimmed != "." {
				n, err := strconv.ParseFloat(trimmed, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n != math.Trunc(n) {
					return nil, fmt.Errorf("FRED返回无效申领数量: %s %s %q", id, dateText, text)
				}
				point.Count = &n
			}
			values[id][key] = point
		}
	}
	if len(values["ICSA"]) == 0 {
		return nil, fmt.Errorf("FRED未返回申领历史数据")
	}
	return values, nil
}
