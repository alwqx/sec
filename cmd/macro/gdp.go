package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/nbs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newGDPCLI() *cobra.Command {
	command := &cobra.Command{Use: "gdp", Short: "实际GDP季度同比、季调环比及不变价金额", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := gdpQuarters(cmd, time.Now())
		if err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		result, err := client.GDP(cmd.Context(), &nbs.GDPRequest{Begin: begin, End: end})
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.CNGDP(result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printGDP(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，区分不变价金额、官方指数、计算同比和季调环比")
	command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7个季度；最早1992Q1"
	command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认上一个已结束季度"
	return command
}

func gdpQuarters(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.In(time.FixedZone("CST", 8*60*60))
		q := now.Year()*4 + (int(now.Month())-1)/3 - 1
		end = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
	}
	if err := (&nbs.GDPRequest{Begin: end, End: end}).Validate(); err != nil {
		return "", "", err
	}
	if begin == "" {
		year, _ := strconv.Atoi(end[:4])
		q := year*4 + int(end[5]-'1') - 7
		begin = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
		if begin < "1992Q1" {
			begin = "1992Q1"
		}
	}
	if err := (&nbs.GDPRequest{Begin: begin, End: end}).Validate(); err != nil {
		return "", "", err
	}
	return begin, end, nil
}

func printGDP(out io.Writer, result *nbs.GDPResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"季度", "实际同比（%）", "累计实际同比（%）", "季调环比（%）", "不变价当季（亿元）", "不变价累计（亿元）"})
	format := func(n *float64) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', 1, 64)
	}
	for _, item := range result.Observations {
		table.Append([]string{item.Period, format(item.YoY), format(item.YTDYoY), format(item.QoQ), format(item.RealValue), format(item.RealYTDValue)})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局季度发布库。同比=官方上年同期指数−100；环比已季调、未年化。不变价金额按不同基期分段计算，不跨基期比较。当前可见修订版本，逐期基期与实际公布时间不可得；空值为 —。")
	return err
}
