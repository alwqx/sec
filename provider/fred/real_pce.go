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

const realPCECSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=PCEC96,DGDSRX1,PCEDGC96,PCENDC96,PCESC96"

var realPCESeriesIDs = []string{"PCEC96", "DGDSRX1", "PCEDGC96", "PCENDC96", "PCESC96"}

var realPCEComponents = map[string]struct{ ID, Code string }{
	"total":            {"PCEC96", "DPCERX"},
	"goods":            {"DGDSRX1", "DGDSRX"},
	"durable-goods":    {"PCEDGC96", "DDURRX"},
	"nondurable-goods": {"PCENDC96", "DNDGRX"},
	"services":         {"PCESC96", "DSERRX"},
}

type RealPCERequest struct{ Component, Begin, End string }

func (r *RealPCERequest) Validate() error {
	if r == nil {
		return fmt.Errorf("实际个人消费支出查询请求不能为空")
	}
	if _, ok := realPCEComponents[r.Component]; !ok {
		return fmt.Errorf("不支持的实际消费分项: %s", r.Component)
	}
	for _, period := range []string{r.Begin, r.End} {
		if _, err := time.Parse("200601", period); err != nil || len(period) != 6 {
			return fmt.Errorf("无效月份 %q，请使用YYYYMM", period)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "200701" {
		return fmt.Errorf("实际个人消费支出月度查询支持自200701起")
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

// RealPCEValue 是十亿美元计价的链式实际金额，水平采用官方季调折年率。
type RealPCEValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawValue string   `json:"raw_value"`
}

type RealPCEObservation struct {
	RealPCEValue
	YoY     *float64     `json:"yoy_percent"`
	MoM     *float64     `json:"mom_percent"`
	YoYBase RealPCEValue `json:"yoy_base"`
	MoMBase RealPCEValue `json:"mom_base"`
	Note    string       `json:"note,omitempty"`
}

type RealPCEResult struct {
	Region             string               `json:"region"`
	Indicator          string               `json:"indicator"`
	Component          string               `json:"component"`
	SeriesID           string               `json:"series_id"`
	BEAAccountCode     string               `json:"bea_account_code"`
	Frequency          string               `json:"frequency"`
	Measure            string               `json:"measure"`
	Unit               string               `json:"unit"`
	ReferenceYear      int                  `json:"reference_year"`
	SeasonallyAdjusted bool                 `json:"seasonally_adjusted"`
	LevelAnnualized    bool                 `json:"level_annualized"`
	GrowthAnnualized   bool                 `json:"growth_annualized"`
	ComponentsAdditive bool                 `json:"components_additive"`
	Begin              string               `json:"begin"`
	End                string               `json:"end"`
	Producer           string               `json:"producer"`
	Distributor        string               `json:"distributor"`
	Source             string               `json:"source"`
	SeriesSource       string               `json:"series_source"`
	OriginalSource     string               `json:"original_source"`
	FetchedAt          time.Time            `json:"fetched_at"`
	DataVersion        string               `json:"data_version"`
	PublishedAt        *time.Time           `json:"published_at"`
	RevisionID         *string              `json:"revision_id"`
	Observations       []RealPCEObservation `json:"observations"`
	Note               string               `json:"note"`
}

// RealPCE 使用官方总量/分项水平；链式金额不可简单相加，不构造分项和或贡献度。
func (c *Client) RealPCE(ctx context.Context, req *RealPCERequest) (*RealPCEResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var values map[string]map[string]RealPCEValue
	_, fetched, err := c.csvSnapshot(ctx, c.realPCEEndpoint, "real-pce.json", func(raw string) error { var err error; values, err = parseRealPCE(raw); return err })
	if err != nil {
		return nil, err
	}
	component := realPCEComponents[req.Component]
	result := &RealPCEResult{Region: "US", Indicator: "real-pce", Component: req.Component, SeriesID: component.ID, BEAAccountCode: component.Code, Frequency: "monthly", Measure: "real-personal-consumption-expenditures", Unit: "十亿美元（2017年链式美元）", ReferenceYear: 2017, SeasonallyAdjusted: true, LevelAnnualized: true, Begin: req.Begin, End: req.End, Producer: "BEA", Distributor: "FRED", Source: realPCECSVURL, SeriesSource: "https://fred.stlouisfed.org/series/" + component.ID, OriginalSource: "https://www.bea.gov/data/consumer-spending/main", FetchedAt: fetched, DataVersion: "latest-available", Observations: []RealPCEObservation{}, Note: "实际个人消费支出，十亿美元（2017年链式美元），月度水平为季调折年率（SAAR）；不是当月消费实际发生额，也不是PCE价格指数。同比和月环比为同一季调水平序列计算值，增速未年化，不跨缺口。链式金额分项一般不具有可加性，商品和总量均直接取官方系列，不相加、不以金额比率构造贡献度。历史可修订或调整参考年；CSV不提供逐期实际公布时间、修订编号和完整历史版本，published_at/revision_id=null。"}
	value := func(period string) RealPCEValue {
		if v, ok := values[component.ID][period]; ok {
			return v
		}
		return RealPCEValue{Period: period}
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		item := RealPCEObservation{RealPCEValue: value(month.Format("200601")), YoYBase: value(month.AddDate(-1, 0, 0).Format("200601")), MoMBase: value(month.AddDate(0, -1, 0).Format("200601"))}
		item.YoY = pceChange(item.Level, item.YoYBase.Level)
		item.MoM = pceChange(item.Level, item.MoMBase.Level)
		notes := []string{}
		if item.Level == nil {
			notes = append(notes, "当月实际消费水平缺失")
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

func parseRealPCE(raw string) (map[string]map[string]RealPCEValue, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED实际消费CSV表头失败: %w", err)
	}
	if len(header) != len(realPCESeriesIDs)+1 {
		return nil, fmt.Errorf("FRED实际消费CSV表头不完整")
	}
	values := map[string]map[string]RealPCEValue{}
	for _, id := range realPCESeriesIDs {
		values[id] = map[string]RealPCEValue{}
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if _, ok := values[name]; name != "observation_date" && !ok {
			return nil, fmt.Errorf("FRED返回未知实际消费列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("FRED返回重复列: %s", name)
		}
		columns[name] = i
	}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED实际消费CSV失败: %w", err)
		}
		dateText := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", dateText)
		if err != nil || date.Day() != 1 || date.Format("2006-01-02") != dateText || dateText < "2007-01-01" {
			return nil, fmt.Errorf("FRED返回无效实际消费月度日期: %s", dateText)
		}
		period := date.Format("200601")
		if _, ok := values["PCEC96"][period]; ok {
			return nil, fmt.Errorf("FRED返回重复实际消费月份: %s", period)
		}
		for _, id := range realPCESeriesIDs {
			text := row[columns[id]]
			v := RealPCEValue{Period: period, RawValue: text}
			if trimmed := strings.TrimSpace(text); trimmed != "" && trimmed != "." {
				n, err := strconv.ParseFloat(trimmed, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
					return nil, fmt.Errorf("FRED返回无效实际消费水平: %s %s %q", id, period, text)
				}
				v.Level = &n
			}
			values[id][period] = v
		}
	}
	if len(values["PCEC96"]) == 0 {
		return nil, fmt.Errorf("FRED未返回实际消费历史数据")
	}
	return values, nil
}
