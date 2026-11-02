package strategy

import (
	"fmt"

	"github.com/alwqx/sec/indicator"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/spf13/cobra"
)

// ComputeMA calculates fast and slow moving averages from closing prices.
// A buy signal is generated when fast MA crosses above slow MA (golden cross).
// A sell signal is generated when fast MA crosses below slow MA (dead cross).
func ComputeMA(quotes []*eastmoney.Quote, fastPeriod, slowPeriod int) (headers []string, data [][]string, signals []Signal) {
	if len(quotes) < slowPeriod {
		return nil, nil, nil
	}

	prices, dates := strategyPrices(quotes)
	fastMA := indicator.SMA(prices, fastPeriod)
	slowMA := indicator.SMA(prices, slowPeriod)
	signals, labels := toSignals(quotes, prices, indicator.MASignals(fastMA, slowMA, max(fastPeriod, slowPeriod)-1), "☍ 金叉买入", "☍ 死叉卖出")

	headers = []string{"日期", "收盘", fmt.Sprintf("MA%d", fastPeriod), fmt.Sprintf("MA%d", slowPeriod), "信号"}
	data = make([][]string, len(prices))
	for i := range prices {
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), fv(fastMA[i]), fv(slowMA[i]), labels[i]}
	}
	return headers, data, signals
}

func fv(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}

func NewMACLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "ma",
		Short:         "Dual Moving Average crossover strategy",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE:          runMA,
	}
	cmd.Flags().IntP("fast", "f", 5, "Fast MA period")
	cmd.Flags().IntP("slow", "s", 20, "Slow MA period")
	return cmd
}

func runMA(cmd *cobra.Command, args []string) error {
	fast, _ := cmd.Flags().GetInt("fast")
	slow, _ := cmd.Flags().GetInt("slow")

	exCode, name, quotes, err := fetchOHLCV(cmd, args[0], 250)
	if err != nil {
		return err
	}

	headers, data, signals := ComputeMA(quotes, fast, slow)
	if headers == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "数据不足（需要至少 %d 个交易日）\n", slow)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s  策略: 双均线(%d,%d)\n\n", exCode, name, fast, slow)
	displayTable(cmd, headers, data, signals)
	return nil
}
