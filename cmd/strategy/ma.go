package strategy

import (
	"fmt"

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

	prices := make([]float64, len(quotes))
	dates := make([]string, len(quotes))
	for i, q := range quotes {
		prices[i] = q.Close
		dates[i] = q.Date.Format("2006-01-02")
	}

	fastMA := sma(prices, fastPeriod)
	slowMA := sma(prices, slowPeriod)

	headers = []string{"日期", "收盘", fmt.Sprintf("MA%d", fastPeriod), fmt.Sprintf("MA%d", slowPeriod), "信号"}
	data = make([][]string, len(prices))
	var prevFast, prevSlow float64

	for i := range prices {
		f, s := fastMA[i], slowMA[i]
		sig := "-"
		if i > 0 && prevFast > 0 && prevSlow > 0 && f > 0 && s > 0 {
			if prevFast <= prevSlow && f > s {
				sig = "☍ 金叉买入"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "buy", Price: prices[i], Reason: "金叉"})
			} else if prevFast >= prevSlow && f < s {
				sig = "☍ 死叉卖出"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "sell", Price: prices[i], Reason: "死叉"})
			}
		}
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), fv(f), fv(s), sig}
		prevFast, prevSlow = f, s
	}
	return
}

// sma returns the simple moving average for a given period.
// Values before the period length are 0 (insufficient data).
func sma(prices []float64, period int) []float64 {
	result := make([]float64, len(prices))
	if period <= 0 || len(prices) < period {
		return result
	}
	sum := 0.0
	for i := 0; i < period-1; i++ {
		sum += prices[i]
	}
	for i := period - 1; i < len(prices); i++ {
		sum += prices[i]
		result[i] = sum / float64(period)
		sum -= prices[i-period+1]
	}
	return result
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
