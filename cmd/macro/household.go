package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newHouseholdCLI() *cobra.Command {
	command := &cobra.Command{Use: "household", Short: "全国居民人均累计收入、消费支出及实际同比", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := householdQuarters(cmd, time.Now())
		if err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		result, err := client.Household(cmd.Context(), &nbs.HouseholdRequest{Begin: begin, End: end})
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printHousehold(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7季；最早2013Q1"
	command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认北京时间上一完整季度"
	return command
}

func householdQuarters(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, end, err := gdpQuarters(cmd, now)
	if err != nil {
		return "", "", err
	}
	explicit, _ := cmd.Flags().GetString("begin")
	if explicit == "" && begin < "2013Q1" {
		begin = "2013Q1"
	}
	return begin, end, (&nbs.HouseholdRequest{Begin: begin, End: end}).Validate()
}

func printHousehold(out io.Writer, result *nbs.HouseholdResult) error {
	if _, err := fmt.Fprintln(out, "全国居民人均收支：现价累计金额与官方累计实际同比，未季调、未年化。"); err != nil {
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"所属期", "收入（元/人）", "实际同比（%）", "消费（元/人）", "实际同比（%）"})
	format := func(n *float64, digits int) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', digits, 64)
	}
	for _, item := range result.Observations {
		period := item.Period[:4] + map[byte]string{'1': "一季度", '2': "上半年", '3': "前三季度", '4': "全年"}[item.Period[5]]
		table.Append([]string{period, format(item.IncomeYTD, 0), format(item.IncomeRealYTDYoY, 1), format(item.ConsumptionYTD, 0), format(item.ConsumptionRealYTDYoY, 1)})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局；全年含自产自用，季度不含，不拆分单季。— 表示缺失；详细口径及原值见 --json 和 docs/cn-household.md。")
	return err
}
