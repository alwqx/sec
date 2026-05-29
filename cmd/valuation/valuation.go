package valuation

import (
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/olekukonko/tablewriter"
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
	cmd.Flags().StringP("method", "m", "overview",
		"Valuation method: overview, pe, pb, ps, peg, graham, dcf")
	// DCF parameters
	cmd.Flags().Float64("growth-rate", 0, "DCF: growth rate %, 0=auto from CAGR")
	cmd.Flags().Float64("terminal-growth", 3, "DCF: terminal growth rate %")
	cmd.Flags().Float64("wacc", 0, "DCF: discount rate %, 0=auto 8%")
	cmd.Flags().Float64("margin-of-safety", 20, "DCF: margin of safety %")
	return cmd
}

// Metrics holds all valuation metrics for a stock.
type Metrics struct {
	Code   string
	Name   string
	Price  float64
	MktCap float64
	Shares float64

	PE     float64
	PB     float64
	PS     float64
	PEG    float64
	Graham float64
	ROE    float64

	EPS        float64
	BVPS       float64
	RevenueTTM float64
	ProfitTTM  float64
	GrowthRate float64

	HistPE     []float64
	HistYears  []string
	HistMedian float64
	HistMin    float64
	HistMax    float64

	Assessment string
}

func ValuationHandler(cmd *cobra.Command, args []string) error {
	key := args[0]
	secs := sina.Search(cmd.Context(), key)
	if len(secs) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "未找到证券: %s\n", key)
		return nil
	}
	sec := secs[0]

	var (
		profile          *sina.CorpProfile
		incomeItems      []*eastmoney.FinancialReportItem
		balanceItems     []*eastmoney.FinancialReportItem
		err1, err2, err3 error
		wg               sync.WaitGroup
	)
	wg.Add(3)

	opts := &types.InfoOptions{Code: sec.Code, ExCode: sec.ExCode}
	go func() {
		defer wg.Done()
		profile, err1 = sina.Profile(cmd.Context(), opts)
	}()
	go func() {
		defer wg.Done()
		incomeItems, err2 = eastmoney.GetFinancialReport(cmd.Context(), &eastmoney.GetFinancialReportReq{
			Code: sec.Code, ReportType: eastmoney.ReportIncome, Period: eastmoney.PeriodAnnual,
		})
	}()
	go func() {
		defer wg.Done()
		balanceItems, err3 = eastmoney.GetFinancialReport(cmd.Context(), &eastmoney.GetFinancialReportReq{
			Code: sec.Code, ReportType: eastmoney.ReportBalance, Period: eastmoney.PeriodAnnual,
		})
	}()
	wg.Wait()

	if err1 != nil {
		return fmt.Errorf("获取公司信息失败: %w", err1)
	}
	if err2 != nil {
		return fmt.Errorf("获取利润表失败: %w", err2)
	}
	if err3 != nil {
		return fmt.Errorf("获取资产负债表失败: %w", err3)
	}

	m := computeMetrics(sec.ExCode, sec.Name, profile, incomeItems, balanceItems)

	method, _ := cmd.Flags().GetString("method")
	switch method {
	case "pe":
		printPEMethod(cmd, m)
	case "pb":
		printPBMethod(cmd, m)
	case "ps":
		printPSMethod(cmd, m)
	case "peg":
		printPEGMethod(cmd, m, incomeItems)
	case "graham":
		printGrahamMethod(cmd, m)
	case "dcf":
		growthRate, _ := cmd.Flags().GetFloat64("growth-rate")
		terminalGrowth, _ := cmd.Flags().GetFloat64("terminal-growth")
		wacc, _ := cmd.Flags().GetFloat64("wacc")
		marginOfSafety, _ := cmd.Flags().GetFloat64("margin-of-safety")
		printDCFMethod(cmd, m, growthRate, terminalGrowth, wacc, marginOfSafety)
	default:
		printOverview(cmd, m, incomeItems)
	}
	return nil
}

// computeMetrics builds valuation metrics from financial data.
// It extracts EPS, revenue, net profit from income statements, equity from balance sheets,
// then computes derived metrics: PS, PEG, BVPS, ROE, Graham Number.
func computeMetrics(code, name string, profile *sina.CorpProfile,
	incomeItems, balanceItems []*eastmoney.FinancialReportItem) *Metrics {

	m := &Metrics{
		Code:   code,
		Name:   name,
		Price:  profile.Current,
		MktCap: profile.MarketCap,
		PE:     profile.PeTTM,
		PB:     profile.PB,
	}

	if len(incomeItems) > 0 {
		latest := incomeItems[0]
		m.EPS = fieldFloat(latest, "BASIC_EPS")
		m.RevenueTTM = fieldFloat(latest, "TOTAL_OPERATE_INCOME")
		m.ProfitTTM = fieldFloat(latest, "PARENT_NETPROFIT")

		if m.RevenueTTM > 0 && m.MktCap > 0 {
			m.PS = m.MktCap / m.RevenueTTM
		}
		m.GrowthRate = computeGrowthRate(incomeItems)
		m.PEG = computePEG(m.PE, m.GrowthRate)
	}

	if len(balanceItems) > 0 {
		latest := balanceItems[0]
		totalEquity := fieldFloat(latest, "TOTAL_EQUITY")

		if m.Price > 0 && m.MktCap > 0 {
			m.Shares = m.MktCap / m.Price
		}
		if m.Shares > 0 && totalEquity > 0 {
			m.BVPS = totalEquity / m.Shares
		}
		if totalEquity > 0 && m.ProfitTTM > 0 {
			m.ROE = m.ProfitTTM / totalEquity * 100
		}
		if m.PB == 0 && m.BVPS > 0 && m.Price > 0 {
			m.PB = m.Price / m.BVPS
		}
		if m.PE == 0 && m.EPS > 0 && m.Price > 0 {
			m.PE = m.Price / m.EPS
		}
	}

	if m.EPS > 0 && m.BVPS > 0 {
		m.Graham = math.Sqrt(22.5 * m.EPS * m.BVPS)
	}
	computeHistPE(m, incomeItems)
	m.Assessment = assess(m)
	return m
}

// computeGrowthRate calculates the net profit CAGR (Compound Annual Growth Rate)
// from a list of annual income statement items sorted by date descending.
// Returns percentage (e.g. 15.2 means 15.2% annual growth). Returns 0 if insufficient data.
func computeGrowthRate(incomeItems []*eastmoney.FinancialReportItem) float64 {
	profits := make([]float64, 0, len(incomeItems))
	for _, item := range incomeItems {
		if v := fieldFloat(item, "PARENT_NETPROFIT"); v > 0 {
			profits = append(profits, v)
		}
	}
	if len(profits) < 2 {
		return 0
	}
	latest := profits[0]
	oldest := profits[len(profits)-1]
	years := float64(len(profits) - 1)
	if oldest <= 0 || years <= 0 {
		return 0
	}
	return (math.Pow(latest/oldest, 1.0/years) - 1) * 100
}

// computePEG returns the PEG ratio: P/E divided by earnings growth rate (%).
// PEG = 1 is considered fair value; < 1 suggests undervaluation; > 2 suggests overvaluation.
// Returns 0 if either input is invalid.
func computePEG(pe, growthRate float64) float64 {
	if pe <= 0 || growthRate <= 0 {
		return 0
	}
	return pe / growthRate
}

// computeHistPE populates historical PE data from annual income statements.
// Uses current price divided by each year's EPS as a rough historical PE proxy.
// Computes min, max, median, and sorted HistPE for percentile evaluation.
func computeHistPE(m *Metrics, items []*eastmoney.FinancialReportItem) {
	for _, item := range items {
		eps := fieldFloat(item, "BASIC_EPS")
		if eps <= 0 || m.Price <= 0 {
			continue
		}
		m.HistPE = append(m.HistPE, m.Price/eps)
		if len(item.ReportDate) >= 4 {
			m.HistYears = append(m.HistYears, item.ReportDate[:4])
		}
	}
	if n := len(m.HistPE); n > 0 {
		m.HistMin, m.HistMax = m.HistPE[0], m.HistPE[0]
		for _, v := range m.HistPE {
			if v < m.HistMin {
				m.HistMin = v
			}
			if v > m.HistMax {
				m.HistMax = v
			}
		}
		for i := 1; i < n; i++ {
			for j := i; j > 0 && m.HistPE[j] < m.HistPE[j-1]; j-- {
				m.HistPE[j], m.HistPE[j-1] = m.HistPE[j-1], m.HistPE[j]
			}
		}
		if n%2 == 0 {
			m.HistMedian = (m.HistPE[n/2-1] + m.HistPE[n/2]) / 2
		} else {
			m.HistMedian = m.HistPE[n/2]
		}
	}
}

// assess produces a comprehensive valuation assessment by aggregating
// signals from PE historical percentile, PB level, PEG ratio, and Graham Number.
// Returns one of: "偏低区间", "合理区间", "偏高区间".
func assess(m *Metrics) string {
	low, neutral, high := 0, 0, 0
	if m.PE > 0 && m.HistMedian > 0 {
		pct := (m.PE - m.HistMin) / (m.HistMax - m.HistMin + 0.01) * 100
		if pct < 30 {
			low++
		} else if pct > 70 {
			high++
		} else {
			neutral++
		}
	}
	if m.PB > 0 && m.PB < 1.0 {
		low++
	} else if m.PB > 3.0 {
		high++
	} else if m.PB > 0 {
		neutral++
	}
	if m.PEG > 0 {
		if m.PEG < 0.8 {
			low++
		} else if m.PEG > 2.0 {
			high++
		} else {
			neutral++
		}
	}
	if m.Graham > 0 && m.Price > 0 && m.Price < m.Graham {
		low++
	}
	if low > high {
		return "偏低区间"
	} else if high > low {
		return "偏高区间"
	}
	return "合理区间"
}

// --- Display Functions ---

func printHeader(cmd *cobra.Command, m *Metrics) {
	fmt.Fprintf(cmd.OutOrStdout(), "\n证券代码: %s  证券名称: %s\n", m.Code, m.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "当前股价: %.2f  总市值: %s\n\n", m.Price, types.HumanNum(m.MktCap))
}

func printOverview(cmd *cobra.Command, m *Metrics, incomeItems []*eastmoney.FinancialReportItem) {
	out := cmd.OutOrStdout()
	printHeader(cmd, m)

	fmt.Fprintf(out, "【市场数据】\n")
	mt := tablewriter.NewWriter(out)
	mt.SetHeader([]string{"当前股价", "总市值", "EPS", "BVPS"})
	mt.SetHeaderColor(tablewriter.Colors{tablewriter.Bold}, tablewriter.Colors{tablewriter.Bold},
		tablewriter.Colors{tablewriter.Bold}, tablewriter.Colors{tablewriter.Bold})
	mt.Append([]string{fmt.Sprintf("%.2f", m.Price), types.HumanNum(m.MktCap),
		fmt.Sprintf("%.2f", m.EPS), fmt.Sprintf("%.2f", m.BVPS)})
	mt.SetHeaderLine(false)
	mt.SetBorder(false)
	mt.SetAlignment(tablewriter.ALIGN_LEFT)
	mt.Render()

	fmt.Fprintf(out, "\n【估值指标】\n")
	richTable(out, []string{"指标", "当前值", "说明"}, [][]string{
		{"P/E (TTM)", ff(m.PE), peDesc(m)},
		{"P/B", ff(m.PB), pbDesc(m)},
		{"P/S", ff(m.PS), "-"},
		{"PEG", ff(m.PEG), pegDesc(m)},
		{"ROE", fpct(m.ROE), roeDesc(m)},
		{"格雷厄姆数", ff(m.Graham), grahamDesc(m)},
	})

	if len(m.HistYears) > 0 && m.HistMax > 0 {
		fmt.Fprintf(out, "\n【历史 PE 范围（年报）】\n")
		fmt.Fprintf(out, "  最低: %.2f  中位: %.2f  最高: %.2f  当前: %.2f\n",
			m.HistMin, m.HistMedian, m.HistMax, m.PE)

		fmt.Fprintf(out, "  ")
		for i, y := range m.HistYears {
			if i < len(m.HistPE) {
				fmt.Fprintf(out, "%s(%.1fx)  ", y, m.HistPE[i])
			}
		}
		fmt.Fprintln(out)
	}

	if len(incomeItems) > 0 {
		fmt.Fprintf(out, "\n【近年盈利趋势】\n")
		var rows [][]string
		for i, item := range incomeItems {
			if i >= 5 {
				break
			}
			date := item.ReportDate
			if len(date) >= 10 {
				date = date[:10]
			}
			rows = append(rows, []string{
				date,
				fmt.Sprintf("%.2f", fieldFloat(item, "BASIC_EPS")),
				types.HumanNum(fieldFloat(item, "TOTAL_OPERATE_INCOME")),
				types.HumanNum(fieldFloat(item, "PARENT_NETPROFIT")),
			})
		}
		richTable(out, []string{"报告期", "EPS", "营收", "净利润"}, rows)
	}

	fmt.Fprintf(out, "\n【综合评估】→ %s\n", m.Assessment)
	fmt.Fprintf(out, "  P/E: %s | P/B: %s | PEG: %s | 格雷厄姆: %s\n\n",
		peDesc(m), pbDesc(m), pegDesc(m), grahamDesc(m))
}

func printPEMethod(cmd *cobra.Command, m *Metrics) {
	out := cmd.OutOrStdout()
	printHeader(cmd, m)
	fmt.Fprintf(out, "方法: 市盈率法 (P/E) — 合理股价 = 合理PE × EPS\n\n")

	richTable(out, []string{"项目", "数值"}, [][]string{
		{"当前 P/E (TTM)", ff(m.PE)},
		{"历史最低 PE", ff(m.HistMin)},
		{"历史中位 PE", ff(m.HistMedian)},
		{"历史最高 PE", ff(m.HistMax)},
		{"当前 EPS", fmt.Sprintf("%.2f", m.EPS)},
		{"盈利增长率 (CAGR)", fpct(m.GrowthRate)},
	})

	if m.HistMedian > 0 {
		fmt.Fprintf(out, "\n【估值情景】\n")
		richTable(out, []string{"PE 假设", "对应股价", "涨跌幅"}, [][]string{
			{"历史最低 (" + fmt.Sprintf("%.1f", m.HistMin) + "x)", fmt.Sprintf("%.2f", m.HistMin*m.EPS), fmt.Sprintf("%+.0f%%", (m.HistMin/m.PE-1)*100)},
			{"历史中位 (" + fmt.Sprintf("%.1f", m.HistMedian) + "x)", fmt.Sprintf("%.2f", m.HistMedian*m.EPS), fmt.Sprintf("%+.0f%%", (m.HistMedian/m.PE-1)*100)},
			{"历史最高 (" + fmt.Sprintf("%.1f", m.HistMax) + "x)", fmt.Sprintf("%.2f", m.HistMax*m.EPS), fmt.Sprintf("%+.0f%%", (m.HistMax/m.PE-1)*100)},
		})
	}
	fmt.Fprintf(out, "\n【评估】%s\n\n", peDesc(m))
}

func printPBMethod(cmd *cobra.Command, m *Metrics) {
	out := cmd.OutOrStdout()
	printHeader(cmd, m)
	fmt.Fprintf(out, "方法: 市净率法 (P/B) — 合理股价 = 合理PB × BVPS\n\n")

	richTable(out, []string{"项目", "数值"}, [][]string{
		{"当前 P/B", ff(m.PB)},
		{"每股净资产 BVPS", fmt.Sprintf("%.2f", m.BVPS)},
		{"ROE", fpct(m.ROE)},
		{"合理 PB (ROE/8%%)", fmt.Sprintf("%.2f", m.ROE/8)},
	})

	if m.BVPS > 0 {
		fmt.Fprintf(out, "\n【估值情景】\n")
		richTable(out, []string{"PB 假设", "对应股价", "涨跌幅"}, [][]string{
			{"破净 (0.8x)", fmt.Sprintf("%.2f", 0.8*m.BVPS), fmt.Sprintf("%+.0f%%", (0.8*m.BVPS/m.Price-1)*100)},
			{"合理 (1.0x)", fmt.Sprintf("%.2f", m.BVPS), fmt.Sprintf("%+.0f%%", (m.BVPS/m.Price-1)*100)},
			{"溢价 (1.5x)", fmt.Sprintf("%.2f", 1.5*m.BVPS), fmt.Sprintf("%+.0f%%", (1.5*m.BVPS/m.Price-1)*100)},
		})
	}
	fmt.Fprintf(out, "\n【评估】%s\n\n", pbDesc(m))
}

func printPSMethod(cmd *cobra.Command, m *Metrics) {
	out := cmd.OutOrStdout()
	printHeader(cmd, m)
	fmt.Fprintf(out, "方法: 市销率法 (P/S) — 合理市值 = 合理PS × 营业收入\n\n")

	richTable(out, []string{"项目", "数值"}, [][]string{
		{"当前 P/S", ff(m.PS)},
		{"营业收入 (TTM)", types.HumanNum(m.RevenueTTM)},
		{"总市值", types.HumanNum(m.MktCap)},
	})

	fmt.Fprintf(out, "\n【评估】")
	if m.PS > 0 && m.PS < 1 {
		fmt.Fprintf(out, "P/S < 1: 市值低于年营收，偏低区间\n")
	} else if m.PS > 5 {
		fmt.Fprintf(out, "P/S > 5: 偏高区间，需要高增长支撑\n")
	} else {
		fmt.Fprintf(out, "P/S 在合理区间\n")
	}
	fmt.Fprintf(out, "适用: 营收稳定但尚未盈利或利润波动大的公司\n\n")
}

func printPEGMethod(cmd *cobra.Command, m *Metrics, incomeItems []*eastmoney.FinancialReportItem) {
	out := cmd.OutOrStdout()
	printHeader(cmd, m)
	fmt.Fprintf(out, "方法: PEG 指标 — PEG = P/E / 盈利增长率(%%)\n")
	fmt.Fprintf(out, "合理 PEG = 1，即 P/E 应等于增长率\n\n")

	richTable(out, []string{"项目", "数值"}, [][]string{
		{"P/E (TTM)", ff(m.PE)},
		{"净利润 CAGR", fpct(m.GrowthRate)},
		{"PEG", ff(m.PEG)},
	})

	if len(incomeItems) > 0 {
		fmt.Fprintf(out, "\n【历年净利润】\n")
		for _, item := range incomeItems {
			date := item.ReportDate
			if len(date) >= 10 {
				date = date[:10]
			}
			fmt.Fprintf(out, "  %s  %s\n", date, types.HumanNum(fieldFloat(item, "PARENT_NETPROFIT")))
		}
	}

	fmt.Fprintf(out, "\n【评估】")
	switch {
	case m.PEG > 0 && m.PEG < 0.5:
		fmt.Fprintf(out, "PEG < 0.5，显著低估\n")
	case m.PEG > 0 && m.PEG < 1:
		fmt.Fprintf(out, "PEG < 1，估值偏低\n")
	case m.PEG > 2:
		fmt.Fprintf(out, "PEG > 2，估值偏高\n")
	case m.PEG > 0:
		fmt.Fprintf(out, "PEG 在合理区间\n")
	default:
		fmt.Fprintf(out, "盈利无增长或无利润，PEG 不适用\n")
	}
	fmt.Fprintln(out)
}

func printGrahamMethod(cmd *cobra.Command, m *Metrics) {
	out := cmd.OutOrStdout()
	printHeader(cmd, m)
	fmt.Fprintf(out, "方法: 格雷厄姆公式 — √(22.5 × EPS × BVPS)\n")
	fmt.Fprintf(out, "含义: P/E≤15 且 P/B≤1.5 时的最高合理买入价\n\n")

	richTable(out, []string{"项目", "数值"}, [][]string{
		{"EPS", fmt.Sprintf("%.2f", m.EPS)},
		{"BVPS", fmt.Sprintf("%.2f", m.BVPS)},
		{"格雷厄姆数", fmt.Sprintf("%.2f", m.Graham)},
		{"当前股价", fmt.Sprintf("%.2f", m.Price)},
		{"安全边际", fmt.Sprintf("%+.1f%%", (m.Graham/m.Price-1)*100)},
	})

	fmt.Fprintf(out, "\n【评估】")
	if m.Price < m.Graham {
		fmt.Fprintf(out, "当前价 < 格雷厄姆数，具有安全边际 ✓\n")
	} else {
		fmt.Fprintf(out, "当前价 > 格雷厄姆数，超出格雷厄姆合理价位\n")
	}
	fmt.Fprintln(out)
}

func printDCFMethod(cmd *cobra.Command, m *Metrics,
	growthRate, terminalGrowth, wacc, marginOfSafety float64) {
	out := cmd.OutOrStdout()
	printHeader(cmd, m)
	fmt.Fprintf(out, "方法: 自由现金流折现 (DCF)\n")
	fmt.Fprintf(out, "公式: 企业价值 = Σ(FCF_t/(1+WACC)^t) + 终值/(1+WACC)^n\n\n")

	if growthRate <= 0 {
		growthRate = m.GrowthRate
	}
	if growthRate <= 0 {
		growthRate = 5
	}
	if wacc <= 0 {
		wacc = 8
	}

	fcf := m.ProfitTTM * 0.7
	years := 5

	fmt.Fprintf(out, "【DCF 参数】\n")
	richTable(out, []string{"参数", "数值", "说明"}, [][]string{
		{"当前 FCF (近似)", types.HumanNum(fcf), "净利润 × 70%"},
		{"增长阶段", fmt.Sprintf("%d 年", years), "高增长期"},
		{"增长率", fpct(growthRate), "--growth-rate"},
		{"永续增长率", fpct(terminalGrowth), "--terminal-growth"},
		{"折现率 WACC", fpct(wacc), "--wacc"},
		{"安全边际", fpct(marginOfSafety), "--margin-of-safety"},
	})

	pv := 0.0
	cf := fcf
	for t := 1; t <= years; t++ {
		cf = cf * (1 + growthRate/100)
		pv += cf / math.Pow(1+wacc/100, float64(t))
	}
	terminalCF := cf * (1 + terminalGrowth/100)
	terminalValue := terminalCF / ((wacc - terminalGrowth) / 100)
	pvTerminal := terminalValue / math.Pow(1+wacc/100, float64(years))

	enterpriseValue := pv + pvTerminal
	equityValue := enterpriseValue / m.Shares
	fairPrice := equityValue * (1 - marginOfSafety/100)

	fmt.Fprintf(out, "\n【DCF 估值结果】\n")
	richTable(out, []string{"项目", "数值"}, [][]string{
		{"增长期现值", types.HumanNum(pv)},
		{"终值现值", types.HumanNum(pvTerminal)},
		{"企业价值", types.HumanNum(enterpriseValue)},
		{"每股内在价值", fmt.Sprintf("%.2f", equityValue)},
		{"安全边际后合理价", fmt.Sprintf("%.2f", fairPrice)},
		{"当前股价", fmt.Sprintf("%.2f", m.Price)},
		{"上涨/下跌空间", fmt.Sprintf("%+.1f%%", (fairPrice/m.Price-1)*100)},
	})

	fmt.Fprintf(out, "\n【评估】")
	if fairPrice > m.Price {
		fmt.Fprintf(out, "内在价值高于当前价，存在低估\n")
	} else {
		fmt.Fprintf(out, "内在价值低于当前价，当前价已反映或高估\n")
	}
	fmt.Fprintf(out, "注意: DCF 对参数高度敏感，可通过 --growth-rate/--wacc/--terminal-growth 调整\n\n")
}

// --- Helpers ---

func richTable(out io.Writer, headers []string, data [][]string) {
	table := tablewriter.NewWriter(out)
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
	for _, row := range data {
		table.Append(row)
	}
	table.Render()
}

func fieldFloat(item *eastmoney.FinancialReportItem, key string) float64 {
	if v, ok := item.Fields[key].(float64); ok {
		return v
	}
	return 0
}

func ff(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", v)
}

func fpct(v float64) string {
	if v == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", v)
}

// peDesc returns a human-readable description of the current P/E level
// based on its percentile rank within the historical PE range.
func peDesc(m *Metrics) string {
	if m.PE <= 0 {
		return "亏损，P/E 无意义"
	}
	if m.HistMedian > 0 {
		pct := int((m.PE - m.HistMin) / (m.HistMax - m.HistMin + 0.01) * 100)
		return fmt.Sprintf("历史分位 ~%d%%", pct)
	}
	return "-"
}

func pbDesc(m *Metrics) string {
	if m.PB <= 0 {
		return "-"
	}
	switch {
	case m.PB < 1.0:
		return "破净"
	case m.PB < 1.5:
		return "低估值区间"
	case m.PB > 3.0:
		return "高估值区间"
	default:
		return "合理区间"
	}
}

// pegDesc returns a human-readable PEG assessment:
// <0.5 = 显著低估, <1 = 偏低, >2 = 偏高, 1-2 = 合理.
func pegDesc(m *Metrics) string {
	if m.PEG <= 0 {
		return "-"
	}
	switch {
	case m.PEG < 0.5:
		return "显著低估"
	case m.PEG < 1.0:
		return "偏低 (PEG<1)"
	case m.PEG > 2.0:
		return "偏高 (PEG>2)"
	default:
		return "合理"
	}
}

// roeDesc returns a human-readable ROE assessment:
// <5% = 低, <10% = 一般, ≥20% = 优秀, 10-20% = 良好.
func roeDesc(m *Metrics) string {
	if m.ROE <= 0 {
		return "-"
	}
	switch {
	case m.ROE < 5:
		return "低"
	case m.ROE < 10:
		return "一般"
	case m.ROE >= 20:
		return "优秀 (>20%)"
	default:
		return "良好"
	}
}

// grahamDesc compares current price to Graham Number and returns an assessment.
func grahamDesc(m *Metrics) string {
	if m.Graham <= 0 {
		return "-"
	}
	if m.Price < m.Graham {
		return "当前价 < 格雷厄姆数 ✓"
	}
	return "当前价 > 格雷厄姆数"
}
