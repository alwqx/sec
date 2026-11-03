package insider

import (
	"fmt"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
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
	cmd.Flags().Bool("json", false, "Output JSON")
	return cmd
}

func runInsider(cmd *cobra.Command, args []string) error {
	key := args[0]
	secs, err := sina.Search(cmd.Context(), key)
	if err != nil {
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if len(secs) == 0 {
		if asJSON {
			return fmt.Errorf("未找到证券: %s", key)
		}
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
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"ex_code": sec.ExCode, "name": sec.Name, "truncated": truncated, "results": cninfo.ToRows(anns)})
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
		t := a.DateText()
		typeName := a.TypeName
		if typeName == "" {
			typeName = "-"
		}
		data = append(data, []string{t, title, typeName, formatSize(a.SizeBytes())})

		style := make([]tablewriter.Colors, len(headers))
		if strings.Contains(title, "增持") {
			style[1] = termtable.Up(false)
		} else if strings.Contains(title, "减持") {
			style[1] = termtable.Down(false)
		}
		styles = append(styles, style)
	}

	table := termtable.New(out, headers, termtable.Compact, termtable.ColWidth(60))
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
