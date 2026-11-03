package bond

import (
	"fmt"
	"io"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/provider/bond"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewBondHistoryCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "bond-history",
		Aliases: []string{"bh"},
		Short:   "Print US Treasury yield curve history",
		RunE:    BondHistoryHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().Bool("json", false, "Output JSON")
	rootCmd.Flags().StringP("begin", "b", "", "Begin date 20260101")
	rootCmd.Flags().StringP("end", "e", "", "End date 20260131")

	return rootCmd
}

// BondHistoryHandler 打印美国国债收益率历史数据
func BondHistoryHandler(cmd *cobra.Command, args []string) error {
	req := &bond.QueryBondReq{}
	var err error
	beginStr, _ := cmd.Flags().GetString("begin")
	endStr, _ := cmd.Flags().GetString("end")
	req.Start, req.End, err = utils.ParseBeginEnd(beginStr, endStr, 30, utils.ParseMetalCmdArgTimeLayout, utils.LayoutYYMMDD)
	if err != nil {
		return err
	}

	resp, err := bond.QueryBond(cmd.Context(), req)
	if err != nil {
		return err
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return printYieldJSON(cmd, resp.Data)
	}
	printBondHistory(cmd.OutOrStdout(), resp.Data)

	return nil
}

// printBondHistory 打印美国国债收益率历史数据
func printBondHistory(out io.Writer, items []*bond.BondYieldItem) {
	num := len(items)
	if num == 0 {
		return
	}

	headers := []string{"日期", "1个月", "3个月", "6个月", "5年", "10年", "变动(bp)"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, num)
	for _, item := range items {
		changeBpStr := "-"
		if item.YClose != -1 {
			changeBpStr = fmt.Sprintf("%+.1f", item.Change*100)
		}

		row := []string{
			item.Date,
			fmt.Sprintf("%.2f%%", item.BC1Month),
			fmt.Sprintf("%.2f%%", item.BC3Month),
			fmt.Sprintf("%.2f%%", item.BC6Month),
			fmt.Sprintf("%.2f%%", item.BC5Year),
			fmt.Sprintf("%.2f%%", item.BC10Year),
			changeBpStr,
		}
		data = append(data, row)

		styles := make([]tablewriter.Colors, 0, len(headers))
		for _, title := range headers {
			var itemStyle tablewriter.Colors = tablewriter.Colors{}
			if title == "10年" {
				itemStyle = termtable.Change(item.ChangeRate, true)
			}
			styles = append(styles, itemStyle)
		}
		columnsStyles = append(columnsStyles, styles)
	}

	table := termtable.New(out, headers, termtable.LeftHeader)

	for i, row := range data {
		table.Rich(row, columnsStyles[i])
	}

	table.Render()
}
