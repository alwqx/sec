// Package watch implements a persistent stock watchlist at ~/.sec/watchlist.json.
//
// Subcommands:
//
//	sec watch              show list with real-time quotes
//	sec watch add <code>   add stock(s) to watchlist
//	sec watch remove <code>  remove stock(s) from watchlist
package watch

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// WatchItem represents a single watched stock.
type WatchItem struct {
	Code    string `json:"code"`
	ExCode  string `json:"excode"`
	Name    string `json:"name"`
	AddedAt string `json:"added_at"`
}

// watchlistPath returns the path to the watchlist JSON file.
func watchlistPath() (string, error) {
	dir, err := utils.SecDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "watchlist.json"), nil
}

func loadWatchlist() ([]WatchItem, error) {
	path, err := watchlistPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var items []WatchItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func saveWatchlist(items []WatchItem) error {
	path, err := watchlistPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

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
	items, err := loadWatchlist()
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
	items, err := loadWatchlist()
	if err != nil {
		return fmt.Errorf("读取自选列表失败: %w", err)
	}

	existing := make(map[string]bool)
	for _, item := range items {
		existing[item.Code] = true
		existing[item.ExCode] = true
	}

	added := 0
	for _, code := range args {
		code = strings.TrimSpace(code)
		if existing[code] {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s 已在自选中\n", code)
			continue
		}

		secs := sina.Search(cmd.Context(), code)
		if len(secs) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s 未找到\n", code)
			continue
		}
		sec := secs[0]

		items = append(items, WatchItem{
			Code:    sec.Code,
			ExCode:  sec.ExCode,
			Name:    sec.Name,
			AddedAt: time.Now().Format("2006-01-02"),
		})
		existing[sec.Code] = true
		existing[sec.ExCode] = true
		added++
		fmt.Fprintf(cmd.OutOrStdout(), "  ✓ %s %s\n", sec.ExCode, sec.Name)
	}

	// Sort by code
	sort.Slice(items, func(i, j int) bool { return items[i].Code < items[j].Code })

	if added > 0 {
		if err := saveWatchlist(items); err != nil {
			return fmt.Errorf("保存失败: %w", err)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n已添加 %d 只，共 %d 只\n", added, len(items))
	return nil
}

func runWatchRemove(cmd *cobra.Command, args []string) error {
	items, err := loadWatchlist()
	if err != nil {
		return fmt.Errorf("读取自选列表失败: %w", err)
	}

	removeSet := make(map[string]bool)
	for _, code := range args {
		removeSet[strings.TrimSpace(code)] = true
	}

	removed := 0
	filtered := make([]WatchItem, 0, len(items))
	for _, item := range items {
		if removeSet[item.Code] || removeSet[item.ExCode] {
			fmt.Fprintf(cmd.OutOrStdout(), "  ✗ %s %s\n", item.ExCode, item.Name)
			removed++
		} else {
			filtered = append(filtered, item)
		}
	}

	if removed > 0 {
		if err := saveWatchlist(filtered); err != nil {
			return fmt.Errorf("保存失败: %w", err)
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\n已移除 %d 只，剩余 %d 只\n", removed, len(filtered))
	return nil
}
