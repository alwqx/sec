package bls

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var earningsHoursSeriesIDs = []string{"CES0500000003", "CES0500000002"}

type EarningsHoursRequest struct{ Indicator, Begin, End string }

func (r *EarningsHoursRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("时薪与工时查询请求不能为空")
	}
	if r.Indicator != "earnings" && r.Indicator != "weekly-hours" {
		return fmt.Errorf("不支持的美国时薪与工时指标: %s", r.Indicator)
	}
	return validateMonthlyRange(r.Begin, r.End, "200603")
}

// EarningsHoursValue 保留CES原始水平、脚注和抓取时间；具体单位由结果元数据指定。
type EarningsHoursValue struct {
	Period      string     `json:"period"`
	SeriesID    string     `json:"series_id"`
	Value       *float64   `json:"value"`
	RawValue    string     `json:"raw_value"`
	Preliminary bool       `json:"preliminary"`
	Footnotes   []Footnote `json:"footnotes"`
	Latest      bool       `json:"latest"`
	FetchedAt   time.Time  `json:"fetched_at"`
}

type EarningsHoursObservation struct {
	EarningsHoursValue
	Change  *float64           `json:"monthly_change"`
	MoM     *float64           `json:"mom_percent"`
	YoY     *float64           `json:"yoy_percent"`
	MoMBase EarningsHoursValue `json:"mom_base"`
	YoYBase EarningsHoursValue `json:"yoy_base"`
	Note    string             `json:"note,omitempty"`
}

type EarningsHoursResult struct {
	Region             string                     `json:"region"`
	Indicator          string                     `json:"indicator"`
	SeriesID           string                     `json:"series_id"`
	Population         string                     `json:"population"`
	Frequency          string                     `json:"frequency"`
	Unit               string                     `json:"unit"`
	PriceBasis         string                     `json:"price_basis,omitempty"`
	SeasonallyAdjusted bool                       `json:"seasonally_adjusted"`
	Begin              string                     `json:"begin"`
	End                string                     `json:"end"`
	Source             string                     `json:"source"`
	DataVersion        string                     `json:"data_version"`
	PublishedAt        *time.Time                 `json:"published_at"`
	Batches            []CPIBatch                 `json:"batches"`
	Observations       []EarningsHoursObservation `json:"observations"`
	Note               string                     `json:"note"`
}

// EarningsHours 查询私人非农全部雇员的季调平均时薪或平均每周工时。
// 同比和环比的当前值及基期均取自同一响应，不跨批次混用修订版本。
func (c *Client) EarningsHours(ctx context.Context, req *EarningsHoursRequest) (*EarningsHoursResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	id, unit, priceBasis := earningsHoursSeriesIDs[0], "美元/小时", "current-dollars"
	if req.Indicator == "weekly-hours" {
		id, unit, priceBasis = earningsHoursSeriesIDs[1], "小时/周", ""
	}
	result := &EarningsHoursResult{Region: "US", Indicator: req.Indicator, SeriesID: id, Population: "all employees on private nonfarm payrolls, full-time and part-time", Frequency: "monthly", Unit: unit, PriceBasis: priceBasis, SeasonallyAdjusted: true, Begin: req.Begin, End: req.End, Source: apiURL, DataVersion: "latest-available", Batches: []CPIBatch{}, Observations: []EarningsHoursObservation{}, Note: "BLS CES私人非农部门全部雇员，含全职与兼职，不含政府雇员；时薪为名义税前平均收入，工时为平均支付工时。月变化为相邻月水平差，同/环比均为同口径季调水平计算值，不年化。当前值与基期取自同一响应，缺口不补，零基期不计算百分比。P为初步估计，未标P仍可能修订；历史为当前可见版本，未提供官方历史版本档案及实际公布时间，published_at=null。"}
	point := func(period string, values map[string]*monthlyValue) EarningsHoursValue {
		item := EarningsHoursValue{Period: period, SeriesID: id, Footnotes: []Footnote{}}
		if v := values[period]; v != nil {
			item.Value, item.RawValue, item.Footnotes, item.Latest, item.FetchedAt = v.Number, v.RawValue, v.Footnotes, v.Latest, v.FetchedAt
			for _, footnote := range v.Footnotes {
				if footnote.Code == "P" {
					item.Preliminary = true
				}
			}
		}
		return item
	}
	first := begin.AddDate(-1, 0, 0).Year()
	if first < 2006 {
		first = 2006
	}
	for year := first; ; year += 9 {
		last := year + 9
		if last > end.Year() {
			last = end.Year()
		}
		response, fetched, err := c.query(ctx, earningsHoursSeriesIDs, year, last)
		if err != nil {
			return nil, err
		}
		batch := map[string]map[string]*monthlyValue{}
		for _, seriesID := range earningsHoursSeriesIDs {
			batch[seriesID] = map[string]*monthlyValue{}
		}
		if err := mergeMonthly(response, year, last, fetched, batch); err != nil {
			return nil, err
		}
		result.Batches = append(result.Batches, CPIBatch{year, last, response.Messages, fetched})
		start := year
		if year != first {
			start++
		} // 重叠年仅提供同批同比/环比基期。
		for month := time.Date(start, 1, 1, 0, 0, 0, 0, time.UTC); month.Year() <= last && !month.After(end); month = month.AddDate(0, 1, 0) {
			if month.Before(begin) {
				continue
			}
			item := EarningsHoursObservation{EarningsHoursValue: point(month.Format("200601"), batch[id]), MoMBase: point(month.AddDate(0, -1, 0).Format("200601"), batch[id]), YoYBase: point(month.AddDate(-1, 0, 0).Format("200601"), batch[id])}
			notes := []string{}
			if item.Value == nil {
				notes = append(notes, "当月水平缺失")
			} else {
				if item.MoMBase.Value != nil {
					change := *item.Value - *item.MoMBase.Value
					item.Change = &change
				} else {
					notes = append(notes, "上月水平缺失")
				}
				item.MoM = earningsHoursPercent(item.Value, item.MoMBase.Value)
				item.YoY = earningsHoursPercent(item.Value, item.YoYBase.Value)
				if item.MoMBase.Value != nil && *item.MoMBase.Value == 0 {
					notes = append(notes, "上月基期为零，不计算环比百分比")
				}
				if item.YoY == nil {
					notes = append(notes, "同比基期缺失或为零")
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

func earningsHoursPercent(current, base *float64) *float64 {
	if current == nil || base == nil || *base == 0 {
		return nil
	}
	n := (*current / *base - 1) * 100
	return &n
}
