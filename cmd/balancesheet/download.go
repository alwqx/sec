package balancesheet

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/spf13/cobra"
)

func NewBalanceSheetDownloadCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "balance-sheet-download",
		Aliases:       []string{"bsd"},
		Short:         "Download annual report PDFs from CNINFO",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: BalanceSheetDownloadHandler,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().StringP("year", "y", "", "Year, e.g. 2024")
	cmd.Flags().String("start-year", "", "Start year for range download, e.g. 2024")
	cmd.Flags().String("end-year", "", "End year for range download, e.g. 2026")
	cmd.Flags().StringP("output-dir", "o", ".", "Directory to save downloaded PDFs")

	return cmd
}

func BalanceSheetDownloadHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	key := args[0]
	secs := sina.Search(cmd.Context(), key)
	if len(secs) == 0 {
		slog.Info("search no sec", "code", key)
		return nil
	}
	sec := secs[0]

	// Look up orgId from CNINFO
	orgID, cnName, err := cninfo.LookupOrgID(cmd.Context(), sec.Code)
	if err != nil {
		return fmt.Errorf("查找CNINFO证券代码失败: %w (仅支持A股，代码: %s)", err, sec.Code)
	}

	stockParam := fmt.Sprintf("%s,%s", sec.Code, orgID)

	// Determine year range
	var startDate, endDate string
	yearStr, _ := cmd.Flags().GetString("year")
	startYearStr, _ := cmd.Flags().GetString("start-year")
	endYearStr, _ := cmd.Flags().GetString("end-year")
	startDate, endDate, err = genStartEndDate(yearStr, startYearStr, endYearStr)
	if err != nil {
		slog.ErrorContext(cmd.Context(), "genStartEndDate", "error", err)
		return err
	}

	slog.DebugContext(cmd.Context(), "downloadHandler", "startDate", startDate, "endDate", endDate)

	// Query CNINFO for annual reports
	req := &cninfo.QueryRequest{
		StockCode: stockParam,
		Category:  cninfo.CategoryAnnual,
		StartDate: startDate,
		EndDate:   endDate,
		PageNum:   1,
		PageSize:  30,
	}
	resp, err := cninfo.QueryAnnouncements(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("查询年报公告失败: %w", err)
	}

	if resp == nil || len(resp.Data) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到 %s(%s) 的年报公告\n", sec.Code, cnName)
		return nil
	}

	// Filter valid PDFs (exclude corrections, summaries, English versions)
	var validPDFs []*cninfo.Announcement
	for _, a := range resp.Data {
		if a.ExistFlag != 0 || a.InvalidationFlag != 0 {
			continue
		}
		title := a.Title
		skip := false
		for _, kw := range []string{"摘要", "英文", "已取消", "更正", "修订"} {
			if strings.Contains(title, kw) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		validPDFs = append(validPDFs, a)
	}

	if len(validPDFs) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到 %s(%s) 的可用年报PDF\n", sec.Code, cnName)
		return nil
	}

	// Show found announcements
	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s\n", sec.ExCode, cnName)
	fmt.Fprintf(cmd.OutOrStdout(), "找到 %d 份年报公告:\n\n", len(validPDFs))

	summaryHeaders := []string{"公告标题", "公告日期", "文件大小"}
	summaryData := make([][]string, 0, len(validPDFs))
	for _, a := range validPDFs {
		t := time.Unix(a.Time/1000, 0).Format("2006-01-02")
		size := types.HumanNum(float64(a.AdjunctSize))
		summaryData = append(summaryData, []string{a.Title, t, size})
	}
	renderTable(cmd, summaryHeaders, summaryData)

	// Download each PDF
	outputDir, _ := cmd.Flags().GetString("output-dir")
	fmt.Fprintf(cmd.OutOrStdout(), "\n下载年报PDF到 %s ...\n\n", outputDir)

	for _, a := range validPDFs {
		year := extractYear(a.Title)
		fileName := fmt.Sprintf("%s_%s_%s_年报.pdf", sec.Code, cnName, year)
		destPath := filepath.Join(outputDir, fileName)

		fmt.Fprintf(cmd.OutOrStdout(), "  下载: %s", a.Title)
		if err := cninfo.DownloadPDF(cmd.Context(), a.AdjunctURL, destPath); err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), " ... 失败: %v\n", err)
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), " ... 完成 (%s)\n", types.HumanNum(float64(a.AdjunctSize)))
	}

	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// extractYear tries to extract a year string from an announcement title.
func extractYear(title string) string {
	for i := 0; i < len(title)-3; i++ {
		if title[i] >= '0' && title[i] <= '9' &&
			title[i+1] >= '0' && title[i+1] <= '9' &&
			title[i+2] >= '0' && title[i+2] <= '9' &&
			title[i+3] >= '0' && title[i+3] <= '9' {
			return title[i : i+4]
		}
	}
	return "unknown"
}

// genStartEndDate 生成查询报表的时间范围
func genStartEndDate(yearStr, startYearStr, endYearStr string) (startDate string, endDate string, err error) {
	var year, startYear, endYear int
	// 优先判断 year
	if yearStr != "" {
		year, err = strconv.Atoi(yearStr)
		if err != nil {
			return
		}
		startDate = fmt.Sprintf("%d-01-01", year)
		endDate = fmt.Sprintf("%d-12-31", year)

		return
	}

	if startYearStr != "" {
		startYear, err = strconv.Atoi(startYearStr)
		if err != nil {
			slog.Error("genStartEndDate", "start-year", startYearStr, "error", err)
			return
		}
		if endYearStr != "" {
			endYear, err = strconv.Atoi(endYearStr)
			if err != nil {
				slog.Error("genStartEndDate", "end-year", endYearStr, "error", err)
				return
			}
		} else {
			endYear = time.Now().Year()
		}

		// check year range
		if startYear > endYear {
			err = fmt.Errorf("invalid year range, start=%s, end=%s", startYearStr, endYearStr)
		} else {
			startDate = fmt.Sprintf("%d-01-01", startYear)
			endDate = fmt.Sprintf("%d-12-31", endYear)
		}

		return
	}

	curYear := time.Now().Year()
	if endYearStr != "" {
		endYear, err = strconv.Atoi(endYearStr)
		if err != nil {
			slog.Error("genStartEndDate", "end-year", endYearStr, "error", err)
		} else {
			startDate = fmt.Sprintf("%d-01-01", endYear)
			endDate = fmt.Sprintf("%d-12-31", endYear)
		}
	} else {
		// Default: current year
		startDate = fmt.Sprintf("%d-01-01", curYear)
		endDate = fmt.Sprintf("%d-12-31", curYear)
	}

	return
}
