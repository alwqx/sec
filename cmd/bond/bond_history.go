package bond

import (
	"fmt"
	"io"

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
				if item.ChangeRate > 0 {
					itemStyle = tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgRedColor}
				} else if item.ChangeRate < 0 {
					itemStyle = tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgGreenColor}
				}
			}
			styles = append(styles, itemStyle)
		}
		columnsStyles = append(columnsStyles, styles)
	}

	table := tablewriter.NewWriter(out)
	table.SetHeader(headers)

	for i, row := range data {
		table.Rich(row, columnsStyles[i])
	}

	headerStyles := make([]tablewriter.Colors, 0, len(headers))
	for range headers {
		headerStyles = append(headerStyles, tablewriter.Colors{tablewriter.Bold})
	}
	table.SetHeaderColor(headerStyles...)

	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetNoWhiteSpace(false)
	table.SetTablePadding("\t")
	table.Render()
}
