package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newLPRCLI() *cobra.Command {
	command := &cobra.Command{Use: "lpr", Short: "1年期、5年期以上LPR月度报价及基点变化（自201908起）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		req := pbc.LPRRequest{Begin: begin, End: end}
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
		items, err := client.LPR(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		return printLPR(cmd.OutOrStdout(), items)
	}}
	addQueryFlags(command)
	return command
}

func printLPR(out io.Writer, items []pbc.LPRObservation) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无LPR报价")
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
	table.SetHeader([]string{"报价日期/月份", "公布时间（北京时间）", "1年期（%）", "5年期以上（%）", "1年期变化（BP）", "5年期以上变化（BP）", "说明"})
	beijing := time.FixedZone("CST", 8*60*60)
	for _, item := range items {
		date := item.QuoteDate
		layout, display := "20060102", "2006-01-02"
		if date == "" {
			date = item.Period
			layout, display = "200601", "2006-01"
		}
		if parsed, err := time.Parse(layout, date); err == nil {
			date = parsed.Format(display)
		} else if date == "" {
			date = "—"
		}
		published := item.PublishedAt
		if parsed, err := time.Parse(time.RFC3339, published); err == nil {
			published = parsed.In(beijing).Format("2006-01-02 15:04:05")
		} else if published == "" {
			published = "—"
		}
		table.Append([]string{date, published, format(item.OneYearRate), format(item.FiveYearPlusRate), format(item.OneYearChangeBP), format(item.FiveYearPlusChangeBP), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：人民银行授权发布的全国银行间同业拆借中心LPR公告；覆盖自201908起。BP变化为相邻月报价差×100的计算值，不跨缺口计算；空值为 —，持平为0。网页公布时间可能受修订影响，JSON保留原文和相邻月来源。")
	return err
}
