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

const productionCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=INDPRO,IPMAN,TCU,MCUMFN"

type productionDefinition struct{ ID, Start, Coverage string }

var productionSeries = map[string]map[string]productionDefinition{
	"industrial-production": {
		"total":         {"INDPRO", "191901", "工业总量：美联储口径制造业（含伐木及部分出版业）、采矿业、电力和燃气公用事业"},
		"manufacturing": {"IPMAN", "197201", "制造业：NAICS 31–33，不包含伐木和出版业"},
	},
	"capacity-utilization": {
		"total":         {"TCU", "196701", "工业总量：美联储口径制造业（含伐木及部分出版业）、采矿业、电力和燃气公用事业"},
		"manufacturing": {"MCUMFN", "197201", "制造业：NAICS 31–33，不包含伐木和出版业"},
	},
}

var productionSeriesIDs = []string{"INDPRO", "IPMAN", "TCU", "MCUMFN"}

type ProductionRequest struct{ Indicator, Sector, Begin, End string }

// EarliestMonth 返回选中系列的起点，防止把四个系列的历史覆盖范围混用。
func (r *ProductionRequest) EarliestMonth() (string, error) {
	if r == nil {
		return "", fmt.Errorf("工业生产查询请求不能为空")
	}
	sectors, ok := productionSeries[r.Indicator]
	if !ok {
		return "", fmt.Errorf("不支持的工业指标: %s", r.Indicator)
	}
	definition, ok := sectors[r.Sector]
	if !ok {
		return "", fmt.Errorf("不支持的工业部门: %s，请使用total或manufacturing", r.Sector)
	}
	return definition.Start, nil
}

func (r *ProductionRequest) Validate() error {
	start, err := r.EarliestMonth()
	if err != nil {
		return err
	}
	for _, period := range []string{r.Begin, r.End} {
		if _, err := time.Parse("200601", period); err != nil || len(period) != 6 {
			return fmt.Errorf("无效月份 %q，请使用YYYYMM", period)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < start {
		return fmt.Errorf("所选工业系列支持自%s起", start)
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

type ProductionValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawValue string   `json:"raw_value"`
}

type ProductionObservation struct {
	ProductionValue
	YoY                 *float64        `json:"yoy_percent"`
	MoM                 *float64        `json:"mom_percent"`
	YoYPercentagePoints *float64        `json:"yoy_percentage_points"`
	MoMPercentagePoints *float64        `json:"mom_percentage_points"`
	YoYBase             ProductionValue `json:"yoy_base"`
	MoMBase             ProductionValue `json:"mom_base"`
	Note                string          `json:"note,omitempty"`
}

type ProductionResult struct {
	Region             string                  `json:"region"`
	Indicator          string                  `json:"indicator"`
	Sector             string                  `json:"sector"`
	SeriesID           string                  `json:"series_id"`
	Frequency          string                  `json:"frequency"`
	Coverage           string                  `json:"coverage"`
	Unit               string                  `json:"unit"`
	ChangeUnit         string                  `json:"change_unit"`
	ReferenceYear      *int                    `json:"reference_year"`
	SeasonallyAdjusted bool                    `json:"seasonally_adjusted"`
	GrowthAnnualized   bool                    `json:"growth_annualized"`
	Begin              string                  `json:"begin"`
	End                string                  `json:"end"`
	FirstPeriod        string                  `json:"first_period"`
	Producer           string                  `json:"producer"`
	Distributor        string                  `json:"distributor"`
	Source             string                  `json:"source"`
	SeriesSource       string                  `json:"series_source"`
	OriginalSource     string                  `json:"original_source"`
	FetchedAt          time.Time               `json:"fetched_at"`
	DataVersion        string                  `json:"data_version"`
	PublishedAt        *time.Time              `json:"published_at"`
	RevisionID         *string                 `json:"revision_id"`
	Observations       []ProductionObservation `json:"observations"`
	Note               string                  `json:"note"`
}

// Production 直接选择官方系列，不聚合指数或利用率；两个指标共享完整快照。
func (c *Client) Production(ctx context.Context, req *ProductionRequest) (*ProductionResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var values map[string]map[string]ProductionValue
	_, fetched, err := c.csvSnapshot(ctx, c.productionEndpoint, "production.json", func(raw string) error {
		var parseErr error
		values, parseErr = parseProduction(raw)
		return parseErr
	})
	if err != nil {
		return nil, err
	}
	definition := productionSeries[req.Indicator][req.Sector]
	result := &ProductionResult{Region: "US", Indicator: req.Indicator, Sector: req.Sector, SeriesID: definition.ID, Frequency: "monthly", Coverage: definition.Coverage, SeasonallyAdjusted: true, Begin: req.Begin, End: req.End, FirstPeriod: definition.Start, Producer: "Board of Governors of the Federal Reserve System (US)", Distributor: "FRED", Source: productionCSVURL, SeriesSource: "https://fred.stlouisfed.org/series/" + definition.ID, OriginalSource: "https://www.federalreserve.gov/releases/g17/", FetchedAt: fetched, DataVersion: "latest-available", Observations: []ProductionObservation{}, Note: "美国联邦储备委员会G.17月度季调系列。总量与制造业NAICS口径分别直接取官方系列，指数和利用率不相加。严格使用上月和上年同月基期，不跨缺口。历史数据可能随月度和年度修订、季调或参考年更新而改变；CSV不提供逐期实际公布时间、修订编号或完整历史版本，published_at/revision_id=null；fetched_at仅为采集时间。"}
	if req.Indicator == "industrial-production" {
		year := 2017
		result.ReferenceYear = &year
		result.Unit, result.ChangeUnit = "指数（2017=100）", "percent"
		result.Note += "生产指数衡量实际产出，水平非金额；同比和月环比是计算增速，未年化。"
	} else {
		result.Unit, result.ChangeUnit = "百分比", "percentage-points"
		result.Note += "产能利用率为季调产出指数占相关产能指数的百分比；变化为较上月/上年同月的百分点差，不是相对增速，不设指数参考年。"
	}
	value := func(period string) ProductionValue {
		if v, ok := values[definition.ID][period]; ok {
			return v
		}
		return ProductionValue{Period: period}
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		item := ProductionObservation{ProductionValue: value(month.Format("200601")), YoYBase: value(month.AddDate(-1, 0, 0).Format("200601")), MoMBase: value(month.AddDate(0, -1, 0).Format("200601"))}
		if req.Indicator == "industrial-production" {
			item.YoY, item.MoM = pceChange(item.Level, item.YoYBase.Level), pceChange(item.Level, item.MoMBase.Level)
		} else {
			item.YoYPercentagePoints, item.MoMPercentagePoints = productionDifference(item.Level, item.YoYBase.Level), productionDifference(item.Level, item.MoMBase.Level)
		}
		notes := []string{}
		if item.Level == nil {
			notes = append(notes, "当月数据缺失")
		} else {
			for _, base := range []struct {
				label string
				value *float64
			}{{"上年同月", item.YoYBase.Level}, {"上月", item.MoMBase.Level}} {
				if base.value == nil {
					notes = append(notes, base.label+"基期缺失")
				} else if req.Indicator == "industrial-production" && *base.value == 0 {
					notes = append(notes, base.label+"基期为零，无法计算增速")
				}
			}
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

func productionDifference(current, base *float64) *float64 {
	if current == nil || base == nil {
		return nil
	}
	difference := *current - *base
	return &difference
}

func parseProduction(raw string) (map[string]map[string]ProductionValue, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED工业CSV表头失败: %w", err)
	}
	if len(header) != len(productionSeriesIDs)+1 {
		return nil, fmt.Errorf("FRED工业CSV表头不完整")
	}
	values := map[string]map[string]ProductionValue{}
	starts := map[string]string{}
	for _, sectors := range productionSeries {
		for _, definition := range sectors {
			values[definition.ID] = map[string]ProductionValue{}
			starts[definition.ID] = definition.Start
		}
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if _, ok := values[name]; name != "observation_date" && !ok {
			return nil, fmt.Errorf("FRED返回未知工业列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("FRED返回重复工业列: %s", name)
		}
		columns[name] = i
	}
	counts := map[string]int{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED工业CSV失败: %w", err)
		}
		dateText := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", dateText)
		if err != nil || date.Day() != 1 || date.Format("2006-01-02") != dateText || dateText < "1919-01-01" {
			return nil, fmt.Errorf("FRED返回无效工业月度日期: %s", dateText)
		}
		period := date.Format("200601")
		if _, ok := values["INDPRO"][period]; ok {
			return nil, fmt.Errorf("FRED返回重复工业月份: %s", period)
		}
		for _, id := range productionSeriesIDs {
			text := row[columns[id]]
			v := ProductionValue{Period: period, RawValue: text}
			if trimmed := strings.TrimSpace(text); trimmed != "" && trimmed != "." {
				n, err := strconv.ParseFloat(trimmed, 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || period < starts[id] {
					return nil, fmt.Errorf("FRED返回无效工业水平: %s %s %q", id, period, text)
				}
				v.Level = &n
				counts[id]++
			}
			values[id][period] = v
		}
	}
	for _, id := range productionSeriesIDs {
		if counts[id] == 0 {
			return nil, fmt.Errorf("FRED未返回%s有效工业历史数据", id)
		}
	}
	return values, nil
}
