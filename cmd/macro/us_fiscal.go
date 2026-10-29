package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSFiscalCLI() *cobra.Command {
	command := &cobra.Command{Use: "fiscal", Short: "美国联邦月度财政收入、支出与赤字（未季调）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "198010")
		if err != nil {
			return err
		}
		req := &fred.FiscalRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "fred")
			if err != nil {
				return err
			}
		}
		client := fred.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.Fiscal(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSFiscal(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早198010"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	return command
}

func printUSFiscal(out io.Writer, result *fred.FiscalResult) error {
	if _, err := fmt.Fprintln(out, "美国联邦月度财政收支：现价、未季调、未折年，单位百万美元。"); err != nil {
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "收入", "支出", "赤字（负为盈余）"})
	format := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatFloat(*v, 'f', 2, 64)
	}
	for _, item := range result.Observations {
		date, _ := time.Parse("200601", item.Period)
		table.Append([]string{date.Format("2006-01"), format(item.Receipts), format(item.Outlays), format(item.Deficit)})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：美国财政部MTS，经FRED转接；列示自然月流量。财年10月开始；严格财年累计、官方盈余正/赤字负原值及口径见 --json 和 docs/us-fiscal.md。— 表示缺失。")
	return err
}
