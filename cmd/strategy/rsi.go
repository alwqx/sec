package strategy

import (
	"fmt"

	"github.com/alwqx/sec/indicator"
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

	prices, dates := strategyPrices(quotes)
	rsiValues := indicator.RSI(prices, period)
	signals, labels := toSignals(quotes, prices, indicator.RSISignals(rsiValues, period, overbought, oversold), "☍ 超卖买入", "☍ 超买卖出")

	headers = []string{"日期", "收盘", "RSI", "信号"}
	data = make([][]string, len(prices))
	for i := range prices {
		rsiStr := "-"
		if i >= period {
			rsiStr = fmt.Sprintf("%.1f", rsiValues[i])
		}
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), rsiStr, labels[i]}
	}
	return headers, data, signals
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
