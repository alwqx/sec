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

func enterpriseSample(t *testing.T, metric string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/us-enterprise-" + metric + ".csv")
	require.NoError(t, err)
	return string(raw)
}

func TestEnterpriseOfficialValuesAndCache(t *testing.T) {
	for _, test := range []struct {
		metric, begin, end, first, id string
		level                         float64
	}{
		{"profit", "2026Q2", "2026Q3", "1947Q1", "CPROFIT", 4709.453},
		{"inventory", "202608", "202609", "199201", "BUSINV", 2818757},
		{"new-orders", "202608", "202609", "199202", "AMTMNO", 663491},
	} {
		t.Run(test.metric, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "curl", r.UserAgent())
				_, _ = w.Write([]byte(enterpriseSample(t, test.metric)))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.enterpriseEndpoints[test.metric] = server.URL
			req := &EnterpriseRequest{Begin: test.begin, End: test.end, Metric: test.metric}
			got, err := client.Enterprise(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, test.id, got.SeriesID)
			require.Equal(t, test.first, got.FirstPeriod)
			require.Equal(t, test.begin, got.LastPeriod)
			require.True(t, got.SeasonallyAdjusted)
			require.False(t, got.GrowthAnnualized)
			require.Equal(t, "current_dollars", got.PriceBasis)
			require.Equal(t, "current_visible_revision", got.DataVersion)
			require.Nil(t, got.PublishedAt)
			require.Nil(t, got.RevisionID)
			require.Len(t, got.Observations, 2)
			item := got.Observations[0]
			require.Equal(t, test.level, *item.Level)
			require.InDelta(t, (test.level / *item.PreviousBase.Level - 1)*100, *item.PreviousGrowth, 1e-10)
			require.InDelta(t, (test.level / *item.YoYBase.Level - 1)*100, *item.YoY, 1e-10)
			require.Nil(t, got.Observations[1].Level)
			require.Contains(t, got.Observations[1].Note, "当前快照未提供数值")
			if test.metric == "inventory" {
				require.Equal(t, "2026-08-31", *item.AsOfDate)
				require.Equal(t, "month_end_stock", got.Measurement)
			} else {
				require.Nil(t, item.AsOfDate)
			}
			if test.metric == "profit" {
				require.True(t, got.LevelAnnualized)
				require.Equal(t, "before_tax", got.TaxBasis)
				require.Equal(t, "A051RC", got.BEAAccountCode)
				require.Equal(t, "2026Q1", item.PreviousBase.Period)
				require.Equal(t, "2025Q2", item.YoYBase.Period)
			} else {
				require.False(t, got.LevelAnnualized)
				require.Equal(t, "202607", item.PreviousBase.Period)
				require.Equal(t, "202508", item.YoYBase.Period)
			}
			cached, err := client.Enterprise(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, got, cached)
			require.Equal(t, 1, calls)
			earliest, err := client.Enterprise(context.Background(), &EnterpriseRequest{Begin: test.first, End: test.first, Metric: test.metric})
			require.NoError(t, err)
			require.NotNil(t, earliest.Observations[0].Level)
			require.Nil(t, earliest.Observations[0].YoY)
			require.Nil(t, earliest.Observations[0].PreviousGrowth)
			require.Equal(t, 1, calls)
		})
	}
}

func TestEnterpriseCalendarGapsAndLosses(t *testing.T) {
	for _, metric := range []string{"profit", "inventory", "new-orders"} {
		spec, _ := enterpriseMetric(metric)
		date, period, prev, yoy := "2026-08-01", "202608", "202607", "202508"
		if metric == "profit" {
			date, period, prev, yoy = "2026-04-01", "2026Q2", "2026Q1", "2025Q2"
		}
		// 有更早观测但严格基期不存在时，不回退到最近非空值。
		first := "2026-06-01"
		if metric == "profit" {
			first = "2025-10-01"
		}
		raw := "observation_date," + spec.id + "\n" + first + ",10\n" + date + ",20\n"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
		client := NewClient("")
		client.enterpriseEndpoints[metric] = server.URL
		got, err := client.Enterprise(context.Background(), &EnterpriseRequest{Begin: period, End: period, Metric: metric})
		require.NoError(t, err)
		item := got.Observations[0]
		require.Equal(t, prev, item.PreviousBase.Period)
		require.Equal(t, yoy, item.YoYBase.Period)
		require.Nil(t, item.PreviousGrowth)
		require.Nil(t, item.YoY)
		require.NotNil(t, item.Level)
		server.Close()
	}
	rows, err := enterpriseParse("DATE,CPROFIT\n2025-04-01,100\n2026-01-01,-10\n2026-04-01,-50\n", "profit")
	require.NoError(t, err)
	growth, err := enterpriseChange(rows["2026Q2"].Level, rows["2025Q2"].Level)
	require.NoError(t, err)
	require.Equal(t, -150.0, *growth)
	growth, err = enterpriseChange(rows["2026Q2"].Level, rows["2026Q1"].Level)
	require.NoError(t, err)
	require.Nil(t, growth)
	zero := 0.0
	growth, err = enterpriseChange(rows["2026Q2"].Level, &zero)
	require.NoError(t, err)
	require.Nil(t, growth)
	rows, err = enterpriseParse("DATE,BUSINV\n2026-08-01,0\n2026-09-01,.\n", "inventory")
	require.NoError(t, err)
	require.Zero(t, *rows["202608"].Level)
	require.Nil(t, rows["202609"].Level)
	require.Equal(t, ".", rows["202609"].RawLevel)
}

func TestEnterpriseRejectsMalformedSnapshots(t *testing.T) {
	for _, metric := range []string{"profit", "inventory", "new-orders"} {
		original, err := csv.NewReader(strings.NewReader(enterpriseSample(t, metric))).ReadAll()
		require.NoError(t, err)
		for _, mode := range []string{"header", "duplicate-column", "duplicate-period", "invalid-date", "wrong-frequency", "earlier-date", "truncated", "nan", "inf", "all-missing", "overflow", "negative"} {
			t.Run(metric+"/"+mode, func(t *testing.T) {
				rows := make([][]string, len(original))
				for i, row := range original {
					rows[i] = append([]string(nil), row...)
				}
				switch mode {
				case "header":
					rows[0][1] = "WRONG"
				case "duplicate-column":
					rows[0][1] = rows[0][0]
				case "duplicate-period":
					rows = append(rows, rows[1])
				case "invalid-date":
					rows[1][0] = "2026-02-30"
				case "wrong-frequency":
					rows[1][0] = "2026-02-02"
					if metric == "profit" {
						rows[1][0] = "2026-02-01"
					}
				case "earlier-date":
					rows[1][0] = "1946-01-01"
				case "truncated":
					rows[1] = rows[1][:1]
				case "nan":
					rows[1][1] = "NaN"
				case "inf":
					rows[1][1] = "Inf"
				case "all-missing":
					for i := 1; i < len(rows); i++ {
						rows[i][1] = "."
					}
				case "overflow":
					rows[1][1], rows[2][1] = "1e-100", "1e308"
				case "negative":
					rows[1][1] = "-1"
				}
				var out strings.Builder
				require.NoError(t, csv.NewWriter(&out).WriteAll(rows))
				_, err := enterpriseParse(out.String(), metric)
				if mode == "negative" && metric == "profit" {
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
		_, err = enterpriseParse(out.String(), metric)
		require.NoError(t, err)
	}
}

func TestEnterpriseBadRefreshAndCacheRecovery(t *testing.T) {
	for _, metric := range []string{"profit", "inventory", "new-orders"} {
		t.Run(metric, func(t *testing.T) {
			raw := enterpriseSample(t, metric)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; _, _ = w.Write([]byte(raw)) }))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.enterpriseEndpoints[metric] = server.URL
			begin := "202608"
			if metric == "profit" {
				begin = "2026Q2"
			}
			req := &EnterpriseRequest{Begin: begin, End: begin, Metric: metric}
			good, err := client.Enterprise(context.Background(), req)
			require.NoError(t, err)
			path := filepath.Join(client.CacheDir, "us-enterprise-"+metric+".json")
			saved, err := os.ReadFile(path)
			require.NoError(t, err)
			spec, _ := enterpriseMetric(metric)
			first, second := "2026-07-01", "2026-08-01"
			if metric == "profit" {
				first, second = "2026-01-01", "2026-04-01"
			}
			client.Refresh = true
			for _, bad := range []string{"broken", "DATE," + spec.id + "\n" + first + ",1e-100\n" + second + ",1e308\n"} {
				raw = bad
				_, err = client.Enterprise(context.Background(), req)
				require.Error(t, err)
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, saved, after)
			}
			client.Refresh = false
			cached, err := client.Enterprise(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, good, cached)
			raw = enterpriseSample(t, metric)
			for _, mutate := range []func(*csvCache){
				func(c *csvCache) { c.CSV = "broken" },
				func(c *csvCache) { c.FetchedAt = time.Now().Add(time.Hour) },
				func(c *csvCache) { c.FetchedAt = time.Now().Add(-7 * time.Hour) },
				func(c *csvCache) { c.Source = "https://wrong.invalid" },
			} {
				var entry csvCache
				require.NoError(t, json.Unmarshal(saved, &entry))
				mutate(&entry)
				data, err := json.Marshal(entry)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, data, 0600))
				before := calls
				_, err = client.Enterprise(context.Background(), req)
				require.NoError(t, err)
				require.Equal(t, before+1, calls)
			}
			client.CacheDir = ""
			before := calls
			for i := 0; i < 2; i++ {
				_, err = client.Enterprise(context.Background(), req)
				require.NoError(t, err)
			}
			require.Equal(t, before+2, calls)
		})
	}
}

func TestEnterpriseRequestValidationAndCancellation(t *testing.T) {
	for _, req := range []*EnterpriseRequest{nil, {Begin: "202601", End: "202601", Metric: "other"}, {Begin: "199112", End: "199201", Metric: "inventory"}, {Begin: "199201", End: "199202", Metric: "new-orders"}, {Begin: "1946Q4", End: "1947Q1", Metric: "profit"}, {Begin: "2026Q0", End: "2026Q1", Metric: "profit"}, {Begin: "2026Q2", End: "2026Q1", Metric: "profit"}, {Begin: "2026Q1", End: "2999Q1", Metric: "profit"}, {Begin: "202613", End: "202613", Metric: "inventory"}, {Begin: "202602", End: "202601", Metric: "inventory"}, {Begin: "202601", End: "299901", Metric: "new-orders"}} {
		_, err := NewClient("").Enterprise(context.Background(), req)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").Enterprise(ctx, &EnterpriseRequest{Begin: "202608", End: "202608", Metric: "inventory"})
	require.ErrorIs(t, err, context.Canceled)
}
