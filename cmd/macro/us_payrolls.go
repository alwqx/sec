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

func newUSPayrollsCLI() *cobra.Command {
	command := &cobra.Command{Use: "payrolls", Aliases: []string{"nonfarm", "nfp"}, Short: "美国非农就业岗位总数及月度变化", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "193901")
		if err != nil {
			return err
		}
		req := &bls.PayrollRequest{Begin: begin, End: end}
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
		result, err := client.Payrolls(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSPayrolls(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认最近 12 个月"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认 UTC 上个月"
	command.Flags().Lookup("no-cache").Usage = "不读写磁盘缓存及本机观测版本档案"
	return command
}

func printUSPayrolls(out io.Writer, result *bls.PayrollResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "非农岗位（千个）", "当月新增（千个）", "估计标记", "本机版本数", "说明"})
	format := func(value *float64) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', 0, 64)
	}
	for _, item := range result.Observations {
		date := item.Period
		if parsed, err := time.Parse("200601", date); err == nil {
			date = parsed.Format("2006-01")
		}
		estimate := "未标初步"
		if item.Preliminary {
			estimate = "初步（P）"
		}
		versions := "—"
		if len(item.ObservedVersions) > 0 {
			versions = strconv.Itoa(len(item.ObservedVersions))
		}
		table.Append([]string{date, format(item.Employment), format(item.Change), estimate, versions, item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：美国劳工统计局（BLS）CES，季调非农工资岗位，单位为千个岗位；岗位数不等于去重就业人数。当月新增由相邻月份当前可用水平相减，历史季调和基准修订可能改变结果。P表示初步估计；未标P不代表最终值。本机版本数是本机实际观测记录，不是官方首次/第二次/第三次估计次数。官方历史修订档案尚未接入；无缓存运行不会保存本机观测版本。")
	return err
}
