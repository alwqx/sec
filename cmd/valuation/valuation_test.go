package valuation

import (
	"testing"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
)

func makeIncomeItem(profit, eps float64, date string) *eastmoney.FinancialReportItem {
	return &eastmoney.FinancialReportItem{
		ReportDate: date,
		Fields: map[string]interface{}{
			"PARENT_NETPROFIT": profit,
			"BASIC_EPS":        eps,
		},
	}
}

func TestComputePEG(t *testing.T) {
	require.Equal(t, 0.0, computePEG(0, 10))       // negative PE
	require.Equal(t, 0.0, computePEG(10, 0))       // zero growth
	require.Equal(t, 0.0, computePEG(10, -5))      // negative growth
	require.Equal(t, 1.0, computePEG(10, 10))      // PEG = 1 (fair)
	require.Equal(t, 0.5, computePEG(5, 10))       // PEG < 1 (undervalued)
	require.Equal(t, 2.0, computePEG(20, 10))      // PEG = 2 (overvalued)
	require.Equal(t, 0.25, computePEG(5, 20))      // high growth, low PE
}

func TestComputeGrowthRate(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		require.Equal(t, 0.0, computeGrowthRate(nil))
		require.Equal(t, 0.0, computeGrowthRate([]*eastmoney.FinancialReportItem{}))
	})

	t.Run("single item", func(t *testing.T) {
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(100, 1, "2024-12-31"),
		}
		require.Equal(t, 0.0, computeGrowthRate(items))
	})

	t.Run("zero profits", func(t *testing.T) {
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(0, 0, "2024-12-31"),
			makeIncomeItem(0, 0, "2023-12-31"),
		}
		require.Equal(t, 0.0, computeGrowthRate(items))
	})

	t.Run("growth", func(t *testing.T) {
		// 3 years: 100 → 121 → 133.1, CAGR = 10% → 100*1.1^2 = 121, 121*1.1 = 133.1... wait
		// Actually: 2022=100, 2023=121, 2024=133.1 → CAGR = (133.1/100)^(1/2)-1 = sqrt(1.331)-1 = 0.153... hmm
		// Let me use simpler numbers: 100 → 144 over 2 years → CAGR = (144/100)^(1/2)-1 = 1.2-1 = 20%
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(144, 1.44, "2024-12-31"),
			makeIncomeItem(120, 1.20, "2023-12-31"),
			makeIncomeItem(100, 1.00, "2022-12-31"),
		}
		// CAGR = (144/100)^(1/2) - 1 = 1.2 - 1 = 0.2 = 20%
		require.InDelta(t, 20.0, computeGrowthRate(items), 0.01)
	})

	t.Run("decline", func(t *testing.T) {
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(80, 0.8, "2024-12-31"),
			makeIncomeItem(100, 1.0, "2023-12-31"),
		}
		// CAGR = (80/100)^1 - 1 = -20%
		require.InDelta(t, -20.0, computeGrowthRate(items), 0.01)
	})

	t.Run("negative profit skipped", func(t *testing.T) {
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(100, 1.0, "2024-12-31"),
			makeIncomeItem(-50, -0.5, "2023-12-31"), // skipped (negative)
			makeIncomeItem(50, 0.5, "2022-12-31"),
		}
		// CAGR = (100/50)^(1/1) - 1 = 100%
		require.InDelta(t, 100.0, computeGrowthRate(items), 0.01)
	})
}

func TestComputeHistPE(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		m := &Metrics{Price: 50}
		computeHistPE(m, nil)
		require.Empty(t, m.HistPE)
	})

	t.Run("with data", func(t *testing.T) {
		m := &Metrics{Price: 50}
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(100, 5.0, "2024-12-31"),  // PE = 50/5 = 10
			makeIncomeItem(80, 2.0, "2023-12-31"),   // PE = 50/2 = 25
			makeIncomeItem(60, 6.25, "2022-12-31"),   // PE = 50/6.25 = 8
		}
		computeHistPE(m, items)

		require.Equal(t, 3, len(m.HistPE))
		require.Equal(t, 8.0, m.HistMin)
		require.Equal(t, 25.0, m.HistMax)
		require.Equal(t, 10.0, m.HistMedian)  // sorted: [8, 10, 25]
		require.Equal(t, 3, len(m.HistYears))
		require.Equal(t, "2024", m.HistYears[0])
	})

	t.Run("zero price skipped", func(t *testing.T) {
		m := &Metrics{Price: 0}
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(100, 5.0, "2024-12-31"),
		}
		computeHistPE(m, items)
		require.Empty(t, m.HistPE)
	})

	t.Run("negative eps skipped", func(t *testing.T) {
		m := &Metrics{Price: 50}
		items := []*eastmoney.FinancialReportItem{
			makeIncomeItem(-100, -2.0, "2024-12-31"),
		}
		computeHistPE(m, items)
		require.Empty(t, m.HistPE)
	})
}

func TestAssess(t *testing.T) {
	t.Run("low", func(t *testing.T) {
		m := &Metrics{
			PE: 6, PB: 0.8, PEG: 0.4, Graham: 60, Price: 50,
			HistMin: 5, HistMax: 15, HistMedian: 10,
		}
		// PE at low percentile, PB<1 (low), PEG<0.8 (low), Graham>Price (low)
		require.Equal(t, "偏低区间", assess(m))
	})

	t.Run("high", func(t *testing.T) {
		m := &Metrics{
			PE: 14, PB: 4, PEG: 3, Graham: 30, Price: 50,
			HistMin: 5, HistMax: 15, HistMedian: 10,
		}
		require.Equal(t, "偏高区间", assess(m))
	})

	t.Run("neutral", func(t *testing.T) {
		m := &Metrics{
			PE: 10, PB: 1.5, PEG: 1.2, Graham: 50, Price: 50,
			HistMin: 5, HistMax: 15, HistMedian: 10,
		}
		require.Equal(t, "合理区间", assess(m))
	})

	t.Run("no historical data", func(t *testing.T) {
		m := &Metrics{PE: 10, PB: 1.5, PEG: 0, Graham: 0, Price: 50}
		require.Equal(t, "合理区间", assess(m))
	})
}

func TestPeDesc(t *testing.T) {
	t.Run("negative pe", func(t *testing.T) {
		m := &Metrics{PE: -1}
		require.Equal(t, "亏损，P/E 无意义", peDesc(m))
	})

	t.Run("with history", func(t *testing.T) {
		m := &Metrics{PE: 6, HistMin: 5, HistMax: 15, HistMedian: 10}
		require.Contains(t, peDesc(m), "历史分位")
	})

	t.Run("no history", func(t *testing.T) {
		m := &Metrics{PE: 10}
		require.Equal(t, "-", peDesc(m))
	})
}

func TestPegDesc(t *testing.T) {
	require.Equal(t, "-", pegDesc(&Metrics{PEG: 0}))
	require.Equal(t, "显著低估", pegDesc(&Metrics{PEG: 0.3}))
	require.Equal(t, "偏低 (PEG<1)", pegDesc(&Metrics{PEG: 0.8}))
	require.Equal(t, "合理", pegDesc(&Metrics{PEG: 1.5}))
	require.Equal(t, "偏高 (PEG>2)", pegDesc(&Metrics{PEG: 3.0}))
}

func TestRoeDesc(t *testing.T) {
	require.Equal(t, "-", roeDesc(&Metrics{ROE: 0}))
	require.Equal(t, "低", roeDesc(&Metrics{ROE: 3}))
	require.Equal(t, "一般", roeDesc(&Metrics{ROE: 8}))
	require.Equal(t, "良好", roeDesc(&Metrics{ROE: 15}))
	require.Equal(t, "优秀 (>20%)", roeDesc(&Metrics{ROE: 25}))
}

func TestGrahamDesc(t *testing.T) {
	require.Equal(t, "-", grahamDesc(&Metrics{Graham: 0}))
	require.Contains(t, grahamDesc(&Metrics{Graham: 60, Price: 50}), "当前价 < 格雷厄姆数")
	require.Contains(t, grahamDesc(&Metrics{Graham: 40, Price: 50}), "当前价 > 格雷厄姆数")
}

func TestFf(t *testing.T) {
	require.Equal(t, "-", ff(0))
	require.Equal(t, "10.50", ff(10.5))
}

func TestFpct(t *testing.T) {
	require.Equal(t, "-", fpct(0))
	require.Equal(t, "15.2%", fpct(15.2))
}
