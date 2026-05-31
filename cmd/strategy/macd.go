package strategy

import (
	"fmt"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/spf13/cobra"
)

// ComputeMACD calculates the MACD indicator:
//
//	MACD = EMA(fast) - EMA(slow)
//	Signal = EMA(MACD, signalPeriod)
//	Histogram = MACD - Signal
//
// A buy signal fires when MACD crosses above Signal; sell when below.
func ComputeMACD(quotes []*eastmoney.Quote, fast, slow, signal int) (headers []string, data [][]string, signals []Signal) {
	if len(quotes) < slow+signal {
		return nil, nil, nil
	}

	prices := make([]float64, len(quotes))
	dates := make([]string, len(quotes))
	for i, q := range quotes {
		prices[i] = q.Close
		dates[i] = q.Date.Format("2006-01-02")
	}

	emaFast := ema(prices, fast)
	emaSlow := ema(prices, slow)

	// MACD line = EMA(fast) - EMA(slow)
	macdLine := make([]float64, len(prices))
	for i := range prices {
		if emaFast[i] > 0 && emaSlow[i] > 0 {
			macdLine[i] = emaFast[i] - emaSlow[i]
		}
	}

	signalLine := ema(macdLine, signal)
	histogram := make([]float64, len(prices))
	for i := range prices {
		if macdLine[i] != 0 || signalLine[i] != 0 {
			histogram[i] = (macdLine[i] - signalLine[i]) * 2 // ×2 for visibility
		}
	}

	headers = []string{"日期", "收盘", "MACD", "信号线", "柱", "信号"}
	data = make([][]string, len(prices))
	var prevMACD, prevSignal float64

	for i := range prices {
		m, s := macdLine[i], signalLine[i]
		sig := "-"
		if i > 0 && prevMACD != 0 && m != 0 {
			if prevMACD <= prevSignal && m > s {
				sig = "☍ 金叉买入"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "buy", Price: prices[i], Reason: "MACD金叉"})
			} else if prevMACD >= prevSignal && m < s {
				sig = "☍ 死叉卖出"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "sell", Price: prices[i], Reason: "MACD死叉"})
			}
		}
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), fv2(m), fv2(s), fv2(histogram[i]), sig}
		prevMACD, prevSignal = m, s
	}
	return
}

// ema returns the exponential moving average for a given period.
func ema(prices []float64, period int) []float64 {
	result := make([]float64, len(prices))
	if period <= 0 || len(prices) < period {
		return result
	}
	k := 2.0 / float64(period+1)

	// Seed with SMA
	sum := 0.0
	for i := 0; i < period && i < len(prices); i++ {
		sum += prices[i]
	}
	result[period-1] = sum / float64(period)

	for i := period; i < len(prices); i++ {
		result[i] = prices[i]*k + result[i-1]*(1-k)
	}
	return result
}

func fv2(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}

func NewMACDCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "macd",
		Short:         "MACD indicator strategy",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE:          runMACD,
	}
	cmd.Flags().IntP("fast", "f", 12, "Fast EMA period")
	cmd.Flags().IntP("slow", "s", 26, "Slow EMA period")
	cmd.Flags().IntP("signal", "g", 9, "Signal line period")
	return cmd
}

func runMACD(cmd *cobra.Command, args []string) error {
	fast, _ := cmd.Flags().GetInt("fast")
	slow, _ := cmd.Flags().GetInt("slow")
	signal, _ := cmd.Flags().GetInt("signal")

	exCode, name, quotes, err := fetchOHLCV(cmd, args[0], 250)
	if err != nil {
		return err
	}

	headers, data, signals := ComputeMACD(quotes, fast, slow, signal)
	if headers == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "数据不足（需要至少 %d 个交易日）\n", slow+signal)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s  策略: MACD(%d,%d,%d)\n\n", exCode, name, fast, slow, signal)
	displayTable(cmd, headers, data, signals)
	return nil
}
