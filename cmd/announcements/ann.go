package announcements

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
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
	cmd.Flags().Bool("json", false, "Output JSON")
	cmd.Flags().String("summary", "", "摘要当前页的公告序号（从 1 开始）或巨潮 PDF 链接")
	cmd.Flags().String("ai", "", "摘要使用的模型配置名，留空使用默认配置")
	return cmd
}

func runAnnouncements(cmd *cobra.Command, args []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	selector, _ := cmd.Flags().GetString("summary")
	if cmd.Flags().Changed("ai") && selector == "" {
		return fmt.Errorf("--ai 只能和 --summary 一起用")
	}
	if cmd.Flags().Changed("summary") && selector == "" {
		return fmt.Errorf("--summary 需要公告序号或巨潮 PDF 链接")
	}
	var summarizer *llm.AnnouncementSummarizer
	var index int
	if selector != "" {
		var direct string
		var err error
		index, direct, err = summarySelector(selector)
		if err != nil {
			return err
		}
		if direct != "" && len(args) > 1 {
			return fmt.Errorf("最多提供一个证券代码")
		}
		if index > 0 {
			latest, _ := cmd.Flags().GetBool("latest")
			if (!latest && len(args) != 1) || (latest && len(args) > 0) {
				return fmt.Errorf("按序号摘要需要证券代码，或使用 --latest")
			}
		}
		summarizer, err = openSummarizer(cmd)
		if err != nil {
			return err
		}
		if direct != "" {
			return printSummary(cmd, summarizer, direct, asJSON)
		}
	}
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
		secs, err := sina.Resolve(cmd.Context(), key)
		if err != nil {
			return fmt.Errorf("搜索证券失败: %w", err)
		}
		if len(secs) == 0 {
			if asJSON || summarizer != nil {
				return fmt.Errorf("未找到证券: %s", key)
			}
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
	if summarizer != nil {
		var rows []cninfo.Row
		if resp != nil {
			rows = cninfo.ToRows(resp.Data)
		}
		if index > len(rows) {
			return fmt.Errorf("公告序号超出当前页范围（本页 %d 条），请先查看公告列表", len(rows))
		}
		// 没有 PDF 附件的公告只有详情页链接，提前说明，不让用户看到要求提供 PDF 链接的错误。
		target := rows[index-1].URL
		if _, err := cninfo.DocumentURL(target); err != nil {
			return fmt.Errorf("第 %d 条公告没有 PDF 原文，无法摘要", index)
		}
		return printSummary(cmd, summarizer, target, asJSON)
	}

	if asJSON {
		output := map[string]any{"page": page, "total": 0, "total_pages": 0, "results": []cninfo.Row{}}
		if resp != nil {
			output["total"], output["total_pages"], output["results"] = resp.Total, resp.TotalPages, cninfo.ToRows(resp.Data)
		}
		return utils.PrintJSON(cmd.OutOrStdout(), output)
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
	headers := []string{"序号", "公告日期", "公告标题", "类型", "大小"}
	data := make([][]string, 0, len(resp.Data))
	styles := make([][]tablewriter.Colors, 0, len(resp.Data))

	for _, a := range resp.Data {
		if a == nil || a.ExistFlag != 0 || a.InvalidationFlag != 0 {
			continue
		}
		title := a.Title
		t := a.DateText()
		typeName := a.TypeName
		if typeName == "" {
			typeName = "-"
		}
		size := formatSize(a.SizeBytes())
		data = append(data, []string{fmt.Sprint(len(data) + 1), t, title, typeName, size})

		style := make([]tablewriter.Colors, len(headers))
		if strings.Contains(title, "年报") || strings.Contains(title, "年度报告") {
			style[2] = tablewriter.Colors{tablewriter.FgRedColor, tablewriter.Bold}
		}
		styles = append(styles, style)
	}

	table := termtable.New(out, headers, termtable.Compact, termtable.ColWidth(60))

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
