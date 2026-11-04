package cmd

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// usInfo 是美股 sec info 的结果：公司资料来自 SEC EDGAR，行情和估值来自新浪。
// 和 A 股、港股的 --json 字段不同：公司资料在 company，行情在 quote。
type usInfo struct {
	ExCode  string         `json:"ex_code"`
	Ticker  string         `json:"ticker"`
	Company *edgar.Profile `json:"company"` // SEC 没有这个代码（比如部分基金、场外交易股票）时为 null
	Quote   *sina.USQuote  `json:"quote"`   // 行情获取失败时为 null
}

// usInfoHandler 打印美股公司资料。公司资料和行情并发请求，任一失败时仍输出另一部分；两者都失败才报错。
func usInfoHandler(cmd *cobra.Command, exCode string, asJSON, dividend bool) error {
	if dividend {
		fmt.Fprintln(cmd.ErrOrStderr(), "美股暂不支持分红数据，忽略 -d")
	}
	cfg, err := config.LoadOrWarn(cmd.Context(), cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	client := &edgar.Client{Email: cfg.SECEmail}
	var (
		wg                   sync.WaitGroup
		profile              *edgar.Profile
		quote                *sina.USQuote
		profileErr, quoteErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		company, err := client.Lookup(cmd.Context(), exCode)
		if err == nil {
			profile, err = client.Profile(cmd.Context(), company.CIK)
		}
		profileErr = err
	}()
	go func() {
		defer wg.Done()
		// 新浪行情用搜索得到的代码，类别股的写法和 SEC 不同（新浪 brk$b，SEC BRK-B）。
		quote, quoteErr = sina.QueryUSQuote(cmd.Context(), exCode)
	}()
	wg.Wait()
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
	info := usInfo{ExCode: exCode, Ticker: edgar.NormalizeTicker(exCode), Company: profile, Quote: quote}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), info)
	}
	printUSInfo(cmd, info)
	return nil
}

func printUSInfo(cmd *cobra.Command, info usInfo) {
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
