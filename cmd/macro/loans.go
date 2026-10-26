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

func newLoansCLI() *cobra.Command {
	command := &cobra.Command{Use: "loans", Short: "金融机构人民币贷款余额、同比及居民/企业期限结构", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		req := pbc.LoanRequest{Begin: begin, End: end}
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
		items, err := client.RMBLoans(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			if items == nil {
				items = []pbc.LoanObservation{}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		details, _ := cmd.Flags().GetBool("details")
		return printLoans(cmd.OutOrStdout(), items, details)
	}}
	addQueryFlags(command)
	command.Flags().Bool("details", false, "展示住户、企事业单位及短期/中长期、票据融资分项")
	return command
}

func printLoans(out io.Writer, items []pbc.LoanObservation, details bool) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无人民币贷款数据")
		return err
	}
	format := func(n *float64) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', -1, 64)
	}
	flow := func(n *float64, approx bool) string {
		if n == nil {
			return "—"
		}
		value := format(n)
		if approx {
			return "≈" + value
		}
		return value
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "余额（亿元）", "同比（%）", "当月新增（亿元）", "累计新增（亿元）", "说明"})
	for _, item := range items {
		table.Append([]string{item.Period, format(item.Balance), format(item.YoY), flow(item.MonthlyFlow, item.MonthlyFlowApproximate), format(item.YTDFlow), item.Note})
	}
	table.Render()
	if details {
		table = tablewriter.NewWriter(out)
		table.SetBorder(false)
		table.SetHeader([]string{"月份", "分项", "余额（亿元）", "当月新增（亿元）", "累计新增（亿元）"})
		for _, item := range items {
			for _, part := range item.Components {
				table.Append([]string{item.Period, part.Name, format(part.Balance), flow(part.MonthlyFlow, part.MonthlyFlowApproximate), format(part.YTDFlow)})
			}
		}
		table.Render()
	}
	_, err := fmt.Fprintln(out, "来源：人民银行金融机构人民币信贷收支表及金融统计报告。≈ 为相邻报告已四舍五入累计额之差，非独立发布的精确月度新增；不使用余额差额代替新增。分项口径见原文，空值为 —。")
	return err
}
