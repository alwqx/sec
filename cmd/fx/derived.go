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

func derivationFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "Output JSON with formulas, original inputs and alignment details")
	cmd.Flags().Bool("refresh", false, "Fetch fresh source data and update the cache")
	cmd.Flags().Bool("no-cache", false, "Disable reading and writing the cache")
	cmd.Flags().BoolP("debug", "D", false, "Show debug logs")
	cmd.Flags().Duration("max-skew", 0, "Maximum time difference for Sina quotes (0s to 60s); nonzero values explicitly permit approximate alignment")
	cmd.Flags().Duration("max-age", 15*time.Minute, "Maximum age of Sina input quotes (greater than 0s, at most 24h)")
}

func newCrossCLI() *cobra.Command {
	cmd := &cobra.Command{Use: "cross", Short: "Derive cross rates from a single source with explicit time alignment", Args: cobra.NoArgs, RunE: runCross}
	derivationFlags(cmd)
	cmd.Flags().String("source", "fred", "Input source: fred (New York noon), cfets (fixed reference time), or sina (latest quotes)")
	cmd.Flags().StringSlice("pairs", []string{"HKD/CNY"}, "Derived currency pairs; CNH is available only with Sina quotes")
	cmd.Flags().StringP("begin", "b", "", "Start date YYYYMMDD for daily inputs; defaults to one year before the end date")
	cmd.Flags().StringP("end", "e", "", "End date YYYYMMDD for daily inputs; defaults to today in Beijing time")
	cmd.Flags().String("at", "16:00", "CFETS reference time in Beijing: 10:00, 11:00, 14:00, 15:00 or 16:00")
	return cmd
}

func crossRequest(cmd *cobra.Command, now time.Time) (*forex.Request, []string, string, error) {
	source, _ := cmd.Flags().GetString("source")
	if source != "fred" && source != "cfets" && source != "sina" {
		return nil, nil, "", fmt.Errorf("交叉汇率来源可选 fred、cfets 或 sina")
	}
	targets, _ := cmd.Flags().GetStringSlice("pairs")
	for i, pair := range targets {
		targets[i] = strings.ToUpper(strings.TrimSpace(pair))
	}
	if err := forex.ValidatePairs(targets, forex.CrossPairs(source)); err != nil {
		return nil, nil, "", err
	}
	if source != "sina" && (cmd.Flags().Changed("max-skew") || cmd.Flags().Changed("max-age")) {
		return nil, nil, "", fmt.Errorf("报价时间差和年龄参数仅适用于新浪报价")
	}
	if source != "cfets" && cmd.Flags().Changed("at") {
		return nil, nil, "", fmt.Errorf("--at 仅适用于CFETS参考汇率")
	}
	req := &forex.Request{}
	if source == "sina" {
		if cmd.Flags().Changed("begin") || cmd.Flags().Changed("end") {
			return nil, nil, "", fmt.Errorf("新浪交叉汇率使用最近报价，不能指定历史日期")
		}
		req.Pairs = []string{"USD/CNY", "USD/CNH", "USD/HKD"}
		if _, err := quotePolicy(cmd, now); err != nil {
			return nil, nil, "", err
		}
		return req, targets, source, nil
	}
	req.Begin, _ = cmd.Flags().GetString("begin")
	req.End, _ = cmd.Flags().GetString("end")
	if req.End == "" {
		req.End = now.In(forex.Beijing).Format("20060102")
	}
	if req.Begin == "" {
		end, err := time.Parse("20060102", req.End)
		if err != nil {
			return nil, nil, "", fmt.Errorf("无效汇率截至日期")
		}
		req.Begin = end.AddDate(-1, 0, 0).Format("20060102")
	}
	if source == "fred" {
		req.Pairs = forex.FREDPairs
		return req, targets, source, req.Validate(forex.FREDPairs, "")
	}
	req.Pairs = forex.OnshorePairs
	req.At, _ = cmd.Flags().GetString("at")
	return req, targets, source, chinamoney.ValidateReference(req)
}

func quotePolicy(cmd *cobra.Command, now time.Time) (forex.QuotePolicy, error) {
	skew, _ := cmd.Flags().GetDuration("max-skew")
	age, _ := cmd.Flags().GetDuration("max-age")
	policy := forex.QuotePolicy{Now: now, MaxSkew: skew, MaxAge: age}
	return policy, policy.Validate()
}

func derivedCache(cmd *cobra.Command, source string) (string, bool, error) {
	refresh, _ := cmd.Flags().GetBool("refresh")
	disabled, _ := cmd.Flags().GetBool("no-cache")
	if disabled {
		return "", refresh, nil
	}
	dir, err := utils.SecDir("cache", "fx", source)
	return dir, refresh, err
}

func runCross(cmd *cobra.Command, _ []string) error {
	req, targets, source, err := crossRequest(cmd, time.Now())
	if err != nil {
		return err
	}
	cacheSource := source
	if source == "cfets" {
		cacheSource = "chinamoney"
	}
	dir, refresh, err := derivedCache(cmd, cacheSource)
	if err != nil {
		return err
	}
	var input *forex.Result
	switch source {
	case "fred":
		client := fred.NewClient(dir)
		client.Refresh = refresh
		input, err = client.ForexHistory(cmd.Context(), req)
	case "cfets":
		client := chinamoney.NewClient(dir)
		client.Refresh = refresh
		input, err = client.Reference(cmd.Context(), req)
	case "sina":
		client := sina.NewForexClient(dir)
		client.Refresh = refresh
		input, err = client.ForexQuotes(cmd.Context(), req)
	}
	if err != nil {
		return err
	}
	policy := forex.QuotePolicy{}
	if source == "sina" {
		policy, err = quotePolicy(cmd, time.Now())
		if err != nil {
			return err
		}
	}
	result, err := forex.CrossRates(input, targets, policy)
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

func newSpreadCLI() *cobra.Command {
	cmd := &cobra.Command{Use: "spread", Short: "Derive the CNY/CNH quote spread with explicit timestamp and age checks", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if _, err := quotePolicy(cmd, time.Now()); err != nil {
			return err
		}
		dir, refresh, err := derivedCache(cmd, "sina")
		if err != nil {
			return err
		}
		client := sina.NewForexClient(dir)
		client.Refresh = refresh
		input, err := client.ForexQuotes(cmd.Context(), &forex.Request{Pairs: []string{"USD/CNY", "USD/CNH"}})
		if err != nil {
			return err
		}
		policy, err := quotePolicy(cmd, time.Now())
		if err != nil {
			return err
		}
		result, err := forex.QuoteSpread(input, policy)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printSpread(cmd.OutOrStdout(), result)
	}}
	derivationFlags(cmd)
	return cmd
}

func alignmentLabel(a *forex.Alignment) string {
	if a == nil {
		return "未核验"
	}
	labels := map[string]string{"same-fixed-time": "同一固定时点", "synchronized": "同一报价时点", "within-tolerance": "近似对齐", "timestamp-mismatch": "时点不一致", "missing-timestamp": "缺少时间", "stale-input": "报价过期", "future-timestamp": "时间异常", "missing-input": "缺少输入", "missing-value": "输入缺失", "unknown-timezone": "时区未知"}
	label := labels[a.Status]
	if label == "" {
		label = "无法对齐"
	}
	if a.Status == "within-tolerance" && a.SkewSeconds != nil {
		label += fmt.Sprintf("（差%.0f秒）", *a.SkewSeconds)
	}
	return label
}

func printSpread(out io.Writer, result *forex.SpreadResult) error {
	format := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(*v, 'f', 6, 64), "0"), ".")
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"输入货币对", "报价时间（北京时间）", "参考报价"})
	for _, item := range result.Inputs {
		table.Append([]string{item.Pair, formatQuoteTime(item.ObservedAt), format(item.Value)})
	}
	table.Render()
	metrics := tablewriter.NewWriter(out)
	metrics.SetBorder(false)
	metrics.SetHeader([]string{"价差（人民币/美元）", "点差", "相对差（%）", "相对差（基点）", "隐含CNH/CNY", "对齐状态"})
	metrics.Append([]string{format(result.Difference), format(result.Pips), format(result.RelativePercent), format(result.RelativeBPS), format(result.ImpliedCNHCNY), alignmentLabel(&result.Alignment)})
	metrics.Render()
	if _, err := fmt.Fprintf(out, "来源：新浪财经；结果为推导值。允许时间差：%.0f秒；最大报价年龄：%.0f秒。\n", result.MaxSkewSeconds, result.MaxAgeSeconds); err != nil {
		return err
	}
	for _, note := range append(append([]string{}, result.Notes...), result.Alignment.Note) {
		if note != "" {
			if _, err := fmt.Fprintln(out, note); err != nil {
				return err
			}
		}
	}
	return nil
}
