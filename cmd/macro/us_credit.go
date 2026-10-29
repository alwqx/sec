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

func newUSBankCreditCLI() *cobra.Command       { return usCreditCommand("bank") }
func newUSLendingStandardsCLI() *cobra.Command { return usCreditCommand("standards") }
func newUSDebtServiceCLI() *cobra.Command      { return usCreditCommand("debt-service") }

func usCreditCommand(metric string) *cobra.Command {
	use, short := "bank-credit", "美国商业银行信贷（月平均余额，季调）"
	if metric == "standards" {
		use, short = "lending-standards", "美国大中型企业C&I贷款标准净收紧比例（季度调查，未季调）"
	}
	if metric == "debt-service" {
		use, short = "debt-service", "美国家庭偿债比率（季度，季调，当前征信数据方法）"
	}
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var begin, end string
		var err error
		if metric == "bank" {
			begin, end, err = queryUSMonths(cmd, time.Now(), "194701")
		} else {
			begin, end, err = creditQueryQuarters(cmd, time.Now(), metric)
		}
		if err != nil {
			return err
		}
		req := &fred.CreditRequest{Begin: begin, End: end, Metric: metric}
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
		result, err := client.Credit(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSCredit(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早194701"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	if metric != "bank" {
		earliest := "1990Q2"
		if metric == "debt-service" {
			earliest = "2005Q1"
		}
		command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7季；最早" + earliest
		command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认UTC上一完整季度"
	}
	return command
}

func creditQueryQuarters(cmd *cobra.Command, now time.Time, metric string) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.UTC()
		q := now.Year()*4 + (int(now.Month())-1)/3 - 1
		end = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
	}
	if err := (&fred.CreditRequest{Begin: end, End: end, Metric: metric}).Validate(); err != nil {
		return "", "", err
	}
	if begin == "" {
		earliest := "1990Q2"
		if metric == "debt-service" {
			earliest = "2005Q1"
		}
		year, _ := strconv.Atoi(end[:4])
		q := year*4 + int(end[5]-'1') - 7
		begin = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
		if begin < earliest {
			begin = earliest
		}
	}
	return begin, end, (&fred.CreditRequest{Begin: begin, End: end, Metric: metric}).Validate()
}

func printUSCredit(out io.Writer, result *fred.CreditResult) error {
	adjustment := "未季调"
	if result.SeasonallyAdjusted {
		adjustment = "季调"
	}
	if _, err := fmt.Fprintf(out, "美国%s（%s）：%s、未折年，单位%s。\n", result.Name, result.SeriesID, adjustment, result.Unit); err != nil {
		return err
	}
	period, level, previous, yearAgo := "季度", "水平（%）", "较上季（百分点）", "较上年同季（百分点）"
	if result.Metric == "bank" {
		period, level, previous, yearAgo = "月份", "月平均余额（十亿美元）", "月环比（%）", "同比（%）"
	}
	if result.Metric == "standards" {
		period = "调查季度"
	}
	format := func(v *float64, digits int) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatFloat(*v, 'f', digits, 64)
	}
	formatPeriod := func(period string) string {
		if result.Frequency == "monthly" {
			if date, err := time.Parse("200601", period); err == nil {
				return date.Format("2006-01")
			}
		}
		return period
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{period, level, previous, yearAgo})
	for _, item := range result.Observations {
		prev, yoy := item.PreviousChangePP, item.YearAgoChangePP
		if result.ChangeUnit == "percent" {
			prev, yoy = item.PreviousChangePercent, item.YearAgoChangePercent
		}
		table.Append([]string{formatPeriod(item.Period), format(item.Level, 4), format(prev, 2), format(yoy, 2)})
	}
	table.Render()
	note := "比率变化为百分点差；当前征信数据方法历史从2005Q1起，不拼接旧方法。"
	if result.Metric == "bank" {
		note = "水平推算增速未年化、未另作断点调整，不是H.8官方增速；余额为月平均。"
	}
	if result.Metric == "standards" {
		note = "正值净收紧、负值净放松；百分点差。调查季度通常回顾此前三个月的变化。"
	}
	_, err := fmt.Fprintf(out, "来源：美联储，经FRED转接；有效范围%s至%s。\n%s\n— 表示缺失；严格基期、定义与逐条说明见 --json 和 docs/us-credit.md。\n", formatPeriod(result.FirstPeriod), formatPeriod(result.LastPeriod), note)
	return err
}
