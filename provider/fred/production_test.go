package fred

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func productionSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/production.csv")
	require.NoError(t, err)
	return string(raw)
}

func TestProductionOfficialSeriesAndCache(t *testing.T) {
	raw := productionSample(t)
	var calls atomic.Int32
	var failed atomic.Bool
	var invalid atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("id") != "INDPRO,IPMAN,TCU,MCUMFN" || r.UserAgent() != "curl" {
			http.Error(w, "bad request", 400)
			return
		}
		if failed.Load() {
			http.Error(w, "failed", 503)
			return
		}
		if invalid.Load() {
			_, _ = w.Write([]byte("observation_date,INDPRO,IPMAN,TCU\n2024-01-01,100,90,70\n"))
			return
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.productionEndpoint = server.URL + "?id=INDPRO,IPMAN,TCU,MCUMFN"
	for _, test := range []struct {
		indicator, sector, id, start string
		current, previous, yearAgo   float64
	}{
		{"industrial-production", "total", "INDPRO", "191901", 100.2850, 99.2223, 100.6416},
		{"industrial-production", "manufacturing", "IPMAN", "197201", 96.9553, 95.7457, 97.4821},
		{"capacity-utilization", "total", "TCU", "196701", 76.5157, 75.7542, 77.6078},
		{"capacity-utilization", "manufacturing", "MCUMFN", "197201", 76.0838, 75.1799, 76.9753},
	} {
		t.Run(test.id, func(t *testing.T) {
			got, err := client.Production(context.Background(), &ProductionRequest{Indicator: test.indicator, Sector: test.sector, Begin: "202402", End: "202403"})
			require.NoError(t, err)
			require.Len(t, got.Observations, 2)
			require.Equal(t, test.id, got.SeriesID)
			require.Equal(t, test.start, got.FirstPeriod)
			require.Equal(t, "monthly", got.Frequency)
			require.Equal(t, "US", got.Region)
			require.True(t, got.SeasonallyAdjusted)
			require.False(t, got.GrowthAnnualized)
			require.Equal(t, "latest-available", got.DataVersion)
			require.Nil(t, got.PublishedAt)
			require.Nil(t, got.RevisionID)
			require.Equal(t, productionCSVURL, got.Source)
			require.Equal(t, "https://fred.stlouisfed.org/series/"+test.id, got.SeriesSource)
			require.False(t, got.FetchedAt.IsZero())
			item := got.Observations[0]
			require.Equal(t, test.current, *item.Level)
			require.NotEmpty(t, item.RawValue)
			require.Equal(t, "202401", item.MoMBase.Period)
			require.Equal(t, "202302", item.YoYBase.Period)
			require.Equal(t, test.previous, *item.MoMBase.Level)
			require.Equal(t, test.yearAgo, *item.YoYBase.Level)
			if test.indicator == "industrial-production" {
				require.Equal(t, "指数（2017=100）", got.Unit)
				require.Equal(t, "percent", got.ChangeUnit)
				require.Equal(t, 2017, *got.ReferenceYear)
				require.InDelta(t, (test.current/test.previous-1)*100, *item.MoM, 1e-10)
				require.InDelta(t, (test.current/test.yearAgo-1)*100, *item.YoY, 1e-10)
				require.Nil(t, item.MoMPercentagePoints)
				require.Nil(t, item.YoYPercentagePoints)
			} else {
				require.Equal(t, "百分比", got.Unit)
				require.Equal(t, "percentage-points", got.ChangeUnit)
				require.Nil(t, got.ReferenceYear)
				require.InDelta(t, test.current-test.previous, *item.MoMPercentagePoints, 1e-10)
				require.InDelta(t, test.current-test.yearAgo, *item.YoYPercentagePoints, 1e-10)
				require.Nil(t, item.MoM)
				require.Nil(t, item.YoY)
			}
			if test.sector == "manufacturing" {
				require.Contains(t, got.Coverage, "NAICS 31–33")
			}
			first, err := client.Production(context.Background(), &ProductionRequest{Indicator: test.indicator, Sector: test.sector, Begin: test.start, End: test.start})
			require.NoError(t, err)
			require.NotNil(t, first.Observations[0].Level)
			require.Nil(t, first.Observations[0].MoMBase.Level)
			require.Nil(t, first.Observations[0].YoYBase.Level)
		})
	}
	require.EqualValues(t, 1, calls.Load(), "四个系列及不同范围共用完整快照")
	req := &ProductionRequest{Indicator: "industrial-production", Sector: "total", Begin: "202402", End: "202402"}
	client.Refresh = true
	_, err := client.Production(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	cachePath := filepath.Join(client.CacheDir, "production.json")
	before, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	for _, malformed := range []bool{false, true} {
		failed.Store(!malformed)
		invalid.Store(malformed)
		got, err := client.Production(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
		after, err := os.ReadFile(cachePath)
		require.NoError(t, err)
		require.Equal(t, before, after, "网络或校验失败不覆盖旧缓存")
	}
	failed.Store(false)
	invalid.Store(false)
	client.Refresh = false
	for _, cache := range []csvCache{
		{Source: client.productionEndpoint, FetchedAt: time.Now().Add(-7 * time.Hour), CSV: raw},
		{Source: client.productionEndpoint, FetchedAt: time.Now().Add(time.Hour), CSV: raw},
		{Source: client.productionEndpoint, FetchedAt: time.Now(), CSV: "invalid"},
		{Source: "wrong-source", FetchedAt: time.Now(), CSV: raw},
	} {
		data, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(cachePath, data, 0600))
		_, err = client.Production(context.Background(), req)
		require.NoError(t, err)
	}
	client.CacheDir = ""
	_, err = client.Production(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 9, calls.Load())
}

func TestProductionStrictPeriodsMissingAndZero(t *testing.T) {
	raw := "DATE,MCUMFN,TCU,IPMAN,INDPRO\n2024-01-01,80,70,50,100\n2025-01-01,0,0,60,0\n2025-02-01,101,102,.,110\n2025-03-01,102,103,70,120\n2025-05-01,103,104,80,130\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer server.Close()
	client := NewClient("")
	client.productionEndpoint = server.URL
	for _, indicator := range []string{"industrial-production", "capacity-utilization"} {
		got, err := client.Production(context.Background(), &ProductionRequest{Indicator: indicator, Sector: "total", Begin: "202501", End: "202505"})
		require.NoError(t, err)
		require.Len(t, got.Observations, 5)
		require.Zero(t, *got.Observations[0].Level, "零值不视为缺失")
		require.Nil(t, got.Observations[3].Level)
		require.Nil(t, got.Observations[4].MoMBase.Level, "四月缺失不能跨到三月")
		require.Equal(t, "202504", got.Observations[4].MoMBase.Period)
		require.Nil(t, got.Observations[1].YoYBase.Level, "上年同月缺失不使用上年一月")
		if indicator == "industrial-production" {
			require.Equal(t, -100.0, *got.Observations[0].YoY)
			require.Nil(t, got.Observations[1].MoM, "指数零基期不能计算增长")
			require.Contains(t, got.Observations[1].Note, "基期为零")
			require.Nil(t, got.Observations[4].MoM)
		} else {
			require.Equal(t, -70.0, *got.Observations[0].YoYPercentagePoints)
			require.Equal(t, 102.0, *got.Observations[1].MoMPercentagePoints, "利用率零基期仍能计算百分点差，且不人为封顶100")
			require.Nil(t, got.Observations[4].MoMPercentagePoints)
		}
	}
	got, err := client.Production(context.Background(), &ProductionRequest{Indicator: "industrial-production", Sector: "manufacturing", Begin: "202501", End: "202503"})
	require.NoError(t, err)
	require.Equal(t, 60.0, *got.Observations[0].Level, "直接用制造业系列，不对总量作差或加总")
	require.Nil(t, got.Observations[1].Level)
	require.Equal(t, ".", got.Observations[1].RawValue)
	require.Nil(t, got.Observations[2].MoM)
}

func TestProductionCSVValidationAndCoverage(t *testing.T) {
	values, err := parseProduction(productionSample(t))
	require.NoError(t, err)
	require.Len(t, values["INDPRO"], 1293)
	require.Equal(t, 4.8739, *values["INDPRO"]["191901"].Level)
	require.Nil(t, values["IPMAN"]["191901"].Level)
	require.Nil(t, values["TCU"]["196612"].Level)
	require.NotNil(t, values["TCU"]["196701"].Level)
	require.Nil(t, values["MCUMFN"]["197112"].Level)
	require.NotNil(t, values["MCUMFN"]["197201"].Level)
	valid := "observation_date,INDPRO,IPMAN,TCU,MCUMFN\n2025-01-01,100,90,70,60\n"
	for _, raw := range []string{
		"<html>error</html>", strings.Split(valid, "\n")[0] + "\n",
		"observation_date,INDPRO,IPMAN,TCU\n2025-01-01,100,90,70\n",
		strings.Replace(valid, "MCUMFN", "CUMF", 1), strings.Replace(valid, "TCU", "IPMAN", 1),
		strings.Replace(valid, "2025-01-01", "2025-01-02", 1), strings.Replace(valid, "2025-01-01", "1918-12-01", 1),
		strings.Replace(valid, "2025-01-01", "1971-12-01", 1),
		strings.Replace(valid, ",100,", ",NaN,", 1), strings.Replace(valid, ",100,", ",+Inf,", 1),
		strings.Replace(valid, ",100,", ",-1,", 1), strings.Replace(valid, ",100,", ",bad,", 1),
		strings.Replace(valid, ",90,", ",.,", 1),
		valid + "2025-01-01,100,90,70,60\n", valid + "2025-02-01,100\n",
	} {
		_, err := parseProduction(raw)
		require.Error(t, err, raw)
	}
}

func TestProductionRequestsAndCancellation(t *testing.T) {
	client := NewClient("")
	for _, req := range []*ProductionRequest{
		nil, {Indicator: "bad", Sector: "total"}, {Indicator: "industrial-production", Sector: "bad"},
		{Indicator: "industrial-production", Sector: "total", Begin: "191812", End: "191901"},
		{Indicator: "industrial-production", Sector: "manufacturing", Begin: "197112", End: "197201"},
		{Indicator: "capacity-utilization", Sector: "total", Begin: "196612", End: "196701"},
		{Indicator: "capacity-utilization", Sector: "manufacturing", Begin: "197112", End: "197201"},
		{Indicator: "industrial-production", Sector: "total", Begin: "202513", End: "202601"},
		{Indicator: "industrial-production", Sector: "total", Begin: "202502", End: "202501"},
		{Indicator: "industrial-production", Sector: "total", Begin: "202501", End: "299901"},
	} {
		got, err := client.Production(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.Production(ctx, &ProductionRequest{Indicator: "industrial-production", Sector: "total", Begin: "202501", End: "202501"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}
