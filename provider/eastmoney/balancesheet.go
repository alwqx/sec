package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/alwqx/sec/utils"
)

const datacenterAPIBase = "https://datacenter-web.eastmoney.com/api/data/v1/get"

// FinancialReportType represents the type of financial report.
type FinancialReportType string

const (
	ReportBalance  FinancialReportType = "RPT_DMSK_FN_BALANCE"  // 资产负债表
	ReportIncome   FinancialReportType = "RPT_DMSK_FN_INCOME"   // 利润表
	ReportCashFlow FinancialReportType = "RPT_DMSK_FN_CASHFLOW" // 现金流量表
)

// AllReportTypes returns all three report types.
func AllReportTypes() []FinancialReportType {
	return []FinancialReportType{ReportBalance, ReportIncome, ReportCashFlow}
}

// DisplayName returns the Chinese display name for a report type.
func (t FinancialReportType) DisplayName() string {
	switch t {
	case ReportBalance:
		return "资产负债表"
	case ReportIncome:
		return "利润表"
	case ReportCashFlow:
		return "现金流量表"
	default:
		return string(t)
	}
}

// ReportFromString converts a user-provided type string to FinancialReportType.
func ReportFromString(s string) (FinancialReportType, bool) {
	switch strings.ToLower(s) {
	case "balance", "bs":
		return ReportBalance, true
	case "income", "is":
		return ReportIncome, true
	case "cashflow", "cf":
		return ReportCashFlow, true
	default:
		return "", false
	}
}

// Period constants for DATE_TYPE_CODE filter.
const (
	PeriodAnnual   = "001"
	PeriodHalfYear = "002"
	PeriodQ1       = "003"
	PeriodQ3       = "004"
)

// PeriodDisplayName returns the Chinese name for a period code.
func PeriodDisplayName(code string) string {
	switch code {
	case PeriodAnnual:
		return "年报"
	case PeriodHalfYear:
		return "中报"
	case PeriodQ1:
		return "一季报"
	case PeriodQ3:
		return "三季报"
	default:
		return code
	}
}

// PeriodFromString converts a user-provided period string to a period code.
func PeriodFromString(s string) string {
	switch strings.ToLower(s) {
	case "annual", "yearly", "y":
		return PeriodAnnual
	case "halfyear", "half", "h":
		return PeriodHalfYear
	case "q1":
		return PeriodQ1
	case "q3":
		return PeriodQ3
	default:
		return ""
	}
}

// FinancialReportItem represents a single financial report record.
type FinancialReportItem struct {
	ReportDate   string
	SecurityCode string
	SecurityName string
	PeriodCode   string // DATE_TYPE_CODE
	Fields       map[string]interface{}
}

// keyField defines a display field mapping from API field name to Chinese label.
type keyField struct {
	API string
	CN  string
}

// KeyFields returns the key display fields for a given report type.
func KeyFields(rt FinancialReportType) []keyField {
	switch rt {
	case ReportBalance:
		return []keyField{
			{"REPORT_DATE", "报告期"},
			{"TOTAL_ASSETS", "资产总计"},
			{"TOTAL_LIABILITIES", "负债合计"},
			{"TOTAL_EQUITY", "所有者权益"},
			{"TOTAL_CURRENT_ASSETS", "流动资产"},
			{"TOTAL_NON_CURRENT_ASSETS", "非流动资产"},
			{"TOTAL_CURRENT_LIABILITIES", "流动负债"},
			{"TOTAL_NON_CURRENT_LIABILITIES", "非流动负债"},
			{"MONETARYFUNDS", "货币资金"},
		}
	case ReportIncome:
		return []keyField{
			{"REPORT_DATE", "报告期"},
			{"TOTAL_OPERATE_INCOME", "营业总收入"},
			{"OPERATE_PROFIT", "营业利润"},
			{"TOTAL_PROFIT", "利润总额"},
			{"INCOME_TAX", "所得税"},
			{"PARENT_NETPROFIT", "归属母公司净利润"},
			{"BASIC_EPS", "基本每股收益"},
		}
	case ReportCashFlow:
		return []keyField{
			{"REPORT_DATE", "报告期"},
			{"NETCASH_OPERATE", "经营活动现金流"},
			{"NETCASH_INVEST", "投资活动现金流"},
			{"NETCASH_FINANCE", "筹资活动现金流"},
			{"CASH_EQUIVALENTS_INCREASE", "现金净增加额"},
		}
	default:
		return nil
	}
}

// GetFinancialReportReq request for fetching financial reports.
type GetFinancialReportReq struct {
	Code       string              // security code, e.g. "600036"
	ReportType FinancialReportType // report type to fetch
	Period     string              // period filter, "" for all
}

// GetFinancialReport fetches financial report data from East Money datacenter API.
func GetFinancialReport(ctx context.Context, req *GetFinancialReportReq) ([]*FinancialReportItem, error) {
	if req == nil {
		return nil, fmt.Errorf("req is nil")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`(SECURITY_CODE="%s")`, req.Code))
	if req.Period != "" {
		sb.WriteString(fmt.Sprintf(`(DATE_TYPE_CODE="%s")`, req.Period))
	}

	params := url.Values{}
	params.Set("reportName", string(req.ReportType))
	params.Set("columns", "ALL")
	params.Set("filter", sb.String())
	params.Set("sortColumns", "REPORT_DATE")
	params.Set("sortTypes", "-1")
	params.Set("pageSize", "50")
	params.Set("pageNumber", "1")

	reqURL := fmt.Sprintf("%s?%s", datacenterAPIBase, params.Encode())
	slog.DebugContext(ctx, "GetFinancialReport", "reqURL", reqURL)

	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqURL, nil, nil, 0)
	if err != nil {
		slog.ErrorContext(ctx, "failed request", "url", reqURL, "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	jsonData, err := stripJSONP(body)
	if err != nil {
		return nil, fmt.Errorf("strip JSONP: %w", err)
	}

	var apiResp finReportResponse
	if err := json.Unmarshal(jsonData, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if !apiResp.Success {
		msg := apiResp.Message
		if msg == "" {
			msg = "unknown error"
		}
		return nil, fmt.Errorf("API error: %s", msg)
	}

	if apiResp.Result == nil {
		return nil, nil
	}

	items := make([]*FinancialReportItem, 0, len(apiResp.Result.Data))
	for _, d := range apiResp.Result.Data {
		item := &FinancialReportItem{Fields: d}
		if v, ok := d["REPORT_DATE"].(string); ok {
			item.ReportDate = v
		}
		if v, ok := d["SECURITY_CODE"].(string); ok {
			item.SecurityCode = v
		}
		if v, ok := d["SECURITY_NAME_ABBR"].(string); ok {
			item.SecurityName = v
		}
		if v, ok := d["DATE_TYPE_CODE"].(string); ok {
			item.PeriodCode = v
		}
		items = append(items, item)
	}

	return items, nil
}

// finReportResponse is the API response envelope.
type finReportResponse struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Result  *finReportResult `json:"result"`
}

// finReportResult holds the paginated data.
type finReportResult struct {
	Pages int                      `json:"pages"`
	Data  []map[string]interface{} `json:"data"`
	Count int                      `json:"count"`
}

// stripJSONP removes the JSONP wrapper from a response body.
// East Money returns: jQuery1123061445791234567890_1234567890({...});
func stripJSONP(data []byte) ([]byte, error) {
	s := strings.TrimSpace(string(data))
	if len(s) == 0 {
		return data, nil
	}

	// If it doesn't look like JSONP, return as-is
	if s[0] == '{' || s[0] == '[' {
		return data, nil
	}

	start := strings.IndexByte(s, '(')
	if start == -1 {
		return data, nil
	}

	end := strings.LastIndexByte(s, ')')
	if end == -1 || end <= start {
		return nil, fmt.Errorf("invalid JSONP: missing closing paren")
	}

	return []byte(strings.TrimSpace(s[start+1 : end])), nil
}
