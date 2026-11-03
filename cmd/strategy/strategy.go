// Package strategy implements technical analysis strategies as CLI subcommands.
// Each strategy is a pure computation function + a display wrapper.
package strategy

import (
	"fmt"
	"sort"
	"time"

	"github.com/alwqx/sec/backtest"
	"github.com/alwqx/sec/indicator"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/history"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// Signal represents a trading signal at a specific date.
type Signal struct {
	Date   time.Time
	Type   string // "buy", "sell", "hold"
	Price  float64
	Reason string
}

func strategyPrices(quotes []*eastmoney.Quote) ([]float64, []string) {
	prices := make([]float64, len(quotes))
	dates := make([]string, len(quotes))
	for i, q := range quotes {
		prices[i] = q.Close
		dates[i] = q.Date.Format("2006-01-02")
	}
	return prices, dates
}

// toSignals 转为带日期和价格的信号，并返回每个位置在表格里显示的文字。
func toSignals(quotes []*eastmoney.Quote, prices []float64, items []indicator.Signal, buyLabel, sellLabel string) ([]Signal, []string) {
	signals := make([]Signal, 0, len(items))
	labels := make([]string, len(quotes))
	for i := range labels {
		labels[i] = "-"
	}
	for _, item := range items {
		signals = append(signals, Signal{Date: quotes[item.Index].Date, Type: item.Type, Price: prices[item.Index], Reason: item.Reason})
		labels[item.Index] = buyLabel
		if item.Type == indicator.Sell {
			labels[item.Index] = sellLabel
		}
	}
	return signals, labels
}

// jsonSeries 是一条指标序列，下标 start 之前窗口不足，输出 null。
type jsonSeries struct {
	name   string
	values []float64
	start  int
}

// printJSON 输出全部日期，不像表格只显示最后 20 行。指标保留完整精度。result 为 nil 时不输出 backtest。
func printJSON(cmd *cobra.Command, exCode, name, kind string, params map[string]any, note string, quotes []*eastmoney.Quote, series []jsonSeries, signals []Signal, result *backtest.Result) error {
	freq, _ := cmd.Flags().GetString("freq")
	parsed, err := eastmoney.ParseFreq(freq)
	if err != nil {
		return err
	}
	results := make([]map[string]any, 0, len(quotes))
	for i, quote := range quotes {
		row := map[string]any{"date": quote.Date.Format("2006-01-02"), "close": quote.Close}
		for _, item := range series {
			if i < item.start {
				row[item.name] = nil
				continue
			}
			row[item.name] = item.values[i]
		}
		results = append(results, row)
	}
	items := make([]map[string]any, 0, len(signals))
	for _, signal := range signals {
		items = append(items, map[string]any{"date": signal.Date.Format("2006-01-02"), "type": signal.Type, "price": signal.Price, "reason": signal.Reason})
	}
	output := map[string]any{
		"ex_code": exCode, "name": name, "strategy": kind, "params": params, "freq": string(parsed), "source_note": note, "results": results, "signals": items,
	}
	if result != nil {
		output["backtest"] = result
	}
	return utils.PrintJSON(cmd.OutOrStdout(), output)
}

// checkPeriods 校验周期参数。周期小于 1 时指标全是无意义的 0。
func checkPeriods(periods map[string]int) error {
	for name, value := range periods {
		if value < 1 {
			return fmt.Errorf("--%s 必须大于 0，当前 %d", name, value)
		}
	}
	return nil
}

func wantBacktest(cmd *cobra.Command) bool {
	value, _ := cmd.Flags().GetBool("backtest")
	return value
}

func wantJSON(cmd *cobra.Command) bool {
	asJSON, _ := cmd.Flags().GetBool("json")
	return asJSON
}

// display shows the last N rows of a strategy result table.
func displayTable(cmd *cobra.Command, headers []string, data [][]string, signals []Signal) {
	out := cmd.OutOrStdout()
	// Show last 20 rows
	start := 0
	if len(data) > 20 {
		start = len(data) - 20
	}
	visible := data[start:]

	table := plainTable(out, headers)
	hs := make([]tablewriter.Colors, len(headers))
	for i := range headers {
		hs[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hs...)

	sigIdx := 0
	for i, row := range visible {
		colors := make([]tablewriter.Colors, len(row))
		// Color the signal column if present
		if sigIdx < len(signals) && start+i < len(data) {
			idx := start + i
			for _, s := range signals {
				if s.Date.Format("2006-01-02") == data[idx][0] {
					switch s.Type {
					case "buy":
						colors[len(row)-1] = tablewriter.Colors{tablewriter.FgRedColor, tablewriter.Bold}
					case "sell":
						colors[len(row)-1] = tablewriter.Colors{tablewriter.FgGreenColor, tablewriter.Bold}
					}
					sigIdx++
					break
				}
			}
		}
		table.Rich(row, colors)
	}
	table.Render()

	// Signal summary
	buyCount, sellCount := 0, 0
	for _, s := range signals {
		switch s.Type {
		case "buy":
			buyCount++
		case "sell":
			sellCount++
		}
	}
	fmt.Fprintf(out, "\n信号统计: 买入 %d 次 / 卖出 %d 次\n\n", buyCount, sellCount)
}

// analysis 是一只证券的行情。quotes 包含区间之前用来计算指标的预热数据，first 是区间第一个交易日的下标。
type analysis struct {
	exCode, name, note string
	quotes             []*eastmoney.Quote
	first              int
}

// defaultDays 是没有指定 --begin 时往前取的日历天数。
const defaultDays = 250

// fetchOHLCV 查询区间行情，并往前多取预热数据，和 Desktop 技术分析一致。回测使用前复权价格，避免除权日出现虚假的涨跌。
func fetchOHLCV(cmd *cobra.Command, code string, required int) (*analysis, error) {
	freqText, _ := cmd.Flags().GetString("freq")
	freq, err := eastmoney.ParseFreq(freqText)
	if err != nil {
		return nil, err
	}
	// 能在本地判断的错误先报，不用等联网查询。
	if wantBacktest(cmd) {
		if freq != eastmoney.FreqDay {
			return nil, fmt.Errorf("回测只支持日线")
		}
		if err := backtestConfig(cmd).Validate(); err != nil {
			return nil, err
		}
	}
	begin, end, err := strategyRange(cmd, freq)
	if err != nil {
		return nil, err
	}

	secs, err := sina.Search(cmd.Context(), code)
	if err != nil {
		return nil, fmt.Errorf("搜索证券失败: %w", err)
	}
	if len(secs) == 0 {
		return nil, fmt.Errorf("未找到证券: %s", code)
	}
	sec := secs[0]

	req := &eastmoney.GetQuoteHistoryReq{Code: sec.Code}
	switch sec.ExChange {
	case "sh":
		req.MarketCode = 1
	case "sz":
		req.MarketCode = 0
	default:
		return nil, fmt.Errorf("不支持的交易所: %s", sec.ExChange)
	}

	req.Freq = freq
	if wantBacktest(cmd) {
		if err := backtest.Supported(sec.ExCode); err != nil {
			return nil, err
		}
		req.FQT = eastmoney.QuoteFQTFront
	}
	beginDay, endDay := begin.Format("20060102"), end.Format("20060102")
	req.Begin = history.IndicatorWarmup(begin, required, freq).Format("20060102")
	req.End = endDay

	result, err := history.GetFrom(cmd.Context(), req, beginDay)
	if err != nil {
		return nil, err
	}
	if result.Note != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), result.Note)
	}
	quotes := make([]*eastmoney.Quote, 0, len(result.Quotes))
	for _, quote := range result.Quotes {
		if quote != nil {
			quotes = append(quotes, quote)
		}
	}
	first := sort.Search(len(quotes), func(i int) bool { return !quotes[i].Date.Before(begin) })
	if first == len(quotes) {
		return nil, fmt.Errorf("%s 至 %s 没有交易日", beginDay, endDay)
	}
	return &analysis{sec.ExCode, sec.Name, result.Note, quotes, first}, nil
}

// strategyRange 读取 --begin、--end。没有指定开始日期时，从结束日期往前取 defaultDays 个日历日，周线、月线按周期放大。
func strategyRange(cmd *cobra.Command, freq eastmoney.Freq) (time.Time, time.Time, error) {
	beginText, _ := cmd.Flags().GetString("begin")
	endText, _ := cmd.Flags().GetString("end")
	end := time.Now()
	var err error
	if endText != "" {
		if end, err = time.Parse("20060102", endText); err != nil {
			return end, end, fmt.Errorf("--end 须为 YYYYMMDD: %s", endText)
		}
	}
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	begin := end.AddDate(0, 0, -defaultDays*freq.BarDays())
	if beginText != "" {
		if begin, err = time.Parse("20060102", beginText); err != nil {
			return begin, end, fmt.Errorf("--begin 须为 YYYYMMDD: %s", beginText)
		}
	}
	if begin.After(end) {
		return begin, end, fmt.Errorf("--begin %s 晚于 --end %s", begin.Format("20060102"), end.Format("20060102"))
	}
	return begin, end, nil
}

// finish 去掉预热数据后输出表格或 JSON，指定 --backtest 时再按区间内的信号回测。
func (a *analysis) finish(cmd *cobra.Command, kind string, params map[string]any, title string, headers []string, data [][]string, signals []Signal, series []jsonSeries) error {
	begin := a.quotes[a.first].Date
	inRange := []Signal{}
	for _, signal := range signals {
		if !signal.Date.Before(begin) {
			inRange = append(inRange, signal)
		}
	}
	var result *backtest.Result
	if wantBacktest(cmd) {
		var err error
		if result, err = a.backtest(cmd, inRange); err != nil {
			return err
		}
	}
	if wantJSON(cmd) {
		trimmed := make([]jsonSeries, len(series))
		for i, item := range series {
			trimmed[i] = jsonSeries{item.name, item.values[a.first:], max(item.start-a.first, 0)}
		}
		return printJSON(cmd, a.exCode, a.name, kind, params, a.note, a.quotes[a.first:], trimmed, inRange, result)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s  策略: %s\n\n", a.exCode, a.name, title)
	displayTable(cmd, headers, data[a.first:], inRange)
	if result != nil {
		printBacktest(cmd.OutOrStdout(), result)
	}
	return nil
}

// NewStrategyCLI returns the parent strategy command with subcommands.
func NewStrategyCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "strategy",
		Aliases:       []string{"st"},
		Short:         "Technical analysis strategies",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Print(cmd.UsageString())
		},
	}
	defaults := backtest.DefaultConfig()
	for _, sub := range []*cobra.Command{NewMACLI(), NewMACDCLI(), NewRSICLI(), NewBollCLI()} {
		sub.Flags().String("freq", "day", "K-line frequency: day, week, month")
		sub.Flags().Bool("json", false, "Output JSON")
		sub.Flags().StringP("begin", "b", "", "Begin date 20250101, default 250 days before the end")
		sub.Flags().StringP("end", "e", "", "End date 20251231, default today")
		sub.Flags().Bool("backtest", false, "Backtest the signals on forward-adjusted daily prices")
		sub.Flags().Float64("cash", defaults.Cash, "Backtest: initial cash")
		sub.Flags().Float64("commission", defaults.Commission*100, "Backtest: commission rate %, charged on buys and sells")
		sub.Flags().Float64("min-commission", defaults.MinCommission, "Backtest: minimum commission per trade")
		sub.Flags().Float64("stamp-tax", defaults.StampTax*100, "Backtest: stamp tax rate % on sells")
		sub.Flags().Float64("slippage", defaults.Slippage*100, "Backtest: slippage %, added to buys and taken from sells")
		cmd.AddCommand(sub)
	}
	return cmd
}
