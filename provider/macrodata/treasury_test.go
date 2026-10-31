package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fiscaldata"
	"github.com/stretchr/testify/require"
)

var treasuryFetched = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func debtRow(transaction, market, security, description string, today float64) fiscaldata.DebtRow {
	return fiscaldata.DebtRow{Date: "2026-09-30", Transaction: transaction, Market: market, Security: security, Description: description, Today: value(today), MTD: value(today * 2), FYTD: value(today * 3), Raw: map[string]string{"transaction_today_amt": "t", "transaction_mtd_amt": "m", "transaction_fytd_amt": "f"}, Source: "url", FetchedAt: treasuryFetched}
}

func debtSample() *fiscaldata.DebtResult {
	return &fiscaldata.DebtResult{Indicator: "treasury_debt_transactions", Frequency: "daily", DateBasis: "DTS record date", Unit: "百万美元", Begin: "20260930", End: "20260930", Source: "url", FetchedAt: treasuryFetched,
		Rows:      []fiscaldata.DebtRow{debtRow("Issues", "Marketable", "Bills", "Regular Series", -10), debtRow("Redemptions", "Marketable", "Bills", "null", 5)},
		Summaries: []fiscaldata.DebtSummary{{Date: "2026-09-30", Market: "Marketable", Issues: value(-10), Redemptions: value(5), Net: value(-15), IsDerived: true, InputRows: 2}}}
}

func cashRow(transaction, category, description string, today float64, prefix string) *fiscaldata.CashRow {
	return &fiscaldata.CashRow{CashAmounts: fiscaldata.CashAmounts{Today: value(today), MTD: value(today * 2), FYTD: value(today * 3)}, Date: "2026-09-30", Transaction: transaction, Category: category, Description: description, Source: "url", FetchedAt: treasuryFetched, Raw: map[string]string{prefix + "_today_amt": "t", prefix + "_mtd_amt": "m", prefix + "_fytd_amt": "f"}}
}

func cashSample() *fiscaldata.CashResult {
	return &fiscaldata.CashResult{Indicator: "treasury_debt_cash", Frequency: "daily", Basis: "DTS cash basis; record date", Unit: "百万美元", Begin: "20260930", End: "20260930", Sources: []string{"totals", "adjustments"}, FetchedAt: treasuryFetched, Observations: []fiscaldata.CashObservation{{
		Date: "2026-09-30", Issues: cashRow("Issues", "Public Debt Cash Issues (Table IIIB)", "", 100, "transaction"), Redemptions: cashRow("Redemptions", "Public Debt Cash Redemp. (Table IIIB)", "", 60, "transaction"), NetIsDerived: true,
		Net:         fiscaldata.CashAmounts{Today: value(40), MTD: value(80), FYTD: value(120)},
		Adjustments: []fiscaldata.CashRow{*cashRow("Issues", "Inflation-Protected Securities Increment (-)", "null", -6, "adj")},
	}}}
}

func auctionSample() *fiscaldata.AuctionsResult {
	event := func(cusip, auction, issue string) fiscaldata.AuctionObservation {
		yes, no := true, false
		return fiscaldata.AuctionObservation{CUSIP: cusip, SecurityType: "Bill", Term: "4-Week", AuctionDate: auction, IssueDate: issue, MaturityDate: "2026-10-29", Reopening: &yes, SOMAIncluded: &no, Amounts: fiscaldata.AuctionAmounts{Offering: value(100), TotalAccepted: value(108), SOMA: value(8), Competitive: value(0)}, PricePer100: value(99.7), Source: "url", FetchedAt: treasuryFetched, Raw: map[string]string{"offering_amt": "100", "fima_noncomp_accepted": "null", "price_per100": "99.7"}}
	}
	return &fiscaldata.AuctionsResult{Indicator: "treasury_auctions", Frequency: "event", Market: "marketable", DateBasis: "issue_date", AmountBasis: "par value", AmountUnit: "USD", Begin: "20260901", End: "20260930", Source: "url", FetchedAt: treasuryFetched, Observations: []fiscaldata.AuctionObservation{event("912797VE4", "2026-08-27", "2026-09-01"), event("912797VF1", "2026-08-28", "2026-09-01")}}
}

func seriesByID(d *Dataset) map[string]Series {
	series := map[string]Series{}
	for _, s := range d.Series {
		series[s.ID] = s
	}
	return series
}

func observationOf(t *testing.T, d *Dataset, seriesID string) Observation {
	t.Helper()
	for _, o := range d.Observations {
		if o.SeriesID == seriesID {
			return o
		}
	}
	require.FailNow(t, "未找到观测", seriesID)
	return Observation{}
}

func TestDebtKeepsOfficialRowsAndDerivedSummaries(t *testing.T) {
	r := debtSample()
	d, err := Debt(r)
	require.NoError(t, err)
	series := seriesByID(d)
	require.Len(t, d.Series, 9)
	require.Len(t, d.Observations, 9)
	daily := series["us.treasury-debt.marketable.issues.bills-regular-series.daily"]
	require.Equal(t, "official", daily.ValueOrigin)
	require.Equal(t, "USD_million", daily.Unit)
	require.Equal(t, "Marketable / Issues / Bills / Regular Series", daily.Definition)
	require.Contains(t, series, "us.treasury-debt.marketable.redemptions.bills.fiscal_year_to_date", "null细项不进入标识")
	o := observationOf(t, d, daily.ID)
	require.Equal(t, -10.0, *o.Value, "负调整保留")
	require.Equal(t, "20260930", o.Period)
	require.Equal(t, "t", *o.RawValue)
	require.Nil(t, o.PublishedAt)
	require.Equal(t, 0, o.FiscalYear)
	fytd := observationOf(t, d, "us.treasury-debt.marketable.issues.bills-regular-series.fiscal_year_to_date")
	require.Equal(t, -30.0, *fytd.Value, "累计用官方值")
	require.Equal(t, "20251001", fytd.PeriodBegin)
	require.Equal(t, 2026, fytd.FiscalYear)
	require.Equal(t, "20260901", observationOf(t, d, "us.treasury-debt.marketable.issues.bills-regular-series.month_to_date").PeriodBegin)
	net := series["us.treasury-debt.marketable.net_recorded_issuance"]
	require.Equal(t, "derived", net.ValueOrigin)
	require.Equal(t, -15.0, *observationOf(t, d, net.ID).Value)
	var original fiscaldata.DebtResult
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Len(t, original.Rows, 2)

	r = debtSample()
	r.Rows[0].Date, r.Begin, r.End = "2026-10-01", "20261001", "20261001"
	r.Summaries = nil
	r.Rows = r.Rows[:1]
	d, err = Debt(r)
	require.NoError(t, err)
	october := observationOf(t, d, "us.treasury-debt.marketable.issues.bills-regular-series.fiscal_year_to_date")
	require.Equal(t, 2027, october.FiscalYear, "10月重新开始财年累计")
	require.Equal(t, "20261001", october.PeriodBegin)

	r = debtSample()
	r.Rows[0].Today, r.Summaries[0].Issues, r.Summaries[0].Net = nil, nil, nil
	d, err = Debt(r)
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *observationOf(t, d, "us.treasury-debt.marketable.issues.bills-regular-series.daily").MissingReason)
	require.Equal(t, "calculation_unavailable", *observationOf(t, d, net.ID).MissingReason)

	r = debtSample()
	repeated := r.Rows[0]
	r.Rows[0].Raw = map[string]string{"src_line_nbr": "10", "transaction_today_amt": "t"}
	repeated.Raw = map[string]string{"src_line_nbr": "12"}
	r.Rows = append(r.Rows, repeated)
	d, err = Debt(r)
	require.NoError(t, err, "上游内容相同的重复行按一条输出")
	require.Len(t, d.Observations, 9)
	kept := observationOf(t, d, "us.treasury-debt.marketable.issues.bills-regular-series.daily")
	require.Contains(t, kept.Note, "10、12")

	r = debtSample()
	r.Rows[0].Raw = map[string]string{"src_line_nbr": "10"}
	for _, line := range []string{"12", "14"} {
		again := r.Rows[0]
		again.Raw = map[string]string{"src_line_nbr": line}
		r.Rows = append(r.Rows, again)
	}
	d, err = Debt(r)
	require.NoError(t, err)
	require.Contains(t, observationOf(t, d, "us.treasury-debt.marketable.issues.bills-regular-series.daily").Note, "10、12、14", "重复三次时不丢行号")
	require.Empty(t, observationOf(t, d, "us.treasury-debt.marketable.redemptions.bills.daily").Note)

	r = debtSample()
	r.Rows, r.Summaries = []fiscaldata.DebtRow{}, []fiscaldata.DebtSummary{}
	d, err = Debt(r)
	require.NoError(t, err)
	require.Empty(t, d.Series)
	require.Empty(t, d.Observations)

	for _, mutate := range []func(*fiscaldata.DebtResult){
		func(r *fiscaldata.DebtResult) { r.Indicator = "x" },
		func(r *fiscaldata.DebtResult) { r.Frequency = "event" },
		func(r *fiscaldata.DebtResult) { r.DateBasis = "auction" },
		func(r *fiscaldata.DebtResult) { r.Unit = "美元" },
		func(r *fiscaldata.DebtResult) { r.Source = "" },
		func(r *fiscaldata.DebtResult) { r.Rows[0].Market = "Other" },
		func(r *fiscaldata.DebtResult) { r.Rows[0].Transaction = "Other" },
		func(r *fiscaldata.DebtResult) { r.Rows[0].Security, r.Rows[0].Description = "null", "null" },
		func(r *fiscaldata.DebtResult) { r.Rows[0].Date = "20260930" },
		func(r *fiscaldata.DebtResult) { r.Rows[0].Date = "2026-10-01" },
		func(r *fiscaldata.DebtResult) { r.Rows[0].Source = "" },
		func(r *fiscaldata.DebtResult) { r.Rows[0].FetchedAt = time.Time{} },
		func(r *fiscaldata.DebtResult) {
			other := r.Rows[0]
			other.MTD = value(1)
			r.Rows = append(r.Rows, other)
		},
		func(r *fiscaldata.DebtResult) { r.Summaries[0].IsDerived = false },
		func(r *fiscaldata.DebtResult) { r.Summaries[0].Issues, r.Summaries[0].Net = value(-20), value(-25) },
		func(r *fiscaldata.DebtResult) { r.Summaries[0].Net = value(1) },
		func(r *fiscaldata.DebtResult) { r.Summaries[0].Redemptions = nil },
		func(r *fiscaldata.DebtResult) { r.Summaries = append(r.Summaries, r.Summaries[0]) },
		// 两个官方标签仅标点不同，不能合并到同一序列。
		func(r *fiscaldata.DebtResult) {
			other := debtRow("Issues", "Marketable", "Bills", "Regular-Series", 1)
			other.Date, r.End = "2026-10-01", "20261001"
			r.Rows = append(r.Rows, other)
		},
	} {
		r := debtSample()
		mutate(r)
		d, err := Debt(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = Debt(nil)
	require.Error(t, err)
}

func TestDebtCashSeparatesOfficialDerivedAndAdjustments(t *testing.T) {
	r := cashSample()
	d, err := DebtCash(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 12)
	require.Len(t, d.Observations, 12)
	series := seriesByID(d)
	require.Equal(t, "official", series["us.treasury-debt-cash.issues.daily"].ValueOrigin)
	issues := observationOf(t, d, "us.treasury-debt-cash.issues.month_to_date")
	require.Equal(t, 200.0, *issues.Value)
	require.Equal(t, "m", *issues.RawValue)
	net := series["us.treasury-debt-cash.net.fiscal_year_to_date"]
	require.Equal(t, "derived", net.ValueOrigin)
	require.Equal(t, "fiscal-year-to-date", net.PeriodBasis)
	o := observationOf(t, d, net.ID)
	require.Equal(t, 120.0, *o.Value)
	require.Len(t, o.Inputs, 2)
	require.Equal(t, 300.0, *o.Inputs[0].Value)
	require.Equal(t, "f", *o.Inputs[1].RawValue)
	require.Equal(t, 2026, o.FiscalYear, "计算累计与官方累计同样标明财年")
	require.Equal(t, "20251001", o.PeriodBegin)
	require.Equal(t, "20260930", o.PeriodEnd)
	require.Equal(t, "20260901", observationOf(t, d, "us.treasury-debt-cash.net.month_to_date").PeriodBegin)
	adjustment := observationOf(t, d, "us.treasury-debt-cash.adjustment.issues.inflation-protected-securities-increment-minus.daily")
	require.Equal(t, -6.0, *adjustment.Value, "方向标签不翻转符号")
	require.Equal(t, "t", *adjustment.RawValue)
	require.Contains(t, series[adjustment.SeriesID].Definition, "(-)")

	r = cashSample()
	r.Observations[0].Redemptions, r.Observations[0].Net = nil, fiscaldata.CashAmounts{}
	r.Observations[0].Note = "IIIB调整明细未列示"
	d, err = DebtCash(r)
	require.NoError(t, err)
	missing := observationOf(t, d, "us.treasury-debt-cash.redemptions.daily")
	require.Equal(t, "source_value_missing", *missing.MissingReason)
	require.Contains(t, missing.Note, "未推算")
	require.Equal(t, "totals", *missing.Source)
	netMissing := observationOf(t, d, "us.treasury-debt-cash.net.daily")
	require.Equal(t, "calculation_unavailable", *netMissing.MissingReason)
	require.Equal(t, 100.0, *netMissing.Inputs[0].Value)
	require.Nil(t, netMissing.Inputs[1].Value)
	require.Empty(t, observationOf(t, d, "us.treasury-debt-cash.issues.daily").Note)
	missingFYTD := observationOf(t, d, "us.treasury-debt-cash.redemptions.fiscal_year_to_date")
	require.Equal(t, 2026, missingFYTD.FiscalYear, "缺失观测与同序列其他日期结构一致")
	require.Equal(t, "20251001", missingFYTD.PeriodBegin)
	require.Equal(t, "fiscal-year-to-date", series["us.treasury-debt-cash.net.fiscal_year_to_date"].PeriodBasis)

	r = cashSample()
	repeated := r.Observations[0].Adjustments[0]
	r.Observations[0].Adjustments[0].Raw = map[string]string{"src_line_nbr": "3", "adj_today_amt": "t"}
	repeated.Raw = map[string]string{"src_line_nbr": "9"}
	r.Observations[0].Adjustments = append(r.Observations[0].Adjustments, repeated)
	d, err = DebtCash(r)
	require.NoError(t, err, "溯源明细的相同重复行不影响官方总额")
	require.Len(t, d.Observations, 12)
	kept := observationOf(t, d, "us.treasury-debt-cash.adjustment.issues.inflation-protected-securities-increment-minus.daily")
	require.Contains(t, kept.Note, "3、9")

	for _, mutate := range []func(*fiscaldata.CashResult){
		func(r *fiscaldata.CashResult) { r.Indicator = "x" },
		func(r *fiscaldata.CashResult) { r.Basis = "record" },
		func(r *fiscaldata.CashResult) { r.Unit = "美元" },
		func(r *fiscaldata.CashResult) { r.Sources = nil },
		func(r *fiscaldata.CashResult) { r.Observations[0].Date = "20260930" },
		func(r *fiscaldata.CashResult) { r.Observations[0].NetIsDerived = false },
		func(r *fiscaldata.CashResult) { r.Observations[0].Net.Today = value(1) },
		func(r *fiscaldata.CashResult) { r.Observations[0].Redemptions = nil },
		func(r *fiscaldata.CashResult) { r.Observations[0].Issues.Transaction = "Redemptions" },
		func(r *fiscaldata.CashResult) { r.Observations[0].Issues.Source = "" },
		func(r *fiscaldata.CashResult) { r.Observations[0].Adjustments[0].Transaction = "Other" },
		func(r *fiscaldata.CashResult) { r.Observations[0].Adjustments[0].Category = "null" },
		func(r *fiscaldata.CashResult) { r.Observations[0].Adjustments[0].Date = "2026-09-29" },
		func(r *fiscaldata.CashResult) {
			other := r.Observations[0].Adjustments[0]
			other.Today = value(1)
			r.Observations[0].Adjustments = append(r.Observations[0].Adjustments, other)
		},
		func(r *fiscaldata.CashResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := cashSample()
		mutate(r)
		d, err := DebtCash(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = DebtCash(nil)
	require.Error(t, err)

	require.Equal(t, "government-account-transactions-minus", dtsSlug("Government Account Transactions (-)"))
	require.NotEqual(t, dtsSlug("Inflation-Protected Securities Increment"), dtsSlug("Inflation-Protected Securities Increment (-)"), "有无方向标签不共用序列")
}

func TestAuctionsSeparateEventsOnSameDate(t *testing.T) {
	r := auctionSample()
	d, err := Auctions(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 10)
	require.Len(t, d.Observations, 20)
	require.Len(t, d.Events, 2)
	series := seriesByID(d)
	offering := series["us.treasury-auctions.offering"]
	require.Equal(t, "USD", offering.Unit)
	require.Equal(t, "issue-date", offering.PeriodBasis)
	require.Equal(t, "par-value", offering.PriceBasis)
	require.False(t, *offering.ComponentsAdditive)
	require.Equal(t, "USD_per_100_par", series["us.treasury-auctions.price_per_100"].Unit)
	require.Nil(t, series["us.treasury-auctions.price_per_100"].ComponentsAdditive)

	first := d.Observations[0]
	require.Equal(t, "912797VE4/20260901/20260827", first.EventID)
	require.Equal(t, "20260901", first.Period)
	require.Equal(t, "100", *first.RawValue)
	require.Equal(t, "912797VF1/20260901/20260828", d.Observations[10].EventID)
	require.Equal(t, first.Period, d.Observations[10].Period, "同日事件靠event_id区分")
	require.Equal(t, first.SeriesID, d.Observations[10].SeriesID)
	require.Equal(t, d.Events[0].ID, first.EventID)
	require.Equal(t, "2026-08-27", d.Events[0].AuctionDate)
	require.True(t, *d.Events[0].Reopening)
	require.False(t, *d.Events[0].SOMAInsideOffering)
	require.Nil(t, d.Events[0].FIMACompetitiveInsideOffering, "未知不写成false")
	require.Equal(t, 0.0, *observationOf(t, d, "us.treasury-auctions.competitive_accepted").Value, "零值不是缺失")
	fima := observationOf(t, d, "us.treasury-auctions.fima_noncompetitive_accepted")
	require.Equal(t, "source_value_missing", *fima.MissingReason)
	require.Equal(t, "null", *fima.RawValue)

	r = auctionSample()
	r.DateBasis, r.Begin = "auction_date", "20260801"
	d, err = Auctions(r)
	require.NoError(t, err)
	require.Equal(t, "20260827", d.Observations[0].Period)
	require.Equal(t, "auction-date", d.Series[0].PeriodBasis)
	require.Equal(t, "912797VE4/20260901/20260827", d.Observations[0].EventID, "事件标识不随日期口径变化")

	r = auctionSample()
	r.Observations = []fiscaldata.AuctionObservation{}
	d, err = Auctions(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 10)
	require.Empty(t, d.Observations)
	encoded, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), `"events"`, "其他导出不增加events")

	for _, mutate := range []func(*fiscaldata.AuctionsResult){
		func(r *fiscaldata.AuctionsResult) { r.DateBasis = "announcement" },
		func(r *fiscaldata.AuctionsResult) { r.Indicator = "x" },
		func(r *fiscaldata.AuctionsResult) { r.Frequency = "daily" },
		func(r *fiscaldata.AuctionsResult) { r.Market = "other" },
		func(r *fiscaldata.AuctionsResult) { r.AmountBasis = "cash" },
		func(r *fiscaldata.AuctionsResult) { r.AmountUnit = "百万美元" },
		func(r *fiscaldata.AuctionsResult) { r.Observations[0].CUSIP = "" },
		func(r *fiscaldata.AuctionsResult) { r.Observations[0].Source = "" },
		func(r *fiscaldata.AuctionsResult) { r.Observations[0].FetchedAt = time.Time{} },
		func(r *fiscaldata.AuctionsResult) { r.Observations[0].IssueDate = "20260901" },
		func(r *fiscaldata.AuctionsResult) { r.Observations[0].AuctionDate = "bad" },
		func(r *fiscaldata.AuctionsResult) { r.Observations[0].IssueDate = "2026-10-01" },
		func(r *fiscaldata.AuctionsResult) { r.Observations = append(r.Observations, r.Observations[0]) },
	} {
		r := auctionSample()
		mutate(r)
		d, err := Auctions(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	_, err = Auctions(nil)
	require.Error(t, err)
}
