package strategy

import (
	"fmt"
	"math"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/spf13/cobra"
)

// ComputeBollinger calculates Bollinger Bands:
//
//	Middle = SMA(period)
//	Upper = Middle + k × σ
//	Lower = Middle - k × σ
//
// Buy when price touches lower band and starts rising.
// Sell when price touches upper band and starts falling.
func ComputeBollinger(quotes []*eastmoney.Quote, period int, k float64) (headers []string, data [][]string, signals []Signal) {
	if len(quotes) < period {
		return nil, nil, nil
	}

	n := len(quotes)
	prices := make([]float64, n)
	dates := make([]string, n)
	for i, q := range quotes {
		prices[i] = q.Close
		dates[i] = q.Date.Format("2006-01-02")
	}

	middle := sma(prices, period)
	upper := make([]float64, n)
	lower := make([]float64, n)

	for i := period - 1; i < n; i++ {
		// Calculate stddev
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := prices[j] - middle[i]
			sum += diff * diff
		}
		stddev := math.Sqrt(sum / float64(period))
		upper[i] = middle[i] + k*stddev
		lower[i] = middle[i] - k*stddev
	}

	headers = []string{"日期", "收盘", "下轨", "中轨", "上轨", "信号"}
	data = make([][]string, n)

	for i := range prices {
		lo, mid, hi := lower[i], middle[i], upper[i]
		sig := "-"
		if i > 0 && lo > 0 && mid > 0 && hi > 0 {
			// Touch lower band → buy signal on next day's rise
			if prices[i-1] <= lower[i-1] && prices[i] > lower[i] {
				sig = "☍ 下轨买入"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "buy", Price: prices[i], Reason: "触及下轨回升"})
			} else if prices[i-1] >= upper[i-1] && prices[i] < upper[i] {
				sig = "☍ 上轨卖出"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "sell", Price: prices[i], Reason: "触及上轨回落"})
			}
		}
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), fv(lo), fv(mid), fv(hi), sig}
	}
	return
}

func NewBollCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "boll",
		Short:         "Bollinger Bands strategy",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE:          runBoll,
	}
	cmd.Flags().IntP("period", "p", 20, "MA period")
	cmd.Flags().Float64P("k", "k", 2.0, "Standard deviation multiplier")
	return cmd
}

func runBoll(cmd *cobra.Command, args []string) error {
	period, _ := cmd.Flags().GetInt("period")
	k, _ := cmd.Flags().GetFloat64("k")

	exCode, name, quotes, err := fetchOHLCV(cmd, args[0], 250)
	if err != nil {
		return err
	}

	headers, data, signals := ComputeBollinger(quotes, period, k)
	if headers == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "数据不足（需要至少 %d 个交易日）\n", period)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s  策略: 布林带(%d,%.1f)\n\n", exCode, name, period, k)
	displayTable(cmd, headers, data, signals)
	return nil
}
