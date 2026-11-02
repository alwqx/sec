package valuation

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func num(value float64) *float64 { return &value }

func point(date string, pe, pb, ps *float64) *eastmoney.ValuationPoint {
	return &eastmoney.ValuationPoint{Date: date, Name: "招商银行", Board: "汽车", Close: 50, MarketCap: 5e9, Shares: 1e8, PE: pe, PB: pb, PS: ps}
}

func flat(n int, pe, pb, ps float64) []*eastmoney.ValuationPoint {
	points := make([]*eastmoney.ValuationPoint, n)
	for i := range points {
		points[i] = point("2026-01-01", num(pe), num(pb), num(ps))
	}
	return points
}

func annual(year string, profit float64) *eastmoney.PerformanceReport {
	return &eastmoney.PerformanceReport{Date: year + "-12-31", EPS: num(1), BPS: num(2), ROE: num(10), Revenue: num(1000), Profit: num(profit)}
}

func TestComputeDerivesFromMultiples(t *testing.T) {
	r := Compute(flat(3, 10, 2, 5), nil, DefaultDCFParams())
	require.Equal(t, 50.0, r.Price)
	require.Equal(t, 5.0, *r.EPS)
	require.Equal(t, 25.0, *r.BVPS)
	require.Equal(t, 5e8, *r.ProfitTTM)
	require.Equal(t, 1e9, *r.RevenueTTM)
	require.InDelta(t, math.Sqrt(22.5*5*25), *r.Graham, 1e-9)
	require.InDelta(t, (*r.Graham/50-1)*100, *r.GrahamMargin, 1e-9)
}

func TestHistoryPercentileAndScenarios(t *testing.T) {
	points := []*eastmoney.ValuationPoint{point("2026-01-01", num(-5), nil, nil)} // 亏损日不计入分布
	for i := 0; i < 300; i++ {
		value := 10.0
		if i >= 100 {
			value = 20
		}
		if i >= 200 {
			value = 40
		}
		points = append(points, point("2026-01-02", num(value), nil, nil))
	}
	points = append(points, point("2026-01-03", num(20), nil, nil))
	r := Compute(points, nil, DefaultDCFParams())
	history := r.PE.History
	require.Equal(t, "2026-01-02", history.Since)
	require.Equal(t, 301, history.Samples)
	require.Equal(t, 10.0, history.Min)
	require.Equal(t, 20.0, history.Median)
	require.Equal(t, 40.0, history.Max)
	require.False(t, history.Short)
	require.InDelta(t, 201.0/301*100, *history.Percentile, 1e-9, "10 和 20 不高于当前值")
	require.Nil(t, r.PE.Series[0])
	require.Equal(t, 10.0, *r.PE.Series[1])
	require.Equal(t, "历史最低", r.PE.Scenarios[0].Label)
	require.Equal(t, 25.0, r.PE.Scenarios[0].Price)
	require.Equal(t, -50.0, r.PE.Scenarios[0].Change)
	require.Equal(t, 100.0, r.PE.Scenarios[2].Price)
	require.Nil(t, r.PB.History, "没有有效值")
	require.Nil(t, r.PB.Current)
}

func TestShortHistoryHasNoPercentile(t *testing.T) {
	r := Compute(flat(minPercentileSamples-1, 10, 2, 5), nil, DefaultDCFParams())
	require.True(t, r.PE.History.Short)
	require.Nil(t, r.PE.History.Percentile)
	require.Empty(t, r.PE.Scenarios)
	require.Equal(t, 10.0, r.PE.History.Median, "分布仍然保留")
	r = Compute(flat(minPercentileSamples, 10, 2, 5), nil, DefaultDCFParams())
	require.False(t, r.PE.History.Short)
	require.NotNil(t, r.PE.History.Percentile)
}

func TestMedianOfEvenSamples(t *testing.T) {
	r := Compute([]*eastmoney.ValuationPoint{point("a", num(10), nil, nil), point("b", num(20), nil, nil)}, nil, DefaultDCFParams())
	require.Equal(t, 15.0, r.PE.History.Median)
}

func TestLossCompany(t *testing.T) {
	points := flat(3, 10, 2, 5)
	points = append(points, point("2026-01-04", num(-20), num(2), num(5)))
	reports := []*eastmoney.PerformanceReport{annual("2025", 100), annual("2023", 50)}
	r := Compute(points, reports, DefaultDCFParams())
	require.Nil(t, r.PE.Current)
	require.Equal(t, -20.0, *r.PE.Raw)
	require.Nil(t, r.PE.History.Percentile, "历史还在，但当前值没有分位")
	require.Empty(t, r.PE.Scenarios)
	require.Equal(t, -2.5, *r.EPS)
	require.Less(t, *r.ProfitTTM, 0.0)
	require.Nil(t, r.PEG)
	require.Nil(t, r.Graham)
	require.False(t, r.DCF.Applicable)
	require.Contains(t, r.DCF.Note, "亏损")
}

func TestGrowth(t *testing.T) {
	r := func(reports ...*eastmoney.PerformanceReport) Growth {
		annuals := []Annual{}
		for _, report := range reports {
			annuals = append(annuals, Annual{Year: report.Date[:4], Profit: report.Profit})
		}
		return computeGrowth(annuals)
	}
	growth := r(annual("2025", 144), annual("2024", 120), annual("2023", 100))
	require.InDelta(t, 20, *growth.Rate, 1e-9)
	require.Equal(t, "2023", growth.FromYear)

	// 跨度按实际年份计算，缺一年时不能算成一年。
	growth = r(annual("2025", 144), annual("2023", 100))
	require.InDelta(t, 20, *growth.Rate, 1e-9)

	// 只回看 5 年。
	growth = r(annual("2025", 200), annual("2020", 100), annual("2019", 1))
	require.Equal(t, "2020", growth.FromYear)
	require.InDelta(t, (math.Pow(2, 0.2)-1)*100, *growth.Rate, 1e-9)

	growth = r(annual("2025", 80), annual("2023", 100))
	require.Less(t, *growth.Rate, 0.0, "下滑是有效的负增长")

	// 中间年份亏损不影响起止；起止年份亏损时没有结果。
	growth = r(annual("2025", 144), annual("2024", -5), annual("2023", 100))
	require.InDelta(t, 20, *growth.Rate, 1e-9)
	require.Nil(t, r(annual("2025", -1), annual("2023", 100)).Rate)
	require.Nil(t, r(annual("2025", 100), annual("2023", -1)).Rate)
	require.Contains(t, r(annual("2025", 100), annual("2024", 90)).Note, "少于 3 年")
	require.Nil(t, r().Rate)
}

func TestPEG(t *testing.T) {
	reports := []*eastmoney.PerformanceReport{annual("2025", 144), annual("2023", 100)}
	r := Compute(flat(3, 10, 2, 5), reports, DefaultDCFParams())
	require.InDelta(t, 0.5, *r.PEG, 1e-9)
	r = Compute(flat(3, 10, 2, 5), []*eastmoney.PerformanceReport{annual("2025", 80), annual("2023", 100)}, DefaultDCFParams())
	require.Nil(t, r.PEG, "负增长时 PEG 不适用")
}

// 没有年报时也要返回数组，页面按数组读取。
func TestNoAnnualsSerializeAsEmptyArray(t *testing.T) {
	body, err := json.Marshal(Compute(flat(1, 10, 2, 5), nil, DefaultDCFParams()))
	require.NoError(t, err)
	require.Contains(t, string(body), `"annuals":[]`)
}

func TestAnnualsIgnoreQuarterlyReports(t *testing.T) {
	reports := []*eastmoney.PerformanceReport{{Date: "2026-09-30", ROE: num(99)}, annual("2025", 100)}
	r := Compute(flat(1, 10, 2, 5), reports, DefaultDCFParams())
	require.Len(t, r.Annuals, 1)
	require.Equal(t, "2025", r.ROEYear)
	require.Equal(t, 10.0, *r.ROE)
}

func TestDCF(t *testing.T) {
	params := DCFParams{Growth: num(10), TerminalGrowth: 3, WACC: 8, MarginOfSafety: 20}
	r := Compute(flat(3, 10, 2, 5), nil, params)
	dcf := r.DCF
	require.True(t, dcf.Applicable)
	require.Equal(t, "手动输入", dcf.GrowthSource)
	base := 5e8 * 0.7
	pv, cash := 0.0, base
	for year := 1; year <= 5; year++ {
		cash *= 1.1
		pv += cash / math.Pow(1.08, float64(year))
	}
	terminal := cash * 1.03 / 0.05 / math.Pow(1.08, 5)
	require.Equal(t, base, dcf.BaseCash)
	require.InDelta(t, pv, dcf.GrowthPV, 1e-3)
	require.InDelta(t, terminal, dcf.TerminalPV, 1e-3)
	require.InDelta(t, (pv+terminal)/1e8, dcf.PerShare, 1e-6)
	require.InDelta(t, dcf.PerShare*0.8, dcf.FairPrice, 1e-6)
	require.InDelta(t, (dcf.FairPrice/50-1)*100, dcf.Upside, 1e-6)
	require.InDelta(t, terminal/(pv+terminal)*100, dcf.TerminalShare, 1e-6)
	require.Empty(t, dcf.Warning)
}

func TestDCFGrowthSource(t *testing.T) {
	cases := []struct {
		reports []*eastmoney.PerformanceReport
		growth  float64
		source  string
	}{
		{[]*eastmoney.PerformanceReport{annual("2025", 144), annual("2023", 100)}, 15, "±15%"},
		{[]*eastmoney.PerformanceReport{annual("2025", 16), annual("2023", 100)}, -15, "±15%"},
		{[]*eastmoney.PerformanceReport{annual("2025", 121), annual("2023", 100)}, 10, "CAGR 10.0%"},
		{[]*eastmoney.PerformanceReport{annual("2025", 81), annual("2023", 100)}, -10, "CAGR -10.0%"},
		{nil, 5, "默认 5%"},
	}
	for _, tc := range cases {
		dcf := Compute(flat(1, 10, 2, 5), tc.reports, DefaultDCFParams()).DCF
		require.InDelta(t, tc.growth, *dcf.Params.Growth, 1e-9)
		require.Contains(t, dcf.GrowthSource, tc.source)
	}
}

// 手动输入 0 是零增长，不能换成历史增长率。
func TestDCFZeroGrowthIsNotAuto(t *testing.T) {
	params := DefaultDCFParams()
	params.Growth = num(0)
	reports := []*eastmoney.PerformanceReport{annual("2025", 121), annual("2023", 100)}
	dcf := Compute(flat(1, 10, 2, 5), reports, params).DCF
	require.Equal(t, 0.0, *dcf.Params.Growth)
	require.Equal(t, "手动输入", dcf.GrowthSource)
	base := 5e8 * 0.7
	pv := 0.0
	for year := 1; year <= 5; year++ {
		pv += base / math.Pow(1.08, float64(year))
	}
	require.InDelta(t, pv, dcf.GrowthPV, 1e-3)
}

func TestDCFFinancialWarning(t *testing.T) {
	points := flat(1, 10, 2, 5)
	points[0].Board = "银行Ⅱ"
	require.Contains(t, Compute(points, nil, DefaultDCFParams()).DCF.Warning, "金融行业")
}

func TestDCFMissingShares(t *testing.T) {
	points := flat(1, 10, 2, 5)
	points[0].Shares = 0
	require.False(t, Compute(points, nil, DefaultDCFParams()).DCF.Applicable)
}

func TestValidate(t *testing.T) {
	require.NoError(t, DefaultDCFParams().Validate())
	require.NoError(t, DCFParams{Growth: num(0), WACC: 8}.Validate())
	for _, bad := range []DCFParams{
		{}, {WACC: math.NaN()}, {WACC: 8, Growth: num(math.NaN())}, {WACC: 8, TerminalGrowth: math.NaN()}, {WACC: 8, MarginOfSafety: math.NaN()},
		{WACC: 8, MarginOfSafety: 95}, {WACC: 8, MarginOfSafety: -1}, {WACC: 60}, {WACC: 8, Growth: num(200)}, {TerminalGrowth: 30, WACC: 40},
		{WACC: 8, TerminalGrowth: 8}, {WACC: 3, TerminalGrowth: 8}, {WACC: 8, TerminalGrowth: 7.5}, {WACC: math.Inf(1)},
	} {
		require.Error(t, bad.Validate(), "%+v", bad)
	}
}

func TestAssessment(t *testing.T) {
	// 当前值是历史最低，PE 和 PB 都偏低；样本不足时不参与判断。
	points := flat(300, 20, 4, 5)
	points = append(points, point("2026-01-02", num(5), num(1), num(5)))
	r := Compute(points, nil, DefaultDCFParams())
	require.Equal(t, "偏低区间", r.Assessment)
	require.Equal(t, "low", r.Signals[0].Level)

	points = flat(300, 5, 1, 5)
	points = append(points, point("2026-01-02", num(50), num(40), num(5)))
	require.Equal(t, "偏高区间", Compute(points, nil, DefaultDCFParams()).Assessment)

	short := flat(10, 20, 4, 5)
	r = Compute(short, nil, DefaultDCFParams())
	require.Len(t, r.Signals, 1, "样本不足时只剩格雷厄姆数")
	require.Equal(t, "数据不足", r.Assessment)

	require.Equal(t, "合理区间", assess([]Signal{{Level: "low"}, {Level: "high"}}))
	require.Equal(t, "合理区间", assess([]Signal{{Level: "neutral"}, {Level: "neutral"}}))
}

func mockValuation(valuationBody, performanceBody string) {
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_VALUEANALYSIS_DET").MatchParam("filter", `\(SECUCODE="000001.SZ"\)`).Reply(200).BodyString(valuationBody)
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_LICO_FN_CPD").MatchParam("filter", `\(SECUCODE="000001.SZ"\)`).Reply(200).BodyString(performanceBody)
}

func TestAnalyze(t *testing.T) {
	defer gock.Off()
	mockValuation(`{"success":true,"code":0,"result":{"data":[
		{"SECURITY_NAME_ABBR":"平安银行","BOARD_NAME":"银行Ⅱ","TRADE_DATE":"2026-10-30 00:00:00","CLOSE_PRICE":11.64,"TOTAL_MARKET_CAP":2e11,"TOTAL_SHARES":2e10,"PE_TTM":5,"PB_MRQ":0.5,"PS_TTM":null},
		{"SECURITY_NAME_ABBR":"平安银行","BOARD_NAME":"银行Ⅱ","TRADE_DATE":"2026-10-29 00:00:00","CLOSE_PRICE":11,"TOTAL_MARKET_CAP":2e11,"TOTAL_SHARES":2e10,"PE_TTM":4,"PB_MRQ":0.4,"PS_TTM":1}]}}`,
		`{"success":true,"code":0,"result":{"data":[
		{"REPORTDATE":"2026-09-30 00:00:00","BASIC_EPS":1,"PARENT_NETPROFIT":10},
		{"REPORTDATE":"2025-12-31 00:00:00","BASIC_EPS":2,"BPS":20,"WEIGHTAVG_ROE":11,"TOTAL_OPERATE_INCOME":100,"PARENT_NETPROFIT":40}]}}`)
	r, err := Analyze(context.Background(), "sz000001", DefaultDCFParams())
	require.NoError(t, err)
	require.Equal(t, "SZ000001", r.Code)
	require.Equal(t, "平安银行", r.Name)
	require.Equal(t, "2026-10-30", r.TradeDate)
	require.Equal(t, []string{"2026-10-29", "2026-10-30"}, r.Dates, "升序")
	require.Equal(t, 5.0, *r.PE.Current)
	require.Nil(t, r.PS.Current, "上游缺失时是 nil，不用前一天补")
	require.Equal(t, 1.0, *r.PS.Series[0])
	require.Len(t, r.Annuals, 1)
	require.Equal(t, 40.0, *r.Annuals[0].Profit)
}

func TestAnalyzeNoData(t *testing.T) {
	defer gock.Off()
	empty := `{"success":false,"code":9201,"message":"返回数据为空","result":null}`
	mockValuation(empty, empty)
	_, err := Analyze(context.Background(), "SZ000001", DefaultDCFParams())
	require.ErrorIs(t, err, ErrNoData)
}

func TestAnalyzeUpstreamFailureHasNoPartialResult(t *testing.T) {
	defer gock.Off()
	mockValuation(`{"success":true,"code":0,"result":{"data":[{"TRADE_DATE":"2026-10-30 00:00:00","CLOSE_PRICE":10}]}}`, `{"success":false,"code":9501,"message":"报表配置不存在"}`)
	_, err := Analyze(context.Background(), "SZ000001", DefaultDCFParams())
	require.ErrorContains(t, err, "业绩报表")

}

func TestAnalyzeUnsupportedCode(t *testing.T) {
	defer gock.Off()
	for _, code := range []string{"HK00700", "$AAPL", "OF000001"} {
		_, err := Analyze(context.Background(), code, DefaultDCFParams())
		require.ErrorIs(t, err, ErrNoData, code)
	}
}
