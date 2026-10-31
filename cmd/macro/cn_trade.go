package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/mofcom"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newCNTradeCLI() *cobra.Command {
	command := &cobra.Command{Use: "trade", Short: "中国货物进出口、贸易差额及主要伙伴结构", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		begin, end, err := queryCNTradeMonths(cmd, time.Now())
		if err != nil {
			return err
		}
		currency, _ := cmd.Flags().GetString("partners-currency")
		req := &mofcom.TradeRequest{Begin: begin, End: end, PartnerCurrency: currency}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "mofcom")
			if err != nil {
				return err
			}
		}
		client := mofcom.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.Trade(cmd.Context(), req)
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.CNTrade(result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printCNTrade(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，人民币和美元各成序列，伙伴行不可相加")
	command.Flags().String("partners-currency", "usd", "伙伴统计币种 usd 或 cny；不改变月度总量的双币种输出")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，最早支持201501；默认截至月份前11个月"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认北京时间上个月"
	return command
}

func queryCNTradeMonths(cmd *cobra.Command, now time.Time) (string, string, error) {
	beijing := time.FixedZone("CST", 8*60*60)
	now = now.In(beijing)
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		end = now.AddDate(0, 0, -now.Day()).Format("200601")
	}
	if begin == "" {
		date, err := time.ParseInLocation("200601", end, beijing)
		if err != nil {
			return "", "", fmt.Errorf("无效截至月份：%s", end)
		}
		begin = date.AddDate(0, -11, 0).Format("200601")
	}
	return begin, end, nil
}

func printCNTrade(out io.Writer, result *mofcom.TradeResult) error {
	format := func(value *float64) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', 2, 64)
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", "人民币出口", "人民币进口", "人民币差额", "美元出口", "美元进口", "美元差额"})
	for _, item := range result.Observations {
		period := item.Period
		if date, err := time.Parse("200601", period); err == nil {
			period = date.Format("2006-01")
		}
		cny, usd := item.CNY, item.USD
		if cny == nil {
			cny = &mofcom.TradeCurrencyValues{}
		}
		if usd == nil {
			usd = &mofcom.TradeCurrencyValues{}
		}
		table.Append([]string{period, format(cny.Exports.Monthly), format(cny.Imports.Monthly), format(cny.Balance.Monthly), format(usd.Exports.Monthly), format(usd.Imports.Monthly), format(usd.Balance.Monthly)})
	}
	table.Render()
	unit := "亿美元"
	if result.PartnerCurrency == "CNY" {
		unit = "亿元人民币"
	}
	if _, err := fmt.Fprintf(out, "人民币单位：亿元；美元单位：亿美元。人民币来源：商务部投资促进事务局发布的海关总署月报；美元来源：商务部数据中心转发海关月度统计。伙伴数据为截至%s的主要伙伴年初累计值（%s），不是全体伙伴；未由累计值计算单月值。服务贸易未纳入。\n", result.PartnerPeriod, unit); err != nil {
		return err
	}
	if len(result.Partners) == 0 {
		_, err := fmt.Fprintln(out, result.PartnerNote)
		return err
	}
	partners := tablewriter.NewWriter(out)
	partners.SetBorder(false)
	partners.SetHeader([]string{"主要伙伴", "进出口累计", "出口累计", "进口累计", "同比%"})
	for _, item := range result.Partners {
		partners.Append([]string{item.Name, format(item.TotalYTD), format(item.ExportsYTD), format(item.ImportsYTD), format(item.TotalYTDYoY)})
	}
	partners.Render()
	if result.PartnerNote != "" {
		_, err := fmt.Fprintln(out, result.PartnerNote)
		return err
	}
	return nil
}
