// Package strategy implements technical analysis strategies as CLI subcommands.
// Each strategy is a pure computation function + a display wrapper.
package strategy

import (
	"fmt"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
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
func fetchOHLCV(cmd *cobra.Command, code string, days int) (string, string, []*eastmoney.Quote, error) {
	secs := sina.Search(cmd.Context(), code)
	if len(secs) == 0 {
		return "", "", nil, fmt.Errorf("未找到证券: %s", code)
	}
	sec := secs[0]

	req := &eastmoney.GetQuoteHistoryReq{Code: sec.Code}
	switch sec.ExChange {
	case "sh":
		req.MarketCode = 1
	case "sz":
		req.MarketCode = 0
	default:
		return "", "", nil, fmt.Errorf("不支持的交易所: %s", sec.ExChange)
	}

	end := time.Now()
	begin := end.Add(-time.Duration(days) * 24 * time.Hour)
	req.Begin = begin.Format(eastmoney.TimeYYMMDD)
	req.End = end.Format(eastmoney.TimeYYMMDD)

	quotes, err := eastmoney.GetQuoteHistory(cmd.Context(), req)
	if err != nil {
		return "", "", nil, err
	}
	return sec.ExCode, sec.Name, quotes, nil
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
	cmd.AddCommand(NewMACLI(), NewMACDCLI(), NewRSICLI(), NewBollCLI())
	return cmd
}
