package balancesheet

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// usFinancials 是美股 sec financials 的 JSON：报表来自 SEC XBRL，金额用公司的列报币种（每张报表的 currency）。
type usFinancials struct {
	ExCode     string          `json:"ex_code"`
	Ticker     string          `json:"ticker"`
	Name       string          `json:"name"`
	CIK        int             `json:"cik"`
	Standard   edgar.Standard  `json:"standard"` // 会计准则：US GAAP 或 IFRS
	Quarterly  bool            `json:"quarterly"`
	Statements []*edgar.Report `json:"statements"`
}

// usStatements 解析 --type。留空时三张报表都输出；美股不用 A 股的报表类型名之外的写法。
func usStatements(typeText string) ([]edgar.Statement, error) {
	switch strings.ToLower(strings.TrimSpace(typeText)) {
	case "":
		return edgar.Statements, nil
	case "income":
		return []edgar.Statement{edgar.Income}, nil
	case "balance":
		return []edgar.Statement{edgar.Balance}, nil
	case "cashflow":
		return []edgar.Statement{edgar.CashFlow}, nil
	}
	return nil, fmt.Errorf("invalid report type: %s (use: balance, income, cashflow)", typeText)
}

// usFinancialsHandler 输出美股财报：SEC XBRL 公司数据整理成利润表、资产负债表、现金流量表的主要项目。
func usFinancialsHandler(cmd *cobra.Command, exCode string) error {
	typeText, _ := cmd.Flags().GetString("type")
	statements, err := usStatements(typeText)
	if err != nil {
		return err
	}
	periodText, _ := cmd.Flags().GetString("period")
	quarterly := false
	switch strings.ToLower(periodText) {
	case "", "annual":
	case "quarter", "quarterly", "q":
		quarterly = true
	default:
		return fmt.Errorf("美股的 --period 须为 annual 或 quarter：%s", periodText)
	}
	output, _ := cmd.Flags().GetString("output")
	if output != "" && strings.ToLower(filepath.Ext(output)) != ".json" {
		return fmt.Errorf("美股财报暂只支持导出 .json：%s", output)
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	cfg, err := config.LoadOrWarn(cmd.Context(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	client := &edgar.Client{Email: cfg.SECEmail}
	company, err := client.Lookup(cmd.Context(), exCode)
	if err != nil {
		return err
	}
	facts, err := client.CompanyFacts(cmd.Context(), company.CIK)
	if err != nil {
		return err
	}
	result := usFinancials{ExCode: exCode, Ticker: company.Ticker, Name: facts.Name, CIK: company.CIK, Standard: facts.Standard, Quarterly: quarterly}
	limit := 5
	if quarterly {
		limit = 8
	}
	for _, statement := range statements {
		// JSON 和导出给出全部报告期，表格只显示最近几期。
		all := 0
		if !asJSON && output == "" {
			all = limit
		}
		result.Statements = append(result.Statements, facts.Build(statement, quarterly, all))
	}
	if output != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(output, append(data, '\n'), 0644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "已导出 %s\n", output)
		return nil
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), result)
	}
	printUSFinancials(cmd.OutOrStdout(), result)
	return nil
}

func printUSFinancials(out io.Writer, result usFinancials) {
	fmt.Fprintf(out, "%s（%s，CIK %d）\n", result.Name, result.Ticker, result.CIK)
	derived := false
	for _, report := range result.Statements {
		fmt.Fprintf(out, "\n【%s】", report.Statement.Name())
		if len(report.Periods) == 0 {
			if report.Quarterly && report.Standard == edgar.IFRS {
				fmt.Fprintln(out, "\n没有季度数据；按 IFRS 申报的外国公司在 SEC 只提交 20-F 年报，请查看年度报表。")
			} else if report.Quarterly {
				fmt.Fprintln(out, "\n没有季度数据；部分外国公司只披露年报，不提交 10-Q 季报。")
			} else {
				fmt.Fprintln(out, "\n没有数据。")
			}
			continue
		}
		headers := []string{"项目"}
		for _, p := range report.Periods {
			label := fmt.Sprintf("FY%d", p.FiscalYear)
			if report.Quarterly {
				label += p.FiscalPeriod
			}
			headers = append(headers, label+" "+p.End[5:])
		}
		fmt.Fprintln(out)
		table := termtable.New(out, headers, termtable.LeftHeader, termtable.NoWrap)
		for _, line := range report.Lines {
			row := []string{line.Label + unitLabel(line.Unit, report.Currency, report.CurrencyName)}
			has := false
			for _, p := range report.Periods {
				value := p.Values[line.Key]
				if value == nil {
					row = append(row, "-")
					continue
				}
				has = true
				text := format(*value, line.Unit)
				for _, key := range p.Derived {
					if key == line.Key {
						text += "*"
						derived = true
					}
				}
				row = append(row, text)
			}
			if has {
				table.Append(row)
			}
		}
		table.Render()
	}
	fmt.Fprintf(out, "\n数据来自 SEC EDGAR 的 XBRL 财务数据（%s），金额用公司的列报币种，同一报告期有重述时用最新申报的数值；财年按截止日所在年份标注。\n", result.Standard)
	if derived {
		fmt.Fprintln(out, "* 推算值：没有单季数时用年初累计数相减（第四季度 = 全年 - 前三季度），自由现金流 = 经营活动现金流 - 资本支出，没有申报归母股东权益时 = 股东权益合计 - 少数股东权益。")
	}
}

// unitLabel 返回行名后的单位，比如（亿美元）、（人民币）、（亿股）。
func unitLabel(unit, currency, name string) string {
	switch unit {
	case currency:
		return "（亿" + name + "）"
	case currency + "/shares":
		return "（" + name + "）"
	case "shares":
		return "（亿股）"
	}
	return ""
}

// format 按单位显示：金额和股数用亿，每股收益保留两位小数。
func format(value float64, unit string) string {
	if strings.HasSuffix(unit, "/shares") {
		return fmt.Sprintf("%.2f", value)
	}
	return fmt.Sprintf("%.2f", value/1e8)
}
