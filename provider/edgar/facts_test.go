package edgar

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// testFacts：财年截至 9 月底。收入 2024 财年用 Revenues、2025 财年改用 RevenueFromContract…；
// 2025 财年有 Q1、Q2 单季和 9 个月累计，Q4 要用全年减 9 个月；现金流只有累计数；2024 财年净利润后来重述过。
const testFacts = `{"entityName":"Test Inc.","facts":{"us-gaap":{
"Revenues":{"units":{"USD":[{"start":"2023-10-01","end":"2024-09-28","val":100,"fp":"FY","form":"10-K","filed":"2024-11-01"}]}},
"RevenueFromContractWithCustomerExcludingAssessedTax":{"units":{"USD":[
 {"start":"2024-09-29","end":"2025-09-27","val":200,"fp":"FY","form":"10-K","filed":"2025-11-01"},
 {"start":"2024-09-29","end":"2024-12-28","val":40,"fp":"Q1","form":"10-Q","filed":"2025-02-01"},
 {"start":"2024-12-29","end":"2025-03-29","val":50,"fp":"Q2","form":"10-Q","filed":"2025-05-01"},
 {"start":"2024-09-29","end":"2025-06-28","val":150,"fp":"Q3","form":"10-Q","filed":"2025-08-01"}]}},
"NetIncomeLoss":{"units":{"USD":[
 {"start":"2023-10-01","end":"2024-09-28","val":10,"fp":"FY","form":"10-K","filed":"2024-11-01"},
 {"start":"2023-10-01","end":"2024-09-28","val":12,"fp":"FY","form":"10-K","filed":"2025-11-01"}]}},
"EarningsPerShareDiluted":{"units":{"USD/shares":[
 {"start":"2024-09-29","end":"2025-09-27","val":2.5,"fp":"FY","form":"10-K","filed":"2025-11-01"},
 {"start":"2024-09-29","end":"2024-12-28","val":0.5,"fp":"Q1","form":"10-Q","filed":"2025-02-01"}]}},
"NetCashProvidedByUsedInOperatingActivities":{"units":{"USD":[
 {"start":"2024-09-29","end":"2024-12-28","val":30,"fp":"Q1","form":"10-Q","filed":"2025-02-01"},
 {"start":"2024-09-29","end":"2025-03-29","val":70,"fp":"Q2","form":"10-Q","filed":"2025-05-01"},
 {"start":"2024-09-29","end":"2025-09-27","val":160,"fp":"FY","form":"10-K","filed":"2025-11-01"},
 {"start":"2024-09-29","end":"2025-06-28","val":110,"fp":"Q3","form":"10-Q","filed":"2025-08-01"}]}},
"PaymentsToAcquirePropertyPlantAndEquipment":{"units":{"USD":[{"start":"2024-09-29","end":"2025-09-27","val":20,"fp":"FY","form":"10-K","filed":"2025-11-01"}]}},
"Assets":{"units":{"USD":[{"end":"2025-09-27","val":1000,"fp":"FY","form":"10-K","filed":"2025-11-01"},{"end":"2025-03-29","val":900,"fp":"Q2","form":"10-Q","filed":"2025-05-01"}]}}
}}}`

func value(t *testing.T, p Period, key string) float64 {
	t.Helper()
	require.NotNil(t, p.Values[key], key)
	return *p.Values[key]
}

func TestBuildAnnual(t *testing.T) {
	f, err := parseFacts(1, []byte(testFacts))
	require.NoError(t, err)
	income := f.Build(Income, false, 0)
	require.Len(t, income.Periods, 2)
	require.Equal(t, "2025-09-27", income.Periods[0].End)
	require.Equal(t, 2025, income.Periods[0].FiscalYear)
	require.Equal(t, 200.0, value(t, income.Periods[0], "revenue"), "2025 财年用 RevenueFromContract…")
	require.Equal(t, 100.0, value(t, income.Periods[1], "revenue"), "2024 财年用 Revenues")
	require.Equal(t, 12.0, value(t, income.Periods[1], "net_income"), "重述后用最新申报的数值")
	require.Equal(t, "2024-11-01", income.Periods[1].Filed["net_income"], "重述后的数值仍按首次披露日期可用")
	require.Equal(t, "2025-11-01", income.Periods[0].Filed["revenue"])

	cash := f.Build(CashFlow, false, 0)
	require.Equal(t, 140.0, value(t, cash.Periods[0], "free_cash_flow"))
	require.Contains(t, cash.Periods[0].Derived, "free_cash_flow")

	balance := f.Build(Balance, false, 1)
	require.Len(t, balance.Periods, 1)
	require.Equal(t, 1000.0, value(t, balance.Periods[0], "total_assets"))
}

func TestBuildQuarterly(t *testing.T) {
	f, err := parseFacts(1, []byte(testFacts))
	require.NoError(t, err)
	income := f.Build(Income, true, 0)
	byEnd := map[string]Period{}
	for _, p := range income.Periods {
		byEnd[p.End] = p
	}
	q1, q3, q4 := byEnd["2024-12-28"], byEnd["2025-06-28"], byEnd["2025-09-27"]
	require.Equal(t, "Q1", q1.FiscalPeriod)
	require.Equal(t, 2025, q1.FiscalYear)
	require.Equal(t, 40.0, value(t, q1, "revenue"))
	require.Nil(t, q3.Values["revenue"], "没有单季数，也没有 6 个月累计数可以相减，留空，不用单季数凑")
	require.Equal(t, "Q4", q4.FiscalPeriod)
	require.Equal(t, 50.0, value(t, q4, "revenue"), "Q4 = 全年 200 - 9 个月 150")
	require.Contains(t, q4.Derived, "revenue")
	require.Nil(t, q4.Values["eps_diluted"], "每股收益不能相减")
	require.Equal(t, 0.5, value(t, q1, "eps_diluted"))

	cash := f.Build(CashFlow, true, 0)
	byEnd = map[string]Period{}
	for _, p := range cash.Periods {
		byEnd[p.End] = p
	}
	require.Equal(t, 30.0, value(t, byEnd["2024-12-28"], "operating_cash_flow"), "Q1 的累计数就是单季")
	require.Equal(t, 40.0, value(t, byEnd["2025-03-29"], "operating_cash_flow"), "Q2 = 6 个月 70 - 3 个月 30")
	require.Equal(t, 40.0, value(t, byEnd["2025-06-28"], "operating_cash_flow"), "Q3 = 9 个月 110 - 6 个月 70")
	require.Equal(t, 50.0, value(t, byEnd["2025-09-27"], "operating_cash_flow"), "Q4 = 全年 160 - 9 个月 110")
}

func TestParseFactsErrors(t *testing.T) {
	f, err := parseFacts(1, []byte(`{"entityName":"TSMC","facts":{"ifrs-full":{"Revenue":{"units":{}}}}}`))
	require.NoError(t, err)
	require.Equal(t, IFRS, f.Standard, "只有 IFRS 数据")
	_, err = parseFacts(1, []byte(`{"facts":{}}`))
	require.ErrorContains(t, err, "没有这家公司的 XBRL")
	_, err = parseFacts(1, []byte(`{`))
	require.ErrorContains(t, err, "格式无效")
}

func TestCompanyFacts(t *testing.T) {
	defer gock.Off()
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000000001.json").Reply(200).BodyString(testFacts)
	f, err := client(t).CompanyFacts(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "Test Inc.", f.Name)
}

func TestBuildForeignAnnualForms(t *testing.T) {
	for _, form := range []string{"20-F", "20-F/A", "40-F", "40-F/A", "10-KT", "10-KT/A"} {
		t.Run(form, func(t *testing.T) {
			f, err := parseFacts(1, []byte(strings.ReplaceAll(testFacts, "10-K", form)))
			require.NoError(t, err)
			for _, statement := range Statements {
				report := f.Build(statement, false, 1)
				require.Len(t, report.Periods, 1)
				require.Equal(t, 2025, report.Periods[0].FiscalYear)
			}
		})
	}
}

func TestQuarterValueMissingOrDifferentConcepts(t *testing.T) {
	item := Lines[Income][0]
	f := &Facts{Currency: "USD", concepts: map[string]map[string][]fact{
		"Revenues": {"USD": {
			{Start: "2025-01-01", End: "2025-12-31", Val: 200},
		}},
		"RevenueFromContractWithCustomerExcludingAssessedTax": {"USD": {
			{Start: "2025-01-01", End: "2025-09-30", Val: 120},
		}},
	}}
	_, _, ok, _ := f.quarterValue(item, "2025-09-30", "", "2025-01-01")
	require.False(t, ok, "缺少上一季度时不能把 9 个月累计当成单季")
	_, _, ok, _ = f.quarterValue(item, "2025-12-31", "2025-09-30", "2025-01-01")
	require.False(t, ok, "不同科目的累计数不能相减")

	// 第一候选科目缺少基期时，允许用另一个同时有当期和基期的科目。
	f.concepts["RevenueFromContractWithCustomerExcludingAssessedTax"]["USD"] = append(
		f.concepts["RevenueFromContractWithCustomerExcludingAssessedTax"]["USD"],
		fact{Start: "2025-01-01", End: "2025-12-31", Val: 170},
	)
	v, derived, ok, _ := f.quarterValue(item, "2025-12-31", "2025-09-30", "2025-01-01")
	require.True(t, ok)
	require.True(t, derived)
	require.Equal(t, 50.0, v)
	_, _, ok, _ = f.quarterValue(item, "2025-12-31", "2025-06-30", "2025-01-01")
	require.False(t, ok, "上一条截止日相隔两季时不能推算")
}

func TestQuarterEndsExcludeStubPeriods(t *testing.T) {
	f := &Facts{Currency: "USD", concepts: map[string]map[string][]fact{
		"Revenues": {"USD": {
			{Start: "2025-01-01", End: "2025-12-31", Val: 200, Form: "10-K"},
			{Start: "2025-01-01", End: "2025-03-31", Val: 40},
			{Start: "2025-01-01", End: "2025-05-01", Val: 55},
			{Start: "2025-02-01", End: "2025-06-30", Val: 60},
			{Start: "2025-01-01", End: "2025-06-30", Val: 90},
			{Start: "2025-04-02", End: "2025-06-30", Val: 50},
			{Start: "2025-05-02", End: "2025-07-31", Val: 50},
			{Start: "2025-01-01", End: "2025-09-30", Val: 140},
		}},
	}}
	require.Equal(t, []string{"2025-03-31", "2025-06-30", "2025-09-30", "2025-12-31"}, f.quarterEnds(f.fiscalYearEnds()))
	report := f.Build(Income, true, 0)
	require.Len(t, report.Periods, 4)
	require.Equal(t, 50.0, value(t, report.Periods[1], "revenue"))
}

func TestParseFactsAccountingStandardChange(t *testing.T) {
	for _, tt := range []struct {
		name, gaapEnd, ifrsEnd, gaapFiled, ifrsFiled string
		wantIFRS                                     bool
	}{
		{"转用 IFRS，旧 US GAAP 后来重新提交", "2021-03-31", "2025-03-31", "2026-06-01", "2025-06-01", true},
		{"转用 US GAAP", "2025-03-31", "2021-03-31", "2025-06-01", "2026-06-01", false},
		{"同一期后来改用 IFRS", "2025-03-31", "2025-03-31", "2025-06-01", "2026-06-01", true},
		{"同一期后来改用 US GAAP", "2025-03-31", "2025-03-31", "2026-06-01", "2025-06-01", false},
		{"最新申报也包含 IFRS", "2025-03-31", "2025-03-31", "2025-06-01", "2025-06-01", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"facts": map[string]any{
				"us-gaap": map[string]factConcept{"NetIncomeLoss": {Units: map[string][]fact{"USD": {
					{End: tt.gaapEnd, Val: 10, Form: "20-F", Filed: tt.gaapFiled},
				}}}},
				"ifrs-full": map[string]factConcept{"ProfitLoss": {Units: map[string][]fact{"JPY": {
					{End: tt.ifrsEnd, Val: 20, Form: "20-F", Filed: tt.ifrsFiled},
				}}}},
			}})
			require.NoError(t, err)
			facts, err := parseFacts(1, body)
			require.NoError(t, err)
			if tt.wantIFRS {
				require.Equal(t, IFRS, facts.Standard, "不静默回退到历史 US GAAP 数据")
				require.Equal(t, "JPY", facts.Currency)
			} else {
				require.Equal(t, USGAAP, facts.Standard)
			}
		})
	}
}

func TestLatestAnnualFilingIgnoresFutureAndInvalidPeriods(t *testing.T) {
	concepts := map[string]factConcept{"Revenues": {Units: map[string][]fact{"USD": {
		{End: "2025-12-31", Form: "10-K", Filed: "2026-02-01"},
		{End: "2028-12-31", Form: "10-K", Filed: "2026-02-01"},
		{End: "invalid", Form: "10-K", Filed: "2026-02-01"},
	}}}}
	filing := latestAnnualFiling(concepts, USGAAP)
	require.Equal(t, "2025-12-31", filing.end)
	require.Equal(t, 1, filing.total())
}

func TestAccountingStandardIgnoresGenericShareFacts(t *testing.T) {
	f, err := parseFacts(1, []byte(`{"facts":{
"us-gaap":{"CommonStockSharesOutstanding":{"units":{"shares":[{"end":"2026-03-31","val":100,"form":"10-K","filed":"2026-06-01"}]}}},
"ifrs-full":{"Revenue":{"units":{"JPY":[{"start":"2025-01-01","end":"2025-12-31","val":200,"form":"20-F","filed":"2026-03-01"}]}}}
}}`))
	require.NoError(t, err)
	require.Equal(t, IFRS, f.Standard, "较新的通用股数不能让 IFRS 报表被误判为 US GAAP")
}

// US GAAP 公司在年报里零星标注了 ifrs-full 数值（哪怕截止日更晚），不能让报表整体切到 IFRS 而变空。
func TestAccountingStandardIgnoresStrayIFRSFacts(t *testing.T) {
	f, err := parseFacts(1, []byte(`{"facts":{
"us-gaap":{
 "Revenues":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":200,"form":"10-K","filed":"2026-02-01","accn":"a1"}]}},
 "Assets":{"units":{"USD":[{"end":"2025-12-31","val":900,"form":"10-K","filed":"2026-02-01","accn":"a1"}]}}},
"ifrs-full":{"Revenue":{"units":{"USD":[{"start":"2025-02-01","end":"2026-01-31","val":5,"form":"10-K","filed":"2026-02-01","accn":"a1"}]}}}
}}`))
	require.NoError(t, err)
	require.Equal(t, USGAAP, f.Standard)
	require.Equal(t, 200.0, value(t, f.Build(Income, false, 0).Periods[0], "revenue"))
}

// 首份年报之前的 10-Q：同时包含当前单季、累计值和前一年比较数，fy/fp 仍属于所在申报。
const quarterlyOnlyFacts = `{"entityName":"New IPO","facts":{"us-gaap":{
"Revenues":{"units":{"USD":[
 {"start":"2025-10-01","end":"2025-12-31","val":40,"fy":2026,"fp":"Q1","form":"10-Q","filed":"2026-02-01"},
 {"start":"2026-01-01","end":"2026-03-31","val":50,"fy":2026,"fp":"Q2","form":"10-Q","filed":"2026-05-01"},
 {"start":"2025-10-01","end":"2026-03-31","val":90,"fy":2026,"fp":"Q2","form":"10-Q","filed":"2026-05-01"},
 {"start":"2026-04-01","end":"2026-06-30","val":60,"fy":2026,"fp":"Q3","form":"10-Q","filed":"2026-08-01"},
 {"start":"2025-10-01","end":"2026-06-30","val":150,"fy":2026,"fp":"Q3","form":"10-Q","filed":"2026-08-01"},
 {"start":"2025-04-01","end":"2025-06-30","val":45,"fy":2026,"fp":"Q3","form":"10-Q","filed":"2026-08-01"},
 {"start":"2024-10-01","end":"2025-06-30","val":120,"fy":2026,"fp":"Q3","form":"10-Q","filed":"2026-08-01"}]}},
"NetCashProvidedByUsedInOperatingActivities":{"units":{"USD":[
 {"start":"2025-10-01","end":"2025-12-31","val":30,"fy":2026,"fp":"Q1","form":"10-Q","filed":"2026-02-01"},
 {"start":"2025-10-01","end":"2026-03-31","val":70,"fy":2026,"fp":"Q2","form":"10-Q","filed":"2026-05-01"},
 {"start":"2025-10-01","end":"2026-06-30","val":110,"fy":2026,"fp":"Q3","form":"10-Q","filed":"2026-08-01"}]}},
"Assets":{"units":{"USD":[
 {"end":"2025-12-31","val":800,"fy":2026,"fp":"Q1","form":"10-Q","filed":"2026-02-01"},
 {"end":"2026-03-31","val":900,"fy":2026,"fp":"Q2","form":"10-Q","filed":"2026-05-01"},
 {"end":"2026-06-30","val":1000,"fy":2026,"fp":"Q3","form":"10-Q","filed":"2026-08-01"}]}}
}}}`

func TestBuildQuarterlyBeforeFirstAnnualReport(t *testing.T) {
	f, err := parseFacts(1, []byte(quarterlyOnlyFacts))
	require.NoError(t, err)
	require.Empty(t, f.fiscalYearEnds())
	for _, statement := range Statements {
		require.Empty(t, f.Build(statement, false, 0).Periods, "不虚构年报")
	}
	income := f.Build(Income, true, 0)
	require.Len(t, income.Periods, 4)
	require.Equal(t, 60.0, value(t, income.Periods[0], "revenue"))
	require.Equal(t, "Q3", income.Periods[0].FiscalPeriod)
	require.Equal(t, 2026, income.Periods[0].FiscalYear)
	require.Equal(t, 2025, income.Periods[3].FiscalYear, "比较数不能套用本次申报的 fy")
	require.Equal(t, "Q3", income.Periods[3].FiscalPeriod)

	cash := f.Build(CashFlow, true, 0)
	require.Len(t, cash.Periods, 3)
	require.Equal(t, 40.0, value(t, cash.Periods[0], "operating_cash_flow"))
	require.Contains(t, cash.Periods[0].Derived, "operating_cash_flow")
	require.Equal(t, 40.0, value(t, cash.Periods[1], "operating_cash_flow"))
	require.Equal(t, 30.0, value(t, cash.Periods[2], "operating_cash_flow"))

	balance := f.Build(Balance, true, 0)
	require.Len(t, balance.Periods, 3)
	require.Equal(t, 1000.0, value(t, balance.Periods[0], "total_assets"))
	for _, p := range balance.Periods {
		require.Equal(t, 2026, p.FiscalYear)
	}
}

func TestBuildQuarterlyWithOnlySingleQuarterFacts(t *testing.T) {
	f := &Facts{Currency: "USD", concepts: map[string]map[string][]fact{"Revenues": {"USD": {
		{Start: "2025-07-01", End: "2025-09-30", Val: 50, FP: "Q2", Form: "10-Q"},
	}}}}
	report := f.Build(Income, true, 0)
	require.Len(t, report.Periods, 1)
	require.Equal(t, 50.0, value(t, report.Periods[0], "revenue"))
	require.Equal(t, 2026, report.Periods[0].FiscalYear)
	require.Equal(t, "Q2", report.Periods[0].FiscalPeriod)
	require.Empty(t, report.Periods[0].Derived)

	// 相同截止日可能还有非年初累计的半年数；它不能遮住最新单季的季度信息。
	f.concepts["Revenues"]["USD"] = []fact{
		{Start: "2025-07-01", End: "2025-12-31", Val: 100, FP: "Q3", Form: "10-Q"},
		{Start: "2025-10-01", End: "2025-12-31", Val: 60, FP: "Q3", Form: "10-Q"},
	}
	report = f.Build(Income, true, 0)
	require.Len(t, report.Periods, 1)
	require.Equal(t, 60.0, value(t, report.Periods[0], "revenue"))
	require.Equal(t, 2026, report.Periods[0].FiscalYear)
	require.Equal(t, "Q3", report.Periods[0].FiscalPeriod)
}

func TestQuarterlyOnlyMissingPreviousQuarter(t *testing.T) {
	f := &Facts{Currency: "USD", concepts: map[string]map[string][]fact{"NetCashProvidedByUsedInOperatingActivities": {"USD": {
		{Start: "2025-01-01", End: "2025-09-30", Val: 110, FP: "Q3", Form: "10-Q"},
	}}}}
	require.Empty(t, f.Build(CashFlow, true, 0).Periods, "无上一季度累计数时不能将 9 个月累计数当成单季")
}

// 封面股数按申报合计各类普通股，同一截止日的多次申报分别保留。
func TestSharesOutstanding(t *testing.T) {
	f, err := parseFacts(1, []byte(`{"entityName":"Test Inc.","facts":{
"us-gaap":{"Revenues":{"units":{"USD":[{"start":"2024-01-01","end":"2024-12-31","val":100,"fp":"FY","form":"10-K","filed":"2025-02-01"}]}}},
"dei":{"EntityCommonStockSharesOutstanding":{"units":{"shares":[
 {"end":"2025-04-20","val":60,"fp":"Q1","form":"10-Q","filed":"2025-04-30","accn":"b"},
 {"end":"2025-04-20","val":40,"fp":"Q1","form":"10-Q","filed":"2025-04-30","accn":"b"},
 {"end":"2025-01-20","val":90,"fp":"FY","form":"10-K","filed":"2025-02-01","accn":"a"},
 {"end":"2025-01-20","val":95,"fp":"FY","form":"10-K/A","filed":"2025-03-01","accn":"c"},
 {"end":"2025-07-20","val":0,"fp":"Q2","form":"10-Q","filed":"2025-07-30","accn":"d"}]}}}
}}`))
	require.NoError(t, err)
	require.Equal(t, []SharePoint{
		{Date: "2025-01-20", Filed: "2025-02-01", Shares: 90, Classes: 1},
		{Date: "2025-01-20", Filed: "2025-03-01", Shares: 95, Classes: 1},
		{Date: "2025-04-20", Filed: "2025-04-30", Shares: 100, Classes: 2},
	}, f.SharesOutstanding())
}

// 股息优先取普通股股息，PaymentsOfDividends 可能包含优先股股息和少数股东分红。
func TestDividendsPreferCommonStock(t *testing.T) {
	f, err := parseFacts(1, []byte(`{"entityName":"Bank","facts":{"us-gaap":{
"PaymentsOfDividends":{"units":{"USD":[{"start":"2024-01-01","end":"2024-12-31","val":120,"fp":"FY","form":"10-K","filed":"2025-02-01"}]}},
"PaymentsOfDividendsCommonStock":{"units":{"USD":[{"start":"2024-01-01","end":"2024-12-31","val":100,"fp":"FY","form":"10-K","filed":"2025-02-01"}]}}
}}}`))
	require.NoError(t, err)
	cash := f.Build(CashFlow, false, 0)
	require.Equal(t, 100.0, value(t, cash.Periods[0], "dividends"))
	require.Equal(t, "2025-02-01", cash.Periods[0].Filed["dividends"])
}

// 以人民币列报的公司（比如阿里巴巴）年报附带最近一年的美元便利折算数：报表用人民币，不能只读到零散的美元数。
const cnyFacts = `{"entityName":"Alibaba","facts":{"us-gaap":{
"Revenues":{"units":{
 "CNY":[{"start":"2024-04-01","end":"2025-03-31","val":996,"fp":"FY","form":"20-F","filed":"2025-05-15"},
        {"start":"2023-04-01","end":"2024-03-31","val":941,"fp":"FY","form":"20-F","filed":"2024-05-15"}],
 "USD":[{"start":"2024-04-01","end":"2025-03-31","val":137,"fp":"FY","form":"20-F","filed":"2025-05-15"}]}},
"NetIncomeLoss":{"units":{"CNY":[{"start":"2024-04-01","end":"2025-03-31","val":130,"fp":"FY","form":"20-F","filed":"2025-05-15"}]}},
"EarningsPerShareDiluted":{"units":{"CNY/shares":[{"start":"2024-04-01","end":"2025-03-31","val":5.5,"fp":"FY","form":"20-F","filed":"2025-05-15"}]}},
"Assets":{"units":{"CNY":[{"end":"2025-03-31","val":1800,"fp":"FY","form":"20-F","filed":"2025-05-15"}],
 "USD":[{"end":"2030-12-31","val":1,"fp":"FY","form":"20-F","filed":"2025-05-15"}]}}
}}}`

func TestReportingCurrency(t *testing.T) {
	f, err := parseFacts(1, []byte(cnyFacts))
	require.NoError(t, err)
	require.Equal(t, "CNY", f.Currency, "未来承诺金额的美元数不影响识别")
	income := f.Build(Income, false, 0)
	require.Equal(t, "CNY", income.Currency)
	require.Equal(t, "人民币", income.CurrencyName)
	require.Len(t, income.Periods, 2, "两个财年都有人民币数")
	require.Equal(t, 996.0, value(t, income.Periods[0], "revenue"))
	require.Equal(t, 941.0, value(t, income.Periods[1], "revenue"))
	require.Equal(t, 5.5, value(t, income.Periods[0], "eps_diluted"))
	for _, line := range income.Lines {
		if line.Key == "eps_diluted" {
			require.Equal(t, "CNY/shares", line.Unit)
		}
	}

	usd := f.In("USD").Build(Income, false, 0)
	require.Equal(t, "USD", usd.Currency)
	require.Len(t, usd.Periods, 1, "美元只有最近一年的便利折算数")
	require.Equal(t, 137.0, value(t, usd.Periods[0], "revenue"))
	require.Equal(t, "CNY", f.Currency, "In 返回副本，不改原数据")

	f, err = parseFacts(1, []byte(testFacts))
	require.NoError(t, err)
	require.Equal(t, "USD", f.Currency)
	require.Equal(t, "美元", f.Build(Income, false, 0).CurrencyName)
	require.Equal(t, "XYZ", CurrencyName("XYZ"))
}

// 币种识别的几种误判场景。
func TestReportingCurrencyMisdetection(t *testing.T) {
	year := func(unit, start, end, filed, accn string, val float64) string {
		return `"` + unit + `":[{"start":"` + start + `","end":"` + end + `","val":` + strconv.FormatFloat(val, 'f', -1, 64) + `,"fp":"FY","form":"10-K","filed":"` + filed + `","accn":"` + accn + `"}]`
	}
	tests := []struct {
		name, body, want string
	}{
		{"期后事项的截止日晚于报告期，不能只凭它判断", `{
"Revenues":{"units":{` + year("USD", "2024-09-29", "2025-09-27", "2025-10-31", "a1", 100) + `}},
"NetIncomeLoss":{"units":{` + year("USD", "2024-09-29", "2025-09-27", "2025-10-31", "a1", 10) + `}},
"LongTermDebt":{"units":{"EUR":[{"end":"2025-10-15","val":500,"fp":"FY","form":"10-K","filed":"2025-10-31","accn":"a1"}]}}}`, "USD"},
		{"整套报表附带美元折算时，本币有前两年的比较数", `{
"Revenues":{"units":{"CNY":[
 {"start":"2024-04-01","end":"2025-03-31","val":996,"form":"20-F","filed":"2025-05-15","accn":"b1"},
 {"start":"2023-04-01","end":"2024-03-31","val":941,"form":"20-F","filed":"2025-05-15","accn":"b1"},
 {"start":"2022-04-01","end":"2023-03-31","val":869,"form":"20-F","filed":"2025-05-15","accn":"b1"}],
 "USD":[{"start":"2024-04-01","end":"2025-03-31","val":137,"form":"20-F","filed":"2025-05-15","accn":"b1"}]}},
"NetIncomeLoss":{"units":{"CNY":[
 {"start":"2024-04-01","end":"2025-03-31","val":130,"form":"20-F","filed":"2025-05-15","accn":"b1"},
 {"start":"2023-04-01","end":"2024-03-31","val":80,"form":"20-F","filed":"2025-05-15","accn":"b1"}],
 "USD":[{"start":"2024-04-01","end":"2025-03-31","val":18,"form":"20-F","filed":"2025-05-15","accn":"b1"}]}},
"Cash":{"units":{"USD":[{"end":"2025-03-31","val":5,"form":"20-F","filed":"2025-05-15","accn":"b1"},{"end":"2024-03-31","val":4,"form":"20-F","filed":"2025-05-15","accn":"b1"}]}}}`, "CNY"},
		{"附注里的外币数值不计入", `{
"Revenues":{"units":{` + year("USD", "2024-09-29", "2025-09-27", "2025-10-31", "c1", 100) + `}},
"DebtInstrumentFaceAmount":{"units":{"JPY":[
 {"end":"2025-09-27","val":1,"form":"10-K","filed":"2025-10-31","accn":"c1"},{"end":"2025-09-27","val":2,"form":"10-K","filed":"2025-10-31","accn":"c1"},
 {"end":"2025-09-27","val":3,"form":"10-K","filed":"2025-10-31","accn":"c1"}]}}}`, "USD"},
		{"选出的币种没有全年数据时，退回到全年数据最多的币种", `{
"Revenues":{"units":{` + year("USD", "2023-10-01", "2024-09-28", "2024-11-01", "d1", 100) + `}},
"Assets":{"units":{"EUR":[{"end":"2025-09-27","val":9,"form":"10-K","filed":"2025-11-01","accn":"d2"}]}}}`, "USD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parseFacts(1, []byte(`{"facts":{"us-gaap":`+tt.body+`}}`))
			require.NoError(t, err)
			require.Equal(t, tt.want, f.Currency)
			require.NotEmpty(t, f.Build(Income, false, 0).Periods, "识别出的币种有利润表数据")
		})
	}
}

// IFRS 公司（比如台积电）：按 ifrs-full 科目和列报币种整理年报，附带的美元便利折算不当作列报币种；
// 没有对应 IFRS 科目的行不显示；只有 20-F 年报，不整理季度报表。
const ifrsFacts = `{"entityName":"TSMC","facts":{"ifrs-full":{
"Revenue":{"units":{
 "TWD":[{"start":"2024-01-01","end":"2024-12-31","val":2894,"form":"20-F","filed":"2025-04-17","accn":"t1"},
        {"start":"2023-01-01","end":"2023-12-31","val":2161,"form":"20-F","filed":"2025-04-17","accn":"t1"},
        {"start":"2023-01-01","end":"2023-12-31","val":2161,"form":"20-F","filed":"2024-04-18","accn":"t0"}],
 "USD":[{"start":"2024-01-01","end":"2024-12-31","val":88,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}},
"ProfitLossAttributableToOwnersOfParent":{"units":{"TWD":[{"start":"2024-01-01","end":"2024-12-31","val":1173,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}},
"ProfitLoss":{"units":{"TWD":[{"start":"2024-01-01","end":"2024-12-31","val":1172,"form":"20-F","filed":"2025-04-17","accn":"t1"},
 {"start":"2023-01-01","end":"2023-12-31","val":838,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}},
"DilutedEarningsLossPerShare":{"units":{"TWD/shares":[{"start":"2024-01-01","end":"2024-12-31","val":45.25,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}},
"Assets":{"units":{"TWD":[{"end":"2024-12-31","val":6691,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}},
"CashFlowsFromUsedInOperatingActivities":{"units":{"TWD":[{"start":"2024-01-01","end":"2024-12-31","val":1826,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}},
"PurchaseOfPropertyPlantAndEquipmentClassifiedAsInvestingActivities":{"units":{"TWD":[{"start":"2024-01-01","end":"2024-12-31","val":956,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}},
"ProfitLossBeforeTax":{"units":{"TWD":[{"start":"2024-07-01","end":"2024-12-31","val":700,"form":"6-K","filed":"2025-01-16","accn":"t2"}]}}
},"us-gaap":{"Revenues":{"units":{"USD":[{"start":"2018-01-01","end":"2018-12-31","val":1,"form":"20-F","filed":"2019-04-01","accn":"g1"}]}}}}}`

func TestBuildIFRS(t *testing.T) {
	f, err := parseFacts(1, []byte(ifrsFacts))
	require.NoError(t, err)
	require.Equal(t, IFRS, f.Standard)
	require.Equal(t, "TWD", f.Currency)

	income := f.Build(Income, false, 0)
	require.Equal(t, IFRS, income.Standard)
	require.Equal(t, "新台币", income.CurrencyName)
	require.Len(t, income.Periods, 2)
	require.Equal(t, 2894.0, value(t, income.Periods[0], "revenue"))
	require.Equal(t, 1173.0, value(t, income.Periods[0], "net_income"), "归母净利润优先")
	require.NotContains(t, income.Periods[1].Values, "net_income", "申报过归母净利润的公司缺个别年份时不用净利润补")
	require.Equal(t, 45.25, value(t, income.Periods[0], "eps_diluted"))
	keys := map[string]string{}
	for _, line := range income.Lines {
		keys[line.Key] = line.Unit
	}
	require.NotContains(t, keys, "credit_loss", "没有对应 IFRS 科目的行不显示")
	require.Equal(t, "TWD/shares", keys["eps_diluted"])

	cash := f.Build(CashFlow, false, 0)
	require.Equal(t, 870.0, value(t, cash.Periods[0], "free_cash_flow"), "推算行保留")
	require.Equal(t, 6691.0, value(t, f.Build(Balance, false, 0).Periods[0], "total_assets"))

	quarterly := f.Build(Income, true, 0)
	require.True(t, quarterly.Quarterly)
	require.Empty(t, quarterly.Periods, "IFRS 不整理季度报表")
}

// 完全不申报归母净利润的 IFRS 公司（比如没有少数股东的诺和诺德）用净利润。
func TestBuildIFRSNetIncomeFallback(t *testing.T) {
	f, err := parseFacts(1, []byte(`{"facts":{"ifrs-full":{"ProfitLoss":{"units":{"DKK":[
{"start":"2024-01-01","end":"2024-12-31","val":101,"form":"20-F","filed":"2025-02-05","accn":"n1"}]}}}}}`))
	require.NoError(t, err)
	require.Equal(t, "DKK", f.Currency)
	require.Equal(t, 101.0, value(t, f.Build(Income, false, 0).Periods[0], "net_income"))
}
