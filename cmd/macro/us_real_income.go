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

func newUSRealIncomeCLI() *cobra.Command {
	command := &cobra.Command{Use: "real-income", Short: "美国实际可支配个人收入（季调折年水平）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "195901")
		if err != nil {
			return err
		}
		req := &fred.RealIncomeRequest{Begin: begin, End: end}
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
		result, err := client.RealIncome(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.RealIncome(result)
			return writeDataset(cmd, dataset, err)
		}
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSRealIncome(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，保留实际收入总量与折年口径")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早195901"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	return command
}

func printUSRealIncome(out io.Writer, result *fred.RealIncomeResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "实际可支配收入（十亿美元，SAAR）", "同比（%）", "月环比（%）", "说明"})
	format := func(value *float64, digits int) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', digits, 64)
	}
	for _, item := range result.Observations {
		date, _ := time.Parse("200601", item.Period)
		table.Append([]string{date.Format("2006-01"), format(item.Level, 1), format(item.YoY, 2), format(item.MoM, 2), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：BEA，经FRED转接；实际可支配个人收入总量（扣除个人当期税及价格影响），单位为十亿美元（2017年链式美元）。月度水平采用季调折年率（SAAR），不是当月收入或人均收入；同比和月环比为计算值、增速未年化。当前可见修订版本，缺口不补，JSON保留严格基期；不提供历史公布版本和逐期实际公布时间。")
	return err
}
