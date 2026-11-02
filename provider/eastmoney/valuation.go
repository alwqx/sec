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

	"github.com/alwqx/sec/utils"
)

const (
	reportValuation   = "RPT_VALUEANALYSIS_DET" // 个股每日估值
	reportPerformance = "RPT_LICO_FN_CPD"       // 业绩报表
	// 数据中心查询为空时不返回空列表，而是返回这个错误码。
	datacenterEmptyCode = 9201
)

// ErrUnsupportedCode 表示证券不是沪深北市场的，数据中心查不了。
var ErrUnsupportedCode = errors.New("不支持的证券代码")

// ValuationPoint 是某个交易日的估值。PE、PB、PS 由东方财富计算；上游缺失时为 nil，亏损或净资产为负时为负数。
type ValuationPoint struct {
	Date      string // YYYY-MM-DD
	Name      string
	Board     string // 行业
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
	rows, err := datacenterRows(ctx, reportValuation, "SECURITY_NAME_ABBR,BOARD_NAME,TRADE_DATE,CLOSE_PRICE,TOTAL_MARKET_CAP,TOTAL_SHARES,PE_TTM,PB_MRQ,PS_TTM", filter, "TRADE_DATE", 5000)
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
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", report, resp.StatusCode)
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
