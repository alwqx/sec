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

func realIncomeSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/real-income.csv")
	require.NoError(t, err)
	return string(raw)
}

func TestRealIncomeOfficialValuesAndCache(t *testing.T) {
	raw := realIncomeSample(t)
	var calls atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "DSPIC96", r.URL.Query().Get("id"))
		require.Equal(t, "curl", r.UserAgent())
		if fail.Load() {
			http.Error(w, "failed", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.incomeEndpoint = server.URL + "?id=DSPIC96"
	req := &RealIncomeRequest{Begin: "202608", End: "202609"}
	got, err := client.RealIncome(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got.Observations, 2)
	require.Equal(t, "US", got.Region)
	require.Equal(t, "real-income", got.Indicator)
	require.Equal(t, "DSPIC96", got.SeriesID)
	require.Equal(t, "A067RX", got.BEAAccountCode)
	require.Equal(t, "monthly", got.Frequency)
	require.Equal(t, "real-disposable-personal-income", got.Measure)
	require.Equal(t, "十亿美元（2017年链式美元）", got.Unit)
	require.Equal(t, 2017, got.ReferenceYear)
	require.True(t, got.SeasonallyAdjusted)
	require.True(t, got.LevelAnnualized)
	require.False(t, got.GrowthAnnualized)
	require.Equal(t, "BEA", got.Producer)
	require.Equal(t, "FRED", got.Distributor)
	require.Equal(t, realIncomeCSVURL, got.Source)
	require.Equal(t, "https://fred.stlouisfed.org/series/DSPIC96", got.SeriesSource)
	require.Equal(t, "https://www.bea.gov/data/income-saving/personal-income", got.OriginalSource)
	require.Equal(t, "latest-available", got.DataVersion)
	require.Nil(t, got.PublishedAt)
	require.Nil(t, got.RevisionID)
	require.False(t, got.FetchedAt.IsZero())
	aug := got.Observations[0]
	require.Equal(t, 18410.5, *aug.Level)
	require.Equal(t, "18410.5", aug.RawValue)
	require.Equal(t, "202607", aug.MoMBase.Period)
	require.Equal(t, 18415.4, *aug.MoMBase.Level)
	require.Equal(t, "202508", aug.YoYBase.Period)
	require.Equal(t, 18174.0, *aug.YoYBase.Level)
	require.InDelta(t, (18410.5/18415.4-1)*100, *aug.MoM, 1e-10)
	require.InDelta(t, (18410.5/18174.0-1)*100, *aug.YoY, 1e-10)
	require.Nil(t, got.Observations[1].Level)
	require.Nil(t, got.Observations[1].MoM)
	require.Nil(t, got.Observations[1].YoY)
	first, err := client.RealIncome(context.Background(), &RealIncomeRequest{Begin: "195901", End: "195901"})
	require.NoError(t, err)
	require.Equal(t, 2318.4, *first.Observations[0].Level)
	require.Nil(t, first.Observations[0].YoY)
	require.Nil(t, first.Observations[0].MoM)
	require.Equal(t, got.FetchedAt, first.FetchedAt)
	require.EqualValues(t, 1, calls.Load(), "不同查询范围共享已校验快照")
	client.Refresh = true
	_, err = client.RealIncome(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	path := filepath.Join(client.CacheDir, "real-income.json")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	fail.Store(true)
	got, err = client.RealIncome(context.Background(), req)
	require.ErrorContains(t, err, "503")
	require.Nil(t, got)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "刷新失败保留已有缓存，但不能回退返回它")
	fail.Store(false)
	client.Refresh = false
	for _, cache := range []csvCache{
		{Source: client.incomeEndpoint, FetchedAt: time.Now().Add(-7 * time.Hour), CSV: raw},
		{Source: client.incomeEndpoint, FetchedAt: time.Now(), CSV: "invalid"},
		{Source: client.incomeEndpoint + "other", FetchedAt: time.Now(), CSV: raw},
	} {
		data, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0600))
		_, err = client.RealIncome(context.Background(), req)
		require.NoError(t, err)
	}
	client.CacheDir = ""
	_, err = client.RealIncome(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 7, calls.Load())
}

func TestRealIncomeMissingZeroAndStrictBaselines(t *testing.T) {
	raw := "DATE,DSPIC96\n2024-01-01,100\n2025-01-01,0\n2025-02-01,110\n2025-03-01,.\n2025-04-01,120\n2025-06-01,130\n2025-07-01,\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer server.Close()
	client := NewClient("")
	client.incomeEndpoint = server.URL
	got, err := client.RealIncome(context.Background(), &RealIncomeRequest{Begin: "202501", End: "202507"})
	require.NoError(t, err)
	require.Len(t, got.Observations, 7)
	require.Zero(t, *got.Observations[0].Level)
	require.Equal(t, -100.0, *got.Observations[0].YoY)
	require.Nil(t, got.Observations[1].MoM, "零基期不能计算百分比")
	require.Zero(t, *got.Observations[1].MoMBase.Level)
	require.Nil(t, got.Observations[2].Level)
	require.Equal(t, ".", got.Observations[2].RawValue)
	require.Nil(t, got.Observations[3].MoM, "不跳过显式缺失月份")
	require.Nil(t, got.Observations[4].Level)
	require.Equal(t, "202505", got.Observations[4].Period)
	require.Nil(t, got.Observations[5].MoM, "不跳过源中缺少的月份")
	require.Equal(t, "202505", got.Observations[5].MoMBase.Period)
	require.Nil(t, got.Observations[5].YoY, "不能替代缺失的去年同月")
	require.Nil(t, got.Observations[6].Level)
}

func TestRealIncomeCSVAndRequests(t *testing.T) {
	values, err := parseRealIncome(realIncomeSample(t))
	require.NoError(t, err)
	require.Len(t, values, 812)
	valid := "observation_date,DSPIC96\n2025-01-01,100\n"
	for _, raw := range []string{
		"<html>error</html>", "observation_date,DSPIC96\n", "DATE,DATE\n2025-01-01,100\n",
		strings.Replace(valid, "DSPIC96", "DSPI", 1), strings.Replace(valid, "2025-01-01", "2025-01-02", 1),
		strings.Replace(valid, "2025-01-01", "1958-12-01", 1), valid + "2025-01-01,200\n", valid + "2025-02-01\n",
		strings.Replace(valid, "100", "NaN", 1), strings.Replace(valid, "100", "+Inf", 1), strings.Replace(valid, "100", "-1", 1),
	} {
		_, err := parseRealIncome(raw)
		require.Error(t, err, raw)
	}
	values, err = parseRealIncome("\ufeffDSPIC96,DATE\n0,1959-01-01\n")
	require.NoError(t, err)
	require.Zero(t, *values["195901"].Level)
	client := NewClient("")
	for _, req := range []*RealIncomeRequest{nil, {Begin: "195812", End: "195901"}, {Begin: "202513", End: "202601"}, {Begin: "202502", End: "202501"}, {Begin: "202501", End: "299901"}, {Begin: "20251", End: "202501"}} {
		got, err := client.RealIncome(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.RealIncome(ctx, &RealIncomeRequest{Begin: "202501", End: "202502"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

func TestRealIncomeCacheIsolation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/income" {
			_, _ = w.Write([]byte(realIncomeSample(t)))
		} else {
			_, _ = w.Write([]byte(realPCESample(t)))
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.incomeEndpoint, client.realPCEEndpoint = server.URL+"/income", server.URL+"/pce"
	for i := 0; i < 2; i++ {
		income, err := client.RealIncome(context.Background(), &RealIncomeRequest{Begin: "202608", End: "202608"})
		require.NoError(t, err)
		require.Equal(t, 18410.5, *income.Observations[0].Level)
		pce, err := client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "202608", End: "202608"})
		require.NoError(t, err)
		require.Equal(t, 16955.3, *pce.Observations[0].Level)
	}
	require.EqualValues(t, 2, calls.Load())
}

func TestRealIncomeRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := NewClient("")
	client.incomeEndpoint = server.URL
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.RealIncome(ctx, &RealIncomeRequest{Begin: "202608", End: "202608"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("收入请求未到达测试服务器")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("取消上下文未中止收入请求")
	}
}
