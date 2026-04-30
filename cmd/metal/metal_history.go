package metal

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/metal"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewMetalHistoryCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "metal-history",
		Aliases: []string{"mh"},
		Short:   "Print quote history of precious metal(default au999)",
		RunE:    MetalHistoryHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().StringP("begin", "b", "", "Begin date 20250101")
	rootCmd.Flags().StringP("end", "e", "", "End date 20250131")

	return rootCmd
}

// MetalHistoryHandler 打印贵金属历史数据，默认 Au999
func MetalHistoryHandler(cmd *cobra.Command, args []string) error {
	req := &metal.QueryAu999Req{}
	defaultEnd := time.Now()
	defaultBegin := defaultEnd.Add(-30 * 24 * time.Hour)

	beginStr, err := cmd.Flags().GetString("begin")
	if err != nil {
		return err
	}
	// 校验
	if beginStr != "" {
		defaultBegin, err = time.Parse(utils.ParseMetalCmdArgTimeLayout, beginStr)
		if err != nil {
			return err
		}
	}
	req.Start = defaultBegin.Format(utils.LayoutYYMMDD)

	endStr, err := cmd.Flags().GetString("end")
	if err != nil {
		return err
	}
	if endStr != "" {
		defaultEnd, err = time.Parse(utils.ParseMetalCmdArgTimeLayout, endStr)
		if err != nil {
			return err
		}
	}
	req.End = defaultEnd.Format(utils.LayoutYYMMDD)

	if defaultEnd.Before(defaultBegin) {
		bs := defaultBegin.Format(utils.LayoutYYMMDD)
		es := defaultEnd.Format(utils.LayoutYYMMDD)
		slog.Error("invalid time range", "begin", bs, "end", es)
		return fmt.Errorf("invalid begin %s and end %s", bs, es)
	}

	resp, err := metal.QueryAu999(cmd.Context(), req)
	if err != nil {
		return err
	}
	printAu999History(resp.Data)

	return nil
}

// printAu999History 打印 Au999 信息
func printAu999History(aus []*metal.DailyHQItem) {
	num := len(aus)
	if num == 0 {
		return
	}

	headers := []string{"日期", "名称", "收盘", "开盘", "最高", "最低"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, num)
	for _, au := range aus {
		combineClose := ""
		if au.YClose == -1 {
			combineClose = fmt.Sprintf("%-.5g %-.5g %-.2g%s", au.Close, 0.0, 0.00, "%")
		} else {
			combineClose = fmt.Sprintf("%-.5g %-.5g %-.3g%s", au.Close, au.Change, au.ChangeRate*100, "%")
		}

		row := []string{
			au.Date,
			"Au99.99",
			combineClose,
			strconv.FormatFloat(au.Open, 'g', -1, 64),
			strconv.FormatFloat(au.High, 'g', -1, 64),
			strconv.FormatFloat(au.Low, 'g', -1, 64),
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
