package eastmoney

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const (
	reportValuation   = "RPT_VALUEANALYSIS_DET" // 个股每日估值
	reportPerformance = "RPT_LICO_FN_CPD"       // 业绩报表
	reportDividend    = "RPT_SHAREBONUS_DET"    // 分红送转
	// 数据中心查询为空时不返回空列表，而是返回这个错误码。
	datacenterEmptyCode = 9201
	// 最大的行业约 280 家，一页取完。
	boardPageSize = 1000
)

// ErrUnsupportedCode 表示证券不是沪深北市场的，数据中心查不了。
var ErrUnsupportedCode = errors.New("不支持的证券代码")

// ValuationPoint 是某个交易日的估值。PE、PB、PS 由东方财富计算；上游缺失时为 nil，亏损或净资产为负时为负数。
type ValuationPoint struct {
	Date      string // YYYY-MM-DD
	Name      string
	Board     string // 行业
	BoardCode string
	Close     float64
	MarketCap float64
	Shares    float64
	PE        *float64 // 市盈率 TTM
	PB        *float64 // 市净率 MRQ
	PS        *float64 // 市销率 TTM
}

// PerformanceReport 是一期业绩报表，利润和营收是报告期累计值。
type PerformanceReport struct {
	Date    string // YYYY-MM-DD
	EPS     *float64
	BPS     *float64
	ROE     *float64 // 加权净资产收益率，单位 %
	Revenue *float64
	Profit  *float64 // 归母净利润
}

// IsAnnual 判断是否为年报。
func (r *PerformanceReport) IsAnnual() bool {
	return strings.HasSuffix(r.Date, "-12-31")
}

// secuFilter 按 600036.SH 这样的代码过滤。代码在不同交易所可能重复（上证指数和平安银行都是 000001），不能只按数字查询。
func secuFilter(exCode string) (string, error) {
	exCode = strings.ToUpper(strings.TrimSpace(exCode))
	if len(exCode) != 8 || (exCode[:2] != "SH" && exCode[:2] != "SZ" && exCode[:2] != "BJ") {
		return "", fmt.Errorf("%w %s", ErrUnsupportedCode, exCode)
	}
	return fmt.Sprintf(`(SECUCODE="%s.%s")`, exCode[2:], exCode[:2]), nil
}

// GetValuationHistory 返回每日估值，按日期升序。上游没有数据时返回空列表。
func GetValuationHistory(ctx context.Context, exCode string) ([]*ValuationPoint, error) {
	filter, err := secuFilter(exCode)
	if err != nil {
		return nil, err
	}
	// 2018 年以来约 2100 个交易日，一页取完。
	rows, err := datacenterRows(ctx, reportValuation, "SECURITY_NAME_ABBR,BOARD_CODE,BOARD_NAME,TRADE_DATE,CLOSE_PRICE,TOTAL_MARKET_CAP,TOTAL_SHARES,PE_TTM,PB_MRQ,PS_TTM", filter, "TRADE_DATE", 5000)
	if err != nil {
		return nil, err
	}
	points := make([]*ValuationPoint, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		date, closePrice := rowDate(row, "TRADE_DATE"), rowNumber(row, "CLOSE_PRICE")
		if date == "" || closePrice == nil {
			continue
		}
		point := &ValuationPoint{Date: date, Close: *closePrice, PE: rowNumber(row, "PE_TTM"), PB: rowNumber(row, "PB_MRQ"), PS: rowNumber(row, "PS_TTM")}
		point.Name, _ = row["SECURITY_NAME_ABBR"].(string)
		point.Board, _ = row["BOARD_NAME"].(string)
		point.BoardCode, _ = row["BOARD_CODE"].(string)
		if value := rowNumber(row, "TOTAL_MARKET_CAP"); value != nil {
			point.MarketCap = *value
		}
		if value := rowNumber(row, "TOTAL_SHARES"); value != nil {
			point.Shares = *value
		}
		points = append(points, point)
	}
	return points, nil
}

// DividendPlan 是一条分红送转记录，包括还没有实施的预案。
type DividendPlan struct {
	ReportDate  string   // 报告期 YYYY-MM-DD
	ExDate      string   // 除权除息日，未确定时为空字符串
	Progress    string   // 进度，如 实施分配、董事会决议通过、股东大会决议通过
	Profile     string   // 方案说明
	CashPerTen  float64  // 每 10 股税前现金分红
	StockPerTen float64  // 每 10 股送转股数
	EPS         *float64 // 该报告期的基本每股收益，当时的口径
}

// Implemented 判断方案是否已经进入实施。
func (p *DividendPlan) Implemented() bool {
	return p.Progress == "实施分配"
}

// Current 判断方案是否还在有效期：报告期不早于交易日前 24 个月。更早的预案通常已经被放弃。
func (p *DividendPlan) Current(tradeDate string) bool {
	trade, err := time.Parse("2006-01-02", tradeDate)
	return err == nil && p.ReportDate != "" && p.ReportDate >= trade.AddDate(0, -24, 0).Format("2006-01-02")
}

// GetDividendPlans 返回分红送转记录。上游没有数据时返回空列表。
func GetDividendPlans(ctx context.Context, exCode string) ([]*DividendPlan, error) {
	filter, err := secuFilter(exCode)
	if err != nil {
		return nil, err
	}
	rows, err := datacenterRows(ctx, reportDividend, "REPORT_DATE,EX_DIVIDEND_DATE,ASSIGN_PROGRESS,IMPL_PLAN_PROFILE,PRETAX_BONUS_RMB,BONUS_IT_RATIO,BASIC_EPS", filter, "REPORT_DATE", 500)
	if err != nil {
		return nil, err
	}
	plans := make([]*DividendPlan, 0, len(rows))
	for _, row := range rows {
		plan := &DividendPlan{ReportDate: rowDate(row, "REPORT_DATE"), ExDate: rowDate(row, "EX_DIVIDEND_DATE"), EPS: rowNumber(row, "BASIC_EPS")}
		plan.Progress, _ = row["ASSIGN_PROGRESS"].(string)
		plan.Profile, _ = row["IMPL_PLAN_PROFILE"].(string)
		if value := rowNumber(row, "PRETAX_BONUS_RMB"); value != nil {
			plan.CashPerTen = *value
		}
		if value := rowNumber(row, "BONUS_IT_RATIO"); value != nil {
			plan.StockPerTen = *value
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// PeerValuation 是一家公司某个交易日的估值，用于同行业比较。倍数的含义同 ValuationPoint。
type PeerValuation struct {
	ExCode    string // SH600036
	Name      string
	MarketCap *float64
	PE        *float64
	PB        *float64
	PS        *float64
}

// GetBoardValuations 返回某个行业在某个交易日的估值，按总市值降序。上游没有数据时返回空列表。
func GetBoardValuations(ctx context.Context, boardCode, date string) ([]*PeerValuation, error) {
	filter := fmt.Sprintf(`(BOARD_CODE="%s")(TRADE_DATE='%s')`, boardCode, date)
	rows, err := datacenterRows(ctx, reportValuation, "SECUCODE,SECURITY_NAME_ABBR,TOTAL_MARKET_CAP,PE_TTM,PB_MRQ,PS_TTM", filter, "TOTAL_MARKET_CAP", boardPageSize)
	if err != nil {
		return nil, err
	}
	// 只取到一部分时分位和中位数都不对，不能当成完整结果。
	if len(rows) >= boardPageSize {
		return nil, fmt.Errorf("行业 %s 的公司超过 %d 家", boardCode, boardPageSize)
	}
	peers := make([]*PeerValuation, 0, len(rows))
	for _, row := range rows {
		secu, _ := row["SECUCODE"].(string)
		code, exchange, ok := strings.Cut(secu, ".")
		if !ok || code == "" || exchange == "" {
			continue
		}
		peer := &PeerValuation{ExCode: exchange + code, MarketCap: rowNumber(row, "TOTAL_MARKET_CAP"), PE: rowNumber(row, "PE_TTM"), PB: rowNumber(row, "PB_MRQ"), PS: rowNumber(row, "PS_TTM")}
		peer.Name, _ = row["SECURITY_NAME_ABBR"].(string)
		peers = append(peers, peer)
	}
	return peers, nil
}

// GetPerformanceReports 返回业绩报表，按报告期降序。上游没有数据时返回空列表。
func GetPerformanceReports(ctx context.Context, exCode string) ([]*PerformanceReport, error) {
	filter, err := secuFilter(exCode)
	if err != nil {
		return nil, err
	}
	rows, err := datacenterRows(ctx, reportPerformance, "REPORTDATE,BASIC_EPS,BPS,WEIGHTAVG_ROE,TOTAL_OPERATE_INCOME,PARENT_NETPROFIT", filter, "REPORTDATE", 500)
	if err != nil {
		return nil, err
	}
	reports := make([]*PerformanceReport, 0, len(rows))
	for _, row := range rows {
		date := rowDate(row, "REPORTDATE")
		if date == "" {
			continue
		}
		reports = append(reports, &PerformanceReport{
			Date: date, EPS: rowNumber(row, "BASIC_EPS"), BPS: rowNumber(row, "BPS"), ROE: rowNumber(row, "WEIGHTAVG_ROE"),
			Revenue: rowNumber(row, "TOTAL_OPERATE_INCOME"), Profit: rowNumber(row, "PARENT_NETPROFIT"),
		})
	}
	return reports, nil
}

func rowDate(row map[string]any, key string) string {
	text, _ := row[key].(string)
	if len(text) < 10 {
		return ""
	}
	return text[:10]
}

func rowNumber(row map[string]any, key string) *float64 {
	if value, ok := row[key].(float64); ok {
		return &value
	}
	return nil
}

// datacenterRows 查询一页数据中心报表，按 sortColumn 降序。上游没有数据时返回空列表。
func datacenterRows(ctx context.Context, report, columns, filter, sortColumn string, pageSize int) ([]map[string]any, error) {
	params := url.Values{}
	params.Set("reportName", report)
	params.Set("columns", columns)
	params.Set("filter", filter)
	params.Set("sortColumns", sortColumn)
	params.Set("sortTypes", "-1")
	params.Set("pageSize", fmt.Sprint(pageSize))
	params.Set("pageNumber", "1")
	reqURL := datacenterAPIBase + "?" + params.Encode()
	slog.DebugContext(ctx, "datacenterRows", "reqURL", reqURL)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqURL, nil, nil, 0)
	if err != nil {
		slog.ErrorContext(ctx, "failed request", "url", reqURL, "error", err)
		return nil, err
	}
	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	jsonData, err := stripJSONP(body)
	if err != nil {
		return nil, fmt.Errorf("strip JSONP: %w", err)
	}
	var data finReportResponse
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if data.Code == datacenterEmptyCode {
		return nil, nil
	}
	if !data.Success {
		message := data.Message
		if message == "" {
			message = "unknown error"
		}
		return nil, fmt.Errorf("API error: %s", message)
	}
	if data.Result == nil {
		return nil, nil
	}
	return data.Result.Data, nil
}
