package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/pbc"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newDepositsCLI() *cobra.Command {
	command := &cobra.Command{Use: "deposits", Short: "住户人民币存款月末余额、计算同比及报告新增", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := depositMonths(cmd, time.Now())
		if err != nil {
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
		result, err := client.RMBDeposits(cmd.Context(), &pbc.DepositRequest{Begin: begin, End: end})
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.Deposits(result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			e := json.NewEncoder(cmd.OutOrStdout())
			e.SetIndent("", "  ")
			return e.Encode(result)
		}
		return printDeposits(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，分开住户余额、计算同比、官方同比和新增额")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早202101"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认北京时间上个月"
	return command
}

func depositMonths(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.In(time.FixedZone("CST", 8*3600))
		end = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0).Format("200601")
	}
	if begin == "" {
		date, err := time.Parse("200601", end)
		if err != nil {
			return "", "", fmt.Errorf("无效截至月份: %s", end)
		}
		begin = date.AddDate(0, -11, 0).Format("200601")
		if begin < "202101" {
			begin = "202101"
		}
	}
	return begin, end, (&pbc.DepositRequest{Begin: begin, End: end}).Validate()
}

func printDeposits(out io.Writer, result *pbc.DepositResult) error {
	if _, err := fmt.Fprintln(out, "住户人民币存款：月末余额为存量，新增额来自金融统计报告；金额单位亿元。"); err != nil {
		return err
	}
	format := func(n *float64) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', 2, 64)
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "余额（亿元）", "计算同比（%）", "当月新增（亿元）", "累计新增（亿元）"})
	for _, item := range result.Observations {
		month := item.Period
		if date, err := time.Parse("200601", month); err == nil {
			month = date.Format("2006-01")
		}
		flow := format(item.MonthlyFlow)
		if item.MonthlyFlow != nil && item.MonthlyFlowApproximate {
			flow = "≈" + flow
		}
		table.Append([]string{month, format(item.Balance), format(item.ComputedYoY), flow, format(item.YTDFlow)})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：人民银行。同比按严格上年同月余额计算，未另调统计口径；非官方可比同比。≈ 为相邻已四舍五入累计报告之差，不以余额差代替新增。— 表示缺失；详细口径和分项见 --json 与 docs/cn-deposits.md。")
	return err
}
