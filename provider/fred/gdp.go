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

const gdpCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=GDPC1,A191RL1Q225SBEA"

type GDPRequest struct {
	Begin, End string
	Vintages   bool
}

func gdpQuarter(period string) (int, error) {
	if len(period) != 6 || period[4] != 'Q' || period[5] < '1' || period[5] > '4' {
		return 0, fmt.Errorf("无效季度 %q，请使用YYYYQ1至YYYYQ4", period)
	}
	year, err := strconv.Atoi(period[:4])
	if err != nil || year < 1 || fmt.Sprintf("%04d", year) != period[:4] {
		return 0, fmt.Errorf("无效季度 %q，请使用YYYYQ1至YYYYQ4", period)
	}
	return year*4 + int(period[5]-'1'), nil
}

func gdpPeriod(quarter int) string { return fmt.Sprintf("%04dQ%d", quarter/4, quarter%4+1) }

func (r *GDPRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国GDP查询请求不能为空")
	}
	begin, err := gdpQuarter(r.Begin)
	if err != nil {
		return err
	}
	end, err := gdpQuarter(r.End)
	if err != nil {
		return err
	}
	if begin > end {
		return fmt.Errorf("开始季度不能晚于截至季度")
	}
	if r.Begin < "1947Q1" {
		return fmt.Errorf("美国实际GDP季度查询支持自1947Q1起")
	}
	now := time.Now().UTC()
	if end > now.Year()*4+(int(now.Month())-1)/3 {
		return fmt.Errorf("截至季度不能晚于当前UTC季度")
	}
	return nil
}

type GDPValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawLevel string   `json:"raw_level"`
}

type GDPObservation struct {
	GDPValue
	QoQAnnualized    *float64     `json:"qoq_annualized_percent"`
	RawQoQAnnualized string       `json:"raw_qoq_annualized"`
	QoQ              *float64     `json:"qoq_percent"`
	YoY              *float64     `json:"yoy_percent"`
	QoQBase          GDPValue     `json:"qoq_base"`
	YoYBase          GDPValue     `json:"yoy_base"`
	Vintages         []GDPVintage `json:"vintages"`
	Note             string       `json:"note,omitempty"`
}

type GDPResult struct {
	Region               string           `json:"region"`
	Indicator            string           `json:"indicator"`
	Frequency            string           `json:"frequency"`
	SeriesID             string           `json:"series_id"`
	GrowthSeriesID       string           `json:"growth_series_id"`
	BEAAccountCode       string           `json:"bea_account_code"`
	GrowthBEAAccountCode string           `json:"growth_bea_account_code"`
	Unit                 string           `json:"unit"`
	ReferenceYear        int              `json:"reference_year"`
	SeasonallyAdjusted   bool             `json:"seasonally_adjusted"`
	LevelAnnualized      bool             `json:"level_annualized"`
	Begin                string           `json:"begin"`
	End                  string           `json:"end"`
	Producer             string           `json:"producer"`
	Distributor          string           `json:"distributor"`
	Source               string           `json:"source"`
	SeriesSource         string           `json:"series_source"`
	GrowthSeriesSource   string           `json:"growth_series_source"`
	OriginalSource       string           `json:"original_source"`
	FetchedAt            time.Time        `json:"fetched_at"`
	DataVersion          string           `json:"data_version"`
	PublishedAt          *time.Time       `json:"published_at"`
	RevisionID           *string          `json:"revision_id"`
	VintagesRequested    bool             `json:"vintages_requested"`
	VintagesSource       string           `json:"vintages_source,omitempty"`
	VintagesFetchedAt    *time.Time       `json:"vintages_fetched_at"`
	VintagesFirstPeriod  string           `json:"vintages_first_period,omitempty"`
	VintagesLastPeriod   string           `json:"vintages_last_period,omitempty"`
	VintagesNominalUnit  string           `json:"vintages_nominal_unit,omitempty"`
	VintagesNote         string           `json:"vintages_note,omitempty"`
	Observations         []GDPObservation `json:"observations"`
	Note                 string           `json:"note"`
}

func (c *Client) GDP(ctx context.Context, req *GDPRequest) (*GDPResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	var values map[string]GDPObservation
	_, fetched, err := c.csvSnapshot(ctx, c.gdpEndpoint, "gdp.json", func(raw string) error {
		var parseErr error
		values, parseErr = parseGDP(raw)
		return parseErr
	})
	if err != nil {
		return nil, err
	}
	result := &GDPResult{Region: "US", Indicator: "gdp", Frequency: "quarterly", SeriesID: "GDPC1", GrowthSeriesID: "A191RL1Q225SBEA", BEAAccountCode: "A191RX", GrowthBEAAccountCode: "A191RL", Unit: "十亿美元（2017年链式美元）", ReferenceYear: 2017, SeasonallyAdjusted: true, LevelAnnualized: true, Begin: req.Begin, End: req.End, Producer: "BEA", Distributor: "FRED", Source: gdpCSVURL, SeriesSource: "https://fred.stlouisfed.org/series/GDPC1", GrowthSeriesSource: "https://fred.stlouisfed.org/series/A191RL1Q225SBEA", OriginalSource: "https://www.bea.gov/data/gdp/gross-domestic-product", FetchedAt: fetched, DataVersion: "latest-available", VintagesRequested: req.Vintages, Observations: []GDPObservation{}, Note: "实际GDP为2017年链式美元季调折年水平，非季度实际金额。qoq_annualized_percent直接取BEA官方季调环比年化增速；qoq_percent和yoy_percent由同一快照水平计算，均未年化，严格基期缺失或为零时为null。当前水平为抓取时可见修订版本；CSV不含逐期实际公布时间或修订编号，published_at/revision_id=null。BEA档案的名义GDP金额不能代替历史实际GDP水平；其真实发布日期不推算成具体发布时间。"}
	var vintages map[string][]GDPVintage
	if req.Vintages {
		var stamp time.Time
		vintages, stamp, err = fetchGDPVintages(ctx, c.CacheDir, c.Refresh, gdpVintagesURL)
		if err != nil {
			return nil, err
		}
		result.VintagesSource, result.VintagesFetchedAt = gdpVintagesURL, &stamp
		for period := range vintages {
			if result.VintagesFirstPeriod == "" || period < result.VintagesFirstPeriod {
				result.VintagesFirstPeriod = period
			}
			if period > result.VintagesLastPeriod {
				result.VintagesLastPeriod = period
			}
		}
		result.VintagesNominalUnit = "十亿美元（当期美元，季调折年）"
		result.VintagesNote = "BEA各次发布的实际GDP季调环比年化增速；名义GDP水平单列且使用当期美元，real_level始终为null。版本标签和发布说明保留官方原文，部分记录注明GDP未修订。档案没有具体发布时分或历史实际水平。"
	}
	value := func(period string) GDPValue {
		if item, ok := values[period]; ok {
			return item.GDPValue
		}
		return GDPValue{Period: period}
	}
	begin, _ := gdpQuarter(req.Begin)
	end, _ := gdpQuarter(req.End)
	for quarter := begin; quarter <= end; quarter++ {
		period := gdpPeriod(quarter)
		item, ok := values[period]
		if !ok {
			item.GDPValue.Period = period
		}
		item.QoQBase, item.YoYBase = value(gdpPeriod(quarter-1)), value(gdpPeriod(quarter-4))
		item.QoQ, item.YoY = pceChange(item.Level, item.QoQBase.Level), pceChange(item.Level, item.YoYBase.Level)
		var notes []string
		if item.Level == nil {
			notes = append(notes, "当季实际GDP水平缺失")
		}
		if item.QoQAnnualized == nil {
			notes = append(notes, "官方环比年化增速缺失")
		}
		if item.Level != nil && item.QoQ == nil {
			notes = append(notes, "上季基期缺失或为零")
		}
		if item.Level != nil && item.YoY == nil {
			notes = append(notes, "上年同季基期缺失或为零")
		}
		if req.Vintages {
			item.Vintages = []GDPVintage{}
			if records, ok := vintages[period]; ok {
				item.Vintages = records
			} else {
				notes = append(notes, "BEA发布版本档案未覆盖该季")
			}
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

func parseGDP(raw string) (map[string]GDPObservation, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED GDP CSV表头失败: %w", err)
	}
	if len(header) != 3 {
		return nil, fmt.Errorf("FRED GDP CSV表头不完整")
	}
	columns := map[string]int{}
	for index, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != "GDPC1" && name != "A191RL1Q225SBEA" {
			return nil, fmt.Errorf("FRED返回未知GDP列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("FRED返回重复GDP列: %s", name)
		}
		columns[name] = index
	}
	values := map[string]GDPObservation{}
	counts := map[string]int{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED GDP CSV失败: %w", err)
		}
		dateText := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", dateText)
		if err != nil || date.Format("2006-01-02") != dateText || date.Day() != 1 || (int(date.Month())-1)%3 != 0 || dateText < "1947-01-01" {
			return nil, fmt.Errorf("FRED返回无效GDP季度日期: %s", dateText)
		}
		period := fmt.Sprintf("%04dQ%d", date.Year(), (int(date.Month())-1)/3+1)
		if _, ok := values[period]; ok {
			return nil, fmt.Errorf("FRED返回重复GDP季度: %s", period)
		}
		item := GDPObservation{GDPValue: GDPValue{Period: period, RawLevel: row[columns["GDPC1"]]}, RawQoQAnnualized: row[columns["A191RL1Q225SBEA"]]}
		for _, field := range []struct {
			id, text string
			target   **float64
		}{{"GDPC1", item.RawLevel, &item.Level}, {"A191RL1Q225SBEA", item.RawQoQAnnualized, &item.QoQAnnualized}} {
			trimmed := strings.TrimSpace(field.text)
			if trimmed == "" || trimmed == "." {
				continue
			}
			number, err := strconv.ParseFloat(trimmed, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || (field.id == "GDPC1" && number < 0) {
				return nil, fmt.Errorf("FRED返回无效GDP数值: %s %s %q", period, field.id, field.text)
			}
			*field.target = &number
			counts[field.id]++
		}
		values[period] = item
	}
	if counts["GDPC1"] == 0 || counts["A191RL1Q225SBEA"] == 0 {
		return nil, fmt.Errorf("FRED未返回有效实际GDP水平或官方增速历史")
	}
	return values, nil
}
