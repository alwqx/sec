package valuation

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	shared "github.com/alwqx/sec/valuation"
	"github.com/spf13/cobra"
)

func NewValuationCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "valuation",
		Aliases:       []string{"val", "v"},
		Short:         "Show valuation metrics of specific security",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ExactArgs(1),
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: ValuationHandler,
	}
	cmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	cmd.Flags().Bool("json", false, "Output the full valuation as JSON, regardless of --method")
	cmd.Flags().StringP("method", "m", "overview",
		"Valuation method: overview, pe, pb, ps, peg, graham, dcf, peers, dividend")
	// DCF parameters
	defaults := shared.DefaultDCFParams()
	cmd.Flags().Float64("growth-rate", 0, "DCF: growth rate %, omit to use the 5-year CAGR, limited to ±15%")
	cmd.Flags().Float64("terminal-growth", defaults.TerminalGrowth, "DCF: terminal growth rate %")
	cmd.Flags().Float64("wacc", defaults.WACC, "DCF: discount rate %")
	cmd.Flags().Float64("margin-of-safety", defaults.MarginOfSafety, "DCF: margin of safety %")
	return cmd
}

var methods = []string{"overview", "pe", "pb", "ps", "peg", "graham", "dcf", "peers", "dividend"}

func ValuationHandler(cmd *cobra.Command, args []string) error {
	method, _ := cmd.Flags().GetString("method")
	if !slices.Contains(methods, method) {
		return fmt.Errorf("不支持的估值方法 %s，可选 %s", method, strings.Join(methods, "、"))
	}
	params := shared.DCFParams{}
	if cmd.Flags().Changed("growth-rate") {
		growth, _ := cmd.Flags().GetFloat64("growth-rate")
		params.Growth = &growth
	}
	params.TerminalGrowth, _ = cmd.Flags().GetFloat64("terminal-growth")
	params.WACC, _ = cmd.Flags().GetFloat64("wacc")
	params.MarginOfSafety, _ = cmd.Flags().GetFloat64("margin-of-safety")
	if err := params.Validate(); err != nil {
		return err
	}

	secs, err := sina.Search(cmd.Context(), args[0])
	if err != nil {
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if len(secs) == 0 {
		if asJSON {
			return fmt.Errorf("未找到证券: %s", args[0])
		}
		fmt.Fprintf(cmd.OutOrStdout(), "未找到证券: %s\n", args[0])
		return nil
	}
	// noData 是第一只候选证券没有估值数据的原因，美股会说明具体原因。
	noData := fmt.Errorf("%s %s: %w", secs[0].ExCode, secs[0].Name, shared.ErrNoData)
	first := true
	for _, sec := range candidates(secs, args[0]) {
		var result *shared.Result
		if strings.HasPrefix(sec.ExCode, "$") {
			// 美股估值来自 SEC XBRL 和新浪日线，访问 SEC 需要共享配置里的联系邮箱。
			cfg, cfgErr := config.LoadOrWarn(cmd.Context(), cmd.ErrOrStderr())
			if cfgErr != nil {
				return cfgErr
			}
			result, err = shared.AnalyzeUS(cmd.Context(), &edgar.Client{Email: cfg.SECEmail}, sec.ExCode, params, nil)
		} else {
			result, err = shared.Analyze(cmd.Context(), sec.ExCode, params)
		}
		if errors.Is(err, shared.ErrNoData) {
			if first {
				noData, first = fmt.Errorf("%s %s: %w", sec.ExCode, sec.Name, err), false
			}
			continue
		}
		if err != nil {
			return err
		}
		if asJSON {
			return utils.PrintJSON(cmd.OutOrStdout(), result)
		}
		render(cmd.OutOrStdout(), method, result)
		return nil
	}
	if asJSON {
		return noData
	}
	fmt.Fprintln(cmd.OutOrStdout(), noData)
	return nil
}

// candidates 返回要依次尝试的证券。同一数字可能同时是指数和股票（000001 是上证指数和平安银行），
// 所以纯数字代码要试完所有沪深北结果。其他情况只取搜索的第一条：00700 的第二条是模塑科技（000700），
// 不能因为腾讯不支持就换成别的公司。
func candidates(secs []*sina.BasicSecurity, key string) []*sina.BasicSecurity {
	same := []*sina.BasicSecurity{}
	for _, sec := range secs {
		if strings.EqualFold(sec.ExCode, key) {
			return []*sina.BasicSecurity{sec}
		}
		if sec.Code == key && types.IsACode(sec.ExCode) {
			same = append(same, sec)
		}
	}
	if len(same) > 0 {
		return same
	}
	return secs[:1]
}

func render(out io.Writer, method string, r *shared.Result) {
	fmt.Fprintf(out, "\n证券代码: %s  证券名称: %s  行业: %s\n", r.Code, r.Name, r.Industry)
	unit := "元"
	if r.Currency == "USD" {
		unit = "美元"
	}
	fmt.Fprintf(out, "收盘价: %.2f %s  总市值: %s%s  日期: %s\n\n", r.Price, unit, humanMoney(r.MarketCap), unit, r.TradeDate)
	switch method {
	case "pe":
		printMultiple(out, "市盈率法 (P/E TTM)", "PE", r.PE)
		fmt.Fprintf(out, "每股收益 (TTM，由 PE 反推): %s\n", ff(r.EPS))
		printGrowth(out, r)
	case "pb":
		printMultiple(out, "市净率法 (P/B MRQ)", "PB", r.PB)
		fmt.Fprintf(out, "每股净资产 (由 PB 反推): %s\nROE (%s): %s\n", ff(r.BVPS), roeLabel(r), fpct(r.ROE))
	case "ps":
		printMultiple(out, "市销率法 (P/S TTM)", "PS", r.PS)
		fmt.Fprintf(out, "营业收入 (TTM，由 PS 反推): %s\n", moneyOrDash(r.RevenueTTM))
	case "peg":
		printPEG(out, r)
	case "graham":
		printGraham(out, r)
	case "dcf":
		printDCF(out, r)
	case "peers":
		printPeers(out, r)
	case "dividend":
		printDividend(out, r)
	default:
		printOverview(out, r)
	}
	if r.SharesNote != "" {
		fmt.Fprintf(out, "\n股数说明：%s", r.SharesNote)
	}
	fmt.Fprintf(out, "\n%s\n\n", r.Disclaimer)
}

func printOverview(out io.Writer, r *shared.Result) {
	fmt.Fprintln(out, "【估值指标】")
	richTable(out, []string{"指标", "当前值", "历史分位", "口径"}, [][]string{
		{"P/E", ff(r.PE.Current), percentile(r.PE), multipleNote(r.PE, "TTM")},
		{"P/B", ff(r.PB.Current), percentile(r.PB), multipleNote(r.PB, "MRQ")},
		{"P/S", ff(r.PS.Current), percentile(r.PS), multipleNote(r.PS, "TTM")},
		overviewYield(r),
		{"PEG", ff(r.PEG), "-", "P/E ÷ 净利润 CAGR"},
		{"ROE", fpct(r.ROE), "-", roeLabel(r)},
		{"每股收益", ff(r.EPS), "-", "TTM，由 PE 反推"},
		{"每股净资产", ff(r.BVPS), "-", "由 PB 反推"},
		{"格雷厄姆数", ff(r.Graham), "-", "√(22.5 × 每股收益 × 每股净资产)"},
	})
	printGrowth(out, r)
	if r.Peers != nil {
		printPeerMetrics(out, r.Peers)
	}

	if len(r.Annuals) > 0 {
		fmt.Fprintln(out, "\n【近年年报】")
		rows := [][]string{}
		for i, annual := range r.Annuals {
			if i >= 5 {
				break
			}
			rows = append(rows, []string{annual.Year, ff(annual.EPS), ff(annual.BPS), fpct(annual.ROE), moneyOrDash(annual.Revenue), moneyOrDash(annual.Profit)})
		}
		richTable(out, []string{"年度", epsLabel(r), "每股净资产", "ROE", "营收", "归母净利润"}, rows)
	}

	fmt.Fprintf(out, "\n【规则汇总】→ %s\n", r.Assessment)
	for _, note := range []string{r.PeersNote, r.DividendsNote} {
		if note != "" {
			fmt.Fprintf(out, "  注意: %s\n", note)
		}
	}
	for _, signal := range r.Signals {
		fmt.Fprintf(out, "  %s: %s\n", signal.Name, signal.Rule)
	}
}

func printMultiple(out io.Writer, title, name string, m shared.Multiple) {
	fmt.Fprintf(out, "方法: %s\n\n", title)
	if m.Current == nil {
		fmt.Fprintf(out, "当前 %s 不可用: %s\n", name, multipleNote(m, "不可用"))
	}
	if m.History != nil {
		richTable(out, []string{"项目", "数值"}, [][]string{
			{"当前 " + name, ff(m.Current)},
			{"历史最低", fmt.Sprintf("%.2f", m.History.Min)},
			{"历史中位", fmt.Sprintf("%.2f", m.History.Median)},
			{"历史最高", fmt.Sprintf("%.2f", m.History.Max)},
			{"历史分位", percentile(m)},
			{"样本", fmt.Sprintf("%s 以来 %d 个交易日", m.History.Since, m.History.Samples)},
		})
	}
	if len(m.Scenarios) > 0 {
		fmt.Fprintln(out, "\n【估值情景】假设盈利和净资产不变，只调整估值倍数")
		rows := [][]string{}
		for _, item := range m.Scenarios {
			rows = append(rows, []string{fmt.Sprintf("%s (%.2fx)", item.Label, item.Multiple), fmt.Sprintf("%.2f", item.Price), fmt.Sprintf("%+.0f%%", item.Change)})
		}
		richTable(out, []string{"假设", "对应股价", "涨跌幅"}, rows)
	}
	fmt.Fprintln(out)
}

func printGrowth(out io.Writer, r *shared.Result) {
	if r.Growth.Rate != nil {
		fmt.Fprintf(out, "净利润 CAGR (%s–%s): %.1f%%\n", r.Growth.FromYear, r.Growth.ToYear, *r.Growth.Rate)
	} else {
		fmt.Fprintf(out, "净利润 CAGR: - (%s)\n", r.Growth.Note)
	}
}

func printPEG(out io.Writer, r *shared.Result) {
	fmt.Fprintln(out, "方法: PEG = P/E ÷ 净利润 CAGR(%)")
	fmt.Fprintln(out)
	printGrowth(out, r)
	fmt.Fprintf(out, "P/E (TTM): %s\nPEG: %s\n", ff(r.PE.Current), ff(r.PEG))
	if len(r.Annuals) > 0 {
		fmt.Fprintln(out, "\n【历年归母净利润】")
		for _, annual := range r.Annuals {
			fmt.Fprintf(out, "  %s  %s\n", annual.Year, moneyOrDash(annual.Profit))
		}
	}
	fmt.Fprintln(out)
	if r.PEG == nil {
		fmt.Fprintln(out, "【评估】亏损、盈利没有增长或增长率不可用，PEG 不适用")
	}
}

func printGraham(out io.Writer, r *shared.Result) {
	fmt.Fprintln(out, "方法: 格雷厄姆公式 — √(22.5 × 每股收益 × 每股净资产)，每股数据由 PE、PB 反推")
	fmt.Fprintln(out)
	margin := "-"
	if r.GrahamMargin != nil {
		margin = fmt.Sprintf("%+.1f%%", *r.GrahamMargin)
	}
	richTable(out, []string{"项目", "数值"}, [][]string{
		{"每股收益 (TTM)", ff(r.EPS)}, {"每股净资产", ff(r.BVPS)}, {"格雷厄姆数", ff(r.Graham)},
		{"收盘价", fmt.Sprintf("%.2f", r.Price)}, {"格雷厄姆数相对收盘价", margin},
	})
	if r.Graham == nil {
		fmt.Fprintln(out, "\n【评估】亏损或净资产为负，格雷厄姆数不适用")
	}
}

func printDCF(out io.Writer, r *shared.Result) {
	dcf := r.DCF
	fmt.Fprintln(out, "方法: 两阶段股权现金流折现。现金流近似为归母净利润 × 70%，折现结果视为股权价值，不再扣净负债")
	fmt.Fprintln(out)
	if dcf.Warning != "" {
		fmt.Fprintf(out, "注意: %s\n\n", dcf.Warning)
	}
	if !dcf.Applicable {
		fmt.Fprintf(out, "【评估】%s\n", dcf.Note)
		return
	}
	richTable(out, []string{"参数", "数值", "说明"}, [][]string{
		{"当前现金流 (近似)", humanMoney(*dcf.BaseCash), "TTM 归母净利润 × 70%"},
		{"增长期", "5 年", ""},
		{"增长率", fpct(dcf.Params.Growth), dcf.GrowthSource},
		{"永续增长率", fmt.Sprintf("%.1f%%", dcf.Params.TerminalGrowth), "--terminal-growth"},
		{"折现率", fmt.Sprintf("%.1f%%", dcf.Params.WACC), "--wacc"},
		{"安全边际", fmt.Sprintf("%.1f%%", dcf.Params.MarginOfSafety), "--margin-of-safety"},
	})
	upside := "-"
	if dcf.Upside != nil {
		upside = fmt.Sprintf("%+.1f%%", *dcf.Upside)
	}
	fmt.Fprintln(out, "\n【DCF 估值结果】")
	richTable(out, []string{"项目", "数值"}, [][]string{
		{"增长期现值", humanMoney(*dcf.GrowthPV)},
		{"终值现值", fmt.Sprintf("%s (占 %.0f%%)", humanMoney(*dcf.TerminalPV), *dcf.TerminalShare)},
		{"股权价值", humanMoney(*dcf.Value)},
		{"每股价值", fmt.Sprintf("%.2f", *dcf.PerShare)},
		{"扣除安全边际后", fmt.Sprintf("%.2f", *dcf.FairPrice)},
		{"收盘价", fmt.Sprintf("%.2f", r.Price)},
		{"相对收盘价", upside},
	})
	fmt.Fprintln(out, "\nDCF 对参数高度敏感，可通过 --growth-rate、--wacc、--terminal-growth 调整")
}

// --- Helpers ---

func richTable(out io.Writer, headers []string, data [][]string) {
	table := termtable.New(out, headers, termtable.Compact)
	for _, row := range data {
		table.Append(row)
	}
	table.Render()
}

func ff(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *v)
}

func fpct(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", *v)
}

func humanMoney(v float64) string {
	if v < 0 {
		return "-" + utils.HumanNum(-v)
	}
	return utils.HumanNum(v)
}

func moneyOrDash(v *float64) string {
	if v == nil {
		return "-"
	}
	return humanMoney(*v)
}

func percentile(m shared.Multiple) string {
	return historyPercentile(m.History)
}

func historyPercentile(h *shared.History) string {
	if h != nil && h.Short {
		return "样本不足"
	}
	if h == nil || h.Percentile == nil {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", *h.Percentile)
}

// multipleNote 说明倍数的口径；上游返回负值时说明原因。
func multipleNote(m shared.Multiple, basis string) string {
	if m.Current == nil && m.Raw != nil && *m.Raw <= 0 {
		return fmt.Sprintf("上游值 %.2f，亏损或净资产为负时没有意义", *m.Raw)
	}
	if m.Current == nil {
		return "上游缺失"
	}
	return basis
}
