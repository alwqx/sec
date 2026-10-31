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

func newUSMoneyCLI() *cobra.Command {
	command := &cobra.Command{Use: "money", Aliases: []string{"m2"}, Short: "美国M2名义货币余额及同比、月环比（月平均、季调）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "195901")
		if err != nil {
			return err
		}
		req := &fred.MoneyRequest{Begin: begin, End: end}
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
		result, err := client.Money(cmd.Context(), req)
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.USMoney(result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSMoney(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，包含统计口径、来源和缺失状态")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早195901"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	return command
}

func printUSMoney(out io.Writer, result *fred.MoneyResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "M2余额（十亿美元）", "同比（%）", "月环比（%）", "说明"})
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
	_, err := fmt.Fprintln(out, "来源：美国联邦储备委员会H.6，经FRED转接（M2SL）；单位十亿美元，月平均名义余额、季调，水平不折年；同比和月环比为计算值、增速未年化。缺口不补，JSON保留严格基期和原值。使用当前可见重算历史：2020年M1重分类不令M2断点，2026-07-28的IRA/Keogh扣除方法变化已追溯调整；不提供完整历史版本或逐期实际公布时间。")
	return err
}
