package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newIndustrialCLI() *cobra.Command {
	command := &cobra.Command{Use: "industrial", Aliases: []string{"ip"}, Short: "规模以上工业增加值：单月同比、累计同比和季调环比", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		req := nbs.IndustrialRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		items, err := client.IndustrialProduction(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			if items == nil {
				items = []nbs.IndustrialObservation{}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		return printIndustrial(cmd.OutOrStdout(), items)
	}}
	addQueryFlags(command)
	return command
}

func printIndustrial(out io.Writer, items []nbs.IndustrialObservation) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无规模以上工业增加值数据")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "单月同比（%）", "累计同比（%）", "累计所属期", "季调环比（%）", "说明"})
	format := func(n *float64, precision int) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', precision, 64)
	}
	for _, item := range items {
		table.Append([]string{item.Period, format(item.YoY, 1), format(item.YTD, 1), item.YTDPeriod, format(item.MoM, 2), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局发布库及官方月度发布稿；均为扣除价格因素的实际增速。1—2月累计不拆成单月同比，不通过累计增速相减计算单月增速；环比采用可检索发布稿中的最新可用修订。空值为 —。")
	return err
}
