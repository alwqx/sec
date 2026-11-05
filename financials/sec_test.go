package financials

import (
	"testing"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
)

// 两个财年的一般企业：利润从营业收入逐步到归母净利润，每一步加总等于合计；现金从上期末到本期末，差额合为一项。
const secFacts = `{"entityName":"Test Inc.","facts":{"us-gaap":{
"Revenues":{"units":{"USD":[
 {"start":"2024-01-01","end":"2024-12-31","val":900,"form":"10-K","filed":"2025-02-01"},
 {"start":"2025-01-01","end":"2025-12-31","val":1000,"form":"10-K","filed":"2026-02-01"}]}},
"CostOfRevenue":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":400,"form":"10-K","filed":"2026-02-01"}]}},
"ResearchAndDevelopmentExpense":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":100,"form":"10-K","filed":"2026-02-01"}]}},
"SellingGeneralAndAdministrativeExpense":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":150,"form":"10-K","filed":"2026-02-01"}]}},
"OperatingIncomeLoss":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":300,"form":"10-K","filed":"2026-02-01"}]}},
"IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":320,"form":"10-K","filed":"2026-02-01"}]}},
"IncomeTaxExpenseBenefit":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":60,"form":"10-K","filed":"2026-02-01"}]}},
"NetIncomeLoss":{"units":{"USD":[
 {"start":"2024-01-01","end":"2024-12-31","val":200,"form":"10-K","filed":"2025-02-01"},
 {"start":"2025-01-01","end":"2025-12-31","val":250,"form":"10-K","filed":"2026-02-01"}]}},
"CashAndCashEquivalentsAtCarryingValue":{"units":{"USD":[
 {"end":"2024-12-31","val":100,"form":"10-K","filed":"2025-02-01"},
 {"end":"2025-12-31","val":150,"form":"10-K","filed":"2026-02-01"}]}},
"AssetsCurrent":{"units":{"USD":[{"end":"2025-12-31","val":400,"form":"10-K","filed":"2026-02-01"}]}},
"Assets":{"units":{"USD":[{"end":"2025-12-31","val":1000,"form":"10-K","filed":"2026-02-01"}]}},
"StockholdersEquity":{"units":{"USD":[{"end":"2025-12-31","val":600,"form":"10-K","filed":"2026-02-01"}]}},
"NetCashProvidedByUsedInOperatingActivities":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":300,"form":"10-K","filed":"2026-02-01"}]}},
"NetCashProvidedByUsedInInvestingActivities":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":-120,"form":"10-K","filed":"2026-02-01"}]}},
"NetCashProvidedByUsedInFinancingActivities":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":-125,"form":"10-K","filed":"2026-02-01"}]}}
}}}`

func TestFromSEC(t *testing.T) {
	facts, err := edgar.ParseFacts(1, []byte(secFacts))
	require.NoError(t, err)
	a, err := FromSEC("TEST", facts, false)
	require.NoError(t, err)
	require.Equal(t, "general", a.CompanyType)
	require.Equal(t, "sec", a.Source)
	require.Equal(t, "美元", a.CurrencyName)
	require.Len(t, a.Periods, 2)
	require.Equal(t, "2024-12-31", a.Periods[0].Date, "按日期升序")
	p := a.Periods[1]
	require.Equal(t, "2025", p.Label)

	require.Equal(t, &Item{"营业收入", 1000}, p.Income.Start)
	from := p.Income.Start.Value
	for _, s := range p.Income.Steps {
		require.InDelta(t, s.Total.Value, from+sum(s.Items), 1e-9, s.Total.Label)
		from = s.Total.Value
	}
	require.Equal(t, []Step{
		{[]Item{{"营业成本", -400}}, Item{"毛利", 600}},
		{[]Item{{"研发费用", -100}, {"销售及管理费用", -150}, {"其他营业费用", -50}}, Item{"营业利润", 300}},
		{[]Item{{"营业外收益", 20}}, Item{"税前利润", 320}},
		{[]Item{{"所得税", -60}, {"其他（少数股东损益、终止经营等）", -10}}, Item{"归母净利润", 250}},
	}, p.Income.Steps)

	require.Equal(t, &Item{"期初现金", 100}, p.Cash.Start)
	require.Equal(t, []Item{{"经营活动", 300}, {"投资活动", -120}, {"筹资活动", -125}, {"汇率变动及其他", -5}}, p.Cash.Steps[0].Items)
	require.Nil(t, a.Periods[0].Cash, "第一期没有上期末现金")

	require.Equal(t, []Item{{"现金及等价物", 150}, {"其他流动资产", 250}, {"非流动资产", 600}}, p.Assets)
	require.InDelta(t, 60.0, *p.Metrics.GrossMargin, 1e-9)
	require.InDelta(t, 15.0, *p.Metrics.SGARatio, 1e-9)
	require.InDelta(t, 10.0, *p.Metrics.ResearchRatio, 1e-9)
	require.InDelta(t, 40.0, *p.Metrics.DebtRatio, 1e-9, "没有负债合计时用总资产减股东权益")
	require.InDelta(t, 1.2, *p.Metrics.CashToProfit, 1e-9)
}

// 银行：有净利息收入、没有营业成本和营业利润，营业收入先拆成净利息收入和非利息收入，再扣信用损失准备和营业费用到税前利润。
func TestFromSECBank(t *testing.T) {
	facts, err := edgar.ParseFacts(1, []byte(`{"facts":{"us-gaap":{
"Revenues":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":1000,"form":"10-K","filed":"2026-02-01"}]}},
"InterestIncomeExpenseNet":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":600,"form":"10-K","filed":"2026-02-01"}]}},
"ProvisionForCreditLosses":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":80,"form":"10-K","filed":"2026-02-01"}]}},
"IncomeLossFromContinuingOperationsBeforeIncomeTaxesExtraordinaryItemsNoncontrollingInterest":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":400,"form":"10-K","filed":"2026-02-01"}]}},
"IncomeTaxExpenseBenefit":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":80,"form":"10-K","filed":"2026-02-01"}]}},
"NetIncomeLoss":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":320,"form":"10-K","filed":"2026-02-01"}]}}}}}`))
	require.NoError(t, err)
	a, err := FromSEC("BANK", facts, false)
	require.NoError(t, err)
	require.Equal(t, "bank", a.CompanyType)
	income := a.Periods[0].Income
	require.Nil(t, income.Start)
	require.Equal(t, []Step{
		{[]Item{{"净利息收入", 600}, {"非利息收入", 400}}, Item{"营业收入", 1000}},
		{[]Item{{"信用损失准备", -80}, {"营业费用及其他", -520}}, Item{"税前利润", 400}},
		{[]Item{{"所得税", -80}}, Item{"归母净利润", 320}},
	}, income.Steps)
}

func TestFromSECNoIncome(t *testing.T) {
	facts, err := edgar.ParseFacts(1, []byte(`{"facts":{"us-gaap":{"Assets":{"units":{"USD":[{"end":"2025-12-31","val":1,"form":"10-K","filed":"2026-02-01"}]}}}}}`))
	require.NoError(t, err)
	_, err = FromSEC("X", facts, false)
	require.ErrorIs(t, err, ErrNoSECData)
}

// 期初现金只用紧挨着的上一期：上一期没有资产负债表，或者中间缺一期利润表时，不画现金瀑布图。
func TestFromSECCashNeedsAdjacentPeriod(t *testing.T) {
	body := func(incomeYears, balanceYears []string) []byte {
		income, balance := "", ""
		for i, y := range incomeYears {
			if i > 0 {
				income += ","
			}
			income += `{"start":"` + y + `-01-01","end":"` + y + `-12-31","val":100,"form":"10-K","filed":"` + y + `-12-31"}`
		}
		for i, y := range balanceYears {
			if i > 0 {
				balance += ","
			}
			balance += `{"end":"` + y + `-12-31","val":10,"form":"10-K","filed":"` + y + `-12-31"}`
		}
		return []byte(`{"facts":{"us-gaap":{"Revenues":{"units":{"USD":[` + income + `]}},"NetIncomeLoss":{"units":{"USD":[` + income + `]}},
"CashAndCashEquivalentsAtCarryingValue":{"units":{"USD":[` + balance + `]}},
"NetCashProvidedByUsedInOperatingActivities":{"units":{"USD":[` + income + `]}}}}}`)
	}
	last := func(incomeYears, balanceYears []string) Period {
		facts, err := edgar.ParseFacts(1, body(incomeYears, balanceYears))
		require.NoError(t, err)
		a, err := FromSEC("X", facts, false)
		require.NoError(t, err)
		return a.Periods[len(a.Periods)-1]
	}
	require.NotNil(t, last([]string{"2024", "2025"}, []string{"2024", "2025"}).Cash)
	require.Nil(t, last([]string{"2023", "2024", "2025"}, []string{"2023", "2025"}).Cash, "上一期没有资产负债表")
	require.Nil(t, last([]string{"2023", "2025"}, []string{"2023", "2025"}).Cash, "中间缺一期")
}

// IFRS 公司没有季度数据，返回专门的错误，不说成没有财报。
func TestFromSECIFRSQuarterly(t *testing.T) {
	facts, err := edgar.ParseFacts(1, []byte(`{"facts":{"ifrs-full":{"Revenue":{"units":{"TWD":[{"start":"2024-01-01","end":"2024-12-31","val":1,"form":"20-F","filed":"2025-04-17"}]}}}}}`))
	require.NoError(t, err)
	_, err = FromSEC("TSM", facts, true)
	require.ErrorIs(t, err, ErrIFRSQuarterly)
}
