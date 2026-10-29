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

const housingSalesCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=HSN1F,EXHOSLUSM495S"
const housePriceCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=USSTHPI"

// HousingRequest 查询月度住宅销售，Component 为 all、new-sales 或 existing-sales。
type HousingRequest struct{ Begin, End, Component string }

// HousePriceRequest 查询季度FHFA全交易房价指数，季度使用 YYYYQ1 至 YYYYQ4。
type HousePriceRequest struct{ Begin, End string }

func housingComponent(component string) (string, error) {
	if component == "" {
		return "all", nil
	}
	switch component {
	case "all", "new-sales", "existing-sales":
		return component, nil
	}
	return "", fmt.Errorf("住宅销售分类必须为 all、new-sales（新房）或 existing-sales（成屋）")
}

// Validate 在请求网络前检查月份和分类。
func (r *HousingRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国住宅销售查询请求不能为空")
	}
	if _, err := housingComponent(r.Component); err != nil {
		return err
	}
	for _, period := range []string{r.Begin, r.End} {
		date, err := time.Parse("200601", period)
		if err != nil || date.Format("200601") != period {
			return fmt.Errorf("无效住宅销售月份 %q，请使用 YYYYMM", period)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "196301" {
		return fmt.Errorf("美国住宅销售查询不能早于 196301；成屋仅提供当前公开快照可见范围")
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

// Validate 检查FHFA季度范围，不以月度售价数据替代指数。
func (r *HousePriceRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国房价指数查询请求不能为空")
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
	if r.Begin < "1975Q1" {
		return fmt.Errorf("FHFA全交易房价指数查询支持自 1975Q1 起")
	}
	now := time.Now().UTC()
	if end > now.Year()*4+(int(now.Month())-1)/3 {
		return fmt.Errorf("截至季度不能晚于当前UTC季度")
	}
	return nil
}

// HousingSeries 保留每条系列自己的单位、季调和折年口径；新房与成屋单位不同。
type HousingSeries struct {
	Component          string `json:"component"`
	SeriesID           string `json:"series_id"`
	Name               string `json:"name"`
	Unit               string `json:"unit"`
	IndexBase          string `json:"index_base,omitempty"`
	SeasonallyAdjusted bool   `json:"seasonally_adjusted"`
	LevelAnnualized    bool   `json:"level_annualized"`
	Producer           string `json:"producer"`
	Source             string `json:"source"`
	OriginalSource     string `json:"original_source"`
	Copyright          string `json:"copyright"`
	Definition         string `json:"definition"`
	FirstPeriod        string `json:"first_period"`
	LastPeriod         string `json:"last_period"`
	CoverageNote       string `json:"coverage_note"`
}

// HousingBase 保留严格日历基期及原始值，不回退到最近一次有效观测。
type HousingBase struct {
	Period string   `json:"period"`
	Value  *float64 `json:"value"`
	Raw    string   `json:"raw"`
}

// HousingObservation 的 Value 是原始单位水平，增速均未年化。
type HousingObservation struct {
	HousingBase
	Component            string      `json:"component"`
	SeriesID             string      `json:"series_id"`
	Unit                 string      `json:"unit"`
	YoY                  *float64    `json:"yoy_percent"`
	PreviousPeriodGrowth *float64    `json:"previous_period_percent"`
	PreviousBase         HousingBase `json:"previous_period_base"`
	YoYBase              HousingBase `json:"yoy_base"`
	Note                 string      `json:"note,omitempty"`
}

// HousingResult 区分频率与统计范围，不拼接或加总新房、成屋和房价。
type HousingResult struct {
	Region           string               `json:"region"`
	Indicator        string               `json:"indicator"`
	Frequency        string               `json:"frequency"`
	Component        string               `json:"component"`
	Begin            string               `json:"begin"`
	End              string               `json:"end"`
	GrowthAnnualized bool                 `json:"growth_annualized"`
	Distributor      string               `json:"distributor"`
	Source           string               `json:"source"`
	FetchedAt        time.Time            `json:"fetched_at"`
	DataVersion      string               `json:"data_version"`
	PublishedAt      *time.Time           `json:"published_at"`
	RevisionID       *string              `json:"revision_id"`
	Series           []HousingSeries      `json:"series"`
	Observations     []HousingObservation `json:"observations"`
	Note             string               `json:"note"`
}

type housingCell struct {
	value *float64
	raw   string
}

type housingRows map[string]map[string]housingCell

// HousingSales 采集Census新房与NAR成屋销售的同一份当前可见月度快照。
func (c *Client) HousingSales(ctx context.Context, req *HousingRequest) (*HousingResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	component, _ := housingComponent(req.Component)
	return c.housing(ctx, req.Begin, req.End, component, false)
}

// HousePrice 采集FHFA全交易指数；该季度指数未季调，包含购房和再融资估值。
func (c *Client) HousePrice(ctx context.Context, req *HousePriceRequest) (*HousingResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return c.housing(ctx, req.Begin, req.End, "house-price", true)
}

func (c *Client) housing(ctx context.Context, begin, end, component string, price bool) (*HousingResult, error) {
	endpoint, source, filename := c.housingSalesEndpoint, housingSalesCSVURL, "us-housing-sales.json"
	if price {
		endpoint, source, filename = c.housePriceEndpoint, housePriceCSVURL, "us-housing-price.json"
	}
	var rows housingRows
	_, fetched, err := c.csvSnapshot(ctx, endpoint, filename, func(raw string) error {
		var err error
		rows, err = housingParse(raw, price)
		return err
	})
	if err != nil {
		return nil, err
	}
	result := &HousingResult{Region: "US", Indicator: "housing_sales", Frequency: "monthly", Component: component, Begin: begin, End: end, Distributor: "FRED", Source: source, FetchedAt: fetched, DataVersion: "current_visible_revision", Series: []HousingSeries{}, Observations: []HousingObservation{}, Note: "新房和成屋为不同统计总体及成交阶段，不能直接相加；月度数值均季调折年，非当月实际成交套数，新房单位千套、成屋单位套。同比和月环比由严格日历基期计算、未年化。成屋仅使用当前公开快照可见范围，缺失不补、不跨期寻找基数。NAR成屋数据受版权保护，引用时保留来源归属；不代表获得独立再分发许可。数据为当前可见修订值，无逐期发布时间或修订批次。"}
	if price {
		result.Indicator, result.Frequency = "house_price", "quarterly"
		result.Note = "FHFA全交易房价指数，1980Q1=100、未季调，包含购房价格及再融资评估值，不是售价中位数、纯购房指数或覆盖所有住宅交易的指数。同比及季环比为当前修订指数计算值、未年化；季环比未季调，不能解释为剔除季节效应后的趋势。缺失不补、不跨期寻找基数；无逐期发布时间或修订批次。"
	}
	for _, series := range housingSeriesMetadata(price) {
		if !price && component != "all" && component != series.Component {
			continue
		}
		for period, row := range rows {
			if row[series.SeriesID].value != nil {
				if series.FirstPeriod == "" || period < series.FirstPeriod {
					series.FirstPeriod = period
				}
				if period > series.LastPeriod {
					series.LastPeriod = period
				}
			}
		}
		result.Series = append(result.Series, series)
	}
	appendPeriod := func(period, previous, yearAgo string) error {
		for _, series := range result.Series {
			base := func(period string) HousingBase {
				cell := rows[period][series.SeriesID]
				return HousingBase{Period: period, Value: cell.value, Raw: cell.raw}
			}
			item := HousingObservation{HousingBase: base(period), Component: series.Component, SeriesID: series.SeriesID, Unit: series.Unit, PreviousBase: base(previous), YoYBase: base(yearAgo)}
			item.YoY, err = housingChange(item.Value, item.YoYBase.Value)
			if err != nil {
				return err
			}
			item.PreviousPeriodGrowth, err = housingChange(item.Value, item.PreviousBase.Value)
			if err != nil {
				return err
			}
			var notes []string
			if item.Value == nil {
				notes = append(notes, "当前快照未提供数值")
				if series.Component == "existing-sales" && (period < series.FirstPeriod || period > series.LastPeriod) {
					notes = append(notes, "超出当前成屋公开快照有效覆盖")
				}
			}
			if item.Value != nil && item.YoY == nil {
				notes = append(notes, "严格上年同期基数缺失或为零")
			}
			if item.Value != nil && item.PreviousPeriodGrowth == nil {
				notes = append(notes, "严格上期基数缺失或为零")
			}
			if price {
				notes = append(notes, "季环比未季调")
			}
			item.Note = strings.Join(notes, "；")
			result.Observations = append(result.Observations, item)
		}
		return nil
	}
	if price {
		first, _ := gdpQuarter(begin)
		last, _ := gdpQuarter(end)
		for q := first; q <= last; q++ {
			if err := appendPeriod(gdpPeriod(q), gdpPeriod(q-1), gdpPeriod(q-4)); err != nil {
				return nil, err
			}
		}
	} else {
		first, _ := time.Parse("200601", begin)
		last, _ := time.Parse("200601", end)
		for date := first; !date.After(last); date = date.AddDate(0, 1, 0) {
			if err := appendPeriod(date.Format("200601"), date.AddDate(0, -1, 0).Format("200601"), date.AddDate(-1, 0, 0).Format("200601")); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func housingSeriesMetadata(price bool) []HousingSeries {
	if price {
		return []HousingSeries{{Component: "house-price", SeriesID: "USSTHPI", Name: "FHFA全交易房价指数", Unit: "指数", IndexBase: "1980Q1=100", Producer: "Federal Housing Finance Agency", Source: "https://fred.stlouisfed.org/series/USSTHPI", OriginalSource: "https://www.fhfa.gov/data/hpi", Copyright: "Public Domain: Citation Requested", Definition: "Fannie Mae和Freddie Mac所购入或证券化的常规合规贷款对应单户住宅，采用重复交易法；全交易指数在购房样本中加入再融资评估值，非纯购房价格指数", CoverageNote: "季度自1975Q1起，当前快照可见有效起止期另列；未季调，不折年"}}
	}
	return []HousingSeries{
		{Component: "new-sales", SeriesID: "HSN1F", Name: "新建单户住宅销售", Unit: "千套", SeasonallyAdjusted: true, LevelAnnualized: true, Producer: "U.S. Census Bureau; HUD", Source: "https://fred.stlouisfed.org/series/HSN1F", OriginalSource: "https://www.census.gov/construction/nrs/", Copyright: "Public Domain: Citation Requested", Definition: "新建待售单户住宅签订销售合同或收取定金时计为销售，可发生在建设前、中、后；不追踪最终交割，非全部新建住宅竣工量", CoverageNote: "新房系列自1963年1月起；季调折年水平，单位千套，非单月真实套数"},
		{Component: "existing-sales", SeriesID: "EXHOSLUSM495S", Name: "成屋销售", Unit: "套", SeasonallyAdjusted: true, LevelAnnualized: true, Producer: "National Association of Realtors", Source: "https://fred.stlouisfed.org/series/EXHOSLUSM495S", OriginalSource: "https://www.nar.realtor/research-and-statistics/housing-statistics/existing-home-sales", Copyright: "Copyright, 2016, National Association of Realtors. FRED: Reprinted with permission. Copyrighted: Citation Required.", Definition: "既有单户住宅、共管公寓和合作公寓的已完成交易，区别于新房签约或定金阶段", CoverageNote: "仅当前公开下载快照的有效范围；2026-10-28核验公开CSV仅2025-09至2026-09共13个月有效，不承诺完整历史或固定起点"},
	}
}

func housingChange(current, base *float64) (*float64, error) {
	if current == nil || base == nil || *base == 0 {
		return nil, nil
	}
	change := (*current / *base - 1) * 100
	if math.IsNaN(change) || math.IsInf(change, 0) {
		return nil, fmt.Errorf("住宅数据增速计算超出有限数值范围")
	}
	return &change, nil
}

func housingParse(raw string, price bool) (housingRows, error) {
	ids, earliest := []string{"HSN1F", "EXHOSLUSM495S"}, "1963-01-01"
	if price {
		ids, earliest = []string{"USSTHPI"}, "1975-01-01"
	}
	wanted := map[string]bool{"observation_date": true}
	for _, id := range ids {
		wanted[id] = true
	}
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED住宅CSV表头失败: %w", err)
	}
	if len(header) != len(wanted) {
		return nil, fmt.Errorf("FRED住宅CSV表头不完整")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if !wanted[name] {
			return nil, fmt.Errorf("FRED返回未知住宅列: %s", name)
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("FRED返回重复住宅列: %s", name)
		}
		columns[name] = i
	}
	rows, counts := housingRows{}, map[string]int{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED住宅CSV失败: %w", err)
		}
		text := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", text)
		if err != nil || date.Format("2006-01-02") != text || date.Day() != 1 || text < earliest || price && (int(date.Month())-1)%3 != 0 {
			return nil, fmt.Errorf("FRED返回无效住宅日期或频率: %s", text)
		}
		period := date.Format("200601")
		if price {
			period = fmt.Sprintf("%04dQ%d", date.Year(), (int(date.Month())-1)/3+1)
		}
		if _, exists := rows[period]; exists {
			return nil, fmt.Errorf("FRED返回重复住宅期间: %s", period)
		}
		cells := map[string]housingCell{}
		for _, id := range ids {
			cell := housingCell{raw: row[columns[id]]}
			text := strings.TrimSpace(cell.raw)
			if text != "" && text != "." {
				value, err := strconv.ParseFloat(text, 64)
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || price && value == 0 {
					return nil, fmt.Errorf("FRED返回无效住宅数值: %s %s %q", period, id, cell.raw)
				}
				cell.value = &value
				counts[id]++
			}
			cells[id] = cell
		}
		rows[period] = cells
	}
	for _, id := range ids {
		if counts[id] == 0 {
			return nil, fmt.Errorf("FRED住宅系列无有效观测: %s", id)
		}
	}
	// 增速也在写缓存前校验，避免有限源值相除溢出后留下不可使用的快照。
	for period, row := range rows {
		var previous, yearAgo string
		if price {
			q, _ := gdpQuarter(period)
			previous, yearAgo = gdpPeriod(q-1), gdpPeriod(q-4)
		} else {
			date, _ := time.Parse("200601", period)
			previous, yearAgo = date.AddDate(0, -1, 0).Format("200601"), date.AddDate(-1, 0, 0).Format("200601")
		}
		for _, id := range ids {
			for _, base := range []string{previous, yearAgo} {
				if _, err := housingChange(row[id].value, rows[base][id].value); err != nil {
					return nil, fmt.Errorf("%s %s: %w", period, id, err)
				}
			}
		}
	}
	return rows, nil
}
