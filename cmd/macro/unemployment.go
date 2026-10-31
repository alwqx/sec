package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/nbs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUnemploymentCLI() *cobra.Command {
	command := &cobra.Command{Use: "unemployment", Short: "全国城镇调查失业率及可选年龄分组", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		ageGroups, _ := cmd.Flags().GetBool("age-groups")
		req := nbs.UnemploymentRequest{Begin: begin, End: end, AgeGroups: ageGroups}
		if err := req.Validate(); err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		items, err := client.Unemployment(cmd.Context(), req)
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.Unemployment(items, begin, end)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			if items == nil {
				items = []nbs.UnemploymentObservation{}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		return printUnemployment(cmd.OutOrStdout(), items)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，按口径版本分开年龄分组序列")
	command.Flags().Bool("age-groups", false, "同时查询年龄分组，并标记在校生口径变化与暂停期")
	return command
}

func printUnemployment(out io.Writer, items []nbs.UnemploymentObservation) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无城镇调查失业率数据")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "指标", "调查失业率（%）", "在校生口径", "说明"})
	labels := map[string]string{"urban-surveyed": "全国城镇", "age-16-24": "16—24岁", "age-25-29": "25—29岁", "age-30-59": "30—59岁", "age-25-59": "25—59岁（旧分组）"}
	scopes := map[string]string{"included-if-in-labour-force": "符合劳动力定义者计入", "excluded": "不含在校生", "not-applicable": "暂停发布"}
	for _, item := range items {
		value := "—"
		if item.Rate != nil {
			value = strconv.FormatFloat(*item.Rate, 'f', 1, 64)
		}
		table.Append([]string{item.Period, labels[item.Indicator], value, scopes[item.StudentScope], item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局；调查失业率水平，未季调，非登记失业率或同比增速。年龄分组口径变更前后不直接比较，空值为 —。")
	return err
}
