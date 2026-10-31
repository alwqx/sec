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

func newUSCorporateProfitsCLI() *cobra.Command    { return usEnterpriseCommand("profit") }
func newUSBusinessInventoriesCLI() *cobra.Command { return usEnterpriseCommand("inventory") }
func newUSNewOrdersCLI() *cobra.Command           { return usEnterpriseCommand("new-orders") }

func usEnterpriseCommand(metric string) *cobra.Command {
	use, short, earliest := "business-inventories", "美国商业库存（月末余额，季调、未折年）", "199201"
	switch metric {
	case "profit":
		use, short, earliest = "corporate-profits", "美国企业税前利润（含IVA和CCAdj，季度季调折年）", "1947Q1"
	case "new-orders":
		use, short, earliest = "new-orders", "美国制造业新订单（月度流量，季调、未折年）", "199202"
	}
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var begin, end string
		var err error
		if metric == "profit" {
			begin, end, err = enterpriseQueryQuarters(cmd, time.Now())
		} else {
			begin, end, err = queryUSMonths(cmd, time.Now(), earliest)
		}
		if err != nil {
			return err
		}
		req := &fred.EnterpriseRequest{Begin: begin, End: end, Metric: metric}
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
		result, err := client.Enterprise(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.Enterprise(result)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(dataset)
		}
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSEnterprise(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Bool("standard-json", false, "按统一宏观序列格式输出JSON，区分利润流量、库存余额和新订单")
	command.MarkFlagsMutuallyExclusive("json", "standard-json")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早" + earliest
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	if metric == "profit" {
		command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7季；最早1947Q1"
		command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认UTC上一完整季度"
	}
	return command
}

func enterpriseQueryQuarters(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.UTC()
		q := now.Year()*4 + (int(now.Month())-1)/3 - 1
		end = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
	}
	if err := (&fred.EnterpriseRequest{Begin: end, End: end, Metric: "profit"}).Validate(); err != nil {
		return "", "", err
	}
	if begin == "" {
		year, _ := strconv.Atoi(end[:4])
		q := year*4 + int(end[5]-'1') - 7
		begin = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
		if begin < "1947Q1" {
			begin = "1947Q1"
		}
	}
	return begin, end, (&fred.EnterpriseRequest{Begin: begin, End: end, Metric: "profit"}).Validate()
}

func printUSEnterprise(out io.Writer, result *fred.EnterpriseResult) error {
	basis, period, growth, digits := "季调、未折年", "月份", "月环比（%）", 0
	if result.LevelAnnualized {
		basis, period, growth, digits = "季调折年", "季度", "季环比（%）", 3
	}
	if _, err := fmt.Fprintf(out, "美国%s（%s）：%s，单位%s。\n", result.Name, result.SeriesID, basis, result.Unit); err != nil {
		return err
	}
	format := func(v *float64, digits int) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatFloat(*v, 'f', digits, 64)
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{period, "水平（" + result.Unit + "）", "同比（%）", growth})
	for _, item := range result.Observations {
		date := item.Period
		if result.Frequency == "monthly" {
			if parsed, err := time.Parse("200601", date); err == nil {
				date = parsed.Format("2006-01")
			}
		}
		table.Append([]string{date, format(item.Level, digits), format(item.YoY, 2), format(item.PreviousGrowth, 2)})
	}
	table.Render()
	_, err := fmt.Fprintf(out, "来源：%s，经FRED转接；当前快照有效范围%s至%s。\n增速未年化，严格基期缺失或非正时为空；— 表示缺失，不等于尚未发布。\n完整定义、原值、基期及逐条说明请使用 --json 查看，详见 docs/us-enterprise.md。\n", result.Producer, result.FirstPeriod, result.LastPeriod)
	return err
}
