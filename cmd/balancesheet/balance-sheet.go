package balancesheet

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewBalanceSheetCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "balance-sheet",
		Aliases:       []string{"bs"},
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
	cmd.Flags().StringP("period", "p", "annual", "Period filter: annual, halfyear, q1, q3")
	cmd.Flags().StringP("output", "o", "", "Export to file (.csv or .json)")

	return cmd
}

// result holds fetched report data for a specific type.
type result struct {
	rt    eastmoney.FinancialReportType
	items []*eastmoney.FinancialReportItem
}

func BalanceSheetHandler(cmd *cobra.Command, args []string) error {

	key := args[0]
	secs := sina.Search(cmd.Context(), key)
	if len(secs) == 0 {
		slog.Info("search no sec", "code", key)
		return nil
	}
	sec := secs[0]

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

	// Fetch data
	var results []result
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
			if err != nil {
				slog.Warn("failed fetch report", "type", rt.DisplayName(), "error", err)
				continue
			}
			results = append(results, result{rt: rt, items: items})
		}
	}

	// Check output flag
	outputPath, _ := cmd.Flags().GetString("output")
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

	return nil
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
	table := tablewriter.NewWriter(cmd.OutOrStdout())
	table.SetHeader(headers)

	headerStyles := make([]tablewriter.Colors, 0, len(headers))
	for range headers {
		headerStyles = append(headerStyles, tablewriter.Colors{tablewriter.Bold})
	}
	table.SetHeaderColor(headerStyles...)

	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")

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

func exportJSON(results []result, path string) error {
	type exportItem struct {
		ReportType   string                 `json:"report_type"`
		ReportDate   string                 `json:"report_date"`
		SecurityCode string                 `json:"security_code"`
		SecurityName string                 `json:"security_name"`
		PeriodCode   string                 `json:"period_code"`
		Fields       map[string]interface{} `json:"fields"`
	}

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

	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
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
	case "BASIC_EPS", "DILUTED_EPS":
		if n, ok := v.(float64); ok {
			return fmt.Sprintf("%.2f", n)
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
