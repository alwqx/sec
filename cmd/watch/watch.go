// Package watch implements a persistent stock watchlist at ~/.sec/watchlist.json.
//
// Subcommands:
//
//	sec watch              show list with real-time quotes
//	sec watch add <code>   add stock(s) to watchlist
//	sec watch remove <code>  remove stock(s) from watchlist
package watch

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/alwqx/sec/provider/sina"
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
	item   WatchItem
	price  float64
	chg    float64
	chgPct float64
	high   float64
	low    float64
	vol    float64
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
	if len(items) == 0 {
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
			slog.Warn("获取行情失败", "error", err)
		}
		for _, q := range qlist {
			quoteMap[q.ExCode] = q
		}
	}

	for _, item := range items {
		r := quoteRow{item: item}
		if q, ok := quoteMap[item.ExCode]; ok {
			r.price = q.Current
			r.chg = q.Current - q.YClose
			if q.YClose > 0 {
				r.chgPct = r.chg / q.YClose * 100
			}
			r.high = q.High
			r.low = q.Low
			r.vol = q.Volume
		}
		rows = append(rows, r)
	}

	// Display
	printWatchQuotes(cmd.OutOrStdout(), rows)
	return nil
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
			fmt.Sprintf("%.2f", r.price),
			fmt.Sprintf("%+.2f%%", r.chgPct),
			fmt.Sprintf("%+.2f", r.chg),
			fmt.Sprintf("%.2f", r.high),
			fmt.Sprintf("%.2f", r.low),
		}
		data = append(data, row)

		style := make([]tablewriter.Colors, len(headers))
		if r.chgPct > 0 {
			style[3] = tablewriter.Colors{tablewriter.FgRedColor, tablewriter.Bold}
		} else if r.chgPct < 0 {
			style[3] = tablewriter.Colors{tablewriter.FgGreenColor, tablewriter.Bold}
		}
		styles = append(styles, style)
	}

	table := tablewriter.NewWriter(out)
	table.SetHeader(headers)
	hs := make([]tablewriter.Colors, len(headers))
	for i := range headers {
		hs[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hs...)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetNoWhiteSpace(false)
	table.SetTablePadding("\t")

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
	for _, key := range args {
		secs := sina.Search(cmd.Context(), strings.TrimSpace(key))
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
	return nil
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
