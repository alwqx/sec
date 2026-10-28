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

func newUSRealPCECLI() *cobra.Command {
	command := &cobra.Command{Use: "real-pce", Short: "美国实际个人消费支出及商品/服务分项（季调折年水平）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "200701")
		if err != nil {
			return err
		}
		component, _ := cmd.Flags().GetString("component")
		req := &fred.RealPCERequest{Component: component, Begin: begin, End: end}
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
		result, err := client.RealPCE(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSRealPCE(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早200701"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	command.Flags().String("component", "total", "分项：total（总量）、goods（商品）、durable-goods（耐用品）、nondurable-goods（非耐用品）、services（服务）")
	return command
}

func printUSRealPCE(out io.Writer, result *fred.RealPCEResult) error {
	label := map[string]string{"total": "总量", "goods": "商品", "durable-goods": "耐用品", "nondurable-goods": "非耐用品", "services": "服务"}[result.Component]
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "实际消费水平（十亿美元，SAAR）", "同比（%）", "月环比（%）", "说明"})
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
	_, err := fmt.Fprintf(out, "来源：BEA，经FRED转接；实际个人消费支出（%s），单位为十亿美元（2017年链式美元）。月度水平采用季调折年率（SAAR），同比和月环比为计算值、增速未年化；水平不等于当月实际发生额。链式分项不能简单相加，商品及总量直接取官方系列；不构造分项贡献度。当前可见修订版本，缺口不补，JSON保留严格基期。\n", label)
	return err
}
