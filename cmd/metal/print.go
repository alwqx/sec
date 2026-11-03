package metal

import (
	"fmt"
	"io"
	"strconv"

	"github.com/alwqx/sec/provider/metal"
	"github.com/olekukonko/tablewriter"
)

// printDailyHistory 打印一个品种的日行情
func printDailyHistory(out io.Writer, inst metal.Instrument, items []*metal.DailyHQItem) {
	num := len(items)
	if num == 0 {
		return
	}

	headers := []string{"日期", "品种", "收盘", "开盘", "最高", "最低", "单位"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, num)
	for _, au := range items {
		combineClose := ""
		if au.YClose == -1 {
			combineClose = fmt.Sprintf("%-.5g %-.5g %-.2g%s", au.Close, 0.0, 0.00, "%")
		} else {
			combineClose = fmt.Sprintf("%-.5g %-.5g %-.3g%s", au.Close, au.Change, au.ChangeRate*100, "%")
		}

		row := []string{
			au.Date,
			inst.ID + " " + inst.Name,
			combineClose,
			strconv.FormatFloat(au.Open, 'g', -1, 64),
			strconv.FormatFloat(au.High, 'g', -1, 64),
			strconv.FormatFloat(au.Low, 'g', -1, 64),
			inst.Unit,
		}
		data = append(data, row)

		styles := make([]tablewriter.Colors, 0, len(headers))
		for _, title := range headers {
			var item tablewriter.Colors = tablewriter.Colors{}
			// 收盘
			if title == headers[2] {
				v := au.ChangeRate
				if v > 0 {
					item = tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgRedColor}
				} else if v < 0 {
					item = tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgGreenColor}
				}
			}
			styles = append(styles, item)
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
