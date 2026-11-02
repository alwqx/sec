package strategy

import (
	"fmt"

	"github.com/alwqx/sec/indicator"
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

	prices, dates := strategyPrices(quotes)
	middle, upper, lower := indicator.Bollinger(prices, period, k)
	signals, labels := toSignals(quotes, prices, indicator.BollingerSignals(prices, upper, lower, period-1), "☍ 下轨买入", "☍ 上轨卖出")

	headers = []string{"日期", "收盘", "下轨", "中轨", "上轨", "信号"}
	data = make([][]string, len(prices))
	for i := range prices {
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), fv(lower[i]), fv(middle[i]), fv(upper[i]), labels[i]}
	}
	return headers, data, signals
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
