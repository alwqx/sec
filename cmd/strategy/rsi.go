package strategy

import (
	"fmt"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/spf13/cobra"
)

// ComputeRSI calculates the Relative Strength Index for a given period.
// RSI > overbought suggests overbought (sell signal when crossing back down).
// RSI < oversold suggests oversold (buy signal when crossing back up).
func ComputeRSI(quotes []*eastmoney.Quote, period int, overbought, oversold float64) (headers []string, data [][]string, signals []Signal) {
	if len(quotes) < period+1 {
		return nil, nil, nil
	}

	prices := make([]float64, len(quotes))
	dates := make([]string, len(quotes))
	for i, q := range quotes {
		prices[i] = q.Close
		dates[i] = q.Date.Format("2006-01-02")
	}

	// Calculate RSI
	gains := make([]float64, len(prices))
	losses := make([]float64, len(prices))
	for i := 1; i < len(prices); i++ {
		diff := prices[i] - prices[i-1]
		if diff > 0 {
			gains[i] = diff
		} else {
			losses[i] = -diff
		}
	}

	// Initial average
	avgGain := 0.0
	avgLoss := 0.0
	for i := 1; i <= period; i++ {
		avgGain += gains[i]
		avgLoss += losses[i]
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	rsiValues := make([]float64, len(prices))
	if avgLoss == 0 {
		rsiValues[period] = 100
	} else {
		rsiValues[period] = 100 - 100/(1+avgGain/avgLoss)
	}

	// Wilder's smoothing for subsequent values
	for i := period + 1; i < len(prices); i++ {
		avgGain = (avgGain*float64(period-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + losses[i]) / float64(period)
		if avgLoss == 0 {
			rsiValues[i] = 100
		} else {
			rsiValues[i] = 100 - 100/(1+avgGain/avgLoss)
		}
	}

	headers = []string{"日期", "收盘", "RSI", "信号"}
	data = make([][]string, len(prices))
	var prevRSI float64

	for i := range prices {
		r := rsiValues[i]
		sig := "-"
		if i > 0 && r > 0 && prevRSI > 0 {
			if prevRSI < oversold && r >= oversold {
				sig = "☍ 超卖买入"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "buy", Price: prices[i], Reason: fmt.Sprintf("RSI %.0f回升", r)})
			} else if prevRSI > overbought && r <= overbought {
				sig = "☍ 超买卖出"
				signals = append(signals, Signal{Date: quotes[i].Date, Type: "sell", Price: prices[i], Reason: fmt.Sprintf("RSI %.0f回落", r)})
			}
		}
		rsiStr := "-"
		if r > 0 {
			rsiStr = fmt.Sprintf("%.1f", r)
		}
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), rsiStr, sig}
		prevRSI = r
	}
	return
}

func NewRSICLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "rsi",
		Short:         "RSI overbought/oversold strategy",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		RunE:          runRSI,
	}
	cmd.Flags().IntP("period", "p", 14, "RSI period")
	cmd.Flags().Float64("oversold", 30, "Oversold threshold")
	cmd.Flags().Float64("overbought", 70, "Overbought threshold")
	return cmd
}

func runRSI(cmd *cobra.Command, args []string) error {
	period, _ := cmd.Flags().GetInt("period")
	overbought, _ := cmd.Flags().GetFloat64("overbought")
	oversold, _ := cmd.Flags().GetFloat64("oversold")

	exCode, name, quotes, err := fetchOHLCV(cmd, args[0], 250)
	if err != nil {
		return err
	}

	headers, data, signals := ComputeRSI(quotes, period, overbought, oversold)
	if headers == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "数据不足（需要至少 %d 个交易日）\n", period+1)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s  策略: RSI(%d)\n\n", exCode, name, period)
	fmt.Fprintf(cmd.OutOrStdout(), "超买阈值: %.0f  超卖阈值: %.0f\n\n", overbought, oversold)
	displayTable(cmd, headers, data, signals)
	return nil
}
