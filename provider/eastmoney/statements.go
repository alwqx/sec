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
	"time"

	"github.com/alwqx/sec/utils"
)

// f10Base 是个股 F10 的完整财务报表接口。数据中心的财报只有约 50 个汇总科目，拆不出研发费用和银行的收入结构。
var f10Base = "https://emweb.securities.eastmoney.com/PC_HSF10/NewFinanceAnalysis"

// CompanyType 是 F10 报表的格式，类型不对时接口返回错误或空科目。
type CompanyType int

const (
	CompanySecurities CompanyType = 1 // 证券
	CompanyInsurance  CompanyType = 2 // 保险
	CompanyBank       CompanyType = 3 // 银行
	CompanyGeneral    CompanyType = 4 // 一般企业
)

// Statement 是三张报表之一。
type Statement string

const (
	StatementIncome  Statement = "lrb"   // 利润表
	StatementBalance Statement = "zcfzb" // 资产负债表
	StatementCash    Statement = "xjllb" // 现金流量表
)

// f10DatesPerRequest 是接口一次最多返回的报告期数，多传的会被忽略。
const f10DatesPerRequest = 5

// CompanyTypeOf 按东方财富行业判断报表格式。F10 接口用一般企业格式查银行也会返回数据，只是科目大多为空，所以不能靠试探判断。
func CompanyTypeOf(ctx context.Context, exCode string) (CompanyType, error) {
	filter, err := secuFilter(exCode)
	if err != nil {
		return 0, err
	}
	rows, err := datacenterRows(ctx, reportValuation, "BOARD_NAME,TRADE_DATE", filter, "TRADE_DATE", 1)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return CompanyGeneral, nil
	}
	board, _ := rows[0]["BOARD_NAME"].(string)
	return companyTypeOfBoard(board), nil
}

func companyTypeOfBoard(board string) CompanyType {
	switch {
	case strings.Contains(board, "银行"):
		return CompanyBank
	case strings.Contains(board, "证券"):
		return CompanySecurities
	case strings.Contains(board, "保险"):
		return CompanyInsurance
	}
	return CompanyGeneral
}

// StatementDates 返回有利润表的报告期，按日期降序。annual 为 true 时只返回年报。
func StatementDates(ctx context.Context, exCode string, kind CompanyType, annual bool) ([]string, error) {
	dateType := "0"
	if annual {
		dateType = "1"
	}
	params := url.Values{"companyType": {fmt.Sprint(int(kind))}, "reportDateType": {dateType}, "code": {strings.ToUpper(exCode)}}
	rows, err := f10Rows(ctx, "lrbDateAjaxNew", params)
	if err != nil {
		return nil, err
	}
	dates := make([]string, 0, len(rows))
	for _, row := range rows {
		if date := rowDate(row, "REPORT_DATE"); date != "" {
			dates = append(dates, date)
		}
	}
	return dates, nil
}

// GetStatements 返回指定报告期的报表，键是报告期 YYYY-MM-DD。singleQuarter 为 true 时利润表和现金流量表是单季值，资产负债表总是期末值。
func GetStatements(ctx context.Context, exCode string, kind CompanyType, statement Statement, singleQuarter bool, dates []string) (map[string]map[string]any, error) {
	reportType := "1"
	if singleQuarter && statement != StatementBalance {
		reportType = "2"
	}
	result := map[string]map[string]any{}
	for begin := 0; begin < len(dates); begin += f10DatesPerRequest {
		chunk := dates[begin:min(begin+f10DatesPerRequest, len(dates))]
		params := url.Values{"companyType": {fmt.Sprint(int(kind))}, "reportDateType": {"0"}, "reportType": {reportType}, "dates": {strings.Join(chunk, ",")}, "code": {strings.ToUpper(exCode)}}
		rows, err := f10Rows(ctx, string(statement)+"AjaxNew", params)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if date := rowDate(row, "REPORT_DATE"); date != "" {
				result[date] = row
			}
		}
	}
	return result, nil
}

// f10Limit 限制同时发出的请求数，接口在短时间请求过多时会直接断开连接。
var f10Limit = make(chan struct{}, 2)

// f10Rows 请求一次 F10 接口，连接失败时重试一次。类型不对时接口返回不含 data 的对象，按没有数据处理。
func f10Rows(ctx context.Context, path string, params url.Values) ([]map[string]any, error) {
	reqURL := f10Base + "/" + path + "?" + params.Encode()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		rows, err := f10Request(ctx, reqURL)
		if err == nil {
			return rows, nil
		}
		lastErr = err
		slog.WarnContext(ctx, "F10 报表请求失败", "url", reqURL, "attempt", attempt+1, "error", err)
	}
	return nil, lastErr
}

func f10Request(ctx context.Context, reqURL string) ([]map[string]any, error) {
	select {
	case f10Limit <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-f10Limit }()
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqURL, nil, nil, 0)
	if err != nil {
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
	var data struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("F10 报表不是 JSON: %w", err)
	}
	return data.Data, nil
}
