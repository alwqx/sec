package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fiscaldata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSDebtCLI() *cobra.Command {
	cmd := &cobra.Command{Use: "debt", Short: "美国国债每日记账发行、赎回及计算净额（DTS）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		begin, _ := cmd.Flags().GetString("begin")
		end, _ := cmd.Flags().GetString("end")
		if end == "" {
			end = time.Now().UTC().Format("20060102")
		}
		if begin == "" {
			d, e := time.Parse("20060102", end)
			if e != nil {
				return fmt.Errorf("无效截至日期：%s", end)
			}
			begin = d.AddDate(0, 0, -6).Format("20060102")
		}
		req := &fiscaldata.DebtRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		disabled, _ := cmd.Flags().GetBool("no-cache")
		dir := ""
		var err error
		if !disabled {
			dir, err = utils.SecDir("cache", "fiscaldata")
			if err != nil {
				return err
			}
		}
		client := fiscaldata.NewClient(dir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.Debt(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			e := json.NewEncoder(cmd.OutOrStdout())
			e.SetIndent("", "  ")
			return e.Encode(result)
		}
		return printUSDebt(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(cmd)
	cmd.Flags().Lookup("begin").Usage = "开始日期YYYYMMDD，最早20051003；默认截至日前6天"
	cmd.Flags().Lookup("end").Usage = "截至日期YYYYMMDD，默认UTC今天；未发布日不补"
	return cmd
}
func printUSDebt(out io.Writer, result *fiscaldata.DebtResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"记录日期", "市场", "记账发行≈", "赎回≈", "净记账发行≈"})
	format := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatFloat(*v, 'f', 0, 64)
	}
	for _, r := range result.Summaries {
		market := map[string]string{"Marketable": "可流通", "Nonmarketable": "非流通"}[r.Market]
		table.Append([]string{r.Date, market, format(r.Issues), format(r.Redemptions), format(r.Net)})
	}
	table.Render()
	if len(result.Summaries) == 0 {
		if _, err := fmt.Fprintln(out, "所选日期区间没有DTS交易记录；不代表发行或赎回为零。"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(out, "来源：美国财政部DTS；单位百万美元。≈ 为明细日值汇总／差额计算，包含利息、通胀及更正调整，不等于拍卖募集额或公众净融资。负数保留，— 表示缺失。月累计、财年累计、原始分类及口径见 --json 和 docs/us-debt.md。")
	return err
}
