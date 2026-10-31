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

func newEnterpriseCLI() *cobra.Command {
	command := &cobra.Command{Use: "enterprise", Short: "工业企业经营：累计利润、月末存货、产成品存货及应收账款", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		requestedBegin, _ := cmd.Flags().GetString("begin")
		if requestedBegin == "" && begin < nbs.EnterpriseEarliestMonth {
			begin = nbs.EnterpriseEarliestMonth
		}
		metric, _ := cmd.Flags().GetString("metric")
		req := &nbs.EnterpriseRequest{Begin: begin, End: end, Metric: metric}
		if err := req.Validate(); err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		result, err := client.Enterprise(cmd.Context(), req)
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.CNEnterprise(result, begin, end)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printEnterprise(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，按调查范围分开序列，区分累计利润和月末余额")
	command.Flags().String("metric", "all", "指标：all、profit、inventory、finished-goods、receivables")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早199901"
	return command
}

func printEnterprise(out io.Writer, result *nbs.EnterpriseResult) error {
	if result == nil || len(result.Observations) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无工业企业经营数据")
		return err
	}
	names := map[string]string{}
	for _, metric := range result.Metrics {
		names[metric.Metric] = metric.Name
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"所属月", "指标", "统计期", "金额（亿元）", "官方同比（%）"})
	format := func(number *float64) string {
		if number == nil {
			return "—"
		}
		return strconv.FormatFloat(*number, 'f', 2, 64)
	}
	for _, item := range result.Observations {
		date, err := time.Parse("200601", item.Period)
		if err != nil {
			return fmt.Errorf("无效的工业企业输出月份: %s", item.Period)
		}
		period := item.Period[:4] + "-01至" + date.Format("2006-01") + "累计"
		if item.Measurement == "month_end_stock" && item.AsOfDate != nil {
			period = *item.AsOfDate + "余额"
		}
		table.Append([]string{date.Format("2006-01"), names[item.Metric], period, format(item.Value), format(item.YoY)})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局发布库；利润为累计流量，其余为月末余额，未季调。\n官方同比按可比范围计算；— 表示缺失，不等于尚未发布。\n调查范围、口径变化及逐条说明请使用 --json 查看，详见 docs/cn-enterprise.md。")
	return err
}
