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

func newUSDebtCashCLI() *cobra.Command {
	cmd := &cobra.Command{Use: "debt-cash", Short: "美国国债官方现金发行、现金赎回及现金口径调整明细", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		if e := req.Validate(); e != nil {
			return e
		}
		dir := ""
		disabled, _ := cmd.Flags().GetBool("no-cache")
		var e error
		if !disabled {
			dir, e = utils.SecDir("cache", "fiscaldata")
			if e != nil {
				return e
			}
		}
		client := fiscaldata.NewClient(dir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, e := client.DebtCash(cmd.Context(), req)
		if e != nil {
			return e
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
		return printUSDebtCash(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(cmd)
	cmd.Flags().Lookup("begin").Usage = "开始日期YYYYMMDD，最早20051003；默认截至日前6天"
	cmd.Flags().Lookup("end").Usage = "截至日期YYYYMMDD，默认UTC今天；无记录日不补"
	return cmd
}
func printUSDebtCash(out io.Writer, result *fiscaldata.CashResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"记录日期", "官方现金发行", "官方现金赎回", "净现金发行≈"})
	format := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatFloat(*v, 'f', 0, 64)
	}
	for _, r := range result.Observations {
		var issues, redemptions *float64
		if r.Issues != nil {
			issues = r.Issues.Today
		}
		if r.Redemptions != nil {
			redemptions = r.Redemptions.Today
		}
		table.Append([]string{r.Date, format(issues), format(redemptions), format(r.Net.Today)})
	}
	table.Render()
	if len(result.Observations) == 0 {
		if _, e := fmt.Fprintln(out, "所选日期区间没有现金债务记录，不代表现金发行或赎回为零。"); e != nil {
			return e
		}
	}
	if _, e := fmt.Fprintln(out, "来源：美国财政部DTS表II官方现金总额及IIIB调整表；单位百万美元。≈ 为两项官方现金总额的差，不是记账面值或拍卖面值，也不等于财政赤字或剔除SOMA后的公众净融资。月累计、财年累计及原始调整项见 --json 和 docs/us-debt-cash.md。"); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, r := range result.Observations {
		if r.Note != "" && !seen[r.Note] {
			seen[r.Note] = true
			if _, e := fmt.Fprintln(out, r.Note); e != nil {
				return e
			}
		}
	}
	return nil
}
