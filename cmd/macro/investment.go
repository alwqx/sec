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

func newInvestmentCLI() *cobra.Command {
	command := &cobra.Command{Use: "investment", Short: "固定资产投资：总量、制造业、基建或房地产开发投资累计数据", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		sector, _ := cmd.Flags().GetString("sector")
		req := &nbs.InvestmentRequest{Begin: begin, End: end, Sector: sector}
		if err := req.Validate(); err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		result, err := client.Investment(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printInvestment(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().String("sector", "total", "投资分项：total、manufacturing、infrastructure、real-estate（房地产开发）")
	return command
}

func printInvestment(out io.Writer, result *nbs.InvestmentResult) error {
	if result == nil || len(result.Observations) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无固定资产投资数据")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "分项", "累计金额（亿元）", "累计同比（%）", "累计所属期", "统计范围", "说明"})
	sectors := map[string]string{"total": "固定资产投资(不含农户)", "manufacturing": "制造业", "infrastructure": "基础设施", "real-estate": "房地产开发投资"}
	format := func(n *float64) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', 1, 64)
	}
	month := func(s string) string {
		date, err := time.Parse("200601", s)
		if err != nil {
			return s
		}
		return date.Format("2006-01")
	}
	for _, item := range result.Observations {
		table.Append([]string{month(item.Period), sectors[item.Sector], format(item.YTDAmount), format(item.YTDGrowth), item.Period[:4] + "-01至" + month(item.Period), item.Scope, item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：国家统计局当期官方月报；金额为现价，累计增速为名义同比、未季调，按当期可比口径计算。1月免报、1—2月合并；未公布金额为 —。基建须按每行含电力/不含电力范围分别比较。历史版本不可回溯，详见 JSON 来源和公布时间。")
	return err
}
