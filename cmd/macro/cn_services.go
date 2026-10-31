package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/mofcom"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newCNServicesCLI() *cobra.Command {
	command := &cobra.Command{Use: "services-trade", Short: "中国服务贸易月度进出口与差额（人民币及美元）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		begin, end, err := queryCNTradeMonths(cmd, time.Now())
		if err != nil {
			return err
		}
		req := &mofcom.ServicesRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		dir := ""
		disabled, _ := cmd.Flags().GetBool("no-cache")
		if !disabled {
			dir, err = utils.SecDir("cache", "mofcom")
			if err != nil {
				return err
			}
		}
		client := mofcom.NewClient(dir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.Services(cmd.Context(), req)
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.CNServices(result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printCNServices(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，人民币和美元各成序列，保留官方总额和差额")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，最早201601；默认截至月份前11个月"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认北京时间上个月"
	return command
}

func printCNServices(out io.Writer, result *mofcom.ServicesResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "人民币出口", "人民币进口", "人民币差额", "美元出口", "美元进口", "美元差额"})
	format := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatFloat(*v, 'f', 2, 64)
	}
	for _, item := range result.Observations {
		date, _ := time.Parse("200601", item.Period)
		cny, usd := item.CNY, item.USD
		if cny == nil {
			cny = &mofcom.ServicesValues{}
		}
		if usd == nil {
			usd = &mofcom.ServicesValues{}
		}
		table.Append([]string{date.Format("2006-01"), format(cny.Exports), format(cny.Imports), format(cny.Balance), format(usd.Exports), format(usd.Imports), format(usd.Balance)})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：商务部服务进出口月度统计；人民币单位亿元、美元单位亿美元。未季调、不折年，差额为官方值。— 表示来源缺失；不从累计值推算单月，与海关货物贸易分开。完整总额、原值及版本说明见 --json。")
	return err
}
