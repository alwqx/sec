package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSPCECLI(indicator string) *cobra.Command {
	short := "美国PCE季调价格指数、同比与月环比"
	if indicator == "core-pce" {
		short = "美国核心PCE季调价格指数（剔除食品和能源）"
	}
	command := &cobra.Command{Use: indicator, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "195901")
		if err != nil {
			return err
		}
		req := &fred.PCERequest{Indicator: indicator, Begin: begin, End: end}
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
		result, err := client.PCE(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.PCE(result)
			return writeDataset(cmd, dataset, err)
		}
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSPCE(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，保留价格指数基期和严格计算基期")
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早195901"
	return command
}

func printUSPCE(out io.Writer, result *fred.PCEResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "季调价格指数", "同比（%）", "月环比（%）", "说明"})
	format := func(n *float64, precision int) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', precision, 64)
	}
	for _, item := range result.Observations {
		period := item.Period
		if parsed, err := time.Parse("200601", period); err == nil {
			period = parsed.Format("2006-01")
		}
		table.Append([]string{period, format(item.Index, 3), format(item.YoY, 2), format(item.MoM, 2), item.Note})
	}
	table.Render()
	label := "PCE"
	if result.Indicator == "core-pce" {
		label = "核心PCE（剔除食品和能源）"
	}
	_, err := fmt.Fprintf(out, "来源：BEA，经FRED转接；%s链式价格指数，2017=100，月度季调。同比与月环比均为季调指数计算值，不年化、不跨缺口；这是价格指数，非消费支出金额或实际消费量。当前可见修订版本，空值为 —。\n", label)
	return err
}
