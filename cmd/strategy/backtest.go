package strategy

import (
	"fmt"
	"io"
	"time"

	"github.com/alwqx/sec/backtest"
	"github.com/alwqx/sec/indicator"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// backtestConfig 读取回测参数。命令行的费率以 % 为单位，比如佣金 0.025 表示万分之二点五。
func backtestConfig(cmd *cobra.Command) backtest.Config {
	flags := cmd.Flags()
	cfg := backtest.Config{}
	cfg.Cash, _ = flags.GetFloat64("cash")
	cfg.MinCommission, _ = flags.GetFloat64("min-commission")
	for name, target := range map[string]*float64{"commission": &cfg.Commission, "stamp-tax": &cfg.StampTax, "slippage": &cfg.Slippage} {
		value, _ := flags.GetFloat64(name)
		*target = value / 100
	}
	return cfg
}

// backtest 用区间内的交易日和信号回测。
func (a *analysis) backtest(cmd *cobra.Command, signals []Signal) (*backtest.Result, error) {
	index := map[time.Time]int{}
	for i, quote := range a.quotes {
		index[quote.Date] = i
	}
	items := make([]indicator.Signal, 0, len(signals))
	for _, signal := range signals {
		if i, ok := index[signal.Date]; ok {
			items = append(items, indicator.Signal{Index: i, Type: signal.Type, Reason: signal.Reason})
		}
	}
	return backtest.RunQuotes(a.exCode, a.quotes, a.first, items, backtestConfig(cmd))
}

// plainTable 返回无边框、制表符分隔的表格。
func plainTable(out io.Writer, headers []string) *tablewriter.Table {
	table := tablewriter.NewWriter(out)
	table.SetHeader(headers)
	table.SetAutoWrapText(false)
	table.SetBorder(false)
	table.SetHeaderLine(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")
	return table
}

func percentText(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%+.2f%%", *value)
}

func drawdownText(p backtest.Performance) string {
	if p.MaxDrawdown == 0 {
		return "0.00%"
	}
	return fmt.Sprintf("-%.2f%%（%s 至 %s）", p.MaxDrawdown, p.DrawdownStart, p.DrawdownEnd)
}

// printBacktest 输出回测结果：和买入持有对比的表现、成交明细和没有成交的信号。
func printBacktest(out io.Writer, r *backtest.Result) {
	fmt.Fprintf(out, "回测: %s 至 %s，前复权日线，初始资金 %.0f 元\n", r.Begin, r.End, r.Config.Cash)
	fmt.Fprintf(out, "信号在收盘产生，下一交易日开盘成交，每次全仓买入或全部卖出；按 100 股一手，开盘涨停买不进、跌停卖不出、停牌顺延。\n")
	fmt.Fprintf(out, "成本: 佣金 %.4g%%（最低 %.0f 元），卖出印花税 %.4g%%，滑点 %.4g%%\n\n", r.Config.Commission*100, r.Config.MinCommission, r.Config.StampTax*100, r.Config.Slippage*100)
	table := plainTable(out, []string{"", "策略", "买入持有"})
	total := func(p backtest.Performance) string { return fmt.Sprintf("%+.2f%%", p.TotalReturn) }
	table.AppendBulk([][]string{
		{"总收益", total(r.Strategy), total(r.Benchmark)},
		{"年化收益", percentText(r.Strategy.Annualized), percentText(r.Benchmark.Annualized)},
		{"最大回撤", drawdownText(r.Strategy), drawdownText(r.Benchmark)},
		{"期末资产", fmt.Sprintf("%.2f", r.Strategy.FinalEquity), fmt.Sprintf("%.2f", r.Benchmark.FinalEquity)},
	})
	table.Render()
	winRate := "-"
	if r.WinRate != nil {
		winRate = fmt.Sprintf("%.0f%%", *r.WinRate)
	}
	fmt.Fprintf(out, "\n完成 %d 轮买卖，胜率 %s，总费用 %.2f 元，持仓时间 %.0f%%\n", r.RoundTrips, winRate, r.TotalFees, r.Exposure)
	if r.OpenPosition {
		fmt.Fprintln(out, "区间结束时仍持仓，期末资产按最后收盘价计算，没有扣卖出成本")
	}
	if len(r.Trades) > 0 {
		fmt.Fprintln(out, "\n【成交】")
		trades := plainTable(out, []string{"成交日", "方向", "价格", "股数", "费用", "盈亏", "信号"})
		for _, t := range r.Trades {
			side, profit := "买入", "-"
			if t.Side == indicator.Sell {
				side = "卖出"
			}
			if t.Profit != nil {
				profit = fmt.Sprintf("%+.2f", *t.Profit)
			}
			trades.Append([]string{t.Date, side, fmt.Sprintf("%.3f", t.Price), fmt.Sprint(t.Shares), fmt.Sprintf("%.2f", t.Fee), profit, t.SignalDate + " " + t.Reason})
		}
		trades.Render()
	}
	if len(r.Events) > 0 {
		fmt.Fprintln(out, "\n【未成交或顺延】")
		for _, e := range r.Events {
			fmt.Fprintf(out, "  %s %s\n", e.Date, e.Reason)
		}
	}
	fmt.Fprintln(out, "\n回测结果由规则机械模拟，分红没有单独计入现金，ST 股的 5% 涨跌停按普通股票处理；不构成投资建议。")
}
