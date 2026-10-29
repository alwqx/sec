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

// EnterpriseRequest 分别查询利润、库存或新订单；季度使用 YYYYQn，月份使用 YYYYMM。
type EnterpriseRequest struct{ Begin, End, Metric string }

type enterpriseSpec struct {
	id, name, earliest, frequency, unit, measurement, producer, originalSource, definition string
	notes                                                                                  []string
}

func enterpriseMetric(metric string) (enterpriseSpec, error) {
	switch metric {
	case "profit":
		return enterpriseSpec{"CPROFIT", "企业税前利润（含IVA和CCAdj）", "1947Q1", "quarterly", "十亿美元", "quarterly_flow", "BEA", "https://www.bea.gov/resources/methodologies/nipa-handbook/pdf/chapter-13.pdf", "NIPA当期生产所得的企业税前利润，含存货估价调整IVA和资本消耗调整CCAdj；覆盖上市和非上市企业，并非上市公司财报利润合计", []string{"IVA将存货耗用按当期成本计价，CCAdj将折旧按当期成本计价；该序列仍为当期美元金额，不是剔除通胀的实际利润。", "水平为季调折年季度流量，不是当季实际利润或年初累计值；包含来自世界其他地区的净利润，不等同于仅境内企业利润。"}}, nil
	case "inventory":
		return enterpriseSpec{"BUSINV", "商业库存", "199201", "monthly", "百万美元", "month_end_stock", "U.S. Census Bureau", "https://www.census.gov/mtis/", "制造业、商人批发业和零售业的月末库存账面余额，不是全经济所有企业库存，也不是库存投资流量", []string{"未折年；2001-06-14起历史数据重编为NAICS口径，不把当期修订历史当作当时可得数据。"}}, nil
	case "new-orders":
		return enterpriseSpec{"AMTMNO", "制造业新订单", "199202", "monthly", "百万美元", "monthly_flow", "U.S. Census Bureau", "https://www.census.gov/manufacturing/m3/definitions/index.html", "M3制造业新订单总额，为当月新接订单扣除取消额；不是未完成订单余额、发货量或PMI扩散指数", []string{"未折年；半导体行业新订单数据不可得。主要即时交付行业不直接调查订单，相关总量估计方法见M3说明。"}}, nil
	default:
		return enterpriseSpec{}, fmt.Errorf("美国企业指标必须为 profit、inventory 或 new-orders")
	}
}

func (r *EnterpriseRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国企业指标请求不能为空")
	}
	spec, err := enterpriseMetric(r.Metric)
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
				return fmt.Errorf("无效企业指标月份 %q，请使用YYYYMM", period)
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
		return fmt.Errorf("该企业指标支持自%s起查询", spec.earliest)
	}
	return nil
}

type EnterpriseValue struct {
	Period   string   `json:"period"`
	Level    *float64 `json:"level"`
	RawLevel string   `json:"raw_level"`
}

type EnterpriseObservation struct {
	EnterpriseValue
	AsOfDate       *string         `json:"as_of_date"`
	YoY            *float64        `json:"yoy_percent"`
	PreviousGrowth *float64        `json:"previous_period_percent"`
	PreviousBase   EnterpriseValue `json:"previous_period_base"`
	YoYBase        EnterpriseValue `json:"yoy_base"`
	Note           string          `json:"note,omitempty"`
}

type EnterpriseResult struct {
	Region             string                  `json:"region"`
	Metric             string                  `json:"metric"`
	Name               string                  `json:"name"`
	SeriesID           string                  `json:"series_id"`
	Frequency          string                  `json:"frequency"`
	Unit               string                  `json:"unit"`
	Measurement        string                  `json:"measurement"`
	PriceBasis         string                  `json:"price_basis"`
	SeasonallyAdjusted bool                    `json:"seasonally_adjusted"`
	LevelAnnualized    bool                    `json:"level_annualized"`
	GrowthAnnualized   bool                    `json:"growth_annualized"`
	TaxBasis           string                  `json:"tax_basis,omitempty"`
	BEAAccountCode     string                  `json:"bea_account_code,omitempty"`
	Begin              string                  `json:"begin"`
	End                string                  `json:"end"`
	FirstPeriod        string                  `json:"first_period"`
	LastPeriod         string                  `json:"last_period"`
	Producer           string                  `json:"producer"`
	Distributor        string                  `json:"distributor"`
	Source             string                  `json:"source"`
	SeriesSource       string                  `json:"series_source"`
	OriginalSource     string                  `json:"original_source"`
	Definition         string                  `json:"definition"`
	MethodologyNotes   []string                `json:"methodology_notes"`
	FetchedAt          time.Time               `json:"fetched_at"`
	PublishedAt        *time.Time              `json:"published_at"`
	RevisionID         *string                 `json:"revision_id"`
	DataVersion        string                  `json:"data_version"`
	Observations       []EnterpriseObservation `json:"observations"`
}

func enterpriseCSVURL(id string) string {
	return "https://fred.stlouisfed.org/graph/fredgraph.csv?id=" + id
}

// Enterprise 保留独立源快照，不跨频率下载、拼接或加总三个指标。
func (c *Client) Enterprise(ctx context.Context, req *EnterpriseRequest) (*EnterpriseResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	spec, _ := enterpriseMetric(req.Metric)
	var rows map[string]EnterpriseValue
	_, fetched, err := c.csvSnapshot(ctx, c.enterpriseEndpoints[req.Metric], "us-enterprise-"+req.Metric+".json", func(raw string) error {
		var err error
		rows, err = enterpriseParse(raw, req.Metric)
		return err
	})
	if err != nil {
		return nil, err
	}
	result := &EnterpriseResult{Region: "US", Metric: req.Metric, Name: spec.name, SeriesID: spec.id, Frequency: spec.frequency, Unit: spec.unit, Measurement: spec.measurement, PriceBasis: "current_dollars", SeasonallyAdjusted: true, LevelAnnualized: req.Metric == "profit", Begin: req.Begin, End: req.End, Producer: spec.producer, Distributor: "FRED", Source: enterpriseCSVURL(spec.id), SeriesSource: "https://fred.stlouisfed.org/series/" + spec.id, OriginalSource: spec.originalSource, Definition: spec.definition, MethodologyNotes: spec.notes, FetchedAt: fetched, DataVersion: "current_visible_revision", Observations: []EnterpriseObservation{}}
	if req.Metric == "profit" {
		result.TaxBasis, result.BEAAccountCode = "before_tax", "A051RC"
	}
	for period, row := range rows {
		if row.Level != nil {
			if result.FirstPeriod == "" || period < result.FirstPeriod {
				result.FirstPeriod = period
			}
			if period > result.LastPeriod {
				result.LastPeriod = period
			}
		}
	}
	value := func(period string) EnterpriseValue {
		if v, ok := rows[period]; ok {
			return v
		}
		return EnterpriseValue{Period: period}
	}
	appendPeriod := func(period string) {
		previous, yearAgo := enterpriseBases(period, spec.frequency == "quarterly")
		item := EnterpriseObservation{EnterpriseValue: value(period), PreviousBase: value(previous), YoYBase: value(yearAgo)}
		item.PreviousGrowth, _ = enterpriseChange(item.Level, item.PreviousBase.Level)
		item.YoY, _ = enterpriseChange(item.Level, item.YoYBase.Level)
		var notes []string
		if item.Level == nil {
			notes = append(notes, "当前快照未提供数值")
		} else {
			if item.PreviousGrowth == nil {
				notes = append(notes, "严格上期基数缺失或非正")
			}
			if item.YoY == nil {
				notes = append(notes, "严格上年同期基数缺失或非正")
			}
		}
		if req.Metric == "inventory" {
			date, _ := time.Parse("200601", period)
			asOf := date.AddDate(0, 1, -1).Format("2006-01-02")
			item.AsOfDate = &asOf
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	if spec.frequency == "quarterly" {
		begin, _ := gdpQuarter(req.Begin)
		end, _ := gdpQuarter(req.End)
		for q := begin; q <= end; q++ {
			appendPeriod(gdpPeriod(q))
		}
	} else {
		begin, _ := time.Parse("200601", req.Begin)
		end, _ := time.Parse("200601", req.End)
		for date := begin; !date.After(end); date = date.AddDate(0, 1, 0) {
			appendPeriod(date.Format("200601"))
		}
	}
	return result, nil
}

func enterpriseBases(period string, quarterly bool) (string, string) {
	if quarterly {
		q, _ := gdpQuarter(period)
		return gdpPeriod(q - 1), gdpPeriod(q - 4)
	}
	date, _ := time.Parse("200601", period)
	return date.AddDate(0, -1, 0).Format("200601"), date.AddDate(-1, 0, 0).Format("200601")
}

func enterpriseChange(current, base *float64) (*float64, error) {
	if current == nil || base == nil || *base <= 0 {
		return nil, nil
	}
	return investmentChange(current, base)
}

func enterpriseParse(raw, metric string) (map[string]EnterpriseValue, error) {
	spec, err := enterpriseMetric(metric)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取企业指标CSV表头失败: %w", err)
	}
	if len(header) != 2 {
		return nil, fmt.Errorf("企业指标CSV列数不符")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if name != "observation_date" && name != spec.id {
			return nil, fmt.Errorf("企业指标CSV未知列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("企业指标CSV重复列: %s", name)
		}
		columns[name] = i
	}
	rows := map[string]EnterpriseValue{}
	count := 0
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取企业指标CSV失败: %w", err)
		}
		text := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", text)
		quarterly := spec.frequency == "quarterly"
		if err != nil || date.Format("2006-01-02") != text || date.Day() != 1 || quarterly && (int(date.Month())-1)%3 != 0 {
			return nil, fmt.Errorf("无效企业指标日期或频率: %s", text)
		}
		period := date.Format("200601")
		if quarterly {
			period = fmt.Sprintf("%04dQ%d", date.Year(), (int(date.Month())-1)/3+1)
		}
		if period < spec.earliest {
			return nil, fmt.Errorf("企业指标日期早于已核验范围: %s", period)
		}
		if _, ok := rows[period]; ok {
			return nil, fmt.Errorf("企业指标重复期间: %s", period)
		}
		v := EnterpriseValue{Period: period, RawLevel: row[columns[spec.id]]}
		number := strings.TrimSpace(v.RawLevel)
		if number != "" && number != "." {
			n, err := strconv.ParseFloat(number, 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || metric != "profit" && n < 0 {
				return nil, fmt.Errorf("无效企业指标数值: %s %q", period, v.RawLevel)
			}
			v.Level = &n
			count++
		}
		rows[period] = v
	}
	if count == 0 {
		return nil, fmt.Errorf("企业指标无有效观测: %s", spec.id)
	}
	// 完整快照的衍生增速同样在缓存写入前校验，避免有限源值相除溢出。
	for period, v := range rows {
		prev, yoy := enterpriseBases(period, spec.frequency == "quarterly")
		for _, base := range []string{prev, yoy} {
			if _, err := enterpriseChange(v.Level, rows[base].Level); err != nil {
				return nil, fmt.Errorf("企业指标增速无效: %s 基期%s: %w", period, base, err)
			}
		}
	}
	return rows, nil
}
