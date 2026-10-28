package bls

import (
	"context"
	"fmt"
	"strings"
	"time"
)

var cpiSeriesIDs = []string{"CUUR0000SA0", "CUSR0000SA0", "CUUR0000SA0L1E", "CUSR0000SA0L1E"}

type CPIRequest struct{ Indicator, Begin, End string }

func (r *CPIRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("CPI查询请求不能为空")
	}
	if r.Indicator != "cpi" && r.Indicator != "core-cpi" {
		return fmt.Errorf("不支持的美国CPI指标: %s", r.Indicator)
	}
	return validateMonthlyRange(r.Begin, r.End, "191301")
}

type CPISeries struct {
	ID                 string `json:"series_id"`
	SeasonallyAdjusted bool   `json:"seasonally_adjusted"`
}

// CPIValue 保留源指数文本和脚注，避免将不可用数据误作零值。
type CPIValue struct {
	Period    string     `json:"period"`
	SeriesID  string     `json:"series_id"`
	Index     *float64   `json:"index"`
	RawValue  string     `json:"raw_value"`
	Footnotes []Footnote `json:"footnotes"`
	Latest    bool       `json:"latest"`
	FetchedAt time.Time  `json:"fetched_at"`
}

type CPIObservation struct {
	Period  string    `json:"period"`
	NSA     *CPIValue `json:"not_seasonally_adjusted"`
	SA      *CPIValue `json:"seasonally_adjusted"`
	YoY     *float64  `json:"yoy_percent"`
	YoYBase *CPIValue `json:"yoy_base"`
	MoM     *float64  `json:"mom_percent"`
	MoMBase *CPIValue `json:"mom_base"`
	Note    string    `json:"note,omitempty"`
}

type CPIBatch struct {
	StartYear int       `json:"start_year"`
	EndYear   int       `json:"end_year"`
	Messages  []string  `json:"messages"`
	FetchedAt time.Time `json:"fetched_at"`
}

type CPIResult struct {
	Region       string           `json:"region"`
	Indicator    string           `json:"indicator"`
	Population   string           `json:"population"`
	Frequency    string           `json:"frequency"`
	IndexBase    string           `json:"index_base"`
	Begin        string           `json:"begin"`
	End          string           `json:"end"`
	Source       string           `json:"source"`
	DataVersion  string           `json:"data_version"`
	PublishedAt  *time.Time       `json:"published_at"`
	Series       []CPISeries      `json:"series"`
	Batches      []CPIBatch       `json:"batches"`
	Observations []CPIObservation `json:"observations"`
	Note         string           `json:"note"`
}

// CPI 查询CPI-U美国城市平均指数；同比用NSA，月环比用SA，严格按同月/相邻月基期计算。
func (c *Client) CPI(ctx context.Context, req *CPIRequest) (*CPIResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	first := begin.AddDate(-1, 0, 0).Year()
	if first < 1913 {
		first = 1913
	}
	values := map[string]map[string]*monthlyValue{}
	for _, id := range cpiSeriesIDs {
		values[id] = map[string]*monthlyValue{}
	}
	result := &CPIResult{Region: "US", Indicator: req.Indicator, Population: "CPI-U: U.S. city average, all urban consumers", Frequency: "monthly", IndexBase: "1982-1984=100", Begin: req.Begin, End: req.End, Source: apiURL, DataVersion: "latest-available", Batches: []CPIBatch{}, Observations: []CPIObservation{}, Note: "同比=(当月非季调指数/上年同月非季调指数−1)×100；月环比=(当月季调指数/上月季调指数−1)×100，均为计算值，未年化。不跨缺口寻找基期，不补值。季调序列可能年度修订最近五年；历史查询为当前可见版本。接口未提供逐月实际公布时间或历史版本档案，published_at为null。"}
	for year := first; year <= end.Year(); year += 10 {
		last := year + 9
		if last > end.Year() {
			last = end.Year()
		}
		response, fetched, err := c.query(ctx, cpiSeriesIDs, year, last)
		if err != nil {
			return nil, err
		}
		if err := mergeMonthly(response, year, last, fetched, values); err != nil {
			return nil, err
		}
		result.Batches = append(result.Batches, CPIBatch{year, last, response.Messages, fetched})
	}
	nsaID, saID := cpiSeriesIDs[0], cpiSeriesIDs[1]
	if req.Indicator == "core-cpi" {
		nsaID, saID = cpiSeriesIDs[2], cpiSeriesIDs[3]
	}
	result.Series = []CPISeries{{nsaID, false}, {saID, true}}
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		period := month.Format("200601")
		item := CPIObservation{Period: period, NSA: cpiValue(values[nsaID][period]), SA: cpiValue(values[saID][period]), YoYBase: cpiValue(values[nsaID][month.AddDate(-1, 0, 0).Format("200601")]), MoMBase: cpiValue(values[saID][month.AddDate(0, -1, 0).Format("200601")])}
		item.YoY = percentChange(item.NSA, item.YoYBase)
		item.MoM = percentChange(item.SA, item.MoMBase)
		notes := []string{}
		if item.NSA == nil || item.NSA.Index == nil {
			notes = append(notes, "非季调指数缺失")
		}
		if item.SA == nil || item.SA.Index == nil {
			notes = append(notes, "季调指数缺失")
		}
		if item.NSA != nil && item.NSA.Index != nil && item.YoY == nil {
			notes = append(notes, "同比基期缺失或为零")
		}
		if item.SA != nil && item.SA.Index != nil && item.MoM == nil {
			notes = append(notes, "环比基期缺失或为零")
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

func percentChange(current, base *CPIValue) *float64 {
	if current == nil || base == nil || current.Index == nil || base.Index == nil || *base.Index == 0 {
		return nil
	}
	value := (*current.Index / *base.Index - 1) * 100
	return &value
}

func cpiValue(point *monthlyValue) *CPIValue {
	if point == nil {
		return nil
	}
	return &CPIValue{Period: point.Period, SeriesID: point.SeriesID, Index: point.Number, RawValue: point.RawValue, Footnotes: point.Footnotes, Latest: point.Latest, FetchedAt: point.FetchedAt}
}
