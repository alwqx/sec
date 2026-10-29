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

// CreditRequest 分别查询 bank、standards 或 debt-service，月度用 YYYYMM，季度用 YYYYQn。
type CreditRequest struct{ Begin, End, Metric string }

type creditSpec struct {
	id, name, earliest, frequency, unit, measurement, periodBasis, priceBasis, changeUnit, method, originalSource, definition string
	adjusted                                                                                                                  bool
	notes                                                                                                                     []string
}

func creditMetric(metric string) (creditSpec, error) {
	switch metric {
	case "bank":
		return creditSpec{id: "LOANINV", name: "商业银行信贷", earliest: "194701", frequency: "monthly", unit: "十亿美元", measurement: "monthly_average_stock", periodBasis: "pro_rata_average_of_weekly_levels", priceBasis: "current_dollars", changeUnit: "percent", method: "level_ratio_not_break_adjusted", adjusted: true, originalSource: "https://www.federalreserve.gov/releases/h8/about.htm", definition: "H.8美国商业银行信贷，包含证券及贷款和租赁；是余额的月平均而非月末余额、新增贷款或全社会信用总量", notes: []string{"月度水平为周三周度余额的按比例平均，未折年；覆盖美国商业银行国内办公室及在美外资相关机构，不含储贷机构、信用合作社和消费金融公司等。", "同比和月环比由当前水平计算、未年化；未另行剔除非银行结构活动与会计规则断点影响，不是H.8官方经断点调整的折年增速。"}}, nil
	case "standards":
		return creditSpec{id: "DRTSCILM", name: "大中型企业C&I贷款标准净收紧比例", earliest: "1990Q2", frequency: "quarterly", unit: "%", measurement: "net_tightening_share", periodBasis: "survey_quarter", priceBasis: "not_applicable", changeUnit: "percentage_points", method: "difference", originalSource: "https://www.federalreserve.gov/data/sloos.htm", definition: "SLOOS美国国内受访银行中，收紧大中型企业工商贷款标准的比例减去放松的比例；正值为净收紧，负值为净放松，不是贷款利率或银行贷款金额占比", notes: []string{"未季调、未折年；为定性调查的净百分比，并非审批标准绝对水平或拒贷率，样本会变化。大中型企业按现行定义为年销售额至少5000万美元。", "period沿用FRED调查季度，不擅自回移：如2026Q3对应7月调查，问及过去三个月、通常对应第二季度的变化；不是第三季度全季信贷活动。", "与上季、上年同季比较均为百分点差，不计算净比例的百分比增长；负数和零均为有效值。"}}, nil
	case "debt-service":
		return creditSpec{id: "TDSP", name: "家庭偿债比率", earliest: "2005Q1", frequency: "quarterly", unit: "%", measurement: "required_payments_to_disposable_income", periodBasis: "calendar_quarter", priceBasis: "not_applicable", changeUnit: "percentage_points", method: "difference", adjusted: true, originalSource: "https://www.federalreserve.gov/releases/dsr/about.htm", definition: "家庭按计划须偿付的抵押及消费债务款项占可支配个人收入的比例，为总量估计，不是债务余额收入比、违约率或实际已支付比例", notes: []string{"2024Q2发布起改用征信数据方法，当前方法历史从2005Q1开始；不与旧方法1980年起档案拼接，也不将方法启用时间误作观测起点。", "新方法抵押贷款款项包含随还款托管的房产税和保险，与旧方法不同；该指标不是包含租金等项目的Financial Obligations Ratio。", "季调、未折年；与上季、上年同季比较均为百分点差，不计算比率的百分比增长。"}}, nil
	default:
		return creditSpec{}, fmt.Errorf("美国信用指标必须为 bank、standards 或 debt-service")
	}
}

func (r *CreditRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国信用指标请求不能为空")
	}
	spec, err := creditMetric(r.Metric)
	if err != nil {
		return err
	}
	if spec.frequency == "quarterly" {
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
		now := time.Now().UTC()
		if end > now.Year()*4+(int(now.Month())-1)/3 {
			return fmt.Errorf("截至季度不能晚于当前UTC季度")
		}
	} else {
		for _, period := range []string{r.Begin, r.End} {
			date, err := time.Parse("200601", period)
			if err != nil || date.Format("200601") != period {
				return fmt.Errorf("无效信用指标月份 %q，请使用YYYYMM", period)
			}
		}
		if r.Begin > r.End {
			return fmt.Errorf("开始月份不能晚于截至月份")
		}
		if r.End > time.Now().UTC().Format("200601") {
			return fmt.Errorf("截至月份不能晚于当前UTC月份")
		}
	}
	if r.Begin < spec.earliest {
		return fmt.Errorf("该信用指标支持自%s起查询", spec.earliest)
	}
	return nil
}

type CreditValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawLevel string   `json:"raw_level"`
}

type CreditObservation struct {
	CreditValue
	PreviousBase          CreditValue `json:"previous_period_base"`
	YearAgoBase           CreditValue `json:"year_ago_base"`
	PreviousChangePercent *float64    `json:"previous_change_percent"`
	YearAgoChangePercent  *float64    `json:"year_ago_change_percent"`
	PreviousChangePP      *float64    `json:"previous_change_pp"`
	YearAgoChangePP       *float64    `json:"year_ago_change_pp"`
	Note                  string      `json:"note,omitempty"`
}

type CreditResult struct {
	Region             string              `json:"region"`
	Metric             string              `json:"metric"`
	Name               string              `json:"name"`
	SeriesID           string              `json:"series_id"`
	Frequency          string              `json:"frequency"`
	Unit               string              `json:"unit"`
	Measurement        string              `json:"measurement"`
	PeriodBasis        string              `json:"period_basis"`
	PriceBasis         string              `json:"price_basis"`
	SeasonallyAdjusted bool                `json:"seasonally_adjusted"`
	LevelAnnualized    bool                `json:"level_annualized"`
	GrowthAnnualized   bool                `json:"growth_annualized"`
	ChangeUnit         string              `json:"change_unit"`
	ChangeMethod       string              `json:"change_method"`
	Methodology        string              `json:"methodology"`
	Begin              string              `json:"begin"`
	End                string              `json:"end"`
	FirstPeriod        string              `json:"first_period"`
	LastPeriod         string              `json:"last_period"`
	Producer           string              `json:"producer"`
	Distributor        string              `json:"distributor"`
	Source             string              `json:"source"`
	SeriesSource       string              `json:"series_source"`
	OriginalSource     string              `json:"original_source"`
	Definition         string              `json:"definition"`
	MethodologyNotes   []string            `json:"methodology_notes"`
	FetchedAt          time.Time           `json:"fetched_at"`
	PublishedAt        *time.Time          `json:"published_at"`
	RevisionID         *string             `json:"revision_id"`
	DataVersion        string              `json:"data_version"`
	Observations       []CreditObservation `json:"observations"`
}

func creditCSVURL(id string) string {
	return "https://fred.stlouisfed.org/graph/fredgraph.csv?id=" + id
}

// Credit 采集三个独立快照，百分比水平和百分点变化分别保留，禁止混用。
func (c *Client) Credit(ctx context.Context, req *CreditRequest) (*CreditResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	spec, _ := creditMetric(req.Metric)
	var rows map[string]CreditValue
	_, fetched, err := c.csvSnapshot(ctx, c.creditEndpoints[req.Metric], "us-credit-"+req.Metric+".json", func(raw string) error { var err error; rows, err = creditParse(raw, req.Metric); return err })
	if err != nil {
		return nil, err
	}
	methodology := "current_h8_monthly_levels"
	if req.Metric == "standards" {
		methodology = "sloos_domestic_banks_large_middle_market_ci"
	}
	if req.Metric == "debt-service" {
		methodology = "credit_bureau_based_dsr"
	}
	result := &CreditResult{Region: "US", Metric: req.Metric, Name: spec.name, SeriesID: spec.id, Frequency: spec.frequency, Unit: spec.unit, Measurement: spec.measurement, PeriodBasis: spec.periodBasis, PriceBasis: spec.priceBasis, SeasonallyAdjusted: spec.adjusted, ChangeUnit: spec.changeUnit, ChangeMethod: spec.method, Methodology: methodology, Begin: req.Begin, End: req.End, Producer: "Board of Governors of the Federal Reserve System (US)", Distributor: "FRED", Source: creditCSVURL(spec.id), SeriesSource: "https://fred.stlouisfed.org/series/" + spec.id, OriginalSource: spec.originalSource, Definition: spec.definition, MethodologyNotes: spec.notes, FetchedAt: fetched, DataVersion: "current_visible_revision", Observations: []CreditObservation{}}
	for period, v := range rows {
		if v.Level != nil {
			if result.FirstPeriod == "" || period < result.FirstPeriod {
				result.FirstPeriod = period
			}
			if period > result.LastPeriod {
				result.LastPeriod = period
			}
		}
	}
	value := func(period string) CreditValue {
		if v, ok := rows[period]; ok {
			return v
		}
		return CreditValue{Period: period}
	}
	appendPeriod := func(period string) error {
		previous, yearAgo := creditBases(period, spec.frequency == "quarterly")
		item := CreditObservation{CreditValue: value(period), PreviousBase: value(previous), YearAgoBase: value(yearAgo)}
		prev, err := creditChange(item.Level, item.PreviousBase.Level, spec.changeUnit)
		if err != nil {
			return err
		}
		yoy, err := creditChange(item.Level, item.YearAgoBase.Level, spec.changeUnit)
		if err != nil {
			return err
		}
		if spec.changeUnit == "percent" {
			item.PreviousChangePercent, item.YearAgoChangePercent = prev, yoy
		} else {
			item.PreviousChangePP, item.YearAgoChangePP = prev, yoy
		}
		var notes []string
		if item.Level == nil {
			notes = append(notes, "当前快照未提供数值")
		} else {
			reason := "基期水平缺失"
			if spec.changeUnit == "percent" {
				reason = "基数缺失或为零"
			}
			if prev == nil {
				notes = append(notes, "严格上期"+reason)
			}
			if yoy == nil {
				notes = append(notes, "严格上年同期"+reason)
			}
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
		return nil
	}
	if spec.frequency == "quarterly" {
		begin, _ := gdpQuarter(req.Begin)
		end, _ := gdpQuarter(req.End)
		for q := begin; q <= end; q++ {
			if err := appendPeriod(gdpPeriod(q)); err != nil {
				return nil, err
			}
		}
	} else {
		begin, _ := time.Parse("200601", req.Begin)
		end, _ := time.Parse("200601", req.End)
		for date := begin; !date.After(end); date = date.AddDate(0, 1, 0) {
			if err := appendPeriod(date.Format("200601")); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func creditBases(period string, quarterly bool) (string, string) {
	if quarterly {
		q, _ := gdpQuarter(period)
		return gdpPeriod(q - 1), gdpPeriod(q - 4)
	}
	date, _ := time.Parse("200601", period)
	return date.AddDate(0, -1, 0).Format("200601"), date.AddDate(-1, 0, 0).Format("200601")
}

func creditChange(current, base *float64, unit string) (*float64, error) {
	if current == nil || base == nil {
		return nil, nil
	}
	if unit == "percent" {
		return investmentChange(current, base)
	}
	change := *current - *base
	if math.IsNaN(change) || math.IsInf(change, 0) {
		return nil, fmt.Errorf("信用指标百分点差超出有限数值范围")
	}
	return &change, nil
}

func creditParse(raw, metric string) (map[string]CreditValue, error) {
	spec, err := creditMetric(metric)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取信用指标CSV表头失败: %w", err)
	}
	if len(header) != 2 {
		return nil, fmt.Errorf("信用指标CSV列数不符")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != spec.id {
			return nil, fmt.Errorf("信用指标CSV未知列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("信用指标CSV重复列: %s", name)
		}
		columns[name] = i
	}
	rows := map[string]CreditValue{}
	count := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取信用指标CSV失败: %w", err)
		}
		text := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", text)
		quarterly := spec.frequency == "quarterly"
		if err != nil || date.Format("2006-01-02") != text || date.Day() != 1 || quarterly && (int(date.Month())-1)%3 != 0 {
			return nil, fmt.Errorf("无效信用指标日期或频率: %s", text)
		}
		period := date.Format("200601")
		if quarterly {
			period = fmt.Sprintf("%04dQ%d", date.Year(), (int(date.Month())-1)/3+1)
		}
		if period < spec.earliest {
			return nil, fmt.Errorf("信用指标日期早于已核验范围: %s", period)
		}
		if _, ok := rows[period]; ok {
			return nil, fmt.Errorf("信用指标重复期间: %s", period)
		}
		v := CreditValue{Period: period, RawLevel: row[columns[spec.id]]}
		text = strings.TrimSpace(v.RawLevel)
		if text != "" && text != "." {
			number, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || metric == "standards" && (number < -100 || number > 100) || metric != "standards" && number < 0 {
				return nil, fmt.Errorf("无效信用指标数值: %s %q", period, v.RawLevel)
			}
			v.Level = &number
			count++
		}
		rows[period] = v
	}
	if count == 0 {
		return nil, fmt.Errorf("信用指标无有效观测: %s", spec.id)
	}
	// 写缓存前校验所有可计算的严格基期变化，避免衍生溢出污染快照。
	for period, v := range rows {
		previous, yearAgo := creditBases(period, spec.frequency == "quarterly")
		for _, base := range []string{previous, yearAgo} {
			if _, err := creditChange(v.Level, rows[base].Level, spec.changeUnit); err != nil {
				return nil, fmt.Errorf("信用指标变化无效: %s 基期%s: %w", period, base, err)
			}
		}
	}
	return rows, nil
}
