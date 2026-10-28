package bls

import (
	"context"
	"fmt"
	"time"
)

const payrollSeriesID = "CES0000000001"

type PayrollRequest struct{ Begin, End string }

func (r *PayrollRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("非农就业查询请求不能为空")
	}
	return validateMonthlyRange(r.Begin, r.End, "193901")
}

// PayrollValue 表示CES岗位水平，单位为千个岗位；不是去重后的就业人数。
type PayrollValue struct {
	Period      string     `json:"period"`
	Employment  *float64   `json:"employment_thousands"`
	RawValue    string     `json:"raw_value"`
	Preliminary bool       `json:"preliminary"`
	Footnotes   []Footnote `json:"footnotes"`
	FetchedAt   time.Time  `json:"fetched_at"`
}

type PayrollObservation struct {
	PayrollValue
	Change           *float64         `json:"monthly_change_thousands"`
	Previous         PayrollValue     `json:"previous_month"`
	ObservedVersions []PayrollVersion `json:"observed_versions"`
	Note             string           `json:"note,omitempty"`
}

type PayrollResult struct {
	Region                          string               `json:"region"`
	SeriesID                        string               `json:"series_id"`
	Survey                          string               `json:"survey"`
	Frequency                       string               `json:"frequency"`
	Unit                            string               `json:"unit"`
	SeasonallyAdjusted              bool                 `json:"seasonally_adjusted"`
	Begin                           string               `json:"begin"`
	End                             string               `json:"end"`
	Source                          string               `json:"source"`
	DataVersion                     string               `json:"data_version"`
	PublishedAt                     *time.Time           `json:"published_at"`
	Batches                         []CPIBatch           `json:"batches"`
	HistoryEnabled                  bool                 `json:"history_enabled"`
	OfficialVintageArchiveAvailable bool                 `json:"official_vintage_archive_available"`
	RevisionArchiveSource           string               `json:"revision_archive_source"`
	Observations                    []PayrollObservation `json:"observations"`
	Note                            string               `json:"note"`
}

// Payrolls 采集季调非农岗位水平，严格用相邻月份差额计算当月净新增。
func (c *Client) Payrolls(ctx context.Context, req *PayrollRequest) (*PayrollResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	first := begin.AddDate(0, -1, 0).Year()
	if first < 1939 {
		first = 1939
	}
	values := map[string]*monthlyValue{}
	previous := map[string]*monthlyValue{}
	observed := map[string]*monthlyValue{}
	result := &PayrollResult{Region: "US", SeriesID: payrollSeriesID, Survey: "CES establishment survey", Frequency: "monthly", Unit: "千个岗位", SeasonallyAdjusted: true, Begin: req.Begin, End: req.End, Source: apiURL, DataVersion: "latest-available", Batches: []CPIBatch{}, HistoryEnabled: c.CacheDir != "", RevisionArchiveSource: "https://www.bls.gov/web/empsit/cesnaicsrev.htm", Observations: []PayrollObservation{}, Note: "BLS季调非农工资岗位总数，不是去重就业人数；兼职多份工作的个人可计为多个岗位。新增=当月水平−上月水平，单位均为千个岗位，不跨缺口。P脚注为初步估计，未标P也可能有年度基准/季调修订。本机观测版本不是官方首次发布/第二次/第三次估计；官方历史版本档案尚未接入（公开下载受限），official_vintage_archive_available=false。API不提供实际公布时间，published_at=null。"}
	// 后续批次重叠一个自然年，为一月提供同一响应中的十二月基期。
	for year := first; ; year += 9 {
		last := year + 9
		if last > end.Year() {
			last = end.Year()
		}
		response, fetched, err := c.query(ctx, []string{payrollSeriesID}, year, last)
		if err != nil {
			return nil, err
		}
		batch := map[string]map[string]*monthlyValue{payrollSeriesID: {}}
		if err := mergeMonthly(response, year, last, fetched, batch); err != nil {
			return nil, err
		}
		for period, value := range batch[payrollSeriesID] {
			if old := observed[period]; old == nil || value.FetchedAt.After(old.FetchedAt) {
				observed[period] = value
			}
		}
		start := year
		if year != first {
			start++ // 重叠年只提供下一年基期，不覆盖前批次的展示值。
		}
		for month := time.Date(start, 1, 1, 0, 0, 0, 0, time.UTC); month.Year() <= last; month = month.AddDate(0, 1, 0) {
			period := month.Format("200601")
			values[period] = batch[payrollSeriesID][period]
			previous[period] = batch[payrollSeriesID][month.AddDate(0, -1, 0).Format("200601")]
		}
		result.Batches = append(result.Batches, CPIBatch{year, last, response.Messages, fetched})
		if last == end.Year() {
			break
		}
	}
	history, err := c.observePayrollVersions(ctx, observed)
	if err != nil {
		return nil, err
	}
	point := func(period string, v *monthlyValue) PayrollValue {
		item := PayrollValue{Period: period, Footnotes: []Footnote{}}
		if v != nil {
			item.Employment = v.Number
			item.RawValue = v.RawValue
			item.Footnotes = v.Footnotes
			item.FetchedAt = v.FetchedAt
			for _, note := range v.Footnotes {
				if note.Code == "P" {
					item.Preliminary = true
				}
			}
		}
		return item
	}
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		period := month.Format("200601")
		item := PayrollObservation{PayrollValue: point(period, values[period]), Previous: point(month.AddDate(0, -1, 0).Format("200601"), previous[period]), ObservedVersions: []PayrollVersion{}}
		if versions := history[period]; versions != nil {
			item.ObservedVersions = versions
		}
		if item.Employment != nil && item.Previous.Employment != nil {
			n := *item.Employment - *item.Previous.Employment
			item.Change = &n
		}
		if item.Employment == nil {
			item.Note = "当月岗位水平缺失"
		} else if item.Change == nil {
			item.Note = "上月岗位水平缺失，不计算新增"
		}
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}
