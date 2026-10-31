package fiscaldata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func cashFixture(t *testing.T, old bool, spec cashSpec) envelope {
	t.Helper()
	date := "20260930"
	if old {
		date = "20051003"
	}
	name := "cash-totals-" + date + ".json"
	if spec.adjustments {
		name = "cash-adjustments-" + date + ".json"
	}
	env := fixture(t, name)
	if !spec.adjustments {
		rows := []map[string]string{}
		for _, r := range env.Data {
			if r["transaction_catg"] == spec.category {
				rows = append(rows, r)
			}
		}
		env.Data = rows
	}
	env.Meta.Total, env.Meta.Count = len(env.Data), len(env.Data)
	env.Meta.Pages = 0
	if len(env.Data) > 0 {
		env.Meta.Pages = 1
	}
	return env
}
func mockCashPage(t *testing.T, req *DebtRequest, spec cashSpec, env envelope, page int, status int) {
	t.Helper()
	begin, e := time.Parse("20060102", req.Begin)
	require.NoError(t, e)
	end, e := time.Parse("20060102", req.End)
	require.NoError(t, e)
	filter := "record_date:gte:" + begin.Format("2006-01-02") + ",record_date:lte:" + end.Format("2006-01-02")
	if !spec.adjustments {
		filter += ",transaction_catg:eq:" + spec.category
	}
	address, e := url.Parse(spec.address)
	require.NoError(t, e)
	gock.New("https://api.fiscaldata.treasury.gov").Get(address.Path).MatchParam("filter", regexp.QuoteMeta(filter)).MatchParam("sort", "record_date,src_line_nbr").MatchParam("page[number]", fmt.Sprint(page)).MatchParam("page[size]", "1000").Reply(status).BodyString(encode(t, env))
}
func mockCash(t *testing.T, req *DebtRequest, old bool, change func(cashSpec, *envelope)) {
	t.Helper()
	for _, spec := range cashSpecs() {
		env := cashFixture(t, old, spec)
		if change != nil {
			change(spec, &env)
		}
		mockCashPage(t, req, spec, env, 1, 200)
	}
}
func TestDebtCashOfficialTotalsAndHistoricalLabels(t *testing.T) {
	for _, tc := range []struct {
		date                     string
		old                      bool
		issues, redemptions, net float64
	}{{"20260930", false, 232753, 175898, 56855}, {"20051003", true, 243, 3251, -3008}} {
		t.Run(tc.date, func(t *testing.T) {
			defer gock.Off()
			req := &DebtRequest{Begin: tc.date, End: tc.date}
			mockCash(t, req, tc.old, nil)
			result, e := NewClient("").DebtCash(context.Background(), req)
			require.NoError(t, e)
			require.Len(t, result.Observations, 1)
			row := result.Observations[0]
			require.Equal(t, tc.issues, *row.Issues.Today)
			require.Equal(t, tc.redemptions, *row.Redemptions.Today)
			require.Equal(t, tc.net, *row.Net.Today)
			require.True(t, row.NetIsDerived)
			require.Equal(t, "official_cash_issues - official_cash_redemptions", row.NetFormula)
			require.Len(t, row.Adjustments, 13)
			require.NotEmpty(t, row.Issues.Raw)
			require.NotEmpty(t, row.Issues.Source)
			require.False(t, row.Issues.FetchedAt.IsZero())
			require.Nil(t, result.PublishedAt)
			require.Nil(t, result.RevisionID)
			if !tc.old {
				require.Equal(t, 2898950.0, *row.Issues.MTD)
				require.Equal(t, 2913614.0, *row.Redemptions.MTD)
				require.Equal(t, -14664.0, *row.Net.MTD)
				require.Equal(t, 1777468.0, *row.Net.FYTD)
				require.Equal(t, -6.0, *row.Adjustments[7].Today)
				require.Contains(t, row.Adjustments[7].Category, "(-)")
				require.Equal(t, 852621.0, *row.Adjustments[0].Today)
				require.NotEqual(t, *row.Adjustments[0].Today, *row.Issues.Today, "不能用记账面值冒充现金总额")
			} else {
				require.Equal(t, "Federal Reserve Account", row.Issues.Raw["account_type"])
				require.Equal(t, "Public Debt Cash Issues", row.Adjustments[0].Raw["transaction_type"])
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestDebtCashMissingZeroAndNoReconstruction(t *testing.T) {
	for _, mode := range []string{"missing issues", "null daily", "zero", "missing adjustments", "empty"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			req := &DebtRequest{Begin: "20260930", End: "20260930"}
			mockCash(t, req, false, func(s cashSpec, e *envelope) {
				if mode == "empty" || mode == "missing issues" && !s.adjustments && s.transaction == "Issues" || mode == "missing adjustments" && s.adjustments {
					e.Data = []map[string]string{}
					e.Meta.Total, e.Meta.Count, e.Meta.Pages = 0, 0, 0
				}
				if !s.adjustments {
					for _, r := range e.Data {
						if mode == "null daily" {
							r["transaction_today_amt"] = "null"
						}
						if mode == "zero" {
							r["transaction_today_amt"] = "0"
						}
					}
				}
			})
			result, e := NewClient("").DebtCash(context.Background(), req)
			require.NoError(t, e)
			if mode == "empty" {
				require.Empty(t, result.Observations)
			} else {
				r := result.Observations[0]
				switch mode {
				case "missing issues":
					require.Nil(t, r.Issues)
					require.NotNil(t, r.Redemptions)
					require.Nil(t, r.Net.Today)
					require.Nil(t, r.Net.MTD)
					require.Len(t, r.Adjustments, 13)
					require.Contains(t, r.Note, "总额缺失")
				case "null daily":
					require.Nil(t, r.Net.Today)
					require.Equal(t, -14664.0, *r.Net.MTD)
				case "zero":
					require.Equal(t, 0.0, *r.Issues.Today)
					require.Equal(t, 0.0, *r.Net.Today)
				case "missing adjustments":
					require.Empty(t, r.Adjustments)
					require.Equal(t, 56855.0, *r.Net.Today)
					require.Contains(t, r.Note, "调整明细未列示")
				}
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestCashPageRejectsInvalidSourceData(t *testing.T) {
	req := &DebtRequest{Begin: "20260930", End: "20260930"}
	spec := cashSpecs()[0]
	for name, change := range map[string]func(*envelope){
		"account":        func(e *envelope) { e.Data[0]["account_type"] = "Other" },
		"category":       func(e *envelope) { e.Data[0]["transaction_catg"] = "Public Debt Issues (Table IIIA)" },
		"side":           func(e *envelope) { e.Data[0]["transaction_type"] = "Withdrawals" },
		"table":          func(e *envelope) { e.Data[0]["table_nbr"] = "IIIA" },
		"date":           func(e *envelope) { e.Data[0]["record_date"] = "2026-09-29" },
		"line":           func(e *envelope) { e.Data[0]["src_line_nbr"] = "bad" },
		"unit":           func(e *envelope) { e.Meta.Formats["transaction_today_amt"] = "$1,000" },
		"missing amount": func(e *envelope) { delete(e.Data[0], "transaction_mtd_amt") },
		"nonfinite":      func(e *envelope) { e.Data[0]["transaction_today_amt"] = "Inf" },
		"wrong count":    func(e *envelope) { e.Meta.Count++ },
		"truncated":      func(e *envelope) { e.Meta.Total++ },
	} {
		t.Run(name, func(t *testing.T) {
			env := cashFixture(t, false, spec)
			change(&env)
			_, _, e := parseCashPage([]byte(encode(t, env)), req, spec, 1)
			require.Error(t, e)
		})
	}
	adjustment := cashSpecs()[4]
	env := cashFixture(t, false, adjustment)
	env.Data[0]["transaction_type"] = "Other"
	_, _, e := parseCashPage([]byte(encode(t, env)), req, adjustment, 1)
	require.ErrorContains(t, e, "交易类型变化")
}

func TestCashPaginationAndFailures(t *testing.T) {
	req := &DebtRequest{Begin: "20260930", End: "20260930"}
	spec := cashSpecs()[4]
	for _, mode := range []string{"valid", "overlap", "total changes", "http"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			first := cashFixture(t, false, spec)
			original := first.Data[0]
			first.Data = nil
			for i := 1; i <= pageSize; i++ {
				r := map[string]string{}
				for k, v := range original {
					r[k] = v
				}
				r["src_line_nbr"] = fmt.Sprint(i)
				first.Data = append(first.Data, r)
			}
			first.Meta.Total, first.Meta.Count, first.Meta.Pages = pageSize+1, pageSize, 2
			mockCashPage(t, req, spec, first, 1, 200)
			second := cashFixture(t, false, spec)
			second.Data = second.Data[:1]
			second.Data[0]["src_line_nbr"] = "1001"
			second.Meta.Total, second.Meta.Count, second.Meta.Pages = 1001, 1, 2
			if mode == "overlap" {
				second.Data[0]["src_line_nbr"] = "1000"
			}
			if mode == "total changes" {
				second.Data = append(second.Data, second.Data[0])
				second.Meta.Total, second.Meta.Count = 1002, 2
			}
			status := 200
			if mode == "http" {
				status = 503
			}
			mockCashPage(t, req, spec, second, 2, status)
			rows, _, e := NewClient("").cashRows(context.Background(), req, spec)
			if mode == "valid" {
				require.NoError(t, e)
				require.Len(t, rows, 1001)
			} else {
				require.Error(t, e)
				require.Nil(t, rows)
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestDebtCashCacheAndFailureRefresh(t *testing.T) {
	defer gock.Off()
	req := &DebtRequest{Begin: "20260930", End: "20260930"}
	client := NewClient(t.TempDir())
	mockCash(t, req, false, nil)
	first, e := client.DebtCash(context.Background(), req)
	require.NoError(t, e)
	again, e := client.DebtCash(context.Background(), req)
	require.NoError(t, e)
	require.Equal(t, first, again)
	files, e := filepath.Glob(filepath.Join(client.CacheDir, "*.json"))
	require.NoError(t, e)
	require.Len(t, files, 5)
	spec := cashSpecs()[0]
	before := map[string][]byte{}
	for _, p := range files {
		before[p], e = os.ReadFile(p)
		require.NoError(t, e)
	}
	client.Refresh = true
	address, e := url.Parse(spec.address)
	require.NoError(t, e)
	gock.New("https://api.fiscaldata.treasury.gov").Get(address.Path).Reply(200).BodyString(`<html>error</html>`)
	_, e = client.DebtCash(context.Background(), req)
	require.Error(t, e)
	for p, want := range before {
		got, e := os.ReadFile(p)
		require.NoError(t, e)
		require.Equal(t, want, got)
	}
	client.Refresh = false
	_, e = client.DebtCash(context.Background(), req)
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = client.DebtCash(ctx, req)
	require.ErrorIs(t, e, context.Canceled)
	require.True(t, gock.IsDone())
}

func TestCashDuplicateTotalsAndFiscalYearBoundary(t *testing.T) {
	t.Run("new and old totals cannot be combined", func(t *testing.T) {
		defer gock.Off()
		req := &DebtRequest{Begin: "20260930", End: "20260930"}
		specs := cashSpecs()
		for i := 0; i < 3; i++ {
			spec := specs[i]
			env := cashFixture(t, false, spec)
			if i == 2 {
				env = cashFixture(t, false, specs[0])
				env.Data[0]["transaction_catg"] = spec.category
			}
			mockCashPage(t, req, spec, env, 1, 200)
		}
		r, e := NewClient("").DebtCash(context.Background(), req)
		require.ErrorContains(t, e, "总额重复")
		require.Nil(t, r)
		require.True(t, gock.IsDone())
	})
	t.Run("official fiscal cumulative resets without summing", func(t *testing.T) {
		defer gock.Off()
		req := &DebtRequest{Begin: "20260930", End: "20261001"}
		mockCash(t, req, false, func(s cashSpec, e *envelope) {
			if s.adjustments || len(e.Data) == 0 {
				return
			}
			r := map[string]string{}
			for k, v := range e.Data[0] {
				r[k] = v
			}
			r["record_date"] = "2026-10-01"
			r["transaction_today_amt"], r["transaction_mtd_amt"], r["transaction_fytd_amt"] = "20", "20", "20"
			if s.transaction == "Redemptions" {
				r["transaction_today_amt"], r["transaction_mtd_amt"], r["transaction_fytd_amt"] = "5", "5", "5"
			}
			e.Data = append(e.Data, r)
			e.Meta.Total, e.Meta.Count = 2, 2
		})
		r, e := NewClient("").DebtCash(context.Background(), req)
		require.NoError(t, e)
		require.Len(t, r.Observations, 2)
		require.Equal(t, 15.0, *r.Observations[1].Net.FYTD)
		require.Equal(t, 15.0, *r.Observations[1].Net.MTD)
		require.Contains(t, r.Observations[1].Note, "调整明细未列示")
		require.True(t, gock.IsDone())
	})
}

func TestCashMetadataMustBePresent(t *testing.T) {
	spec := cashSpecs()[0]
	env := cashFixture(t, false, spec)
	raw, e := json.Marshal(env)
	require.NoError(t, e)
	var obj map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &obj))
	delete(obj, "meta")
	raw, e = json.Marshal(obj)
	require.NoError(t, e)
	_, _, e = parseCashPage(raw, &DebtRequest{Begin: "20260930", End: "20260930"}, spec, 1)
	require.Error(t, e)
}
