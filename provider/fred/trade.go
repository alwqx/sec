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

const tradeCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=BOPTEXP,BOPTIMP,BOPGSTB,BOPGEXP,BOPGIMP,BOPGTB,BOPSEXP,BOPSIMP,BOPSTB"
const realTradeCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=EXPGSC1,IMPGSC1,NETEXC,A253RX1Q020SBEA,A255RX1Q020SBEA,A646RX1Q020SBEA,B656RX1Q020SBEA"

// TradeRequest 查询国际收支口径的月度名义贸易，Component为空时查询合计。
type TradeRequest struct{ Begin, End, Component string }

// RealTradeRequest 查询NIPA口径季度实际贸易，与月度名义贸易分别提供入口。
type RealTradeRequest struct{ Begin, End, Component string }

func tradeComponent(component string) (string, error) {
	if component == "" {
		return "total", nil
	}
	switch component {
	case "total", "goods", "services":
		return component, nil
	}
	return "", fmt.Errorf("贸易分类必须为total（合计）、goods（商品）或services（服务）")
}

func (r *TradeRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国名义贸易查询请求不能为空")
	}
	if _, err := tradeComponent(r.Component); err != nil {
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
	if r.Begin < "199201" {
		return fmt.Errorf("美国月度名义贸易查询支持自199201起")
	}
	if r.End > time.Now().UTC().Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前UTC月份")
	}
	return nil
}

func (r *RealTradeRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("美国实际贸易查询请求不能为空")
	}
	if _, err := tradeComponent(r.Component); err != nil {
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
	if r.Begin < "1947Q1" {
		return fmt.Errorf("美国季度实际贸易查询支持自1947Q1起；分项及净出口自1970Q1起")
	}
	now := time.Now().UTC()
	if end > now.Year()*4+(int(now.Month())-1)/3 {
		return fmt.Errorf("截至季度不能晚于当前UTC季度")
	}
	return nil
}

type TradeSeries struct {
	Measure     string `json:"measure"`
	SeriesID    string `json:"series_id"`
	Source      string `json:"source"`
	FirstPeriod string `json:"first_period"`
	LastPeriod  string `json:"last_period"`
}

type TradeValue struct {
	Period     string   `json:"period"`
	Exports    *float64 `json:"exports"`
	Imports    *float64 `json:"imports"`
	Balance    *float64 `json:"balance"`
	RawExports string   `json:"raw_exports"`
	RawImports string   `json:"raw_imports"`
	RawBalance string   `json:"raw_balance"`
}

type TradeObservation struct {
	TradeValue
	ExportsYoY            *float64   `json:"exports_yoy_percent"`
	ImportsYoY            *float64   `json:"imports_yoy_percent"`
	ExportsPreviousGrowth *float64   `json:"exports_previous_period_percent"`
	ImportsPreviousGrowth *float64   `json:"imports_previous_period_percent"`
	BalancePreviousChange *float64   `json:"balance_previous_period_change"`
	BalanceYoYChange      *float64   `json:"balance_yoy_change"`
	PreviousBase          TradeValue `json:"previous_period_base"`
	YoYBase               TradeValue `json:"yoy_base"`
	Note                  string     `json:"note,omitempty"`
}

type TradeResult struct {
	Region             string             `json:"region"`
	Indicator          string             `json:"indicator"`
	Component          string             `json:"component"`
	Frequency          string             `json:"frequency"`
	PriceBasis         string             `json:"price_basis"`
	AccountingBasis    string             `json:"accounting_basis"`
	Unit               string             `json:"unit"`
	ReferenceYear      *int               `json:"reference_year"`
	SeasonallyAdjusted bool               `json:"seasonally_adjusted"`
	LevelAnnualized    bool               `json:"level_annualized"`
	GrowthAnnualized   bool               `json:"growth_annualized"`
	Begin              string             `json:"begin"`
	End                string             `json:"end"`
	Producer           string             `json:"producer"`
	Distributor        string             `json:"distributor"`
	Source             string             `json:"source"`
	OriginalSource     string             `json:"original_source"`
	Series             []TradeSeries      `json:"series"`
	FetchedAt          time.Time          `json:"fetched_at"`
	DataVersion        string             `json:"data_version"`
	PublishedAt        *time.Time         `json:"published_at"`
	RevisionID         *string            `json:"revision_id"`
	Observations       []TradeObservation `json:"observations"`
	Note               string             `json:"note"`
}

var nominalTradeIDs = []string{"BOPTEXP", "BOPTIMP", "BOPGSTB", "BOPGEXP", "BOPGIMP", "BOPGTB", "BOPSEXP", "BOPSIMP", "BOPSTB"}
var realTradeIDs = []string{"EXPGSC1", "IMPGSC1", "NETEXC", "A253RX1Q020SBEA", "A255RX1Q020SBEA", "A646RX1Q020SBEA", "B656RX1Q020SBEA"}

type tradeCell struct {
	number *float64
	raw    string
}
type tradeRows map[string]map[string]tradeCell

func (c *Client) Trade(ctx context.Context, req *TradeRequest) (*TradeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return c.trade(ctx, req.Begin, req.End, req.Component, false)
}

func (c *Client) RealTrade(ctx context.Context, req *RealTradeRequest) (*TradeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return c.trade(ctx, req.Begin, req.End, req.Component, true)
}

func (c *Client) trade(ctx context.Context, begin, end, component string, real bool) (*TradeResult, error) {
	component, _ = tradeComponent(component)
	endpoint, filename, source, ids := c.tradeEndpoint, "us-trade.json", tradeCSVURL, nominalTradeIDs
	if real {
		endpoint, filename, source, ids = c.realTradeEndpoint, "us-real-trade.json", realTradeCSVURL, realTradeIDs
	}
	var rows tradeRows
	_, fetched, err := c.csvSnapshot(ctx, endpoint, filename, func(raw string) error {
		var parseErr error
		rows, parseErr = parseTrade(raw, real)
		return parseErr
	})
	if err != nil {
		return nil, err
	}
	selected := []string{ids[0], ids[1], ids[2]}
	if component == "goods" {
		selected = []string{ids[3], ids[4], ""}
		if !real {
			selected[2] = ids[5]
		}
	}
	if component == "services" {
		selected = []string{ids[5], ids[6], ""}
		if !real {
			selected = ids[6:9]
		}
	}
	result := &TradeResult{Region: "US", Indicator: "trade", Component: component, Frequency: "monthly", PriceBasis: "nominal", AccountingBasis: "BOP", Unit: "百万美元（当期美元）", SeasonallyAdjusted: true, Begin: begin, End: end, Producer: "U.S. Census Bureau; BEA", Distributor: "FRED", Source: source, OriginalSource: "https://www.bea.gov/data/intl-trade-investment/international-trade-goods-and-services", FetchedAt: fetched, DataVersion: "current_visible_revision", Series: []TradeSeries{}, Observations: []TradeObservation{}, Note: "月度名义金额，国际收支口径、季调、不折年；出口、进口同比及月环比为计算值。差额直接使用官方序列，差额变化为金额差，正值表示余额改善；不计算逆差百分比。历史为当前可见修订值，未提供历史版本与逐期实际公布时间。缺口不补、不跨期寻找基数。"}
	if real {
		year := 2017
		result.Indicator, result.Frequency, result.PriceBasis, result.AccountingBasis = "real_trade", "quarterly", "real", "NIPA"
		result.ReferenceYear, result.Unit, result.LevelAnnualized, result.Producer = &year, "十亿美元（2017年链式美元）", true, "BEA"
		result.OriginalSource = "https://www.bea.gov/resources/methodologies/nipa-handbook/pdf/chapter-08.pdf"
		result.Note = "季度实际贸易为NIPA口径、2017年链式美元、季调折年水平，并非当季实际金额；同比、季度环比为当前修订水平计算值、未年化。合计实际净出口直接使用官方NETEXC，自1970Q1起；商品和服务实际净额未采集，保持null，不以链式美元分项相减。分项不可相加还原合计。与月度名义贸易的频率、单位和口径不同，不直接拼接比较。未提供历史版本与逐期实际公布时间；缺口不补。"
	}
	for index, id := range selected {
		if id == "" {
			continue
		}
		series := TradeSeries{Measure: []string{"exports", "imports", "balance"}[index], SeriesID: id, Source: "https://fred.stlouisfed.org/series/" + id}
		for period, row := range rows {
			if row[id].number != nil {
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
	value := func(period string) TradeValue {
		row := rows[period]
		return TradeValue{Period: period, Exports: row[selected[0]].number, Imports: row[selected[1]].number, Balance: row[selected[2]].number, RawExports: row[selected[0]].raw, RawImports: row[selected[1]].raw, RawBalance: row[selected[2]].raw}
	}
	appendPeriod := func(period, previous, yearAgo string) {
		item := TradeObservation{TradeValue: value(period), PreviousBase: value(previous), YoYBase: value(yearAgo)}
		item.ExportsYoY, item.ImportsYoY = pceChange(item.Exports, item.YoYBase.Exports), pceChange(item.Imports, item.YoYBase.Imports)
		item.ExportsPreviousGrowth, item.ImportsPreviousGrowth = pceChange(item.Exports, item.PreviousBase.Exports), pceChange(item.Imports, item.PreviousBase.Imports)
		item.BalancePreviousChange, item.BalanceYoYChange = tradeDifference(item.Balance, item.PreviousBase.Balance), tradeDifference(item.Balance, item.YoYBase.Balance)
		var notes []string
		if item.Exports == nil || item.Imports == nil {
			notes = append(notes, "出口或进口水平缺失")
		}
		if selected[2] == "" {
			notes = append(notes, "未采集该实际分类官方净额")
		} else if item.Balance == nil {
			notes = append(notes, "官方差额缺失")
		}
		if item.Exports != nil && item.ExportsPreviousGrowth == nil || item.Imports != nil && item.ImportsPreviousGrowth == nil {
			notes = append(notes, "上期基数缺失或为零")
		}
		if item.Exports != nil && item.ExportsYoY == nil || item.Imports != nil && item.ImportsYoY == nil {
			notes = append(notes, "上年同期基数缺失或为零")
		}
		if item.Balance != nil && item.BalancePreviousChange == nil {
			notes = append(notes, "上期官方差额缺失")
		}
		if item.Balance != nil && item.BalanceYoYChange == nil {
			notes = append(notes, "上年同期官方差额缺失")
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	if real {
		start, _ := gdpQuarter(begin)
		finish, _ := gdpQuarter(end)
		for q := start; q <= finish; q++ {
			appendPeriod(gdpPeriod(q), gdpPeriod(q-1), gdpPeriod(q-4))
		}
	} else {
		start, _ := time.Parse("200601", begin)
		finish, _ := time.Parse("200601", end)
		for date := start; !date.After(finish); date = date.AddDate(0, 1, 0) {
			appendPeriod(date.Format("200601"), date.AddDate(0, -1, 0).Format("200601"), date.AddDate(-1, 0, 0).Format("200601"))
		}
	}
	return result, nil
}

func tradeDifference(current, base *float64) *float64 {
	if current == nil || base == nil {
		return nil
	}
	difference := *current - *base
	return &difference
}

func parseTrade(raw string, real bool) (tradeRows, error) {
	ids, earliest := nominalTradeIDs, "1992-01-01"
	if real {
		ids, earliest = realTradeIDs, "1947-01-01"
	}
	wanted := map[string]bool{"observation_date": true}
	for _, id := range ids {
		wanted[id] = true
	}
	reader := csv.NewReader(strings.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取FRED贸易CSV表头失败: %w", err)
	}
	if len(header) != len(wanted) {
		return nil, fmt.Errorf("FRED贸易CSV表头不完整")
	}
	columns := map[string]int{}
	for index, name := range header {
		name = strings.TrimPrefix(name, "\ufeff")
		if name == "DATE" {
			name = "observation_date"
		}
		if !wanted[name] {
			return nil, fmt.Errorf("FRED返回未知贸易列: %s", name)
		}
		if _, exists := columns[name]; exists {
			return nil, fmt.Errorf("FRED返回重复贸易列: %s", name)
		}
		columns[name] = index
	}
	rows, counts := tradeRows{}, map[string]int{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取FRED贸易CSV失败: %w", err)
		}
		text := row[columns["observation_date"]]
		date, err := time.Parse("2006-01-02", text)
		if err != nil || date.Format("2006-01-02") != text || date.Day() != 1 || text < earliest || real && (int(date.Month())-1)%3 != 0 {
			return nil, fmt.Errorf("FRED返回无效贸易日期: %s", text)
		}
		period := date.Format("200601")
		if real {
			period = fmt.Sprintf("%04dQ%d", date.Year(), (int(date.Month())-1)/3+1)
		}
		if _, exists := rows[period]; exists {
			return nil, fmt.Errorf("FRED返回重复贸易期间: %s", period)
		}
		cells := map[string]tradeCell{}
		for _, id := range ids {
			cell := tradeCell{raw: row[columns[id]]}
			trimmed := strings.TrimSpace(cell.raw)
			if trimmed != "" && trimmed != "." {
				number, err := strconv.ParseFloat(trimmed, 64)
				balance := id == "BOPGSTB" || id == "BOPGTB" || id == "BOPSTB" || id == "NETEXC"
				if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || !balance && number < 0 {
					return nil, fmt.Errorf("FRED返回无效贸易数值: %s %s %q", period, id, cell.raw)
				}
				cell.number = &number
				counts[id]++
			}
			cells[id] = cell
		}
		rows[period] = cells
	}
	for _, id := range ids {
		if counts[id] == 0 {
			return nil, fmt.Errorf("FRED贸易序列无有效观测: %s", id)
		}
	}
	return rows, nil
}
