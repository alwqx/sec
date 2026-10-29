package fred

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func creditSample(t *testing.T, metric string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/us-credit-" + metric + ".csv")
	require.NoError(t, err)
	return string(raw)
}

func TestCreditOfficialLevelsMetadataAndCache(t *testing.T) {
	for _, test := range []struct {
		metric, begin, end, earliest, id string
		level                            float64
		adjusted                         bool
	}{
		{"bank", "202609", "202610", "194701", "LOANINV", 19882.6426, true},
		{"standards", "2026Q3", "2026Q4", "1990Q2", "DRTSCILM", 0, false},
		{"debt-service", "2026Q2", "2026Q3", "2005Q1", "TDSP", 11.111439, true},
	} {
		t.Run(test.metric, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "curl", r.UserAgent())
				_, _ = w.Write([]byte(creditSample(t, test.metric)))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.creditEndpoints[test.metric] = server.URL
			req := &CreditRequest{Begin: test.begin, End: test.end, Metric: test.metric}
			got, err := client.Credit(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, test.id, got.SeriesID)
			require.Equal(t, test.earliest, got.FirstPeriod)
			require.Equal(t, test.begin, got.LastPeriod)
			require.Equal(t, test.adjusted, got.SeasonallyAdjusted)
			require.False(t, got.LevelAnnualized)
			require.False(t, got.GrowthAnnualized)
			require.Nil(t, got.PublishedAt)
			require.Nil(t, got.RevisionID)
			require.Equal(t, "current_visible_revision", got.DataVersion)
			require.Len(t, got.Observations, 2)
			item := got.Observations[0]
			require.Equal(t, test.level, *item.Level)
			require.Nil(t, got.Observations[1].Level)
			require.Contains(t, got.Observations[1].Note, "当前快照未提供数值")
			if test.metric == "bank" {
				require.Equal(t, "202608", item.PreviousBase.Period)
				require.Equal(t, "202509", item.YearAgoBase.Period)
				require.InDelta(t, (test.level / *item.PreviousBase.Level - 1)*100, *item.PreviousChangePercent, 1e-10)
				require.InDelta(t, (test.level / *item.YearAgoBase.Level - 1)*100, *item.YearAgoChangePercent, 1e-10)
				require.Nil(t, item.PreviousChangePP)
				require.Nil(t, item.YearAgoChangePP)
				require.Equal(t, "monthly_average_stock", got.Measurement)
				require.Equal(t, "level_ratio_not_break_adjusted", got.ChangeMethod)
				require.Equal(t, "pro_rata_average_of_weekly_levels", got.PeriodBasis)
			} else {
				require.InDelta(t, test.level-*item.PreviousBase.Level, *item.PreviousChangePP, 1e-10)
				require.InDelta(t, test.level-*item.YearAgoBase.Level, *item.YearAgoChangePP, 1e-10)
				require.Nil(t, item.PreviousChangePercent)
				require.Nil(t, item.YearAgoChangePercent)
				require.Equal(t, "percentage_points", got.ChangeUnit)
				require.Equal(t, "not_applicable", got.PriceBasis)
				if test.metric == "standards" {
					require.Equal(t, "survey_quarter", got.PeriodBasis)
					require.Equal(t, -8.1, *item.PreviousChangePP)
					require.Contains(t, strings.Join(got.MethodologyNotes, ""), "不擅自回移")
				} else {
					require.Equal(t, "credit_bureau_based_dsr", got.Methodology)
					require.Contains(t, strings.Join(got.MethodologyNotes, ""), "不与旧方法")
				}
			}
			cached, err := client.Credit(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, got, cached)
			require.Equal(t, 1, calls)
			first, err := client.Credit(context.Background(), &CreditRequest{Begin: test.earliest, End: test.earliest, Metric: test.metric})
			require.NoError(t, err)
			require.NotNil(t, first.Observations[0].Level)
			require.Nil(t, first.Observations[0].PreviousChangePP)
			require.Nil(t, first.Observations[0].PreviousChangePercent)
			require.Nil(t, first.Observations[0].YearAgoBase.Level)
		})
	}
}

func TestCreditStrictCalendarGaps(t *testing.T) {
	for _, metric := range []string{"bank", "standards", "debt-service"} {
		spec, _ := creditMetric(metric)
		first, last, period, prev, yoy := "2026-07-01", "2026-09-01", "202609", "202608", "202509"
		if metric != "bank" {
			first, last, period, prev, yoy = "2025-10-01", "2026-04-01", "2026Q2", "2026Q1", "2025Q2"
		}
		raw := "DATE," + spec.id + "\n" + first + ",10\n" + last + ",20\n"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
		client := NewClient("")
		client.creditEndpoints[metric] = server.URL
		got, err := client.Credit(context.Background(), &CreditRequest{Begin: period, End: period, Metric: metric})
		require.NoError(t, err)
		item := got.Observations[0]
		require.Equal(t, prev, item.PreviousBase.Period)
		require.Equal(t, yoy, item.YearAgoBase.Period)
		require.Nil(t, item.PreviousChangePP)
		require.Nil(t, item.PreviousChangePercent)
		require.Nil(t, item.YearAgoChangePP)
		require.Nil(t, item.YearAgoChangePercent)
		server.Close()
	}
}

func TestCreditZeroNegativeAndPointDifferences(t *testing.T) {
	rows, err := creditParse("DATE,DRTSCILM\n2026-01-01,-10\n2026-04-01,0\n2026-07-01,-5\n", "standards")
	require.NoError(t, err)
	change, err := creditChange(rows["2026Q2"].Level, rows["2026Q1"].Level, "percentage_points")
	require.NoError(t, err)
	require.Equal(t, 10.0, *change)
	change, err = creditChange(rows["2026Q3"].Level, rows["2026Q2"].Level, "percentage_points")
	require.NoError(t, err)
	require.Equal(t, -5.0, *change)
	rows, err = creditParse("DATE,LOANINV\n2026-08-01,0\n2026-09-01,20\n", "bank")
	require.NoError(t, err)
	require.Zero(t, *rows["202608"].Level)
	change, err = creditChange(rows["202609"].Level, rows["202608"].Level, "percent")
	require.NoError(t, err)
	require.Nil(t, change)
	for _, metric := range []string{"standards", "debt-service"} {
		spec, _ := creditMetric(metric)
		rows, err := creditParse("DATE,"+spec.id+"\n2026-01-01,0\n2026-04-01,.\n2026-07-01,\n", metric)
		require.NoError(t, err)
		require.NotNil(t, rows["2026Q1"].Level)
		require.Nil(t, rows["2026Q2"].Level)
		require.Equal(t, ".", rows["2026Q2"].RawLevel)
		require.Equal(t, "", rows["2026Q3"].RawLevel)
	}
}

func TestCreditRejectsBadSnapshots(t *testing.T) {
	for _, metric := range []string{"bank", "standards", "debt-service"} {
		original, err := csv.NewReader(strings.NewReader(creditSample(t, metric))).ReadAll()
		require.NoError(t, err)
		modes := []string{"unknown-column", "duplicate-column", "invalid-date", "wrong-frequency", "earlier-date", "duplicate-period", "truncated", "nan", "inf", "all-missing", "negative"}
		if metric == "bank" {
			modes = append(modes, "overflow")
		}
		if metric == "standards" {
			modes = append(modes, "above-100", "below-minus-100")
		}
		for _, mode := range modes {
			t.Run(metric+"/"+mode, func(t *testing.T) {
				rows := make([][]string, len(original))
				for i, row := range original {
					rows[i] = append([]string(nil), row...)
				}
				switch mode {
				case "unknown-column":
					rows[0][1] = "WRONG"
				case "duplicate-column":
					rows[0][1] = rows[0][0]
				case "invalid-date":
					rows[1][0] = "2026-02-30"
				case "wrong-frequency":
					rows[1][0] = "2026-02-02"
					if metric != "bank" {
						rows[1][0] = "2026-02-01"
					}
				case "earlier-date":
					rows[1][0] = "1946-01-01"
				case "duplicate-period":
					rows = append(rows, rows[1])
				case "truncated":
					rows[1] = rows[1][:1]
				case "nan":
					rows[1][1] = "NaN"
				case "inf":
					rows[1][1] = "+Inf"
				case "all-missing":
					for i := 1; i < len(rows); i++ {
						rows[i][1] = "."
					}
				case "negative":
					rows[1][1] = "-1"
				case "overflow":
					rows[1][1], rows[2][1] = "1e-100", "1e308"
				case "above-100":
					rows[1][1] = "100.1"
				case "below-minus-100":
					rows[1][1] = "-100.1"
				}
				var out strings.Builder
				require.NoError(t, csv.NewWriter(&out).WriteAll(rows))
				_, err := creditParse(out.String(), metric)
				if mode == "negative" && metric == "standards" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
		for _, row := range original {
			row[0], row[1] = row[1], row[0]
		}
		original[0][1] = "\ufeffDATE"
		var out strings.Builder
		require.NoError(t, csv.NewWriter(&out).WriteAll(original))
		_, err = creditParse(out.String(), metric)
		require.NoError(t, err)
	}
}

func TestCreditCacheProtectionAndRecovery(t *testing.T) {
	for _, metric := range []string{"bank", "standards", "debt-service"} {
		t.Run(metric, func(t *testing.T) {
			raw := creditSample(t, metric)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(raw)) }))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.creditEndpoints[metric] = server.URL
			period := "2026Q2"
			if metric == "bank" {
				period = "202609"
			}
			req := &CreditRequest{Begin: period, End: period, Metric: metric}
			good, err := client.Credit(context.Background(), req)
			require.NoError(t, err)
			path := filepath.Join(client.CacheDir, "us-credit-"+metric+".json")
			saved, err := os.ReadFile(path)
			require.NoError(t, err)
			client.Refresh = true
			bad := []string{"broken"}
			if metric == "bank" {
				bad = append(bad, "DATE,LOANINV\n2026-08-01,1e-100\n2026-09-01,1e308\n")
			}
			for _, value := range bad {
				raw = value
				_, err = client.Credit(context.Background(), req)
				require.Error(t, err)
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, saved, after)
			}
			client.Refresh = false
			cached, err := client.Credit(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, good, cached)
			raw = creditSample(t, metric)
			for _, mutate := range []func(*csvCache){func(c *csvCache) { c.CSV = "broken" }, func(c *csvCache) { c.FetchedAt = time.Now().Add(-7 * time.Hour) }, func(c *csvCache) { c.FetchedAt = time.Now().Add(time.Hour) }, func(c *csvCache) { c.Source = "https://wrong.invalid" }} {
				var entry csvCache
				require.NoError(t, json.Unmarshal(saved, &entry))
				mutate(&entry)
				data, err := json.Marshal(entry)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, data, 0600))
				before := calls
				_, err = client.Credit(context.Background(), req)
				require.NoError(t, err)
				require.Equal(t, before+1, calls)
			}
			client.CacheDir = ""
			before := calls
			for i := 0; i < 2; i++ {
				_, err = client.Credit(context.Background(), req)
				require.NoError(t, err)
			}
			require.Equal(t, before+2, calls)
		})
	}
}

func TestCreditValidationAndCancellation(t *testing.T) {
	for _, req := range []*CreditRequest{nil, {Begin: "202601", End: "202601", Metric: "other"}, {Begin: "194612", End: "194701", Metric: "bank"}, {Begin: "1990Q1", End: "1990Q2", Metric: "standards"}, {Begin: "2004Q4", End: "2005Q1", Metric: "debt-service"}, {Begin: "2026Q0", End: "2026Q1", Metric: "standards"}, {Begin: "2026Q2", End: "2026Q1", Metric: "debt-service"}, {Begin: "2026Q1", End: "2999Q1", Metric: "standards"}, {Begin: "202613", End: "202613", Metric: "bank"}, {Begin: "202602", End: "202601", Metric: "bank"}, {Begin: "202601", End: "299901", Metric: "bank"}} {
		_, err := NewClient("").Credit(context.Background(), req)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").Credit(ctx, &CreditRequest{Begin: "202609", End: "202609", Metric: "bank"})
	require.ErrorIs(t, err, context.Canceled)
}
