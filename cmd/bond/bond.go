package bond

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/alwqx/sec/provider/bond"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewBondCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "bond",
		Aliases: []string{"b"},
		Short:   "Print US Treasury yield curve (1M/3M/6M/5Y/10Y)",
		RunE:    BondHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")

	return rootCmd
}

// BondHandler 打印美国国债最新收益率曲线
func BondHandler(cmd *cobra.Command, args []string) error {
	end := time.Now()
	req := &bond.QueryBondReq{
		Start: end.Add(-10 * 24 * time.Hour).Format(utils.LayoutYYMMDD),
		End:   end.Format(utils.LayoutYYMMDD),
	}
	resp, err := bond.QueryBond(cmd.Context(), req)
	if err != nil {
		return err
	}
	num := len(resp.Data)
	if num == 0 {
		slog.Warn("no data")
	} else {
		printBondYield(resp.Data[num-1:])
	}

	return nil
}

// printBondYield 打印美国国债收益率曲线
func printBondYield(items []*bond.BondYieldItem) {
	num := len(items)
	if num == 0 {
		return
	}

	headers := []string{"日期", "1个月", "3个月", "6个月", "5年", "10年", "前值", "变动(bp)"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, num)
	for _, item := range items {
		yCloseStr := "-"
		changeBpStr := "-"
		if item.YClose != -1 {
			yCloseStr = fmt.Sprintf("%.2f%%", item.YClose)
			changeBpStr = fmt.Sprintf("%+.1f", item.Change*100)
		}

		row := []string{
			item.Date,
			fmt.Sprintf("%.2f%%", item.BC1Month),
			fmt.Sprintf("%.2f%%", item.BC3Month),
			fmt.Sprintf("%.2f%%", item.BC6Month),
			fmt.Sprintf("%.2f%%", item.BC5Year),
			fmt.Sprintf("%.2f%%", item.BC10Year),
			yCloseStr,
			changeBpStr,
		}
		data = append(data, row)

		styles := make([]tablewriter.Colors, 0, len(headers))
		for _, title := range headers {
			var itemStyle tablewriter.Colors = tablewriter.Colors{}
			// 10年收益率列着色
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

	table := tablewriter.NewWriter(os.Stdout)
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
