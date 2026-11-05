package valuation

import (
	"fmt"
	"io"

	shared "github.com/alwqx/sec/valuation"
)

// dividendYearSize 是文本输出列出的财年数；--json 输出全部。
const dividendYearSize = 5

func printDividend(out io.Writer, r *shared.Result) {
	if us(r) {
		fmt.Fprintln(out, "方法: 股息率 = 最近一个完整财年支付的现金股息总额 ÷ 当前市值，不受拆股影响；每股分红 = 支付总额 ÷ 当前股数，财年表里的每股分红取年报披露的每股宣告股息")
	} else {
		fmt.Fprintln(out, "方法: 股息率 = 最近完整财年的税前每股现金分红（按当前股本）÷ 收盘价，不含送转、配股和回购")
	}
	d := r.Dividends
	if d == nil {
		fmt.Fprintf(out, "\n【评估】%s\n", r.DividendsNote)
		return
	}
	fmt.Fprintln(out)
	median, spread := "-", "-"
	if d.History != nil {
		median = fmt.Sprintf("%.2f%%", d.History.Median)
		spread = fmt.Sprintf("%.2f%% ~ %.2f%%", d.History.Min, d.History.Max)
	}
	fiscal := d.FiscalYear
	if fiscal == "" {
		fiscal = "-"
	}
	yield := "-"
	if d.Yield != nil {
		yield = fmt.Sprintf("%.2f%%", *d.Yield)
	}
	average := "-"
	if d.Average5Y != nil {
		average = fmt.Sprintf("%.2f%%", *d.Average5Y)
	}
	richTable(out, []string{"项目", "数值"}, [][]string{
		{"最近完整财年", fiscal},
		{"每股分红", fmt.Sprintf("%.3f", d.PerShare)},
		{"股息率", yield},
		{"近 5 个财年平均股息率", average},
		{"历史中位", median},
		{"历史范围", spread},
		{"历史分位", historyPercentile(d.History)},
		{"连续分红", fmt.Sprintf("%d 个财年", d.ConsecutiveYears)},
	})
	if len(d.Years) > 0 {
		if us(r) {
			fmt.Fprintln(out, "\n【近年现金分红】按财年，分红率 = 支付的股息 ÷ 归母净利润")
		} else {
			fmt.Fprintln(out, "\n【近年现金分红】按报告期归属，金额是当时的股本口径")
		}
		rows := [][]string{}
		for i, item := range d.Years {
			if i >= dividendYearSize {
				break
			}
			rows = append(rows, []string{item.Year, fmt.Sprintf("%.3f", item.PerShare), ff(item.EPS), fpct(item.Payout)})
		}
		richTable(out, []string{"财年", "每股分红", epsLabel(r), "分红率"}, rows)
	} else {
		fmt.Fprintln(out, "\n【评估】有分红记录，但没有实施过现金分红")
	}
	if !d.MismatchChecked {
		fmt.Fprintln(out, "\n未能和新浪核对，东方财富可能缺少特别分红，股息率可能偏低")
	}
	if len(d.Mismatches) > 0 {
		fmt.Fprintln(out, "\n【与新浪记录不一致】近 5 年，股息率按东方财富计算")
		for _, item := range d.Mismatches {
			east := "缺少"
			if item.EastMoney != nil {
				east = fmt.Sprintf("%.4f", *item.EastMoney)
			}
			fmt.Fprintf(out, "  除息日 %s: 新浪 %.4f，东方财富 %s\n", item.ExDate, item.Sina, east)
		}
	}
	if len(d.Pending) > 0 {
		fmt.Fprintln(out, "\n【未除息】不计入股息率")
		for _, item := range d.Pending {
			fmt.Fprintf(out, "  %s %s: %s\n", item.Report, item.Progress, item.Plan)
		}
	}
}

func overviewYield(r *shared.Result) []string {
	basis := "最近完整财年的税前现金分红"
	if us(r) {
		basis = "最近完整财年支付的现金股息 ÷ 市值"
	}
	if r.Dividends == nil || r.Dividends.Yield == nil {
		return []string{"股息率", "-", "-", basis}
	}
	return []string{"股息率", fmt.Sprintf("%.2f%%", *r.Dividends.Yield), historyPercentile(r.Dividends.History), basis}
}

// us 判断是不是美股估值：口径和 A 股不同，ROE 用期末权益，每股收益是稀释口径，股息率按支付总额计算。
func us(r *shared.Result) bool { return r.Currency == "USD" }

func epsLabel(r *shared.Result) string {
	if us(r) {
		return "稀释每股收益"
	}
	return "基本每股收益"
}

func roeLabel(r *shared.Result) string {
	if us(r) {
		return r.ROEYear + " 财年，期末权益"
	}
	return r.ROEYear + " 年报加权"
}
