// Package strategy implements technical analysis strategies as CLI subcommands.
// Each strategy is a pure computation function + a display wrapper.
package strategy

import (
	"fmt"
	"time"

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

// printJSON 输出全部日期，不像表格只显示最后 20 行。指标保留完整精度。
func printJSON(cmd *cobra.Command, exCode, name, kind string, params map[string]any, note string, quotes []*eastmoney.Quote, series []jsonSeries, signals []Signal) error {
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
	return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{
		"ex_code": exCode, "name": name, "strategy": kind, "params": params, "freq": string(parsed), "source_note": note, "results": results, "signals": items,
	})
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

	table := tablewriter.NewWriter(out)
	table.SetHeader(headers)
	hs := make([]tablewriter.Colors, len(headers))
	for i := range headers {
		hs[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hs...)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")

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

// fetchOHLCV searches the stock and returns daily OHLCV data.
func fetchOHLCV(cmd *cobra.Command, code string, days int) (string, string, []*eastmoney.Quote, string, error) {
	secs, err := sina.Search(cmd.Context(), code)
	if err != nil {
		return "", "", nil, "", fmt.Errorf("搜索证券失败: %w", err)
	}
	if len(secs) == 0 {
		return "", "", nil, "", fmt.Errorf("未找到证券: %s", code)
	}
	sec := secs[0]

	req := &eastmoney.GetQuoteHistoryReq{Code: sec.Code}
	switch sec.ExChange {
	case "sh":
		req.MarketCode = 1
	case "sz":
		req.MarketCode = 0
	default:
		return "", "", nil, "", fmt.Errorf("不支持的交易所: %s", sec.ExChange)
	}

	freqText, _ := cmd.Flags().GetString("freq")
	req.Freq, err = eastmoney.ParseFreq(freqText)
	if err != nil {
		return "", "", nil, "", err
	}
	end := time.Now()
	begin := end.Add(-time.Duration(days*req.Freq.BarDays()) * 24 * time.Hour)
	req.Begin = begin.Format(eastmoney.TimeYYMMDD)
	req.End = end.Format(eastmoney.TimeYYMMDD)

	result, err := history.Get(cmd.Context(), req)
	if err != nil {
		return "", "", nil, "", err
	}
	if result.Note != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), result.Note)
	}
	return sec.ExCode, sec.Name, result.Quotes, result.Note, nil
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
	for _, sub := range []*cobra.Command{NewMACLI(), NewMACDCLI(), NewRSICLI(), NewBollCLI()} {
		sub.Flags().String("freq", "day", "K-line frequency: day, week, month")
		sub.Flags().Bool("json", false, "Output JSON")
		cmd.AddCommand(sub)
	}
	return cmd
}
