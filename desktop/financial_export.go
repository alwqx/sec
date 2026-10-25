//go:build windows || darwin

package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
)

type financialExportRequest struct {
	Code   string `json:"code"`
	Report string `json:"report"`
	Period string `json:"period"`
	Format string `json:"format"`
}

func writeFinancialExport(w io.Writer, items []*eastmoney.FinancialReportItem, reportType eastmoney.FinancialReportType, format string) error {
	if format == "json" {
		records := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if item == nil {
				continue
			}
			records = append(records, map[string]any{"report_type": reportType.DisplayName(), "report_date": item.ReportDate, "security_code": item.SecurityCode, "security_name": item.SecurityName, "period_code": item.PeriodCode, "fields": item.Fields})
		}
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(records)
	}
	if format != "csv" {
		return fmt.Errorf("不支持的导出格式")
	}
	if _, err := io.WriteString(w, "\ufeff"); err != nil {
		return err
	}
	fields := map[string]bool{}
	for _, item := range items {
		if item != nil {
			for field := range item.Fields {
				fields[field] = true
			}
		}
	}
	names := make([]string, 0, len(fields))
	for field := range fields {
		names = append(names, field)
	}
	sort.Strings(names)
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"# " + reportType.DisplayName()}); err != nil {
		return err
	}
	if err := writer.Write(names); err != nil {
		return err
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		row := make([]string, len(names))
		for i, field := range names {
			value := item.Fields[field]
			if value == nil {
				continue
			}
			row[i] = fmt.Sprint(value)
			// 文本按文本导出，避免外部字符串被电子表格当作公式执行；负数数值保持原值。
			if text, ok := value.(string); ok {
				trimmed := strings.TrimLeft(text, " \t\r\n")
				if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
					row[i] = "'" + text
				}
			}
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func (s *desktopServer) handleFinancialExport(w http.ResponseWriter, r *http.Request) {
	var request financialExportRequest
	if !s.readDownloadRequest(w, r, &request) {
		return
	}
	request.Code = strings.ToUpper(strings.TrimSpace(request.Code))
	if !aShareCode.MatchString(request.Code) {
		writeError(w, 400, "财报导出暂仅支持沪深北 A 股")
		return
	}
	reportType, ok := eastmoney.ReportFromString(request.Report)
	if !ok {
		writeError(w, 400, "不支持的财报类型")
		return
	}
	period := eastmoney.PeriodFromString(request.Period)
	if request.Period != "" && period == "" {
		writeError(w, 400, "不支持的报告期")
		return
	}
	if request.Format != "csv" && request.Format != "json" {
		writeError(w, 400, "导出格式必须为 CSV 或 JSON")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	items, err := eastmoney.GetFinancialReport(ctx, &eastmoney.GetFinancialReportReq{Code: request.Code[2:], ReportType: reportType, Period: period})
	if err != nil {
		slog.ErrorContext(ctx, "导出财报查询失败", "error", err)
		writeError(w, 502, "财报数据获取失败，请稍后重试")
		return
	}
	if len(items) == 0 {
		writeError(w, 404, "当前筛选条件没有可导出的财报")
		return
	}
	periodName := request.Period
	if periodName == "" {
		periodName = "all"
	}
	filename := fmt.Sprintf("%s_%s_%s.%s", request.Code, request.Report, periodName, request.Format)
	saved, err := s.saveDownload(filename, func(output io.Writer) error { return writeFinancialExport(output, items, reportType, request.Format) })
	if err != nil {
		slog.ErrorContext(ctx, "保存财报失败", "error", err)
		writeError(w, 500, "保存财报失败，请检查下载目录权限并重试")
		return
	}
	writeJSON(w, saved)
}
