package cmd

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/usstock"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// usInfo 是美股 sec info 的 --json 输出：usstock.Info，加上 -d 时的分红表。
type usInfo struct {
	usstock.Info
	Dividends *usstock.Dividends `json:"dividends,omitempty"`
}

// usInfoHandler 打印美股公司资料。公司资料和行情任一失败时仍输出另一部分；两者都失败才报错。
// --json 格式是 usstock.Info，和 A 股、港股不同：公司资料在 company，行情在 quote。
// -d 时再输出按财年的分红表（来自 SEC XBRL）；分红表获取失败时，文本输出只提示，--json 报错。
func usInfoHandler(cmd *cobra.Command, exCode string, asJSON, dividend bool) error {
	cfg, err := config.LoadOrWarn(cmd.Context(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	client := &edgar.Client{Email: cfg.SECEmail}
	info, profileErr, quoteErr := usstock.Load(cmd.Context(), client, exCode)
	if profileErr != nil && quoteErr != nil {
		return fmt.Errorf("公司资料：%w；行情：%w", profileErr, quoteErr)
	}
	switch {
	case errors.Is(profileErr, edgar.ErrNotFound):
		fmt.Fprintln(cmd.ErrOrStderr(), "SEC EDGAR 没有这个代码，只显示行情；基金、场外交易股票等可能不向 SEC 申报公司资料")
	case profileErr != nil:
		fmt.Fprintf(cmd.ErrOrStderr(), "公司资料获取失败：%v\n", profileErr)
	}
	if quoteErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "行情获取失败：%v\n", quoteErr)
	}
	var dividends *usstock.Dividends
	var dividendsErr error
	// SEC 没有这个代码（基金、场外交易股票）时也没有分红数据，只输出行情。
	if dividend && !errors.Is(profileErr, edgar.ErrNotFound) {
		current := 0.0
		if info.Quote != nil && info.Quote.Shares != nil {
			current = *info.Quote.Shares
		}
		dividends, dividendsErr = usstock.LoadDividends(cmd.Context(), client, exCode, current)
	}
	if asJSON {
		if dividendsErr != nil {
			return fmt.Errorf("查询分红失败: %w", dividendsErr)
		}
		return utils.PrintJSON(cmd.OutOrStdout(), usInfo{info, dividends})
	}
	printUSInfo(cmd, info)
	if dividendsErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "分红数据获取失败：%v\n", dividendsErr)
	} else if dividends != nil {
		printUSDividends(cmd.OutOrStdout(), dividends)
	}
	return nil
}

// usDividendYears 是文本输出的财年数，--json 输出全部。
const usDividendYears = 10

// printUSDividends 打印按财年的分红表，金额单位亿、每股股息用列报币种。
func printUSDividends(out io.Writer, d *usstock.Dividends) {
	fmt.Fprintln(out)
	if len(d.Years) == 0 {
		fmt.Fprintln(out, "SEC 年报中没有分红和回购记录，可能不分红。")
		return
	}
	name := d.CurrencyName
	headers := []string{"财年", "截止日", "每股股息（" + name + "）", "同比", "支付股息（亿" + name + "）", "归母净利润（亿" + name + "）", "分红率", "回购（亿" + name + "）"}
	table := termtable.New(out, headers, termtable.LeftHeader, termtable.NoWrap)
	for i, y := range d.Years {
		if i >= usDividendYears {
			break
		}
		table.Append([]string{strconv.Itoa(y.FiscalYear), y.End, dash(number(y.PerShare, "%.2f")), dash(number(y.Growth, "%+.1f%%")),
			hundredMillion(y.Paid), hundredMillion(y.NetIncome), dash(number(y.Payout, "%.1f%%")), hundredMillion(y.Buyback)})
	}
	table.Render()
	fmt.Fprintln(out, "\n分红数据来自 SEC EDGAR 的 XBRL 年报：每股股息是宣告数，支付股息和回购来自现金流量表，分红率 = 支付股息 ÷ 归母净利润；SEC 数据没有除权除息日、登记日和派息日。")
	switch {
	case d.SplitsUnchecked:
		fmt.Fprintln(out, "新浪日线没有读到，无法识别拆股，拆股前年报的每股股息没有按拆股调整。")
	case len(d.Splits) > 0:
		parts := []string{}
		for _, s := range d.Splits {
			if s.Factor >= 1 {
				parts = append(parts, fmt.Sprintf("%s 1 拆 %g", s.Date, s.Factor))
			} else {
				parts = append(parts, fmt.Sprintf("%s %g 合 1", s.Date, 1/s.Factor))
			}
		}
		fmt.Fprintf(out, "每股股息已按拆股统一到当前股本口径（%s）。\n", strings.Join(parts, "，"))
	}
}

// dash 把空字符串换成 -。
func dash(text string) string {
	if text == "" {
		return "-"
	}
	return text
}

// hundredMillion 把金额换算成亿，保留两位小数；没有数值时为 -。
func hundredMillion(value *float64) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *value/1e8)
}

func printUSInfo(cmd *cobra.Command, info usstock.Info) {
	rows := [][2]string{{"证券代码", info.ExCode}}
	if p := info.Company; p != nil {
		former := []string{}
		for _, name := range p.FormerNames {
			former = append(former, name.Name+period(name.From, name.To))
		}
		industry := p.Industry
		if p.SIC != "" {
			industry = join("", industry, "（SIC "+p.SIC+"）")
		}
		fiscal := p.FiscalYearEnd
		if len(fiscal) == 4 {
			fiscal = fiscal[:2] + "-" + fiscal[2:]
		}
		rows = append(rows, [][2]string{
			{"公司名称", p.Name}, {"曾用名", strings.Join(former, "；")},
			{"SEC CIK", fmt.Sprint(p.CIK)}, {"交易所", strings.Join(p.Exchanges, "/")}, {"行业", industry},
			{"注册地", p.Incorporation}, {"申报人类别", p.Category}, {"财年截至", fiscal},
			{"办公地址", p.Address}, {"电话", p.Phone}, {"公司网址", p.Website},
		}...)
	}
	if q := info.Quote; q != nil {
		rows = append(rows,
			[2]string{"中文名", q.Name},
			[2]string{"当前价格", fmt.Sprintf("%.2f 美元（%+.2f%%，%s 北京时间）", q.Price, q.ChangeRate, q.Time)},
			[2]string{"52周区间", rangeText(q.Low52, q.High52)},
			[2]string{"市盈率TTM", number(q.PE, "%.2f")},
			[2]string{"每股收益TTM", number(q.EPS, "%.2f 美元")},
			[2]string{"总市值", human(q.MarketCap, "美元")},
			[2]string{"总股本", human(q.Shares, "股")},
		)
	}
	out := cmd.OutOrStdout()
	for _, row := range rows {
		value := strings.TrimSpace(row[1])
		if value == "" {
			value = "-"
		}
		fmt.Fprintf(out, "%s\t%s\n", row[0], value)
	}
	fmt.Fprintln(out, "\n公司资料来自 SEC EDGAR，行情和估值来自新浪；每股收益和市盈率按最近四个季度计算，亏损公司没有市盈率。")
}

// join 用 sep 连接非空的部分。
func join(sep string, parts ...string) string {
	kept := []string{}
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, sep)
}

// period 返回“（开始 至 结束）”，缺少的一端不写。
func period(from, to string) string {
	switch {
	case from != "" && to != "":
		return "（" + from + " 至 " + to + "）"
	case from != "":
		return "（" + from + " 起）"
	case to != "":
		return "（至 " + to + "）"
	}
	return ""
}

func number(value *float64, format string) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf(format, *value)
}

// human 用亿、万显示大数，并带上单位，比如 48496.56亿美元、146.12亿股。
func human(value *float64, unit string) string {
	if value == nil {
		return ""
	}
	return utils.HumanNum(*value) + unit
}

func rangeText(low, high *float64) string {
	if low == nil || high == nil {
		return ""
	}
	return fmt.Sprintf("%.2f - %.2f 美元", *low, *high)
}
