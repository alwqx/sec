package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newPropertyCLI() *cobra.Command {
	command := &cobra.Command{Use: "property", Short: "房地产：新建商品房销售面积、销售额和房屋新开工面积的累计值与累计同比", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		component, _ := cmd.Flags().GetString("component")
		req := &nbs.PropertyRequest{Begin: begin, End: end, Component: component}
		if err := req.Validate(); err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		result, err := client.Property(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printProperty(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().String("component", "all", "房地产指标：all、sales-area（销售面积）、sales-value（销售额）、starts-area（新开工面积）")
	return command
}

func printProperty(out io.Writer, result *nbs.PropertyResult) error {
	if result == nil || len(result.Observations) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无房地产数据")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"累计所属期", "指标", "累计值", "单位", "累计同比（%）", "说明"})
	names := map[string]string{"sales-area": "新建商品房销售面积", "sales-value": "新建商品房销售额", "starts-area": "房屋新开工面积"}
	format := func(n *float64, precision int) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', precision, 64)
	}
	for _, item := range result.Observations {
		period := item.YTDPeriod
		parts := strings.Split(period, "-")
		if len(parts) == 2 && len(parts[0]) == 6 && len(parts[1]) == 6 {
			period = parts[0][:4] + "-" + parts[0][4:] + " 至 " + parts[1][:4] + "-" + parts[1][4:]
		}
		table.Append([]string{period, names[item.Component], format(item.YTDAmount, 2), item.AmountUnit, format(item.YTDGrowth, 1), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局发布库；全国房地产开发企业口径，销售包含住宅及其他新建商品房。累计值和官方累计同比，未季调；销售额为名义合同金额。空值为 —；1—2月不拆成单月。历史销售口径和修订说明见 --json 或 docs/cn-property.md。")
	return err
}
