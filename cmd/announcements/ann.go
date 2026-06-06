package announcements

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/sina"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewAnnouncementsCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "announcements",
		Aliases:           []string{"ann"},
		Short:             "Show company announcements from CNINFO",
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              runAnnouncements,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().StringP("type", "t", "", "Filter by type: annual, halfyear, q1, q3")
	cmd.Flags().Bool("latest", false, "Show latest market-wide announcements (no stock filter)")
	cmd.Flags().IntP("page", "p", 1, "Page number")
	return cmd
}

func runAnnouncements(cmd *cobra.Command, args []string) error {
	var stockParam string
	var secName string

	latest, err := cmd.Flags().GetBool("latest")
	if err != nil {
		return err
	}
	if !latest {
		if len(args) != 1 {
			return fmt.Errorf("请提供证券代码，或使用 --latest 查看全市场公告")
		}
		key := args[0]
		secs := sina.Search(cmd.Context(), key)
		if len(secs) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "未找到证券: %s\n", key)
			return nil
		}
		sec := secs[0]
		secName = sec.Name

		orgID, _, err := cninfo.LookupOrgID(cmd.Context(), sec.Code)
		if err != nil {
			return fmt.Errorf("查找证券代码失败: %w", err)
		}
		stockParam = fmt.Sprintf("%s,%s", sec.Code, orgID)
	}

	// Determine category filter
	var category string
	typeStr, err := cmd.Flags().GetString("type")
	if err != nil {
		return err
	}
	switch strings.ToLower(typeStr) {
	case "annual":
		category = cninfo.CategoryAnnual
	case "halfyear":
		category = cninfo.CategoryHalfYear
	case "q1":
		category = cninfo.CategoryQ1
	case "q3":
		category = cninfo.CategoryQ3
	default:
		slog.DebugContext(cmd.Context(), "default category")
	}

	page, _ := cmd.Flags().GetInt("page")
	if page <= 0 {
		page = 1
	}

	req := &cninfo.QueryRequest{
		StockCode: stockParam,
		Category:  category,
		PageNum:   page,
	}
	resp, err := cninfo.QueryAnnouncements(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("查询公告失败: %w", err)
	}

	if resp == nil || len(resp.Data) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到公告\n")
		return nil
	}

	// Display header
	out := cmd.OutOrStdout()
	if secName != "" {
		fmt.Fprintf(out, "\n%s 公告 (共 %d 条)\n\n", secName, resp.Total)
	} else {
		fmt.Fprintf(out, "\n全市场最新公告 (共 %d 条)\n\n", resp.Total)
	}

	// Build table
	headers := []string{"公告日期", "公告标题", "类型", "大小"}
	data := make([][]string, 0, len(resp.Data))
	styles := make([][]tablewriter.Colors, 0, len(resp.Data))

	for _, a := range resp.Data {
		if a.ExistFlag != 0 || a.InvalidationFlag != 0 {
			continue
		}
		title := a.Title
		t := time.Unix(a.Time/1000, 0).Format("2006-01-02")
		typeName := a.TypeName
		if typeName == "" {
			typeName = "-"
		}
		size := formatSize(float64(a.AdjunctSize))
		data = append(data, []string{t, title, typeName, size})

		style := make([]tablewriter.Colors, len(headers))
		if strings.Contains(title, "年报") || strings.Contains(title, "年度报告") {
			style[1] = tablewriter.Colors{tablewriter.FgRedColor, tablewriter.Bold}
		}
		styles = append(styles, style)
	}

	table := tablewriter.NewWriter(out)
	table.SetHeader(headers)
	hs := make([]tablewriter.Colors, len(headers))
	for i := range headers {
		hs[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hs...)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")
	table.SetColWidth(60) // limit title width

	for i, row := range data {
		table.Rich(row, styles[i])
	}
	table.Render()

	// Pagination hint
	if resp.TotalPages > 1 {
		fmt.Fprintf(out, "\n第 %d/%d 页，使用 -p <页码> 翻页\n\n", page, resp.TotalPages)
	} else {
		fmt.Fprintln(out)
	}
	return nil
}

func formatSize(bytes float64) string {
	if bytes <= 0 {
		return "-"
	}
	if bytes >= 1_048_576 {
		return fmt.Sprintf("%.1fMB", bytes/1_048_576)
	}
	if bytes >= 1_024 {
		return fmt.Sprintf("%.1fKB", bytes/1_024)
	}
	return fmt.Sprintf("%.0fB", bytes)
}
