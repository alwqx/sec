package strategy

import (
	"fmt"

	"github.com/alwqx/sec/indicator"
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
	if len(quotes) < max(fast, slow)+signal {
		return nil, nil, nil
	}

	prices, dates := strategyPrices(quotes)
	macdLine, signalLine, histogram := indicator.MACD(prices, fast, slow, signal)
	signals, labels := toSignals(quotes, prices, indicator.MACDSignals(macdLine, signalLine, indicator.MACDStart(fast, slow, signal)), "☍ 金叉买入", "☍ 死叉卖出")

	headers = []string{"日期", "收盘", "MACD", "信号线", "柱", "信号"}
	data = make([][]string, len(prices))
	for i := range prices {
		data[i] = []string{dates[i], fmt.Sprintf("%.2f", prices[i]), fv2(macdLine[i]), fv2(signalLine[i]), fv2(histogram[i]), labels[i]}
	}
	return headers, data, signals
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
	if err := checkPeriods(map[string]int{"fast": fast, "slow": slow, "signal": signal}); err != nil {
		return err
	}

	exCode, name, quotes, note, err := fetchOHLCV(cmd, args[0], 250)
	if err != nil {
		return err
	}

	headers, data, signals := ComputeMACD(quotes, fast, slow, signal)
	if headers == nil {
		if wantJSON(cmd) {
			return fmt.Errorf("数据不足（需要至少 %d 根 K 线）", max(fast, slow)+signal)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "数据不足（需要至少 %d 根 K 线）\n", max(fast, slow)+signal)
		return nil
	}

	if wantJSON(cmd) {
		prices, _ := strategyPrices(quotes)
		line, signalLine, histogram := indicator.MACD(prices, fast, slow, signal)
		start := indicator.MACDStart(fast, slow, signal)
		return printJSON(cmd, exCode, name, "macd", map[string]any{"fast": fast, "slow": slow, "signal": signal}, note, quotes, []jsonSeries{
			{"macd", line, max(fast, slow) - 1},
			{"signal", signalLine, start},
			{"histogram", histogram, start},
		}, signals)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s  策略: MACD(%d,%d,%d)\n\n", exCode, name, fast, slow, signal)
	displayTable(cmd, headers, data, signals)
	return nil
}
