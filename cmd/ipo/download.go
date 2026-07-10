package ipo

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// newDownloadCmd creates the `sec ipo download` subcommand.
func newDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download <code>",
		Short: "Download IPO prospectus (招股说明书) PDFs for a stock",
		Long: "Query IPO-related announcements (招股意向书/招股说明书/发行公告) from CNINFO " +
			"for the given stock code and download the PDF files.\n" +
			"Without --all or --index, enters interactive selection mode.",
		Aliases: []string{"dl", "d"},
		Example: `  sec ipo download 300750                    # interactive selection
  sec ipo download 300750 --all               # download all prospectuses
  sec ipo download 300750 --index 1           # download the first (latest) one
  sec ipo download 300750 -i 1,3,5            # download specific items
  sec ipo download 300750 -o ./prospectuses   # save to directory
  sec ipo download 300750 --dry-run           # preview only
  sec ipo download 300750 --since 2018-01-01 --until 2018-12-31 --all`,
		Args: cobra.ExactArgs(1),
		RunE: runDownload,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().IntP("size", "n", 30, "Maximum number of announcements to query")
	cmd.Flags().StringP("output-dir", "o", ".", "Directory to save downloaded PDFs")
	cmd.Flags().BoolP("all", "a", false, "Download all matching prospectus PDFs")
	cmd.Flags().StringP("index", "i", "", "Download specific items by number (comma-separated, 1-based), e.g. 1,3,5")
	cmd.Flags().Bool("force", false, "Overwrite existing files")
	cmd.Flags().Bool("dry-run", false, "Preview only (list without downloading)")
	cmd.Flags().String("since", "", "Only show/download prospectuses on or after this date (YYYY-MM-DD)")
	cmd.Flags().String("until", "", "Only show/download prospectuses on or before this date (YYYY-MM-DD)")
	return cmd
}

func runDownload(cmd *cobra.Command, args []string) error {
	code := args[0]
	size, _ := cmd.Flags().GetInt("size")
	if size <= 0 {
		size = 30
	}
	if size > 5000 {
		size = 5000
	}
	outputDir, _ := cmd.Flags().GetString("output-dir")
	all, _ := cmd.Flags().GetBool("all")
	indexStr, _ := cmd.Flags().GetString("index")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	since, _ := cmd.Flags().GetString("since")
	until, _ := cmd.Flags().GetString("until")

	ctx := cmd.Context()

	stockCode, orgID, secName, err := resolveStock(ctx, code)
	if err != nil {
		return err
	}

	stockParam := fmt.Sprintf("%s,%s", stockCode, orgID)
	announcements, err := cninfo.QueryIPOs(ctx, stockParam, size)
	if err != nil {
		return fmt.Errorf("查询招股书失败: %w", err)
	}

	if len(announcements) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到 %s (%s) 的 IPO 公告（cninfo 可能未收录或代码有误）\n", secName, stockCode)
		return nil
	}

	// Filter by date
	filtered := filterByDate(announcements, since, until)
	// Filter out invalid/cancelled announcements
	valid := filterValidPDFs(filtered)

	if len(valid) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "%s (%s) 没有符合条件的招股书 PDF\n", secName, stockCode)
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n%s (%s) IPO 相关公告 (来源: 巨潮资讯 cninfo.com.cn)\n\n", secName, stockCode)

	// Always show the list
	printDownloadList(out, valid)

	if dryRun {
		fmt.Fprintln(out)
		return nil
	}

	slog.DebugContext(ctx, "ipo download: entering selection", "count", len(valid))

	// Determine which items to download
	var selected []int
	switch {
	case all:
		for i := range valid {
			selected = append(selected, i)
		}
	case indexStr != "":
		selected, err = parseIndexList(indexStr, len(valid))
		if err != nil {
			return fmt.Errorf("invalid --index: %w", err)
		}
	default:
		// Interactive mode
		selected, err = promptSelection(out, cmd.InOrStdin(), len(valid))
		if err != nil {
			return fmt.Errorf("选择失败: %w", err)
		}
		if len(selected) == 0 {
			fmt.Fprintln(out, "已取消")
			return nil
		}
	}

	slog.DebugContext(ctx, "ipo download: selection done", "selected", selected)

	if len(selected) == 0 {
		fmt.Fprintln(out, "没有选中任何文件")
		return nil
	}

	// Download
	fmt.Fprintf(out, "\n开始下载到 %s ...\n\n", outputDir)
	downloaded, skipped, failed := 0, 0, 0

	for _, idx := range selected {
		a := valid[idx]
		filename := buildFilename(stockCode, secName, a)
		destPath := filepath.Join(outputDir, filename)

		idxDisplay := downloaded + skipped + failed + 1

		// Check if file exists
		if !force {
			if _, statErr := os.Stat(destPath); statErr == nil {
				fmt.Fprintf(out, "  [%d/%d] %s\n      已存在，跳过\n", idxDisplay, len(selected), filename)
				skipped++
				continue
			}
		}

		fmt.Fprintf(out, "  [%d/%d] 下载: %s\n", idxDisplay, len(selected), filename)

		dlErr := downloadOne(ctx, a, destPath)
		if dlErr != nil {
			fmt.Fprintf(out, "      失败: %v\n", dlErr)
			failed++
			continue
		}

		sizeStr := ""
		if a.AdjunctSize > 0 {
			sizeStr = fmt.Sprintf(" (%s)", utils.HumanByte(float64(a.AdjunctSize)))
		}
		fmt.Fprintf(out, "      完成%s\n", sizeStr)
		downloaded++
	}

	fmt.Fprintf(out, "\n下载完成: %d 新下载, %d 跳过, %d 失败\n", downloaded, skipped, failed)
	return nil
}

// filterValidPDFs filters out announcements with invalid flags or non-PDF adjuncts.
func filterValidPDFs(announcements []*cninfo.Announcement) []*cninfo.Announcement {
	out := make([]*cninfo.Announcement, 0, len(announcements))
	for _, a := range announcements {
		if a.ExistFlag != 0 || a.InvalidationFlag != 0 {
			continue
		}
		// Skip if no adjunct URL (can't download)
		if a.AdjunctURL == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// printDownloadList renders the announcement list with index numbers for selection.
func printDownloadList(out io.Writer, announcements []*cninfo.Announcement) {
	table := tablewriter.NewWriter(out)
	headers := []string{"#", "公告日期", "公告标题", "大小"}
	table.SetHeader(headers)
	headerStyles := make([]tablewriter.Colors, len(headers))
	for i := range headers {
		headerStyles[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(headerStyles...)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")

	for i, a := range announcements {
		sizeStr := "-"
		if a.AdjunctSize > 0 {
			sizeStr = utils.HumanByte(float64(a.AdjunctSize))
		}
		title := a.Title
		if len([]rune(title)) > 60 {
			title = string([]rune(title)[:57]) + "..."
		}

		date := a.Date
		if len(date) == 8 {
			date = date[:4] + "-" + date[4:6] + "-" + date[6:]
		}

		idxStr := strconv.Itoa(i + 1)
		styles := make([]tablewriter.Colors, len(headers))
		if strings.Contains(title, "招股") {
			styles[2] = tablewriter.Colors{tablewriter.Bold}
		}
		row := []string{idxStr, date, title, sizeStr}
		table.Rich(row, styles)
	}
	table.Render()
}

// parseIndexList parses a comma-separated list of 1-based indices.
func parseIndexList(s string, max int) ([]int, error) {
	parts := strings.Split(s, ",")
	seen := make(map[int]bool)
	var out []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid index %q: must be a number", p)
		}
		if n < 1 || n > max {
			return nil, fmt.Errorf("index %d out of range [1, %d]", n, max)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n-1) // convert to 0-based
		}
	}
	return out, nil
}

// promptSelection displays a prompt and reads the user's selection from stdin.
func promptSelection(out io.Writer, in io.Reader, max int) ([]int, error) {
	fmt.Fprintf(out, "\n请选择要下载的编号 (all=全部, q=退出, 默认=1):\n")

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("读取输入失败: %w", err)
	}
	line = strings.TrimSpace(line)

	// Default: first item
	if line == "" {
		return []int{0}, nil
	}

	// Quit
	if strings.ToLower(line) == "q" || strings.ToLower(line) == "quit" {
		return nil, nil
	}

	// All
	if strings.ToLower(line) == "all" || strings.ToLower(line) == "a" {
		var all []int
		for i := 0; i < max; i++ {
			all = append(all, i)
		}
		return all, nil
	}

	return parseIndexList(line, max)
}

// downloadOne downloads a single announcement PDF.
func downloadOne(ctx context.Context, a *cninfo.Announcement, destPath string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}
	return cninfo.DownloadPDF(ctx, a.AdjunctURL, destPath)
}

// titleKeywords lists recognized prospectus title patterns, ordered by priority.
var titleKeywords = []string{
	"招股说明书",
	"招股意向书",
	"上市公告书",
	"发行公告",
}

// extractShortTitle extracts a short, filesystem-safe label from an announcement title.
func extractShortTitle(title string) string {
	for _, kw := range titleKeywords {
		if strings.Contains(title, kw) {
			return kw
		}
	}
	// Fallback: look for "招股" more broadly
	if strings.Contains(title, "招股") {
		return "招股书"
	}
	// Generic: use first N chars of title
	runes := []rune(title)
	if len(runes) > 15 {
		return string(runes[:15])
	}
	return title
}

// buildFilename constructs a PDF filename from stock info and announcement.
// Format: {code}_{name}_{shortTitle}_{date}.pdf
func buildFilename(code, name string, a *cninfo.Announcement) string {
	shortTitle := extractShortTitle(a.Title)
	date := a.Date
	if len(date) == 0 && a.Time > 0 {
		date = fmt.Sprintf("%d", a.Time/1000)
	}
	raw := fmt.Sprintf("%s_%s_%s_%s", code, name, shortTitle, date)
	safe := sanitizeFilename(raw)
	return safe + ".pdf"
}

// sanitizeFilename replaces characters that are illegal in Windows/macOS/Linux filenames.
func sanitizeFilename(name string) string {
	// Characters illegal on Windows: < > : " / \ | ? *
	// Also replace newlines and tabs
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
		"\n", "",
		"\r", "",
		"\t", " ",
	)
	return replacer.Replace(name)
}
