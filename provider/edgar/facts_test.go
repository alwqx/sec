package edgar

import (
	"context"
	"encoding/json"
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
	_, err := parseFacts(1, []byte(`{"entityName":"TSMC","facts":{"ifrs-full":{"Revenue":{"units":{}}}}}`))
	require.ErrorIs(t, err, ErrIFRS)
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
	f := &Facts{concepts: map[string]map[string][]fact{
		"Revenues": {"USD": {
			{Start: "2025-01-01", End: "2025-12-31", Val: 200},
		}},
		"RevenueFromContractWithCustomerExcludingAssessedTax": {"USD": {
			{Start: "2025-01-01", End: "2025-09-30", Val: 120},
		}},
	}}
	_, _, ok := f.quarterValue(item, "2025-09-30", "", "2025-01-01")
	require.False(t, ok, "缺少上一季度时不能把 9 个月累计当成单季")
	_, _, ok = f.quarterValue(item, "2025-12-31", "2025-09-30", "2025-01-01")
	require.False(t, ok, "不同科目的累计数不能相减")

	// 第一候选科目缺少基期时，允许用另一个同时有当期和基期的科目。
	f.concepts["RevenueFromContractWithCustomerExcludingAssessedTax"]["USD"] = append(
		f.concepts["RevenueFromContractWithCustomerExcludingAssessedTax"]["USD"],
		fact{Start: "2025-01-01", End: "2025-12-31", Val: 170},
	)
	v, derived, ok := f.quarterValue(item, "2025-12-31", "2025-09-30", "2025-01-01")
	require.True(t, ok)
	require.True(t, derived)
	require.Equal(t, 50.0, v)
	_, _, ok = f.quarterValue(item, "2025-12-31", "2025-06-30", "2025-01-01")
	require.False(t, ok, "上一条截止日相隔两季时不能推算")
}

func TestQuarterEndsExcludeStubPeriods(t *testing.T) {
	f := &Facts{concepts: map[string]map[string][]fact{
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
			if tt.wantIFRS {
				require.ErrorIs(t, err, ErrIFRS)
				require.Nil(t, facts)
			} else {
				require.NoError(t, err)
				require.NotNil(t, facts)
			}
		})
	}
}

func TestLatestReportedFactIgnoresFutureAndInvalidPeriods(t *testing.T) {
	concepts := map[string]factConcept{"Revenues": {Units: map[string][]fact{"USD": {
		{End: "2025-12-31", Filed: "2026-02-01"},
		{End: "2028-12-31", Filed: "2026-02-01"},
		{End: "invalid", Filed: "2026-02-01"},
	}}}}
	require.Equal(t, "2025-12-31", latestReportedFact(concepts).End)
}

func TestAccountingStandardIgnoresGenericShareFacts(t *testing.T) {
	_, err := parseFacts(1, []byte(`{"facts":{
"us-gaap":{"CommonStockSharesOutstanding":{"units":{"shares":[{"end":"2026-03-31","val":100,"filed":"2026-06-01"}]}}},
"ifrs-full":{"Revenue":{"units":{"JPY":[{"start":"2025-01-01","end":"2025-12-31","val":200,"filed":"2026-03-01"}]}}}
}}`))
	require.ErrorIs(t, err, ErrIFRS, "较新的通用股数不能让 IFRS 报表被误判为 US GAAP")
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
	f := &Facts{concepts: map[string]map[string][]fact{"Revenues": {"USD": {
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
	f := &Facts{concepts: map[string]map[string][]fact{"NetCashProvidedByUsedInOperatingActivities": {"USD": {
		{Start: "2025-01-01", End: "2025-09-30", Val: 110, FP: "Q3", Form: "10-Q"},
	}}}}
	require.Empty(t, f.Build(CashFlow, true, 0).Periods, "无上一季度累计数时不能将 9 个月累计数当成单季")
}
