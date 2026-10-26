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

func newFinancingCLI() *cobra.Command {
	command := &cobra.Command{Use: "financing", Aliases: []string{"afre"}, Short: "社会融资规模存量、同比、月度增量及融资结构", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		req := pbc.FinancingRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		noCache, _ := cmd.Flags().GetBool("no-cache")
		cacheDir := ""
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "pbc")
			if err != nil {
				return err
			}
		}
		client := pbc.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		items, err := client.SocialFinancing(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			if items == nil {
				items = []pbc.FinancingObservation{}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		details, _ := cmd.Flags().GetBool("details")
		return printFinancing(cmd.OutOrStdout(), items, details)
	}}
	addQueryFlags(command)
	command.Flags().Bool("details", false, "展示各融资结构分项")
	return command
}

func printFinancing(out io.Writer, items []pbc.FinancingObservation, details bool) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无社会融资规模数据")
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
	table.SetHeader([]string{"月份", "存量（亿元）", "存量同比（%）", "当月增量（亿元）", "年初累计增量（亿元，计算值）", "说明"})
	for _, item := range items {
		table.Append([]string{item.Period, format(item.Stock), format(item.StockYoY), format(item.Flow), format(item.YTDFlow), item.Note})
	}
	table.Render()
	if details {
		table = tablewriter.NewWriter(out)
		table.SetBorder(false)
		table.SetHeader([]string{"月份", "融资结构", "存量（亿元）", "存量同比（%）", "当月增量（亿元）", "累计增量（亿元，计算值）"})
		for _, item := range items {
			for _, component := range item.Components {
				table.Append([]string{item.Period, component.Name, format(component.Stock), format(component.StockYoY), format(component.Flow), format(component.YTDFlow)})
			}
		}
		table.Render()
	}
	_, err := fmt.Fprintln(out, "来源：中国人民银行年度社融表；金额统一为亿元。累计增量为同一年度表从1月起月度增量求和，非独立发布值；不以存量差额替代增量，不假设分项合计等于总额。口径和修订见原表注释，空值为 —。")
	return err
}
