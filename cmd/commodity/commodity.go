// Package commodity 实现 sec commodity：大宗商品期货的最新行情和历史日行情，数据来自新浪期货。
package commodity

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// historyDays 是只指定 --end 时往前取的日历天数。
const historyDays = 30

func NewCommodityCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "commodity",
		Aliases: []string{"co"},
		Short:   "Print futures quotes of commodities: crude oil, copper, iron ore, rebar, soybean meal; history of one with --inst and --begin/--end",
		RunE:    commodityHandler,
	}
	cmd.Flags().Bool("json", false, "Output JSON")
	cmd.Flags().StringP("inst", "i", "", "Commodity, case-insensitive: "+sina.CommodityList()+"; default all of them")
	cmd.Flags().StringP("begin", "b", "", "Begin date 20250101; with --begin or --end, print daily history of --inst")
	cmd.Flags().StringP("end", "e", "", fmt.Sprintf("End date 20250131, default today; with only --end, the range starts %d days earlier", historyDays))
	return cmd
}

func commodityHandler(cmd *cobra.Command, args []string) error {
	instText, _ := cmd.Flags().GetString("inst")
	beginText, _ := cmd.Flags().GetString("begin")
	endText, _ := cmd.Flags().GetString("end")
	asJSON, _ := cmd.Flags().GetBool("json")
	items := sina.Commodities
	if instText != "" {
		item, err := sina.FindCommodity(instText)
		if err != nil {
			return err
		}
		items = []sina.Commodity{item}
	}
	if beginText == "" && endText == "" {
		// 个别品种没有行情时，文本模式先输出其余品种再报错；--json 有失败就报错，不输出缺了品种的结果。
		quotes, failed, err := sina.QueryCommodities(cmd.Context(), items)
		if err != nil {
			return err
		}
		if len(failed) > 0 && (asJSON || len(quotes) == 0) {
			return fmt.Errorf("查询失败：%s", strings.Join(failed, "；"))
		}
		if asJSON {
			return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"results": quotes})
		}
		printQuotes(cmd.OutOrStdout(), quotes)
		if len(failed) > 0 {
			return fmt.Errorf("查询失败：%s", strings.Join(failed, "；"))
		}
		return nil
	}

	if instText == "" {
		return fmt.Errorf("查询历史行情需要用 --inst 指定品种，可选：%s", sina.CommodityList())
	}
	begin, end, err := utils.ParseRange(beginText, endText, historyDays)
	if err != nil {
		return err
	}
	item := items[0]
	bars, err := sina.QueryCommodityHistory(cmd.Context(), item, begin.Format(utils.LayoutYYMMDD), end.Format(utils.LayoutYYMMDD))
	if err != nil {
		return err
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"id": item.ID, "name": item.Name, "exchange": item.Exchange, "unit": item.Unit, "results": bars})
	}
	printHistory(cmd.OutOrStdout(), item, bars)
	return nil
}

func number(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

// volume 为 0 时显示 -：新浪的外盘历史行情大多没有成交量。
func volume(value float64) string {
	if value == 0 {
		return "-"
	}
	return number(value)
}

func optional(value *float64, format string) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf(format, *value)
}

// changeColor 按涨跌着色：红涨绿跌，没有涨跌时不着色。
func changeColor(rate *float64) tablewriter.Colors {
	if rate == nil {
		return tablewriter.Colors{}
	}
	return termtable.Change(*rate, false)
}

func printQuotes(out io.Writer, quotes []*sina.CommodityQuote) {
	headers := []string{"代码", "品种", "最新", "涨跌", "涨跌幅", "开盘", "最高", "最低", "昨结算", "单位", "时间"}
	table := termtable.New(out, headers, termtable.LeftHeader, termtable.NoWrap)
	for _, q := range quotes {
		row := []string{q.ID, q.Name, number(q.Price), optional(q.Change, "%+.2f"), optional(q.ChangeRate, "%+.2f%%"),
			number(q.Open), number(q.High), number(q.Low), optional(q.PrevSettle, "%g"), q.Unit, q.Date + " " + q.Time}
		colors := make([]tablewriter.Colors, len(headers))
		for i := 2; i <= 4; i++ {
			colors[i] = changeColor(q.ChangeRate)
		}
		table.Rich(row, colors)
	}
	table.Render()
	fmt.Fprintln(out, "\n国内为主力连续合约，换月时价格会跳空；涨跌相对昨日结算价，国际品种的日行情没有结算价、用前一日收盘价，和这里的涨跌略有差别。行情来自新浪期货，有延迟，不构成投资建议。")
}

func printHistory(out io.Writer, item sina.Commodity, bars []sina.CommodityBar) {
	if len(bars) == 0 {
		fmt.Fprintln(out, "所选区间没有行情")
		return
	}
	headers := []string{"日期", "收盘", "涨跌", "涨跌幅", "开盘", "最高", "最低", "结算", "成交量"}
	table := termtable.New(out, headers, termtable.LeftHeader, termtable.NoWrap)
	// 国内品种前一天没有结算价时，涨跌相对收盘价，标 *。
	marked := false
	for _, bar := range bars {
		mark := ""
		if item.Domestic && bar.Basis == "close" {
			mark, marked = "*", true
		}
		row := []string{bar.Date, number(bar.Close), optional(bar.Change, "%+.2f") + mark, optional(bar.ChangeRate, "%+.2f%%"),
			number(bar.Open), number(bar.High), number(bar.Low), optional(bar.Settle, "%g"), volume(bar.Volume)}
		colors := make([]tablewriter.Colors, len(headers))
		for i := 1; i <= 3; i++ {
			colors[i] = changeColor(bar.ChangeRate)
		}
		table.Rich(row, colors)
	}
	fmt.Fprintf(out, "%s（%s，%s），单位 %s\n\n", item.Name, item.ID, item.Exchange, item.Unit)
	table.Render()
	if item.Domestic {
		fmt.Fprintln(out, "\n涨跌相对前一日结算价；主力连续合约换月时价格会跳空。")
	} else {
		fmt.Fprintln(out, "\n涨跌相对前一日收盘价（新浪外盘日行情没有结算价）。")
	}
	if marked {
		fmt.Fprintln(out, "* 前一日没有结算价（早年数据常见），涨跌相对前一日收盘价。")
	}
}
