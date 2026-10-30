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

const fiscalCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=MTSR133FMS,MTSO133FMS,MTSDS133FMS"

// FiscalRequest 使用月度所属期，不能把财政年度当作自然年度。
type FiscalRequest struct{ Begin, End string }

func (r *FiscalRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国财政查询请求不能为空")
	}
	for _, p := range []string{r.Begin, r.End} {
		d, err := time.Parse("200601", p)
		if err != nil || d.Format("200601") != p {
			return fmt.Errorf("无效财政月份 %q，请使用YYYYMM", p)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "198010" {
		return fmt.Errorf("美国月度财政查询支持自198010起")
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

type FiscalValue struct {
	Period         string            `json:"period"`
	Receipts       *float64          `json:"receipts"`
	Outlays        *float64          `json:"outlays"`
	SurplusDeficit *float64          `json:"surplus_or_deficit"`
	Deficit        *float64          `json:"deficit_positive"`
	RawValues      map[string]string `json:"raw_values"`
}

// FiscalYTD 按严格财政年度内月份求和，缺少任一期时对应累计指标为空。
type FiscalYTD struct {
	Begin          string   `json:"begin"`
	End            string   `json:"end"`
	Receipts       *float64 `json:"receipts"`
	Outlays        *float64 `json:"outlays"`
	SurplusDeficit *float64 `json:"surplus_or_deficit"`
	Deficit        *float64 `json:"deficit_positive"`
	Method         string   `json:"method"`
}

type FiscalObservation struct {
	FiscalValue
	FiscalYear  int       `json:"fiscal_year"`
	FiscalMonth int       `json:"fiscal_month"`
	FYTD        FiscalYTD `json:"fiscal_year_to_date"`
	Note        string    `json:"note"`
}

type FiscalSeries struct {
	SeriesID string `json:"series_id"`
	Name     string `json:"name"`
	Source   string `json:"source"`
}

type FiscalResult struct {
	Region             string              `json:"region"`
	Frequency          string              `json:"frequency"`
	Measurement        string              `json:"measurement"`
	PriceBasis         string              `json:"price_basis"`
	Unit               string              `json:"unit"`
	SeasonallyAdjusted bool                `json:"seasonally_adjusted"`
	Annualized         bool                `json:"annualized"`
	Begin              string              `json:"begin"`
	End                string              `json:"end"`
	FirstPeriod        string              `json:"first_period"`
	LastPeriod         string              `json:"last_period"`
	Producer           string              `json:"producer"`
	Distributor        string              `json:"distributor"`
	Source             string              `json:"source"`
	OriginalSource     string              `json:"original_source"`
	Series             []FiscalSeries      `json:"series"`
	FetchedAt          time.Time           `json:"fetched_at"`
	DataVersion        string              `json:"data_version"`
	PublishedAt        *time.Time          `json:"published_at"`
	RevisionID         *string             `json:"revision_id"`
	MethodologyNotes   []string            `json:"methodology_notes"`
	Observations       []FiscalObservation `json:"observations"`
}

// Fiscal 保留三个官方独立系列；赤字正值仅对官方盈余正/赤字负系列取反。
func (c *Client) Fiscal(ctx context.Context, req *FiscalRequest) (*FiscalResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var rows map[string]FiscalValue
	_, fetched, err := c.csvSnapshot(ctx, fiscalCSVURL, "us-fiscal.json", func(raw string) error { var err error; rows, err = parseFiscal(raw); return err })
	if err != nil {
		return nil, err
	}
	result := &FiscalResult{Region: "US", Frequency: "monthly", Measurement: "federal-unified-budget-monthly-flow", PriceBasis: "current-dollars", Unit: "百万美元", Begin: req.Begin, End: req.End, Producer: "美国财政部财政服务局", Distributor: "FRED", Source: fiscalCSVURL, OriginalSource: "https://fiscaldata.treasury.gov/datasets/monthly-treasury-statement/", FetchedAt: fetched, DataVersion: "current_visible_revision", Observations: []FiscalObservation{}, MethodologyNotes: []string{
		"金额是所属自然月的联邦财政收支流量，现价、未季调、未折年；不是财年累计、国债余额、国债发行或国民账户政府消费投资。",
		"MTSDS133FMS为官方盈余正、赤字负；deficit_positive取其相反数，正值为赤字、负值为盈余，不以收入减支出替代官方余额。独立系列可能有舍入及修订差异。",
		"美国联邦财政年度为上年10月至当年9月，10月重新累计。JSON财年累计按当前快照内严格月份求和，各指标缺少任一期就为空；该计算不是另行采集的官方累计值。",
		"税收缴纳时点、支付日历和信贷项目重估等会影响月度波动，净收入或支出不强制为正；未推算环比年化或实际增长。",
		"当前可见修订历史；CSV未提供逐期实际公布时间及修订编号，相关字段为null，抓取时间不能代替公布时间。",
	}}
	for _, s := range []FiscalSeries{{"MTSR133FMS", "联邦收入", ""}, {"MTSO133FMS", "联邦支出", ""}, {"MTSDS133FMS", "联邦盈余或赤字（赤字负）", ""}} {
		s.Source = "https://fred.stlouisfed.org/series/" + s.SeriesID
		result.Series = append(result.Series, s)
	}
	for p, v := range rows {
		if v.Receipts == nil && v.Outlays == nil && v.SurplusDeficit == nil {
			continue
		}
		if result.FirstPeriod == "" || p < result.FirstPeriod {
			result.FirstPeriod = p
		}
		if p > result.LastPeriod {
			result.LastPeriod = p
		}
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for d := begin; !d.After(end); d = d.AddDate(0, 1, 0) {
		period := d.Format("200601")
		value, ok := rows[period]
		if !ok {
			value = FiscalValue{Period: period, RawValues: map[string]string{}}
		}
		year := d.Year()
		if d.Month() >= time.October {
			year++
		}
		start := time.Date(year-1, time.October, 1, 0, 0, 0, 0, time.UTC)
		ytd, err := fiscalYTD(rows, start, d)
		if err != nil {
			return nil, err
		}
		item := FiscalObservation{FiscalValue: value, FiscalYear: year, FiscalMonth: (int(d.Month())+2)%12 + 1, FYTD: ytd}
		if value.Receipts == nil || value.Outlays == nil || value.SurplusDeficit == nil {
			item.Note = "当前快照未提供全部月度数值"
		}
		if ytd.Receipts == nil || ytd.Outlays == nil || ytd.SurplusDeficit == nil {
			if item.Note != "" {
				item.Note += "；"
			}
			item.Note += "财年累计严格月份存在缺口"
		}
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

func fiscalYTD(rows map[string]FiscalValue, start, end time.Time) (FiscalYTD, error) {
	result := FiscalYTD{Begin: start.Format("200601"), End: end.Format("200601"), Method: "sum_of_current_snapshot_months"}
	sum := func(field func(FiscalValue) *float64) (*float64, error) {
		total := 0.0
		for d := start; !d.After(end); d = d.AddDate(0, 1, 0) {
			n := field(rows[d.Format("200601")])
			if n == nil {
				return nil, nil
			}
			total += *n
			if math.IsInf(total, 0) || math.IsNaN(total) {
				return nil, fmt.Errorf("财政累计值超出有限数值范围")
			}
		}
		return &total, nil
	}
	var err error
	result.Receipts, err = sum(func(v FiscalValue) *float64 { return v.Receipts })
	if err != nil {
		return result, err
	}
	result.Outlays, err = sum(func(v FiscalValue) *float64 { return v.Outlays })
	if err != nil {
		return result, err
	}
	result.SurplusDeficit, err = sum(func(v FiscalValue) *float64 { return v.SurplusDeficit })
	if err != nil {
		return result, err
	}
	if result.SurplusDeficit != nil {
		n := -*result.SurplusDeficit
		result.Deficit = &n
	}
	return result, nil
}

func parseFiscal(raw string) (map[string]FiscalValue, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取财政CSV表头失败: %w", err)
	}
	if len(header) != 4 {
		return nil, fmt.Errorf("财政CSV列数不符")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != "MTSR133FMS" && name != "MTSO133FMS" && name != "MTSDS133FMS" {
			return nil, fmt.Errorf("财政CSV未知列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("财政CSV重复列: %s", name)
		}
		columns[name] = i
	}
	rows := map[string]FiscalValue{}
	counts := map[string]int{}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取财政CSV失败: %w", err)
		}
		text := record[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", text)
		if err != nil || date.Format("2006-01-02") != text || date.Day() != 1 {
			return nil, fmt.Errorf("无效财政日期: %s", text)
		}
		period := date.Format("200601")
		if period < "198010" {
			return nil, fmt.Errorf("财政CSV早于已核验范围: %s", period)
		}
		if _, ok := rows[period]; ok {
			return nil, fmt.Errorf("财政CSV重复月份: %s", period)
		}
		value := FiscalValue{Period: period, RawValues: map[string]string{}}
		for _, s := range []struct {
			id  string
			dst **float64
		}{{"MTSR133FMS", &value.Receipts}, {"MTSO133FMS", &value.Outlays}, {"MTSDS133FMS", &value.SurplusDeficit}} {
			original := record[columns[s.id]]
			value.RawValues[s.id] = original
			text := strings.TrimSpace(original)
			if text == "" || text == "." {
				continue
			}
			n, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
				return nil, fmt.Errorf("无效财政数值: %s %s %q", period, s.id, original)
			}
			*s.dst = &n
			counts[s.id]++
		}
		if value.SurplusDeficit != nil {
			n := -*value.SurplusDeficit
			value.Deficit = &n
		}
		rows[period] = value
	}
	for _, id := range []string{"MTSR133FMS", "MTSO133FMS", "MTSDS133FMS"} {
		if counts[id] == 0 {
			return nil, fmt.Errorf("财政CSV缺少有效观测: %s", id)
		}
	}
	// 在写缓存前验证整份快照中所有可计算累计值，防止衍生溢出污染缓存。
	for period := range rows {
		d, _ := time.Parse("200601", period)
		year := d.Year()
		if d.Month() >= time.October {
			year++
		}
		if _, err := fiscalYTD(rows, time.Date(year-1, time.October, 1, 0, 0, 0, 0, time.UTC), d); err != nil {
			return nil, fmt.Errorf("%s: %w", period, err)
		}
	}
	return rows, nil
}
