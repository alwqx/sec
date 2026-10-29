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

const moneyCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=M2SL"

type MoneyRequest struct{ Begin, End string }

func (r *MoneyRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("M2查询请求不能为空")
	}
	for _, period := range []string{r.Begin, r.End} {
		if _, err := time.Parse("200601", period); err != nil || len(period) != 6 {
			return fmt.Errorf("无效月份 %q，请使用YYYYMM", period)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "195901" {
		return fmt.Errorf("M2月度查询支持自195901起")
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

type MoneyValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawValue string   `json:"raw_value"`
}

type MoneyObservation struct {
	MoneyValue
	YoY     *float64   `json:"yoy_percent"`
	MoM     *float64   `json:"mom_percent"`
	YoYBase MoneyValue `json:"yoy_base"`
	MoMBase MoneyValue `json:"mom_base"`
	Note    string     `json:"note,omitempty"`
}

type MoneyResult struct {
	Region             string             `json:"region"`
	Indicator          string             `json:"indicator"`
	SeriesID           string             `json:"series_id"`
	Frequency          string             `json:"frequency"`
	Measure            string             `json:"measure"`
	PeriodBasis        string             `json:"period_basis"`
	Unit               string             `json:"unit"`
	SeasonallyAdjusted bool               `json:"seasonally_adjusted"`
	LevelAnnualized    bool               `json:"level_annualized"`
	GrowthAnnualized   bool               `json:"growth_annualized"`
	Begin              string             `json:"begin"`
	End                string             `json:"end"`
	Producer           string             `json:"producer"`
	Distributor        string             `json:"distributor"`
	Source             string             `json:"source"`
	SeriesSource       string             `json:"series_source"`
	OriginalSource     string             `json:"original_source"`
	MethodologySource  string             `json:"methodology_source"`
	MethodologyNotes   []string           `json:"methodology_notes"`
	FetchedAt          time.Time          `json:"fetched_at"`
	DataVersion        string             `json:"data_version"`
	PublishedAt        *time.Time         `json:"published_at"`
	RevisionID         *string            `json:"revision_id"`
	Observations       []MoneyObservation `json:"observations"`
	Note               string             `json:"note"`
}

func (c *Client) Money(ctx context.Context, req *MoneyRequest) (*MoneyResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var values map[string]MoneyValue
	_, fetched, err := c.csvSnapshot(ctx, c.moneyEndpoint, "money.json", func(raw string) error {
		var parseErr error
		values, parseErr = parseMoney(raw)
		return parseErr
	})
	if err != nil {
		return nil, err
	}
	result := &MoneyResult{
		Region: "US", Indicator: "money", SeriesID: "M2SL", Frequency: "monthly", Measure: "nominal-money-stock",
		PeriodBasis: "monthly-average", Unit: "十亿美元", SeasonallyAdjusted: true,
		Begin: req.Begin, End: req.End, Producer: "Board of Governors of the Federal Reserve System (US)", Distributor: "FRED",
		Source: moneyCSVURL, SeriesSource: "https://fred.stlouisfed.org/series/M2SL", OriginalSource: "https://www.federalreserve.gov/releases/h6/",
		MethodologySource: "https://www.federalreserve.gov/releases/h6/h6_technical_qa.htm",
		MethodologyNotes: []string{
			"自2020年5月起储蓄存款纳入M1；M2已包含这些存款，此次重分类不构成M2总量断点。",
			"2026-07-28起IRA和Keogh账户余额改在M2总量层面扣除，并追溯调整历史；非季调M2总量不变，季调M2略有修订。当前快照已采用官方重算历史，不拼接旧版本。",
		},
		FetchedAt: fetched, DataVersion: "latest-available", Observations: []MoneyObservation{},
		Note: "M2SL为美联储H.6月度季调名义货币余额，单位为十亿美元，所属月份为月平均水平，不是月末余额、当月新增、实际购买力或季调折年率。同比和月环比由同一当前快照的严格日历基期计算，增速未年化，缺失不补。CSV不提供逐期实际公布时间、修订编号或完整历史版本，published_at/revision_id=null；fetched_at仅为抓取时间。",
	}
	value := func(period string) MoneyValue {
		if observation, ok := values[period]; ok {
			return observation
		}
		return MoneyValue{Period: period}
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for month := begin; !month.After(end); month = month.AddDate(0, 1, 0) {
		item := MoneyObservation{MoneyValue: value(month.Format("200601")), YoYBase: value(month.AddDate(-1, 0, 0).Format("200601")), MoMBase: value(month.AddDate(0, -1, 0).Format("200601"))}
		item.YoY, item.MoM = pceChange(item.Level, item.YoYBase.Level), pceChange(item.Level, item.MoMBase.Level)
		var notes []string
		if item.Level == nil {
			notes = append(notes, "当月M2余额缺失")
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

func parseMoney(raw string) (map[string]MoneyValue, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED M2 CSV表头失败: %w", err)
	}
	if len(header) != 2 {
		return nil, fmt.Errorf("FRED M2 CSV表头不完整")
	}
	columns := map[string]int{}
	for index, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != "M2SL" {
			return nil, fmt.Errorf("FRED返回未知M2列: %s", name)
		}
		if _, exists := columns[name]; exists {
			return nil, fmt.Errorf("FRED返回重复M2列: %s", name)
		}
		columns[name] = index
	}
	values := map[string]MoneyValue{}
	validCount := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED M2 CSV失败: %w", err)
		}
		dateText := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", dateText)
		if err != nil || date.Day() != 1 || date.Format("2006-01-02") != dateText || dateText < "1959-01-01" {
			return nil, fmt.Errorf("FRED返回无效M2月度日期: %s", dateText)
		}
		period := date.Format("200601")
		if _, exists := values[period]; exists {
			return nil, fmt.Errorf("FRED返回重复M2月份: %s", period)
		}
		text := row[columns["M2SL"]]
		observation := MoneyValue{Period: period, RawValue: text}
		if trimmed := strings.TrimSpace(text); trimmed != "" && trimmed != "." {
			level, err := strconv.ParseFloat(trimmed, 64)
			if err != nil || math.IsNaN(level) || math.IsInf(level, 0) || level < 0 {
				return nil, fmt.Errorf("FRED返回无效M2余额: %s %q", period, text)
			}
			observation.Level = &level
			validCount++
		}
		values[period] = observation
	}
	if validCount == 0 {
		return nil, fmt.Errorf("FRED未返回有效M2历史数据")
	}
	return values, nil
}
