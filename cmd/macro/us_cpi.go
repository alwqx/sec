package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSCPICLI(indicator string) *cobra.Command {
	short := "美国CPI-U：季调/非季调指数、同比及季调环比"
	if indicator == "core-cpi" {
		short = "美国核心CPI-U（剔除食品和能源）：季调/非季调指数及增速"
	}
	command := &cobra.Command{Use: indicator, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := usCPIMonths(cmd, time.Now())
		if err != nil {
			return err
		}
		req := &bls.CPIRequest{Indicator: indicator, Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "bls")
			if err != nil {
				return err
			}
		}
		client := bls.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.CPI(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSCPI(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	return command
}

func usCPIMonths(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.UTC()
		end = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0).Format("200601")
	}
	if begin == "" {
		date, err := time.Parse("200601", end)
		if err != nil {
			return "", "", fmt.Errorf("无效截至月份: %s", end)
		}
		begin = date.AddDate(0, -11, 0).Format("200601")
		if begin < "191301" {
			begin = "191301"
		}
	}
	return begin, end, nil
}

func printUSCPI(out io.Writer, result *bls.CPIResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "非季调指数", "季调指数", "同比（非季调，%）", "环比（季调，%）", "说明"})
	format := func(value *float64, precision int) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', precision, 64)
	}
	index := func(value *bls.CPIValue) *float64 {
		if value == nil {
			return nil
		}
		return value.Index
	}
	for _, item := range result.Observations {
		date := item.Period
		if parsed, err := time.Parse("200601", date); err == nil {
			date = parsed.Format("2006-01")
		}
		table.Append([]string{date, format(index(item.NSA), 3), format(index(item.SA), 3), format(item.YoY, 2), format(item.MoM, 2), item.Note})
	}
	table.Render()
	label := "CPI-U"
	if result.Indicator == "core-cpi" {
		label = "核心CPI-U（剔除食品和能源）"
	}
	_, err := fmt.Fprintf(out, "来源：美国劳工统计局（BLS）；%s，美国城市平均，1982—1984=100。同比使用非季调指数，月环比使用季调指数，均为计算值；不跨缺口、不年化。季调历史可能修订；空值为 —，JSON保留脚注与基期指数。\n", label)
	return err
}
