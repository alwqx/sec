// Package filings 实现 sec filings：美国公司向 SEC 提交的申报文件，数据来自 SEC EDGAR 官方接口。
package filings

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

func NewFilingsCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "filings <ticker>",
		Short: "List SEC EDGAR filings of a US-listed company (10-K, 10-Q, 8-K, 20-F...)",
		Example: `  sec filings AAPL
  sec filings AAPL --form 10-K -n 5
  sec filings TSM --form 20-F,6-K -b 20250101
  sec filings AAPL --form 10-K -n 1 -o ~/Downloads`,
		Args: cobra.ExactArgs(1),
		RunE: filingsHandler,
	}
	cmd.Flags().StringP("form", "f", strings.Join(edgar.DefaultForms, ","), "Comma-separated form types; 10-K also matches 10-K/A; \"all\" for every form")
	cmd.Flags().StringP("begin", "b", "", "Filed on or after 20250101")
	cmd.Flags().StringP("end", "e", "", "Filed on or before 20251231, default today")
	cmd.Flags().IntP("limit", "n", 20, "Maximum number of filings")
	cmd.Flags().StringP("output", "o", "", "Download the primary documents of the listed filings into this directory")
	cmd.Flags().Bool("json", false, "Output JSON")
	return cmd
}

func filingsHandler(cmd *cobra.Command, args []string) error {
	flags := cmd.Flags()
	formText, _ := flags.GetString("form")
	beginText, _ := flags.GetString("begin")
	endText, _ := flags.GetString("end")
	limit, _ := flags.GetInt("limit")
	output, _ := flags.GetString("output")
	asJSON, _ := flags.GetBool("json")
	if limit <= 0 {
		return fmt.Errorf("--limit 须大于 0")
	}
	query := edgar.FilingQuery{Want: limit}
	if !strings.EqualFold(strings.TrimSpace(formText), "all") {
		for _, form := range strings.Split(formText, ",") {
			if form = strings.TrimSpace(form); form != "" {
				query.Forms = append(query.Forms, form)
			}
		}
	}
	if beginText != "" || endText != "" {
		begin, end, err := utils.ParseRange(beginText, endText, 365)
		if err != nil {
			return err
		}
		query.Begin, query.End = begin.Format(utils.LayoutYYMMDD), end.Format(utils.LayoutYYMMDD)
	}

	cfg, err := config.Load(cmd.Context())
	if errors.Is(err, config.ErrInvalid) {
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	} else if err != nil {
		return err
	}
	client := &edgar.Client{Email: cfg.SECEmail}
	company, err := client.Lookup(cmd.Context(), args[0])
	if err != nil {
		return err
	}
	result, err := client.Filings(cmd.Context(), company.CIK, query)
	if err != nil {
		return err
	}
	profile, filings := result.Profile, result.Filings
	if len(filings) > limit {
		filings = filings[:limit]
	}
	if output != "" {
		if err := download(cmd, client, company.Ticker, filings, output); err != nil {
			return err
		}
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"company": profile, "ticker": company.Ticker, "truncated": result.Truncated, "results": filings})
	}
	printFilings(cmd.OutOrStdout(), company, profile, filings)
	if result.Truncated && len(filings) < limit {
		fmt.Fprintln(cmd.OutOrStdout(), "这家公司申报很多，只查了最近一段时间的申报，结果可能不全；用 -b 指定更早的开始日期可以查全。")
	}
	return nil
}

func printFilings(out io.Writer, company edgar.Company, p *edgar.Profile, filings []edgar.Filing) {
	fiscal := ""
	if len(p.FiscalYearEnd) == 4 {
		fiscal = "，财年截至 " + p.FiscalYearEnd[:2] + "-" + p.FiscalYearEnd[2:]
	}
	fmt.Fprintf(out, "%s（%s，CIK %d）%s · %s%s\n\n", p.Name, company.Ticker, p.CIK, strings.Join(p.Exchanges, "/"), p.Industry, fiscal)
	if len(filings) == 0 {
		fmt.Fprintln(out, "没有符合条件的申报。")
		return
	}
	table := termtable.New(out, []string{"申报日", "类型", "报告期", "说明", "链接"}, termtable.Compact, termtable.NoWrap)
	for _, f := range filings {
		report := f.ReportDate
		if report == "" {
			report = "-"
		}
		table.Append([]string{f.FilingDate, f.Form, report, f.Description, f.URL})
	}
	table.Render()
	fmt.Fprintln(out, "\n数据来自 SEC EDGAR。10-K 年报、10-Q 季报、8-K 重大事项、20-F/6-K 外国公司年报和临时报告；用 --form all 列出全部类型。")
}

// download 下载主文件，文件名为 代码-申报日-类型-申报编号.扩展名。
func download(cmd *cobra.Command, client *edgar.Client, ticker string, filings []edgar.Filing, dir string) error {
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, dir[2:])
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, f := range filings {
		body, err := client.Download(cmd.Context(), f.URL)
		if err != nil {
			return fmt.Errorf("下载 %s %s 失败：%w", f.FilingDate, f.Form, err)
		}
		name := fmt.Sprintf("%s-%s-%s-%s%s", ticker, f.FilingDate, strings.NewReplacer("/", "_", " ", "_").Replace(f.Form), f.Accession, path.Ext(f.URL))
		target := filepath.Join(dir, name)
		if err := os.WriteFile(target, body, 0644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "已下载 %s（%s）\n", target, utils.HumanByte(float64(len(body))))
	}
	return nil
}
