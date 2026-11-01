package insider

import (
	"fmt"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/sina"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewInsiderCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "insider",
		Aliases:           []string{"in"},
		Short:             "Show executive shareholding changes",
		SilenceUsage:      true,
		SilenceErrors:     true,
		Args:              cobra.ExactArgs(1),
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              runInsider,
	}
	return cmd
}

func runInsider(cmd *cobra.Command, args []string) error {
	key := args[0]
	secs := sina.Search(cmd.Context(), key)
	if len(secs) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到证券: %s\n", key)
		return nil
	}
	sec := secs[0]
	orgID, _, err := cninfo.LookupOrgID(cmd.Context(), sec.Code)
	if err != nil {
		return fmt.Errorf("查找证券代码失败: %w", err)
	}

	anns, truncated, err := cninfo.QueryInsiderAnnouncements(cmd.Context(), fmt.Sprintf("%s,%s", sec.Code, orgID))
	if err != nil {
		return fmt.Errorf("查询增减持公告失败: %w", err)
	}
	if len(anns) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到 %s 的增减持公告\n", sec.Name)
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n证券代码: %s  证券名称: %s\n", sec.ExCode, sec.Name)
	fmt.Fprintf(out, "增减持公告 (共 %d 条；按标题匹配，不是结构化交易记录)\n\n", len(anns))
	if truncated {
		fmt.Fprintln(out, "结果过多，只显示部分结果，部分年份可能缺失。")
	}

	headers := []string{"公告日期", "公告标题", "类型", "大小"}
	data := make([][]string, 0, len(anns))
	styles := make([][]tablewriter.Colors, 0, len(anns))

	for _, a := range anns {
		title := cninfo.CleanTitle(a.Title)
		t := time.Unix(a.Time/1000, 0).In(cninfo.Beijing).Format("2006-01-02")
		typeName := a.TypeName
		if typeName == "" {
			typeName = "-"
		}
		data = append(data, []string{t, title, typeName, formatSize(float64(a.AdjunctSize))})

		style := make([]tablewriter.Colors, len(headers))
		if strings.Contains(title, "增持") {
			style[1] = tablewriter.Colors{tablewriter.FgRedColor, tablewriter.Bold}
		} else if strings.Contains(title, "减持") {
			style[1] = tablewriter.Colors{tablewriter.FgGreenColor, tablewriter.Bold}
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
	table.SetColWidth(60)
	for i, row := range data {
		table.Rich(row, styles[i])
	}
	table.Render()
	fmt.Fprintln(out)
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
