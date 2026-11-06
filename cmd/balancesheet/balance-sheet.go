package balancesheet

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

func NewBalanceSheetCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "financials",
		Aliases:       []string{"bs", "balance-sheet"},
		Short:         "Print financial statements of specific security",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Args: cobra.ExactArgs(1),
		RunE: BalanceSheetHandler,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().StringP("type", "t", "", "Report type: balance, income, cashflow")
	cmd.Flags().StringP("period", "p", "annual", "Period filter: annual, halfyear, q1, q3; US stocks: annual, quarter")
	cmd.Flags().StringP("output", "o", "", "Export to file (.csv or .json)")
	cmd.Flags().Bool("json", false, "Output JSON to stdout, with all raw fields")
	cmd.MarkFlagsMutuallyExclusive("output", "json")

	return cmd
}

// result holds fetched report data for a specific type.
type result struct {
	rt    eastmoney.FinancialReportType
	items []*eastmoney.FinancialReportItem
}

func BalanceSheetHandler(cmd *cobra.Command, args []string) error {

	key := args[0]
	secs, err := sina.Resolve(cmd.Context(), key)
	if err != nil {
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	// 找不到证券在文本、导出和 JSON 模式下都算失败。
	if len(secs) == 0 {
		return fmt.Errorf("未找到证券: %s", key)
	}
	sec := secs[0]
	// 美股的财报来自 SEC XBRL，报表结构和东方财富的 A 股报表不同。
	if strings.HasPrefix(sec.ExCode, "$") {
		return usFinancialsHandler(cmd, sec.ExCode)
	}

	// Determine which report types to fetch
	var reportTypes []eastmoney.FinancialReportType
	typeStr, _ := cmd.Flags().GetString("type")
	if typeStr != "" {
		rt, ok := eastmoney.ReportFromString(typeStr)
		if !ok {
			return fmt.Errorf("invalid report type: %s (use: balance, income, cashflow)", typeStr)
		}
		reportTypes = []eastmoney.FinancialReportType{rt}
	}

	// Parse period filter
	periodFilter := ""
	if periodStr, _ := cmd.Flags().GetString("period"); periodStr != "" {
		periodFilter = eastmoney.PeriodFromString(periodStr)
		if periodFilter == "" {
			return fmt.Errorf("invalid period: %s (use: annual, halfyear, q1, q3)", periodStr)
		}
	}

	outputPath, _ := cmd.Flags().GetString("output")

	// Fetch data
	var results []result
	var failures []string
	if len(reportTypes) > 0 {
		// Specific type requested
		items, err := eastmoney.GetFinancialReport(cmd.Context(), &eastmoney.GetFinancialReportReq{
			Code:       sec.Code,
			ReportType: reportTypes[0],
			Period:     periodFilter,
		})
		if err != nil {
			return err
		}
		results = append(results, result{rt: reportTypes[0], items: items})
	} else {
		// No type specified: fetch all three
		for _, rt := range eastmoney.AllReportTypes() {
			items, err := eastmoney.GetFinancialReport(cmd.Context(), &eastmoney.GetFinancialReportReq{
				Code:       sec.Code,
				ReportType: rt,
				Period:     periodFilter,
			})
			// 按 Ctrl-C 取消不是报表失败。
			if cmd.Context().Err() != nil {
				return nil
			}
			if err != nil {
				// JSON 和导出文件不能少一类报表；文本先显示拿到的，最后报错。
				if asJSON || outputPath != "" {
					return fmt.Errorf("获取%s失败: %w", rt.DisplayName(), err)
				}
				failures = append(failures, fmt.Sprintf("%s获取失败（%v）", rt.DisplayName(), err))
				continue
			}
			results = append(results, result{rt: rt, items: items})
		}
	}
	partial := func() error {
		if len(failures) == 0 {
			return nil
		}
		return errors.New("部分财报没有获取到：" + strings.Join(failures, "；"))
	}

	// 上游查询为空时返回空列表；没有任何数据时不能写出空报告，也不能当成成功。
	if !hasReports(results) {
		if err := partial(); err != nil {
			return err
		}
		return fmt.Errorf("%s 没有符合条件的财报数据", sec.ExCode)
	}

	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"ex_code": sec.ExCode, "name": sec.Name, "results": exportItems(results)})
	}

	if outputPath != "" {
		return exportToFile(results, outputPath)
	}

	// Print header
	fmt.Fprintf(cmd.OutOrStdout(), "证券代码: %s  证券名称: %s", sec.ExCode, secs[0].Name)
	if len(reportTypes) == 1 {
		fmt.Fprintf(cmd.OutOrStdout(), "  类型: %s", reportTypes[0].DisplayName())
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout())

	// Render tables
	if len(reportTypes) == 0 {
		// Summary view: list all available reports
		printSummary(cmd, results)
	} else {
		// Detailed view for a specific type
		printDetailed(cmd, results[0])
	}

	return partial()
}

// printSummary prints a summary list of available reports across all types.
func printSummary(cmd *cobra.Command, results []result) {
	headers := []string{"报表类型", "报告期", "报告日期"}
	data := make([][]string, 0)

	for _, r := range results {
		for _, item := range r.items {
			data = append(data, []string{
				r.rt.DisplayName(),
				eastmoney.PeriodDisplayName(item.PeriodCode),
				formatDate(item.ReportDate),
			})
		}
	}

	if len(data) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "无财务报表数据")
		return
	}

	renderTable(cmd, headers, data)
}

// printDetailed prints key fields for a specific report type.
func printDetailed(cmd *cobra.Command, r result) {
	fields := eastmoney.KeyFields(r.rt)
	if len(fields) == 0 || len(r.items) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "无%s数据\n", r.rt.DisplayName())
		return
	}

	headers := make([]string, 0, len(fields))
	for _, f := range fields {
		headers = append(headers, f.CN)
	}

	data := make([][]string, 0, len(r.items))
	for _, item := range r.items {
		row := make([]string, 0, len(fields))
		for _, f := range fields {
			row = append(row, formatFieldValue(f.API, item.Fields[f.API]))
		}
		data = append(data, row)
	}

	renderTable(cmd, headers, data)
}

// renderTable outputs a table to stdout using tablewriter.
func renderTable(cmd *cobra.Command, headers []string, data [][]string) {
	table := termtable.New(cmd.OutOrStdout(), headers, termtable.LeftHeader, termtable.Compact)

	for _, row := range data {
		table.Append(row)
	}

	table.Render()
}

// exportToFile exports financial data to a CSV or JSON file.
func exportToFile(results []result, path string) error {
	ext := strings.ToLower(path)
	switch {
	case strings.HasSuffix(ext, ".csv"):
		return exportCSV(results, path)
	case strings.HasSuffix(ext, ".json"):
		return exportJSON(results, path)
	default:
		return fmt.Errorf("unsupported output format: %s (use .csv or .json)", path)
	}
}

func exportCSV(results []result, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// UTF-8 BOM for Excel compatibility
	_, err = f.Write([]byte{0xEF, 0xBB, 0xBF})
	if err != nil {
		return err
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	for _, r := range results {
		if len(r.items) == 0 {
			continue
		}

		// Collect all field names from all items
		fieldSet := make(map[string]struct{})
		for _, item := range r.items {
			for k := range item.Fields {
				fieldSet[k] = struct{}{}
			}
		}
		fieldNames := make([]string, 0, len(fieldSet))
		for k := range fieldSet {
			fieldNames = append(fieldNames, k)
		}

		// Section header
		w.Write([]string{fmt.Sprintf("# %s", r.rt.DisplayName())})
		w.Write(fieldNames)

		for _, item := range r.items {
			row := make([]string, len(fieldNames))
			for i, name := range fieldNames {
				row[i] = csvField(item.Fields[name])
			}
			w.Write(row)
		}
		// Blank line between sections
		w.Write([]string{})
	}

	return nil
}

type exportItem struct {
	ReportType   string                 `json:"report_type"`
	ReportDate   string                 `json:"report_date"`
	SecurityCode string                 `json:"security_code"`
	SecurityName string                 `json:"security_name"`
	PeriodCode   string                 `json:"period_code"`
	Fields       map[string]interface{} `json:"fields"`
}

func exportJSON(results []result, path string) error {
	data, err := json.MarshalIndent(exportItems(results), "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func exportItems(results []result) []exportItem {
	all := make([]exportItem, 0)
	for _, r := range results {
		for _, item := range r.items {
			all = append(all, exportItem{
				ReportType:   r.rt.DisplayName(),
				ReportDate:   item.ReportDate,
				SecurityCode: item.SecurityCode,
				SecurityName: item.SecurityName,
				PeriodCode:   item.PeriodCode,
				Fields:       item.Fields,
			})
		}
	}
	return all
}

func csvField(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// formatDate trims the time portion from a date string.
func formatDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// formatFieldValue formats a field value for terminal display.
func formatFieldValue(apiField string, v interface{}) string {
	if v == nil {
		return "-"
	}

	switch apiField {
	case "REPORT_DATE":
		if s, ok := v.(string); ok {
			return formatDate(s)
		}
	default:
		if n, ok := v.(float64); ok {
			return utils.HumanNum(n)
		}
	}

	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func hasReports(results []result) bool {
	for _, item := range results {
		if len(item.items) > 0 {
			return true
		}
	}
	return false
}
