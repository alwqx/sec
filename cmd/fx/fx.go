// Package fx 提供独立的汇率查询命令。
package fx

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/chinamoney"
	"github.com/alwqx/sec/provider/forex"
	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewFXCLI() *cobra.Command {
	root := &cobra.Command{Use: "fx", Short: "Query foreign exchange (FX) rates for CNY, CNH, USD and HKD"}
	for _, kind := range []string{"quote", "history", "fixing", "reference"} {
		kind := kind
		command := &cobra.Command{Use: kind, Short: map[string]string{"quote": "Show the latest indicative exchange rates from Sina", "history": "Query daily exchange rate history by data source", "fixing": "Query RMB central parity rates from CFETS", "reference": "Query onshore reference rates at a specified time from CFETS"}[kind], Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return run(cmd, kind) }}
		command.Flags().StringSlice("pairs", nil, "Comma-separated pairs, e.g. USD/CNY,USD/CNH; quote currency units per one base currency unit")
		command.Flags().Bool("json", false, "Output JSON with raw fields, sources and observation time definitions")
		command.Flags().Bool("refresh", false, "Fetch fresh data and update the cache")
		command.Flags().Bool("no-cache", false, "Disable reading and writing the cache")
		command.Flags().BoolP("debug", "D", false, "Show debug logs")
		if kind != "quote" {
			command.Flags().StringP("begin", "b", "", "Start date YYYYMMDD; defaults to one year before the end date")
			command.Flags().StringP("end", "e", "", "End date YYYYMMDD; defaults to today in Beijing time")
		}
		if kind == "history" {
			command.Flags().String("source", "sina", "Data source: sina or fred; each retains its own price and time definitions")
		}
		if kind == "reference" {
			command.Flags().String("at", "16:00", "Observation time in Beijing: 10:00, 11:00, 14:00, 15:00 or 16:00")
		}
		root.AddCommand(command)
	}
	return root
}

func request(cmd *cobra.Command, kind string, now time.Time) (*forex.Request, string, error) {
	source := "chinamoney"
	allowed := forex.OnshorePairs
	if kind == "quote" || kind == "history" {
		source = "sina"
		allowed = forex.MarketPairs
	}
	if kind == "history" {
		source, _ = cmd.Flags().GetString("source")
		if source != "sina" && source != "fred" {
			return nil, "", fmt.Errorf("汇率历史来源可选 sina 或 fred")
		}
		if source == "fred" {
			allowed = forex.FREDPairs
		}
	}
	pairs, _ := cmd.Flags().GetStringSlice("pairs")
	if len(pairs) == 0 {
		pairs = append([]string(nil), allowed...)
	}
	for i, pair := range pairs {
		pairs[i] = strings.ToUpper(strings.TrimSpace(pair))
	}
	req := &forex.Request{Pairs: pairs}
	if kind == "quote" {
		return req, source, forex.ValidatePairs(pairs, allowed)
	}
	req.Begin, _ = cmd.Flags().GetString("begin")
	req.End, _ = cmd.Flags().GetString("end")
	if req.End == "" {
		req.End = now.In(forex.Beijing).Format("20060102")
	}
	if req.Begin == "" {
		end, err := time.Parse("20060102", req.End)
		if err != nil {
			return nil, "", fmt.Errorf("无效汇率截至日期：%s", req.End)
		}
		req.Begin = end.AddDate(-1, 0, 0).Format("20060102")
	}
	if kind == "reference" {
		req.At, _ = cmd.Flags().GetString("at")
		return req, source, chinamoney.ValidateReference(req)
	}
	earliest := ""
	if kind == "fixing" {
		earliest = "20060104"
	}
	return req, source, req.Validate(allowed, earliest)
}

func run(cmd *cobra.Command, kind string) error {
	req, source, err := request(cmd, kind, time.Now())
	if err != nil {
		return err
	}
	cacheDir := ""
	noCache, _ := cmd.Flags().GetBool("no-cache")
	if !noCache {
		cacheDir, err = utils.SecDir("cache", "fx", source)
		if err != nil {
			return err
		}
	}
	refresh, _ := cmd.Flags().GetBool("refresh")
	var result *forex.Result
	switch source {
	case "sina":
		client := sina.NewForexClient(cacheDir)
		client.Refresh = refresh
		if kind == "quote" {
			result, err = client.ForexQuotes(cmd.Context(), req)
		} else {
			result, err = client.ForexHistory(cmd.Context(), req)
		}
	case "fred":
		client := fred.NewClient(cacheDir)
		client.Refresh = refresh
		result, err = client.ForexHistory(cmd.Context(), req)
	case "chinamoney":
		client := chinamoney.NewClient(cacheDir)
		client.Refresh = refresh
		if kind == "fixing" {
			result, err = client.Fixing(cmd.Context(), req)
		} else {
			result, err = client.Reference(cmd.Context(), req)
		}
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
	return printRates(cmd.OutOrStdout(), result)
}

func printRates(out io.Writer, result *forex.Result) error {
	format := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(*v, 'f', 6, 64), "0"), ".")
	}
	if len(result.Observations) == 0 {
		if _, err := fmt.Fprintln(out, "所选区间内来源未提供汇率记录；不代表汇率为零"); err != nil {
			return err
		}
	} else {
		table := tablewriter.NewWriter(out)
		table.SetBorder(false)
		switch result.PriceType {
		case "indicative-quote":
			table.SetHeader([]string{"货币对", "观测时间（北京时间）", "参考报价", "买入报价", "卖出报价"})
		case "provider-daily-bar":
			table.SetHeader([]string{"来源交易日", "货币对", "开盘", "最高", "最低", "收盘"})
		default:
			table.SetHeader([]string{"日期", "货币对", "汇率"})
		}
		for _, item := range result.Observations {
			switch result.PriceType {
			case "indicative-quote":
				stamp := item.ObservedAt
				if parsed, err := time.Parse(time.RFC3339, stamp); err == nil {
					stamp = parsed.In(forex.Beijing).Format("2006-01-02 15:04:05")
				}
				table.Append([]string{item.Pair, stamp, format(item.Value), format(item.Bid), format(item.Ask)})
			case "provider-daily-bar":
				table.Append([]string{item.Date, item.Pair, format(item.Open), format(item.High), format(item.Low), format(item.Close)})
			default:
				table.Append([]string{item.Date, item.Pair, format(item.Value)})
			}
		}
		table.Render()
	}
	label := map[string]string{"central-parity": "人民币中间价", "onshore-reference": "在岸市场参考汇率", "indicative-quote": "参考行情", "provider-daily-bar": "来源日线", "noon-buying-rate": "纽约中午买入汇率"}[result.PriceType]
	provider := map[string]string{"CFETS": "中国外汇交易中心", "Sina": "新浪财经", "Federal Reserve via FRED": "美联储（经 FRED）"}[result.Provider]
	if provider == "" {
		provider = result.Provider
	}
	at := result.ObservationTime
	if at == "" {
		at = "来源未提供固定时点"
		if result.PriceType == "indicative-quote" {
			at = "见逐条报价时间"
		}
		if result.PriceType == "central-parity" {
			at = "仅保留公布日"
		}
	}
	zone := result.Timezone
	if zone == "unspecified" {
		zone = "来源未注明"
	}
	if _, err := fmt.Fprintf(out, "来源：%s；价格口径：%s；时点：%s；时区：%s。USD/CNY 表示1美元兑换的在岸人民币，USD/CNH 为离岸人民币。终端最多显示6位小数，JSON保留原始精度。\n", provider, label, at, zone); err != nil {
		return err
	}
	for _, note := range result.Notes {
		if _, err := fmt.Fprintln(out, note); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, item := range result.Observations {
		if item.Note != "" && !seen[item.Note] {
			seen[item.Note] = true
			if _, err := fmt.Fprintln(out, item.Note); err != nil {
				return err
			}
		}
	}
	return nil
}
