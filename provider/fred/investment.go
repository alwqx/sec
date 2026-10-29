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

const housingInvestmentCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=HOUST,PERMIT"
const equipmentInvestmentCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=Y033RC1Q027SBEA,Y033RX1Q020SBEA"

// HousingInvestmentRequest 查询月度住宅建设套数，Component为空时查询住宅开工。
type HousingInvestmentRequest struct{ Begin, End, Component string }

// EquipmentInvestmentRequest 查询季度私人非住宅设备投资，PriceBasis为空时查询实际水平。
type EquipmentInvestmentRequest struct{ Begin, End, PriceBasis string }

func investmentHousingSpec(component string) (string, string, error) {
	switch component {
	case "", "housing-starts":
		return "HOUST", "195901", nil
	case "building-permits":
		return "PERMIT", "196001", nil
	default:
		return "", "", fmt.Errorf("住宅建设分类必须为housing-starts（住宅开工）或building-permits（建筑许可住房套数）")
	}
}

func investmentEquipmentSpec(basis string) (string, string, error) {
	switch basis {
	case "", "real":
		return "Y033RX1Q020SBEA", "2007Q1", nil
	case "nominal":
		return "Y033RC1Q027SBEA", "1947Q1", nil
	default:
		return "", "", fmt.Errorf("设备投资价格口径必须为real（实际）或nominal（名义）")
	}
}

func (r *HousingInvestmentRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国住宅建设查询请求不能为空")
	}
	_, earliest, err := investmentHousingSpec(r.Component)
	if err != nil {
		return err
	}
	begin, err := time.Parse("200601", r.Begin)
	if err != nil || begin.Format("200601") != r.Begin {
		return fmt.Errorf("无效开始月份 %q，请使用YYYYMM", r.Begin)
	}
	end, err := time.Parse("200601", r.End)
	if err != nil || end.Format("200601") != r.End {
		return fmt.Errorf("无效截至月份 %q，请使用YYYYMM", r.End)
	}
	if begin.After(end) {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < earliest {
		return fmt.Errorf("该住宅建设序列支持自%s起查询", earliest)
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

func (r *EquipmentInvestmentRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国设备投资查询请求不能为空")
	}
	_, earliest, err := investmentEquipmentSpec(r.PriceBasis)
	if err != nil {
		return err
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
	if r.Begin < earliest {
		return fmt.Errorf("该设备投资价格口径支持自%s起查询", earliest)
	}
	now := time.Now().UTC()
	if end > now.Year()*4+(int(now.Month())-1)/3 {
		return fmt.Errorf("截至季度不能晚于当前UTC季度")
	}
	return nil
}

type InvestmentValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawLevel string   `json:"raw_level"`
}

type InvestmentObservation struct {
	InvestmentValue
	YoY            *float64        `json:"yoy_percent"`
	PreviousGrowth *float64        `json:"previous_period_percent"`
	PreviousBase   InvestmentValue `json:"previous_period_base"`
	YoYBase        InvestmentValue `json:"yoy_base"`
	Note           string          `json:"note,omitempty"`
}

type InvestmentResult struct {
	Region             string                  `json:"region"`
	Indicator          string                  `json:"indicator"`
	Component          string                  `json:"component"`
	SeriesID           string                  `json:"series_id"`
	BEAAccountCode     string                  `json:"bea_account_code,omitempty"`
	Frequency          string                  `json:"frequency"`
	Measure            string                  `json:"measure"`
	PriceBasis         string                  `json:"price_basis"`
	Unit               string                  `json:"unit"`
	ReferenceYear      *int                    `json:"reference_year"`
	SeasonallyAdjusted bool                    `json:"seasonally_adjusted"`
	LevelAnnualized    bool                    `json:"level_annualized"`
	GrowthAnnualized   bool                    `json:"growth_annualized"`
	FirstPeriod        string                  `json:"first_period"`
	LastPeriod         string                  `json:"last_period"`
	Begin              string                  `json:"begin"`
	End                string                  `json:"end"`
	Producer           string                  `json:"producer"`
	Distributor        string                  `json:"distributor"`
	Source             string                  `json:"source"`
	SeriesSource       string                  `json:"series_source"`
	OriginalSource     string                  `json:"original_source"`
	FetchedAt          time.Time               `json:"fetched_at"`
	DataVersion        string                  `json:"data_version"`
	PublishedAt        *time.Time              `json:"published_at"`
	RevisionID         *string                 `json:"revision_id"`
	MethodologyNotes   []string                `json:"methodology_notes"`
	Observations       []InvestmentObservation `json:"observations"`
	Note               string                  `json:"note"`
}

func (c *Client) HousingInvestment(ctx context.Context, req *HousingInvestmentRequest) (*InvestmentResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	id, _, _ := investmentHousingSpec(req.Component)
	return c.investment(ctx, req.Begin, req.End, id, false)
}

func (c *Client) EquipmentInvestment(ctx context.Context, req *EquipmentInvestmentRequest) (*InvestmentResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	id, _, _ := investmentEquipmentSpec(req.PriceBasis)
	return c.investment(ctx, req.Begin, req.End, id, true)
}

func (c *Client) investment(ctx context.Context, begin, end, id string, quarterly bool) (*InvestmentResult, error) {
	endpoint, source, filename := c.housingInvestmentEndpoint, housingInvestmentCSVURL, "us-investment-housing.json"
	if quarterly {
		endpoint, source, filename = c.equipmentInvestmentEndpoint, equipmentInvestmentCSVURL, "us-investment-equipment.json"
	}
	var rows map[string]map[string]InvestmentValue
	_, fetched, err := c.csvSnapshot(ctx, endpoint, filename, func(raw string) error {
		var parseErr error
		rows, parseErr = investmentParse(raw, quarterly)
		return parseErr
	})
	if err != nil {
		return nil, err
	}
	result := &InvestmentResult{Region: "US", Indicator: "investment", Component: "housing-starts", SeriesID: id, Frequency: "monthly", Measure: "housing_units", PriceBasis: "not_applicable", Unit: "千套（季调折年）", SeasonallyAdjusted: true, LevelAnnualized: true, Begin: begin, End: end, Producer: "U.S. Census Bureau; U.S. Department of Housing and Urban Development", Distributor: "FRED", Source: source, SeriesSource: "https://fred.stlouisfed.org/series/" + id, OriginalSource: "https://www.census.gov/construction/nrc/index.html", FetchedAt: fetched, DataVersion: "current_visible_revision", MethodologyNotes: []string{}, Observations: []InvestmentObservation{}, Note: "水平是季调折年套数，并非当月实际套数；同比与月环比按当前可见修订水平计算，增速未年化。住房套数不是建筑物数量或投资金额。严格上一月及上年同月，缺失不跨越、零基数不计算百分比。缺失可能来自尚未发布或源数据缺口，不将空值等同于未发布。未提供逐期实际公布时间与完整历史版本。"}
	if id == "HOUST" {
		result.MethodologyNotes = append(result.MethodologyNotes, "开工以基础或地基开始开挖为准，多户建筑开挖时计入全部住房单元；自1992-09起包括在原有地基上完全重建的住房。")
	}
	if id == "PERMIT" {
		result.Component = "building-permits"
		result.MethodologyNotes = append(result.MethodologyNotes, "建筑许可统计签发许可地区获准新建的私人住房单元，不是许可证张数，不覆盖无需建筑许可的地区；获批不代表已开工。", "许可调查总体自2023-01起改为每年更新辖区范围；此前也存在总体更新。2025-01所属期起还调整缺报插补方法，历史变化应结合官方方法说明解读。")
	}
	if quarterly {
		result.Indicator, result.Component, result.Frequency, result.Measure, result.Producer = "equipment_investment", "equipment", "quarterly", "investment_flow", "BEA"
		result.PriceBasis, result.Unit, result.BEAAccountCode = "nominal", "十亿美元（当期美元，季调折年）", "Y033RC"
		result.OriginalSource = "https://www.bea.gov/resources/methodologies/nipa-handbook/pdf/chapter-06.pdf"
		if id == "Y033RX1Q020SBEA" {
			year := 2017
			result.PriceBasis, result.Unit, result.BEAAccountCode, result.ReferenceYear = "real", "十亿美元（2017年链式美元，季调折年）", "Y033RX", &year
		}
		result.MethodologyNotes = []string{"设备投资指NIPA私人非住宅固定投资中的设备，不包括建筑、知识产权产品或家庭耐用品消费；不是设备订单、资本品出货或资本存量。", "实际链式美元水平不可与其他分项直接相加；实际序列从2007Q1起，名义序列从1947Q1起，不通过其他口径拼接补齐。"}
		result.Note = "水平为季度季调折年投资流量，并非当季实际金额；同比与季环比按当前可见修订水平计算，增速未年化。严格上一季及上年同季，缺失不跨越、零基数不计算百分比。缺失可能来自尚未发布或源数据缺口，不将空值等同于未发布。未提供逐期实际公布时间与完整历史版本；与月度住宅建设的频率和单位不同，不拼接计算。"
	}
	for period, row := range rows {
		if row[id].Level != nil {
			if result.FirstPeriod == "" || period < result.FirstPeriod {
				result.FirstPeriod = period
			}
			if period > result.LastPeriod {
				result.LastPeriod = period
			}
		}
	}
	value := func(period string) InvestmentValue {
		if item, ok := rows[period][id]; ok {
			return item
		}
		return InvestmentValue{Period: period}
	}
	appendPeriod := func(period, previous, yearAgo string) error {
		item := InvestmentObservation{InvestmentValue: value(period), PreviousBase: value(previous), YoYBase: value(yearAgo)}
		var err error
		item.PreviousGrowth, err = investmentChange(item.Level, item.PreviousBase.Level)
		if err != nil {
			return err
		}
		item.YoY, err = investmentChange(item.Level, item.YoYBase.Level)
		if err != nil {
			return err
		}
		var notes []string
		if item.Level == nil {
			notes = append(notes, "当期水平缺失")
		}
		if item.Level != nil && item.PreviousGrowth == nil {
			notes = append(notes, "上期基数缺失或为零")
		}
		if item.Level != nil && item.YoY == nil {
			notes = append(notes, "上年同期基数缺失或为零")
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
		return nil
	}
	if quarterly {
		start, _ := gdpQuarter(begin)
		finish, _ := gdpQuarter(end)
		for q := start; q <= finish; q++ {
			if err := appendPeriod(gdpPeriod(q), gdpPeriod(q-1), gdpPeriod(q-4)); err != nil {
				return nil, err
			}
		}
	} else {
		start, _ := time.Parse("200601", begin)
		finish, _ := time.Parse("200601", end)
		for date := start; !date.After(finish); date = date.AddDate(0, 1, 0) {
			if err := appendPeriod(date.Format("200601"), date.AddDate(0, -1, 0).Format("200601"), date.AddDate(-1, 0, 0).Format("200601")); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func investmentParse(raw string, quarterly bool) (map[string]map[string]InvestmentValue, error) {
	ids, earliest := []string{"HOUST", "PERMIT"}, "1959-01-01"
	if quarterly {
		ids, earliest = []string{"Y033RC1Q027SBEA", "Y033RX1Q020SBEA"}, "1947-01-01"
	}
	wanted := map[string]bool{"observation_date": true}
	for _, id := range ids {
		wanted[id] = true
	}
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED投资CSV表头失败: %w", err)
	}
	if len(header) != len(wanted) {
		return nil, fmt.Errorf("FRED投资CSV表头不完整")
	}
	columns := map[string]int{}
	for index, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if !wanted[name] {
			return nil, fmt.Errorf("FRED返回未知投资列: %s", name)
		}
		if _, exists := columns[name]; exists {
			return nil, fmt.Errorf("FRED返回重复投资列: %s", name)
		}
		columns[name] = index
	}
	rows, counts := map[string]map[string]InvestmentValue{}, map[string]int{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED投资CSV失败: %w", err)
		}
		text := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", text)
		if err != nil || date.Format("2006-01-02") != text || date.Day() != 1 || text < earliest || quarterly && (int(date.Month())-1)%3 != 0 {
			return nil, fmt.Errorf("FRED返回无效投资日期: %s", text)
		}
		period := date.Format("200601")
		if quarterly {
			period = fmt.Sprintf("%04dQ%d", date.Year(), (int(date.Month())-1)/3+1)
		}
		if _, exists := rows[period]; exists {
			return nil, fmt.Errorf("FRED返回重复投资期间: %s", period)
		}
		values := map[string]InvestmentValue{}
		for _, id := range ids {
			value := InvestmentValue{Period: period, RawLevel: row[columns[id]]}
			text := strings.TrimSpace(value.RawLevel)
			if text != "" && text != "." {
				number, err := strconv.ParseFloat(text, 64)
				if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
					return nil, fmt.Errorf("FRED返回无效投资数值: %s %s %q", period, id, value.RawLevel)
				}
				value.Level = &number
				counts[id]++
			}
			values[id] = value
		}
		rows[period] = values
	}
	for _, id := range ids {
		if counts[id] == 0 {
			return nil, fmt.Errorf("FRED投资序列无有效观测: %s", id)
		}
	}
	// 写缓存前校验完整快照的严格同比及上期增速，拒绝有限源值导致的衍生溢出。
	for period, row := range rows {
		var previous, yearAgo string
		if quarterly {
			q, _ := gdpQuarter(period)
			previous, yearAgo = gdpPeriod(q-1), gdpPeriod(q-4)
		} else {
			date, _ := time.Parse("200601", period)
			previous, yearAgo = date.AddDate(0, -1, 0).Format("200601"), date.AddDate(-1, 0, 0).Format("200601")
		}
		for _, id := range ids {
			for _, base := range []string{previous, yearAgo} {
				if _, err := investmentChange(row[id].Level, rows[base][id].Level); err != nil {
					return nil, fmt.Errorf("FRED投资增速无效: %s %s 基期%s: %w", period, id, base, err)
				}
			}
		}
	}
	return rows, nil
}

func investmentChange(current, base *float64) (*float64, error) {
	if current == nil || base == nil || *base == 0 {
		return nil, nil
	}
	change := (*current / *base - 1) * 100
	if math.IsNaN(change) || math.IsInf(change, 0) {
		return nil, fmt.Errorf("计算所得增速超出有限数值范围")
	}
	return &change, nil
}
