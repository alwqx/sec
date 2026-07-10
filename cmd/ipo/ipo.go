// Package ipo provides the `sec ipo ...` command family:
//
//	sec ipo list        — recent IPO listings (name / code / listing date / price / PE / first-day change)
//	sec ipo calendar    — upcoming IPO calendar (purchase / publish / listing dates)
//	sec ipo prospectus  — IPO prospectus (招股说明书) URLs from CNINFO for a stock code
package ipo

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// constants
const (
	defaultListSize     = 30
	defaultPageSize     = 30
	defaultMaxListTotal = 30
	maxColumnWidth      = 24
)

// NewIPOCLI is the entrypoint; registered in cmd/cmd.go.
func NewIPOCLI() *cobra.Command {
	root := &cobra.Command{
		Use:               "ipo",
		Short:             "China mainland A-share IPO listings & prospectus (沪深京 IPO)",
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.AddCommand(
		newListCmd(),
		newCalendarCmd(),
		newProspectusCmd(),
		newDownloadCmd(),
	)

	return root
}

/* ----------------------------- ipo list ----------------------------- */

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent or historical IPO listings (新股上市列表)",
		Long: "List recent or historical A-share IPO listings from East Money PUSH2, " +
			"listing the most-recent listings first. Filter by listing date with --since / --until.",
		Aliases: []string{"ls"},
		Example: `  sec ipo list                     # most recent 30 listings
  sec ipo list --size 50            # most recent 50
  sec ipo list --since 2024-01-01   # from specific date
  sec ipo list --until 2024-12-31   # up to specific date`,
		RunE: runList,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().IntP("size", "n", defaultListSize, "Number of listings to display (max 5000)")
	cmd.Flags().String("since", "", "Only show listings on or after this date (YYYY-MM-DD)")
	cmd.Flags().String("until", "", "Only show listings on or before this date (YYYY-MM-DD)")
	return cmd
}

func runList(cmd *cobra.Command, args []string) error {
	size, err := cmd.Flags().GetInt("size")
	if err != nil {
		return fmt.Errorf("size flag: %w", err)
	}
	if size <= 0 {
		size = defaultMaxListTotal
	}
	if size > 5000 {
		size = 5000
	}
	since, _ := cmd.Flags().GetString("since")
	until, _ := cmd.Flags().GetString("until")

	ctx := cmd.Context()

	// 拉取足够多的数据以满足日期过滤
	req := &eastmoney.IPOListReq{
		PageNum:  1,
		PageSize: size,
		MaxTotal: size,
	}
	items, _, err := eastmoney.ListIPO(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "failed to ListIPO", "since", since, "until", until, "error", err)
		return fmt.Errorf("获取新股列表失败: %w", err)
	}

	filtered := make([]*eastmoney.IListing, 0, len(items))
	for _, it := range items {
		// 日期区间过滤
		if since != "" && it.ListingDate != "-" && it.ListingDate < strings.ReplaceAll(since, "-", "") {
			continue
		}
		if until != "" && it.ListingDate != "-" && it.ListingDate > strings.ReplaceAll(until, "-", "") {
			continue
		}
		filtered = append(filtered, it)
	}
	if len(filtered) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "未找到 IPO 记录（尝试放宽年份或 --size）")
		return nil
	}

	out := cmd.OutOrStdout()
	printIPOList(out, filtered)
	return nil
}

func printIPOList(out io.Writer, items []*eastmoney.IListing) {
	table := tablewriter.NewWriter(out)
	// 仅保留确定性高的字段（东方财富各股发行价/PE 字段顺序会随 fs 改变，
	// 列表 UI 仅承诺代码、名称、上市日）
	headers := []string{"代码", "名称", "上市日"}
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

	for _, it := range items {
		listingDate := "-"
		if it.ListingDate != "-" && it.ListingDate != "" {
			listingDate = dateSafe(it.ListingDate)
		}

		name := it.Name
		if len([]rune(name)) > maxColumnValueWidth()/2 {
			name = string([]rune(name)[:maxColumnValueWidth()/2]) + ".."
		}

		row := []string{it.Code, name, listingDate}
		table.Append(row)
	}
	table.Render()
	fmt.Fprintf(out, "\n共 %d 条; 使用 --since / --until 按上市日过滤。\n", len(items))
}

// maxColumnValueWidth returns the max display width parameter for name column
func maxColumnValueWidth() int {
	return maxColumnWidth
}

/* --------------------------- ipo calendar --------------------------- */

func newCalendarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Show upcoming IPO calendar (新股排期)",
		Long: "List the upcoming IPO schedule including purchase dates, " +
			"publish dates, and estimated listing dates. Data is from East Money.",
		Aliases: []string{"cal"},
		Example: `  sec ipo calendar                       # next month
  sec ipo calendar --since 2026-06-01
  sec ipo calendar --until 2026-08-31
  sec ipo calendar --since 2026-06-01 --until 2026-06-30`,
		RunE: runCalendar,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().String("since", "", "Start date (YYYY-MM-DD), default: today")
	cmd.Flags().String("until", "", "End date (YYYY-MM-DD), default: start + 30 days")
	return cmd
}

func runCalendar(cmd *cobra.Command, args []string) error {
	since, _ := cmd.Flags().GetString("since")
	until, _ := cmd.Flags().GetString("until")

	ctx := cmd.Context()

	// 东方财富 push2ex 新股日历接口已于 2026-07 前后被下线（无论是 push2 / push2ex 均 404）。
	// 折中：利用 CNINFO 查询 IPO/发行公告日期（首次公开发行及上市公告），
	// 按给定日期范围做过滤，输出"公告日期"作为最接近"排期"的视图。
	announcements, err := cninfo.QueryIPOByDateRange(ctx, since, until, 50)
	if err != nil {
		return fmt.Errorf("获取新股日历失败: %w", err)
	}

	if len(announcements) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "未找到 IPO 相关公告；请确认 --since/--until 范围正确")
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\nIPO 公告日期视图（来源: 巨潮资讯 cninfo.com.cn; 接口: 首次公开发行及上市）\n\n")
	printIPOCalendar(out, announcements)
	return nil
}

// dateSafe 若 s 是 YYYYMMDD 格式则算成 YYYY-MM-DD；否则原样返回
func dateSafe(s string) string {
	if len(s) == 8 {
		return s[:4] + "-" + s[4:6] + "-" + s[6:]
	}
	return s
}

func printIPOCalendar(out io.Writer, announcements []*cninfo.Announcement) {
	table := tablewriter.NewWriter(out)
	headers := []string{"公告日期", "代码", "名称", "公告标题", "大小"}
	table.SetHeader(headers)
	hs := make([]tablewriter.Colors, len(headers))
	for i := range headers {
		hs[i] = tablewriter.Colors{tablewriter.Bold}
	}
	table.SetHeaderColor(hs...)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")

	for _, a := range announcements {
		title := a.Title
		if len([]rune(title)) > 60 {
			title = string([]rune(title)[:57]) + "..."
		}
		sizeStr := "-"
		if a.AdjunctSize > 0 {
			sizeStr = utils.HumanByte(float64(a.AdjunctSize))
		}
		row := []string{dateSafe(a.Date), a.SecCode, a.SecName, title, sizeStr}
		styles := make([]tablewriter.Colors, len(headers))
		if strings.HasPrefix(a.Title, "招股说明") {
			styles[3] = tablewriter.Colors{tablewriter.Bold}
		}
		table.Rich(row, styles)
	}
	table.Render()
	fmt.Fprintf(out, "\n共 %d 条; 这是按公告日期排序的 IPO / 发行相关公告列表，可近似看作\"新股排期\"视图。\n", len(announcements))
}

/* ------------------------- ipo prospectus ------------------------- */

func newProspectusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prospectus",
		Short: "List IPO prospectus (招股说明书) URLs for a stock",
		Long: "Fetch IPO-related announcements (招股意向书/招股说明书/发行公告) from CNINFO " +
			"for the given stock code, and print the download link for each PDF.",
		Aliases: []string{"psp"},
		Example: `  sec ipo prospectus 300750         # prospectus for 宁德时代
  sec ipo prospectus 600036 --size 10   # first 10 prospectus PDFs
  sec ipo prospectus 300750 --since 2018-01-01 --until 2018-12-31`,
		Args: cobra.ExactArgs(1),
		RunE: runProspectus,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().IntP("size", "n", 20, "Maximum number of prospectus entries to list")
	cmd.Flags().String("since", "", "Only show prospectuses on or after this date (YYYY-MM-DD)")
	cmd.Flags().String("until", "", "Only show prospectuses on or before this date (YYYY-MM-DD)")
	return cmd
}

// resolveStock 将用户输入（代码或名称）解析为标准 A 股代码 + cninfo orgId + 证券简称。
func resolveStock(ctx context.Context, input string) (code, orgID, name string, err error) {
	secs := sina.Search(ctx, input)
	sec := firstAShare(secs)
	if sec != nil {
		code = sec.Code
		name = sec.Name
		var resolvedName string
		var lookupErr error
		orgID, resolvedName, lookupErr = cninfo.LookupOrgID(ctx, code)
		if lookupErr != nil {
			err = fmt.Errorf("查找 %s 的公司身份失败: %w", code, lookupErr)
			return
		}
		if orgID == "" {
			err = fmt.Errorf("查找 %s 的公司身份为空", code)
			return
		}
		if resolvedName != "" {
			name = resolvedName
		}
		return
	}

	// sina 没搜到 A 股，尝试把用户输入当作 A 股代码
	code = sanitizeCode(input)
	if !isACode(code) {
		err = fmt.Errorf("未找到 %s 对应的 A 股代码，请确认代码无误", input)
		return
	}
	orgID, name, err = cninfo.LookupOrgID(ctx, code)
	if err != nil || orgID == "" {
		err = fmt.Errorf("查找 %s 的公司身份失败: %w", code, err)
		return
	}
	return
}

func runProspectus(cmd *cobra.Command, args []string) error {
	code := args[0]
	size, _ := cmd.Flags().GetInt("size")
	if size <= 0 {
		size = 20
	}
	if size > 5000 {
		size = 5000
	}
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

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\n%s (%s) IPO 相关公告 (来源: 巨潮资讯 cninfo.com.cn)\n\n", secName, stockCode)

	filtered := filterByDate(announcements, since, until)

	printProspectus(out, filtered)
	fmt.Fprintln(out)
	return nil
}

// firstAShare 从 sina 搜索结果中优先选 A 股条目（跳过港股/美股同名项）
func firstAShare(secs []*sina.BasicSecurity) *sina.BasicSecurity {
	for _, s := range secs {
		if s == nil {
			continue
		}
		// 6 位数字代码 → A 股
		if len(s.Code) == 6 {
			return s
		}
		// SH/BJ 前缀
		if len(s.ExCode) >= 2 {
			prefix := strings.ToLower(s.ExCode[:2])
			if prefix == "sh" || prefix == "sz" || prefix == "bj" {
				return s
			}
		}
	}
	// fallback: 任意一条
	if len(secs) > 0 {
		return secs[0]
	}
	return nil
}

// sanitizeCode 把用户输入形如 "SZ300750" / "300750" 统一为纯 6 位代码
func sanitizeCode(code string) string {
	c := strings.ToUpper(strings.TrimSpace(code))
	c = strings.TrimPrefix(c, "SH")
	c = strings.TrimPrefix(c, "SZ")
	c = strings.TrimPrefix(c, "BJ")
	c = strings.TrimPrefix(c, "HK")
	return strings.TrimSpace(c)
}

func isACode(code string) bool {
	if len(code) != 6 {
		return false
	}
	prefix := code[:2]
	return prefix == "60" || prefix == "68" || prefix == "30" || prefix == "00" || prefix == "83" || prefix == "87" || prefix == "43" || prefix == "40"
}

func filterByDate(announcements []*cninfo.Announcement, since, until string) []*cninfo.Announcement {
	if since == "" && until == "" {
		return announcements
	}
	sinceInt := strings.ReplaceAll(since, "-", "")
	untilInt := strings.ReplaceAll(until, "-", "")
	out := make([]*cninfo.Announcement, 0, len(announcements))
	for _, a := range announcements {
		if since != "" && a.Date < sinceInt {
			continue
		}
		if until != "" && a.Date > untilInt {
			continue
		}
		out = append(out, a)
	}
	return out
}

func printProspectus(out io.Writer, announcements []*cninfo.Announcement) {
	if len(announcements) == 0 {
		fmt.Fprintln(out, "（经日期过滤后无匹配记录）")
		return
	}

	table := tablewriter.NewWriter(out)
	headers := []string{"公告日期", "公告标题", "大小", "PDF 链接"}
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

	seenProspectus := false
	for _, a := range announcements {
		sizeStr := "-"
		if a.AdjunctSize > 0 {
			sizeStr = utils.HumanByte(float64(a.AdjunctSize))
		}
		title := a.Title
		if len([]rune(title)) > 60 {
			title = string([]rune(title)[:57]) + "..."
		}
		isIPO := strings.Contains(title, "招股")
		if isIPO {
			seenProspectus = true
		}

		styles := make([]tablewriter.Colors, len(headers))
		if isIPO {
			styles[1] = tablewriter.Colors{tablewriter.Bold}
		}
		row := []string{a.Date, title, sizeStr, a.PDFURL}
		table.Rich(row, styles)
	}
	table.Render()

	if !seenProspectus {
		slog.Warn("no exact '招股' announcement in results; all IPO-related announcements are shown")
	}
}
