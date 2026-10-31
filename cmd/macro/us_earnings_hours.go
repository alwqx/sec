package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSEarningsHoursCLI(indicator string) *cobra.Command {
	short := "美国私人非农全部雇员平均时薪（季调、名义税前）"
	if indicator == "weekly-hours" {
		short = "美国私人非农全部雇员平均每周工时（季调）"
	}
	command := &cobra.Command{Use: indicator, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "200603")
		if err != nil {
			return err
		}
		req := &bls.EarningsHoursRequest{Indicator: indicator, Begin: begin, End: end}
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
		result, err := client.EarningsHours(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.EarningsHours(result)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(dataset)
		}
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSEarningsHours(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Bool("standard-json", false, "按统一宏观序列格式输出JSON，保留私人非农范围、原单位和初步标记")
	command.MarkFlagsMutuallyExclusive("json", "standard-json")
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	return command
}

func printUSEarningsHours(out io.Writer, result *bls.EarningsHoursResult) error {
	label, unit, precision := "平均时薪", "美元/小时", 2
	if result.Indicator == "weekly-hours" {
		label, unit, precision = "平均每周工时", "小时/周", 1
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", label + "（" + unit + "）", "月变化（" + unit + "）", "环比（%）", "同比（%）", "估计标记", "说明"})
	format := func(value *float64, digits int) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', digits, 64)
	}
	for _, item := range result.Observations {
		date, _ := time.Parse("200601", item.Period)
		estimate := "未标初步"
		if item.Preliminary {
			estimate = "初步（P）"
		}
		if item.Value == nil {
			estimate = "—"
		}
		table.Append([]string{date.Format("2006-01"), format(item.Value, precision), format(item.Change, precision), format(item.MoM, 2), format(item.YoY, 2), estimate, item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：美国劳工统计局（BLS）CES；私人非农部门全部雇员（含全职与兼职，不含政府雇员），季调。时薪为名义税前平均收入，工时为平均支付工时。月变化为相邻月水平差，同/环比均由季调水平计算、未年化；当前值与基期来自同一响应，不跨缺口。P为初步估计，未标P仍可能修订；空值为 —，JSON保留基期、脚注及抓取时间。")
	return err
}
