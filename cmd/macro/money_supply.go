package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newMoneySupplyCLI() *cobra.Command {
	command := &cobra.Command{Use: "money", Aliases: []string{"money-supply"}, Short: "M1、M2 余额及同比", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		req := pbc.MoneySupplyRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "pbc")
			if err != nil {
				return err
			}
		}
		client := pbc.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		items, err := client.MoneySupply(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		return printMoneySupply(cmd.OutOrStdout(), items)
	}}
	addQueryFlags(command)
	return command
}

func printMoneySupply(out io.Writer, items []pbc.MoneySupplyObservation) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无货币供应量数据")
		return err
	}
	format := func(n *float64) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', -1, 64)
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "M1 余额（亿元）", "M1 同比（%）", "M1 口径", "M2 余额（亿元）", "M2 同比（%）", "说明"})
	for _, item := range items {
		table.Append([]string{item.Period, format(item.M1Balance), format(item.M1YoY), item.M1Definition, format(item.M2Balance), format(item.M2YoY), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：中国人民银行金融统计数据报告。余额及同比均为官方公布值，金额统一为亿元；M1 口径自 2025 年 1 月起修订，前后序列不直接拼接，空值为 —。")
	return err
}
