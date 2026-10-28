package bls

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	unemploymentRateSeriesID  = "LNS14000000"
	participationRateSeriesID = "LNS11300000"
)

var laborForceSeriesIDs = []string{unemploymentRateSeriesID, participationRateSeriesID}

type LaborForceRequest struct{ Indicator, Begin, End string }

func (r *LaborForceRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("劳动力比率查询请求不能为空")
	}
	if r.Indicator != "unemployment" && r.Indicator != "participation" {
		return fmt.Errorf("不支持的美国劳动力比率指标: %s", r.Indicator)
	}
	return validateMonthlyRange(r.Begin, r.End, "194801")
}

// LaborForceValue 保留CPS官方季调比率、原始文本和脚注，不从人数重新估算。
type LaborForceValue struct {
	Period    string     `json:"period"`
	SeriesID  string     `json:"series_id"`
	Rate      *float64   `json:"rate_percent"`
	RawValue  string     `json:"raw_value"`
	Footnotes []Footnote `json:"footnotes"`
	Latest    bool       `json:"latest"`
	FetchedAt time.Time  `json:"fetched_at"`
}

type LaborForceObservation struct {
	LaborForceValue
	MoMChange *float64        `json:"mom_change_pp"`
	YoYChange *float64        `json:"yoy_change_pp"`
	MoMBase   LaborForceValue `json:"mom_base"`
	YoYBase   LaborForceValue `json:"yoy_base"`
	Note      string          `json:"note,omitempty"`
}

type LaborForceResult struct {
	Region             string                  `json:"region"`
	Indicator          string                  `json:"indicator"`
	SeriesID           string                  `json:"series_id"`
	Survey             string                  `json:"survey"`
	Population         string                  `json:"population"`
	Numerator          string                  `json:"numerator"`
	Denominator        string                  `json:"denominator"`
	Frequency          string                  `json:"frequency"`
	Unit               string                  `json:"unit"`
	ChangeUnit         string                  `json:"change_unit"`
	SeasonallyAdjusted bool                    `json:"seasonally_adjusted"`
	Begin              string                  `json:"begin"`
	End                string                  `json:"end"`
	Source             string                  `json:"source"`
	DataVersion        string                  `json:"data_version"`
	PublishedAt        *time.Time              `json:"published_at"`
	Batches            []CPIBatch              `json:"batches"`
	Observations       []LaborForceObservation `json:"observations"`
	Note               string                  `json:"note"`
}

// LaborForce 查询16岁及以上非机构平民人口的官方失业率或劳动参与率。
// 变化是同一响应内严格基期的比率差，单位为百分点。
func (c *Client) LaborForce(ctx context.Context, req *LaborForceRequest) (*LaborForceResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	id, numerator, denominator := unemploymentRateSeriesID, "unemployed persons", "civilian labor force"
	if req.Indicator == "participation" {
		id, numerator, denominator = participationRateSeriesID, "civilian labor force", "civilian noninstitutional population"
	}
	result := &LaborForceResult{Region: "US", Indicator: req.Indicator, SeriesID: id, Survey: "CPS household survey", Population: "civilian noninstitutional population age 16 and older", Numerator: numerator, Denominator: denominator, Frequency: "monthly", Unit: "%", ChangeUnit: "百分点", SeasonallyAdjusted: true, Begin: req.Begin, End: req.End, Source: apiURL, DataVersion: "latest-available", Batches: []CPIBatch{}, Observations: []LaborForceObservation{}, Note: "BLS CPS住户调查，16岁及以上非机构平民人口；失业率=失业人数/平民劳动力，劳动参与率=平民劳动力/非机构平民人口，均为官方季调比率。较上月/上年同月变化为比率差，单位为百分点，不是相对百分比增速；同批严格基期缺失时不计算。人口控制更新可能影响跨期可比性，季调历史可能修订；保留来源脚注，当前查询不是当时发布快照。API未提供实际公布时间和完整历史版本档案，published_at=null。"}
	point := func(period string, values map[string]*monthlyValue) LaborForceValue {
		item := LaborForceValue{Period: period, SeriesID: id, Footnotes: []Footnote{}}
		if v := values[period]; v != nil {
			item.Rate, item.RawValue, item.Footnotes, item.Latest, item.FetchedAt = v.Number, v.RawValue, v.Footnotes, v.Latest, v.FetchedAt
		}
		return item
	}
	first := begin.AddDate(-1, 0, 0).Year()
	if first < 1948 {
		first = 1948
	}
	for year := first; ; year += 9 {
		last := year + 9
		if last > end.Year() {
			last = end.Year()
		}
		response, fetched, err := c.query(ctx, laborForceSeriesIDs, year, last)
		if err != nil {
			return nil, err
		}
		batch := map[string]map[string]*monthlyValue{}
		for _, seriesID := range laborForceSeriesIDs {
			batch[seriesID] = map[string]*monthlyValue{}
		}
		if err := mergeMonthly(response, year, last, fetched, batch); err != nil {
			return nil, err
		}
		result.Batches = append(result.Batches, CPIBatch{year, last, response.Messages, fetched})
		start := year
		if year != first {
			start++
		} // 重叠年只提供同批基期。
		for month := time.Date(start, 1, 1, 0, 0, 0, 0, time.UTC); month.Year() <= last && !month.After(end); month = month.AddDate(0, 1, 0) {
			if month.Before(begin) {
				continue
			}
			item := LaborForceObservation{LaborForceValue: point(month.Format("200601"), batch[id]), MoMBase: point(month.AddDate(0, -1, 0).Format("200601"), batch[id]), YoYBase: point(month.AddDate(-1, 0, 0).Format("200601"), batch[id])}
			notes := []string{}
			if item.Rate == nil {
				notes = append(notes, "当月比率缺失")
			} else {
				if item.MoMBase.Rate != nil {
					n := *item.Rate - *item.MoMBase.Rate
					item.MoMChange = &n
				} else {
					notes = append(notes, "上月比率缺失")
				}
				if item.YoYBase.Rate != nil {
					n := *item.Rate - *item.YoYBase.Rate
					item.YoYChange = &n
				} else {
					notes = append(notes, "上年同月比率缺失")
				}
			}
			item.Note = strings.Join(notes, "；")
			result.Observations = append(result.Observations, item)
		}
		if last == end.Year() {
			break
		}
	}
	return result, nil
}
