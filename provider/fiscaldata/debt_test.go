package fiscaldata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func fixture(t *testing.T, name string) envelope {
	t.Helper()
	raw, e := os.ReadFile("testdata/" + name)
	require.NoError(t, e)
	var env envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	return env
}
func encode(t *testing.T, env envelope) string {
	t.Helper()
	raw, e := json.Marshal(env)
	require.NoError(t, e)
	return string(raw)
}
func mock(t *testing.T, env envelope, page int) {
	t.Helper()
	gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions").MatchParam("page[number]", fmt.Sprint(page)).MatchParam("page[size]", "1000").Reply(200).BodyString(encode(t, env))
}

func TestDebtOfficialCurrentAndHistorical(t *testing.T) {
	for _, tc := range []struct {
		file, date             string
		issue, redemption, net float64
	}{{"debt-20260930.json", "20260930", 232391, 174673, 57718}, {"debt-20051003.json", "20051003", 0, 0, 0}} {
		t.Run(tc.date, func(t *testing.T) {
			defer gock.Off()
			mock(t, fixture(t, tc.file), 1)
			result, err := NewClient("").Debt(context.Background(), &DebtRequest{Begin: tc.date, End: tc.date})
			require.NoError(t, err)
			require.Len(t, result.Rows, 23)
			require.Len(t, result.Summaries, 2)
			require.True(t, result.Summaries[0].IsDerived)
			require.Equal(t, 10, result.Summaries[0].InputRows)
			if tc.date == "20260930" {
				require.Equal(t, tc.issue, *result.Summaries[0].Issues)
				require.Equal(t, tc.redemption, *result.Summaries[0].Redemptions)
				require.Equal(t, tc.net, *result.Summaries[0].Net)
				require.Equal(t, -10.0, *result.Rows[0].Today)
				require.Equal(t, 2469797.0, *result.Rows[0].MTD)
			}
			require.NotEmpty(t, result.Rows[0].Source)
			require.False(t, result.Rows[0].FetchedAt.IsZero())
			require.Nil(t, result.PublishedAt)
			require.Nil(t, result.RevisionID)
			require.True(t, gock.IsDone())
		})
	}
}

func TestDebtEmptyNullAndZero(t *testing.T) {
	defer gock.Off()
	mock(t, fixture(t, "debt-empty.json"), 1)
	r, e := NewClient("").Debt(context.Background(), &DebtRequest{Begin: "20260927", End: "20260927"})
	require.NoError(t, e)
	require.Empty(t, r.Rows)
	require.Empty(t, r.Summaries)
	env := fixture(t, "debt-20260930.json")
	env.Data[0]["transaction_today_amt"] = "null"
	mock(t, env, 1)
	r, e = NewClient("").Debt(context.Background(), &DebtRequest{Begin: "20260930", End: "20260930"})
	require.NoError(t, e)
	require.Nil(t, r.Rows[0].Today)
	require.Nil(t, r.Summaries[0].Issues)
	require.Nil(t, r.Summaries[0].Net)
	require.NotNil(t, r.Summaries[0].Redemptions)
	require.Equal(t, 0.0, *r.Rows[1].Today)
	require.True(t, gock.IsDone())
}

func TestDebtInvalidResponses(t *testing.T) {
	for name, change := range map[string]func(*envelope){
		"unit":           func(e *envelope) { e.Meta.Formats["transaction_today_amt"] = "$1,000" },
		"truncated":      func(e *envelope) { e.Meta.Total++ },
		"page count":     func(e *envelope) { e.Meta.Pages++ },
		"row count":      func(e *envelope) { e.Meta.Count-- },
		"missing rows":   func(e *envelope) { e.Data = nil },
		"date":           func(e *envelope) { e.Data[0]["record_date"] = "2026-10-01" },
		"table":          func(e *envelope) { e.Data[0]["table_nm"] = "Other" },
		"market":         func(e *envelope) { e.Data[0]["security_market"] = "Unknown" },
		"transaction":    func(e *envelope) { e.Data[0]["transaction_type"] = "Total" },
		"aggregate row":  func(e *envelope) { e.Data[0]["security_type"] = "Total Issues" },
		"missing amount": func(e *envelope) { delete(e.Data[0], "transaction_today_amt") },
		"nonfinite":      func(e *envelope) { e.Data[0]["transaction_today_amt"] = "NaN" },
		"bad number":     func(e *envelope) { e.Data[0]["transaction_today_amt"] = "unknown" },
		"duplicate":      func(e *envelope) { e.Data[1] = e.Data[0] },
		"unsorted":       func(e *envelope) { e.Data[0], e.Data[1] = e.Data[1], e.Data[0] },
	} {
		t.Run(name, func(t *testing.T) {
			defer gock.Off()
			env := fixture(t, "debt-20260930.json")
			change(&env)
			mock(t, env, 1)
			_, e := NewClient("").Debt(context.Background(), &DebtRequest{Begin: "20260930", End: "20260930"})
			require.Error(t, e)
			require.True(t, gock.IsDone())
		})
	}
}

func TestDebtMultiPageCompleteness(t *testing.T) {
	for _, mode := range []string{"valid", "count changes", "overlap", "http"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			env := fixture(t, "debt-20260930.json")
			row := env.Data[0]
			env.Data = nil
			for i := 1; i <= 1000; i++ {
				copy := map[string]string{}
				for k, v := range row {
					copy[k] = v
				}
				copy["src_line_nbr"] = fmt.Sprint(i)
				env.Data = append(env.Data, copy)
			}
			env.Meta.Total, env.Meta.Count, env.Meta.Pages = 1001, 1000, 2
			mock(t, env, 1)
			second := fixture(t, "debt-20260930.json")
			second.Data = second.Data[:1]
			second.Data[0]["src_line_nbr"] = "1001"
			second.Meta.Total, second.Meta.Count, second.Meta.Pages = 1001, 1, 2
			if mode == "count changes" {
				second.Meta.Total = 1002
				second.Data = append(second.Data, second.Data[0])
				second.Meta.Count = 2
			}
			if mode == "overlap" {
				second.Data[0]["src_line_nbr"] = "1000"
			}
			if mode == "http" {
				gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions").Reply(503)
			} else {
				mock(t, second, 2)
			}
			r, e := NewClient("").Debt(context.Background(), &DebtRequest{Begin: "20260930", End: "20260930"})
			if mode == "valid" {
				require.NoError(t, e)
				require.Len(t, r.Rows, 1001)
				require.Equal(t, -10010.0, *r.Summaries[0].Issues)
				require.Nil(t, r.Summaries[0].Redemptions)
				require.Nil(t, r.Summaries[0].Net)
			} else {
				require.Error(t, e)
				require.Nil(t, r)
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestDebtCacheFailureRefreshAndCancellation(t *testing.T) {
	defer gock.Off()
	c := NewClient(t.TempDir())
	req := &DebtRequest{Begin: "20260930", End: "20260930"}
	mock(t, fixture(t, "debt-20260930.json"), 1)
	first, e := c.Debt(context.Background(), req)
	require.NoError(t, e)
	second, e := c.Debt(context.Background(), req)
	require.NoError(t, e)
	require.Equal(t, first, second)
	files, e := filepath.Glob(filepath.Join(c.CacheDir, "*.json"))
	require.NoError(t, e)
	require.Len(t, files, 1)
	before, e := os.ReadFile(files[0])
	require.NoError(t, e)
	c.Refresh = true
	gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions").Reply(200).BodyString(`<html>error</html>`)
	_, e = c.Debt(context.Background(), req)
	require.Error(t, e)
	after, e := os.ReadFile(files[0])
	require.NoError(t, e)
	require.Equal(t, before, after)
	c.Refresh = false
	_, e = c.Debt(context.Background(), req)
	require.NoError(t, e)
	require.NoError(t, os.WriteFile(files[0], []byte(`{broken`), 0600))
	mock(t, fixture(t, "debt-20260930.json"), 1)
	_, e = c.Debt(context.Background(), req)
	require.NoError(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = c.Debt(ctx, req)
	require.ErrorIs(t, e, context.Canceled)
	require.True(t, gock.IsDone())
}
func TestDebtRequestValidation(t *testing.T) {
	for _, r := range []*DebtRequest{nil, {Begin: "20051002", End: "20051003"}, {Begin: "20260931", End: "20260931"}, {Begin: "20260930", End: "20260929"}, {Begin: "20260930", End: "99991231"}} {
		require.Error(t, r.Validate())
	}
}
