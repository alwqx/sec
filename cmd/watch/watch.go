// Package watch implements a persistent stock watchlist at ~/.sec/watchlist.json.
//
// Subcommands:
//
//	sec watch              show list with real-time quotes
//	sec watch add <code>   add stock(s) to watchlist
//	sec watch remove <code>  remove stock(s) from watchlist
package watch

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/watchlist"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

type WatchItem = watchlist.Item

var newWatchStore = watchlist.Default

// NewWatchCLI returns the watch command with subcommands.
func NewWatchCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "watch",
		Aliases:       []string{"w"},
		Short:         "Manage stock watchlist",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: runWatchShow,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().Bool("json", false, "Output JSON")

	cmd.AddCommand(
		&cobra.Command{
			Use:   "add [codes...]",
			Short: "Add stocks to watchlist",
			Args:  cobra.MinimumNArgs(1),
			RunE:  runWatchAdd,
		},
		&cobra.Command{
			Use:   "remove [codes...]",
			Short: "Remove stocks from watchlist",
			Args:  cobra.MinimumNArgs(1),
			RunE:  runWatchRemove,
		},
	)
	return cmd
}

// quoteRow Build quote request
type quoteRow struct {
	priceIsYClose bool
	hasChange     bool
	digits        int // 价格的小数位数：基金净值 4 位，场内基金 3 位，其他 2 位
	item          WatchItem
	price         float64
	chg           float64
	chgPct        float64
	high          float64
	low           float64
	vol           float64
}

func runWatchShow(cmd *cobra.Command, args []string) error {
	store, err := newWatchStore()
	if err != nil {
		return err
	}
	items, err := store.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("读取自选列表失败: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if len(items) == 0 {
		if asJSON {
			return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"results": []watchJSONRow{}})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "自选列表为空。使用 sec watch add <代码> 添加股票\n")
		return nil
	}

	rows := make([]quoteRow, 0, len(items))
	// Query in batches via multi-search quote pattern
	exCodes := make([]string, len(items))
	for i, item := range items {
		exCodes[i] = item.ExCode
	}
	slog.DebugContext(cmd.Context(), "runWatchShow", "items", strings.Join(exCodes, ","))

	quoteMap := make(map[string]*sina.SecurityQuote)
	if len(exCodes) > 0 {
		qlist, err := sina.QueryQuoteList(cmd.Context(), exCodes)
		if err != nil {
			if asJSON {
				return fmt.Errorf("获取行情失败: %w", err)
			}
			slog.Warn("获取行情失败", "error", err)
		}
		for _, q := range qlist {
			quoteMap[q.ExCode] = q
		}
	}

	for _, item := range items {
		r := quoteRow{item: item}
		if q, ok := quoteMap[item.ExCode]; ok {
			// 开盘前当前价是 0，显示昨收，不计算涨跌。
			r.price, r.priceIsYClose = q.EffectivePrice()
			r.digits = priceDigits(q)
			r.chg, r.chgPct, r.hasChange = q.Change()
			r.high = q.High
			r.low = q.Low
			r.vol = q.Volume
		}
		rows = append(rows, r)
	}

	if asJSON {
		results := make([]watchJSONRow, 0, len(rows))
		for _, r := range rows {
			results = append(results, newWatchJSONRow(r))
		}
		return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"results": results})
	}

	// Display
	printWatchQuotes(cmd.OutOrStdout(), rows)
	return nil
}

// watchJSONRow 是 sec watch 的 --json 输出，不可用的值是 null。
type watchJSONRow struct {
	ExCode        string   `json:"ex_code"`
	Name          string   `json:"name"`
	Price         *float64 `json:"price"`
	PriceIsYClose bool     `json:"price_is_yclose"`
	Change        *float64 `json:"change"`
	ChangePct     *float64 `json:"change_pct"`
	High          *float64 `json:"high"`
	Low           *float64 `json:"low"`
}

func newWatchJSONRow(r quoteRow) watchJSONRow {
	row := watchJSONRow{
		ExCode: r.item.ExCode, Name: r.item.Name, PriceIsYClose: r.priceIsYClose,
		Price: utils.PositiveOrNil(r.price), High: utils.PositiveOrNil(r.high), Low: utils.PositiveOrNil(r.low),
	}
	if r.hasChange {
		row.Change, row.ChangePct = &r.chg, &r.chgPct
	}
	return row
}

// priceDigits 是价格的小数位数：基金净值 4 位，场内基金 3 位（交易所报价精确到 0.001），其他 2 位。
func priceDigits(q *sina.SecurityQuote) int {
	switch {
	case q.NAV:
		return 4
	case sina.IsFundCode(q.ExCode):
		return 3
	}
	return 2
}

// format 返回按价格小数位数格式化的 format，sign 为 true 时带正负号。
func (r quoteRow) format(sign bool) string {
	digits := r.digits
	if digits == 0 {
		digits = 2
	}
	if sign {
		return fmt.Sprintf("%%+.%df", digits)
	}
	return fmt.Sprintf("%%.%df", digits)
}

func (r quoteRow) priceText() string {
	if r.priceIsYClose {
		return fmt.Sprintf(r.format(false)+"（昨收）", r.price)
	}
	return changeText(r.format(false), r.price, r.price > 0)
}

// changeText 在数值可用时按 format 格式化，否则显示 —，不把缺失显示成 0。
func changeText(format string, value float64, known bool) string {
	if !known {
		return "—"
	}
	return fmt.Sprintf(format, value)
}

func printWatchQuotes(out io.Writer, rows []quoteRow) {
	fmt.Fprintf(out, "\n自选组合 (%d 只)\n\n", len(rows))

	headers := []string{"代码", "名称", "现价", "涨跌幅", "涨跌额", "最高", "最低"}
	styles := make([][]tablewriter.Colors, 0, len(rows))
	data := make([][]string, 0, len(rows))

	for _, r := range rows {
		row := []string{
			r.item.ExCode,
			r.item.Name,
			r.priceText(),
			changeText("%+.2f%%", r.chgPct, r.hasChange),
			changeText(r.format(true), r.chg, r.hasChange),
			changeText(r.format(false), r.high, r.high > 0),
			changeText(r.format(false), r.low, r.low > 0),
		}
		data = append(data, row)

		style := make([]tablewriter.Colors, len(headers))
		style[3] = termtable.Change(r.chgPct, false)
		styles = append(styles, style)
	}

	table := termtable.New(out, headers, termtable.LeftHeader)

	for i, row := range data {
		table.Rich(row, styles[i])
	}
	table.Render()
	fmt.Fprintln(out)
}

func runWatchAdd(cmd *cobra.Command, args []string) error {
	store, err := newWatchStore()
	if err != nil {
		return err
	}
	items, err := store.List(cmd.Context())
	if err != nil {
		return err
	}
	added := 0
	var failures []error
	for _, key := range args {
		secs, err := sina.Search(cmd.Context(), strings.TrimSpace(key))
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s 搜索失败\n", key)
			failures = append(failures, fmt.Errorf("搜索 %s 失败: %w", key, err))
			continue
		}
		if len(secs) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s 未找到\n", key)
			continue
		}
		var changed bool
		items, changed, err = store.Add(cmd.Context(), *secs[0])
		if err != nil {
			return err
		}
		if changed {
			added++
			fmt.Fprintf(cmd.OutOrStdout(), "  ✓ %s %s\n", secs[0].ExCode, secs[0].Name)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s 已在自选中\n", secs[0].ExCode)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n已添加 %d 只，共 %d 只\n", added, len(items))
	return errors.Join(failures...)
}

func runWatchRemove(cmd *cobra.Command, args []string) error {
	store, err := newWatchStore()
	if err != nil {
		return err
	}
	items, removed, err := store.Remove(cmd.Context(), args...)
	if err != nil {
		return err
	}
	for _, item := range removed {
		fmt.Fprintf(cmd.OutOrStdout(), "  ✗ %s %s\n", item.ExCode, item.Name)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n已移除 %d 只，剩余 %d 只\n", len(removed), len(items))
	return nil
}
