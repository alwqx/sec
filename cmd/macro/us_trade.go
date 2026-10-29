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

func newUSTradeCLI() *cobra.Command     { return newUSTradeCommand(false) }
func newUSRealTradeCLI() *cobra.Command { return newUSTradeCommand(true) }

func newUSTradeCommand(real bool) *cobra.Command {
	use, short := "trade", "美国月度名义进出口与贸易差额（国际收支口径、季调）"
	if real {
		use, short = "real-trade", "美国季度实际进出口与净出口（2017年链式美元、季调折年）"
	}
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var begin, end string
		var err error
		component, _ := cmd.Flags().GetString("component")
		if real {
			begin, end, err = queryUSRealTradeQuarters(cmd, time.Now())
		} else {
			begin, end, err = queryUSMonths(cmd, time.Now(), "199201")
		}
		if err != nil {
			return err
		}
		if real {
			err = (&fred.RealTradeRequest{Begin: begin, End: end, Component: component}).Validate()
		} else {
			err = (&fred.TradeRequest{Begin: begin, End: end, Component: component}).Validate()
		}
		if err != nil {
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
		var result *fred.TradeResult
		if real {
			result, err = client.RealTrade(cmd.Context(), &fred.RealTradeRequest{Begin: begin, End: end, Component: component})
		} else {
			result, err = client.Trade(cmd.Context(), &fred.TradeRequest{Begin: begin, End: end, Component: component})
		}
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSTrade(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().String("component", "total", "贸易分类：total（合计）、goods（商品）、services（服务）")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早199201"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	if real {
		command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7季；合计自1947Q1，分项与净额自1970Q1"
		command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认UTC上一完整季度"
	}
	return command
}

func queryUSRealTradeQuarters(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.UTC()
		q := now.Year()*4 + (int(now.Month())-1)/3 - 1
		end = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
	}
	if err := (&fred.RealTradeRequest{Begin: end, End: end}).Validate(); err != nil {
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
	if err := (&fred.RealTradeRequest{Begin: begin, End: end}).Validate(); err != nil {
		return "", "", err
	}
	return begin, end, nil
}

func printUSTrade(out io.Writer, result *fred.TradeResult) error {
	format := func(value *float64) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', 2, 64)
	}
	component := map[string]string{"total": "商品与服务合计", "goods": "商品", "services": "服务"}[result.Component]
	if _, err := fmt.Fprintf(out, "美国贸易：%s；单位：%s。\n", component, result.Unit); err != nil {
		return err
	}
	period, growth, balance := "月份", "月环比", "差额"
	if result.Frequency == "quarterly" {
		period, growth, balance = "季度", "季环比", "实际净出口"
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{period, "出口", "进口", balance, "出口同比%", "进口同比%", "出口" + growth + "%", "进口" + growth + "%", "余额较上期变化", "说明"})
	for _, item := range result.Observations {
		date := item.Period
		if result.Frequency == "monthly" {
			if parsed, err := time.Parse("200601", date); err == nil {
				date = parsed.Format("2006-01")
			}
		}
		table.Append([]string{date, format(item.Exports), format(item.Imports), format(item.Balance), format(item.ExportsYoY), format(item.ImportsYoY), format(item.ExportsPreviousGrowth), format(item.ImportsPreviousGrowth), format(item.BalancePreviousChange), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源："+result.Producer+"，经FRED转接。"+result.Note+" 空值为 —；JSON保留原值、官方序列、严格基期与余额同比金额变化。")
	return err
}
