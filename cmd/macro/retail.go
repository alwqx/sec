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

func newRetailCLI() *cobra.Command {
	command := &cobra.Command{Use: "retail", Short: "社会消费品零售总额：当月金额、同比、累计金额和累计同比", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		req := nbs.RetailRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		items, err := client.RetailSales(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.Retail(items, begin, end)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(dataset)
		}
		if asJSON {
			if items == nil {
				items = []nbs.RetailObservation{}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		return printRetail(cmd.OutOrStdout(), items)
	}}
	addQueryFlags(command)
	command.Flags().Bool("standard-json", false, "按统一宏观序列格式输出JSON，区分单月与累计所属期")
	command.MarkFlagsMutuallyExclusive("json", "standard-json")
	return command
}

func printRetail(out io.Writer, items []nbs.RetailObservation) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无社会消费品零售总额数据")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "当月金额（亿元）", "同比（%）", "累计金额（亿元）", "累计同比（%）", "累计所属期", "说明"})
	format := func(n *float64) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', 1, 64)
	}
	for _, item := range items {
		table.Append([]string{item.Period, format(item.Amount), format(item.YoY), format(item.YTDAmount), format(item.YTDGrowth), item.YTDPeriod, item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局；全国总额，非限额以上单位口径；金额为现价，增速为未扣除价格因素的名义同比，未季调。1—2月合并累计不拆成单月值，不用累计值推算缺失值。空值为 —。")
	return err
}
