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

func claimsSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/jobless-claims.csv")
	require.NoError(t, err)
	return string(raw)
}

func TestClaimsOfficialSnapshotAndCache(t *testing.T) {
	raw := claimsSample(t)
	var calls atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "ICSA,CCSA", r.URL.Query().Get("id"))
		require.Equal(t, "curl", r.UserAgent())
		if fail.Load() {
			http.Error(w, "failed", 503)
			return
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.claimsEndpoint = server.URL + "?id=ICSA,CCSA"
	for _, indicator := range []string{"initial-claims", "continued-claims"} {
		got, err := client.Claims(context.Background(), &ClaimsRequest{Indicator: indicator, Begin: "20261001", End: "20261024"})
		require.NoError(t, err)
		require.Len(t, got.Observations, 4)
		require.True(t, got.SeasonallyAdjusted)
		require.Equal(t, "weekly", got.Frequency)
		require.Equal(t, "Saturday", got.WeekEndingDay)
		require.Equal(t, "latest-available", got.DataVersion)
		require.Nil(t, got.PublishedAt)
		require.Nil(t, got.RevisionID)
		require.Equal(t, "2026-10-03", got.Observations[0].WeekEnding)
		require.Nil(t, got.Observations[3].Count, "不填充未发布的周")
		if indicator == "initial-claims" {
			require.Equal(t, "ICSA", got.SeriesID)
			require.Equal(t, "件", got.Unit)
			require.Equal(t, "2026-10-17", got.LatestAvailableWeek)
			last := got.Observations[2]
			require.Equal(t, 194000.0, *last.Count)
			require.Equal(t, "194000", last.RawValue)
			require.Equal(t, -6000.0, *last.WeeklyChange)
			require.Equal(t, 197750.0, *last.FourWeekAverage)
			require.Len(t, last.FourWeekWindow, 4)
			require.Equal(t, "2026-09-26", last.FourWeekWindow[3].WeekEnding)
		} else {
			require.Equal(t, "CCSA", got.SeriesID)
			require.Equal(t, "人", got.Unit)
			require.Equal(t, "2026-10-10", got.LatestAvailableWeek)
			last := got.Observations[1]
			require.Equal(t, 1728000.0, *last.Count)
			require.Equal(t, 25000.0, *last.WeeklyChange)
			require.Equal(t, 1711000.0, *last.FourWeekAverage)
			require.Nil(t, got.Observations[2].Count, "续请不前移到初请最新所属周")
			require.Nil(t, got.Observations[2].FourWeekAverage)
		}
	}
	require.EqualValues(t, 1, calls.Load(), "不同指标共用完整快照")
	_, err := client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "19670107", End: "19670107"})
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "不同范围复用快照")
	client.Refresh = true
	_, err = client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20261017", End: "20261017"})
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	path := filepath.Join(client.CacheDir, "jobless-claims.json")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	fail.Store(true)
	got, err := client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20261017", End: "20261017"})
	require.ErrorContains(t, err, "503")
	require.Nil(t, got)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "失败不能覆盖正常缓存")
	fail.Store(false)
	client.Refresh = false
	for _, cache := range []csvCache{{Source: client.claimsEndpoint, FetchedAt: time.Now().Add(-7 * time.Hour), CSV: raw}, {Source: client.claimsEndpoint, FetchedAt: time.Now(), CSV: "invalid"}, {Source: "other-source", FetchedAt: time.Now(), CSV: raw}} {
		data, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0600))
		_, err = client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20261017", End: "20261017"})
		require.NoError(t, err)
	}
	require.EqualValues(t, 6, calls.Load())
	client.CacheDir = ""
	_, err = client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20261017", End: "20261017"})
	require.NoError(t, err)
	require.EqualValues(t, 7, calls.Load())
}

func TestClaimsGapsCrossYearAndZero(t *testing.T) {
	raw := "CCSA,DATE,ICSA\n100,2025-12-13,10\n200,2025-12-20,20\n300,2025-12-27,30\n400,2026-01-03,40\n500,2026-01-10,.\n600,2026-01-17,60\n700,2026-01-24,0\n800,2026-01-31,80\n900,2026-02-07,90\n1000,2026-02-14,100\n1200,2026-02-28,120\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer server.Close()
	client := NewClient("")
	client.claimsEndpoint = server.URL
	got, err := client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20260103", End: "20260228"})
	require.NoError(t, err)
	require.Len(t, got.Observations, 9)
	jan := got.Observations[0]
	require.Equal(t, 10.0, *jan.WeeklyChange)
	require.Equal(t, 25.0, *jan.FourWeekAverage, "区间外及跨年基期仍参与计算")
	require.Equal(t, "2025-12-13", jan.FourWeekWindow[3].WeekEnding)
	require.Nil(t, got.Observations[1].Count)
	require.Nil(t, got.Observations[2].WeeklyChange, "不跨过缺口计算周变化")
	require.Zero(t, *got.Observations[3].Count)
	require.Equal(t, -60.0, *got.Observations[3].WeeklyChange)
	require.Nil(t, got.Observations[4].FourWeekAverage)
	require.Equal(t, 57.5, *got.Observations[5].FourWeekAverage, "零值有效；四个连续周恢复后计算")
	require.Nil(t, got.Observations[7].Count, "完全未返回的周应保留空行")
	require.Nil(t, got.Observations[8].WeeklyChange)
	require.Nil(t, got.Observations[8].FourWeekAverage)
	got, err = client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20260104", End: "20260109"})
	require.NoError(t, err)
	require.Empty(t, got.Observations, "按周六所属日期筛选，不把周内日期映射到别周")
}

func TestClaimsCSVValidation(t *testing.T) {
	values, err := parseClaims(claimsSample(t))
	require.NoError(t, err)
	require.Equal(t, 208000.0, *values["ICSA"]["19670107"].Count)
	valid := "observation_date,ICSA,CCSA\n2026-01-03,10,100\n"
	for _, raw := range []string{"<html>error</html>", "observation_date,ICSA\n", strings.Replace(valid, "CCSA", "ICNSA", 1), strings.Replace(valid, "CCSA", "ICSA", 1), strings.Replace(valid, "2026-01-03", "2026-01-02", 1), strings.Replace(valid, "2026-01-03", "2026-02-30", 1), strings.Replace(valid, "2026-01-03", "1966-12-31", 1), strings.Replace(valid, ",10,", ",NaN,", 1), strings.Replace(valid, ",10,", ",-1,", 1), strings.Replace(valid, ",10,", ",1.5,", 1), valid + "2026-01-03,20,200\n", valid + "2026-01-10,20\n", "observation_date,ICSA,CCSA\n"} {
		_, err := parseClaims(raw)
		require.Error(t, err, raw)
	}
	values, err = parseClaims("\ufeffCCSA,DATE,ICSA\r\n.,2026-01-03,0\r\n")
	require.NoError(t, err)
	require.Zero(t, *values["ICSA"]["20260103"].Count)
	require.Nil(t, values["CCSA"]["20260103"].Count)
}

func TestClaimsRequestsAndSeriesStart(t *testing.T) {
	client := NewClient("")
	for _, req := range []*ClaimsRequest{nil, {Indicator: "bad", Begin: "20260103", End: "20260110"}, {Indicator: "initial-claims", Begin: "19670106", End: "19670107"}, {Indicator: "initial-claims", Begin: "20260230", End: "20260307"}, {Indicator: "initial-claims", Begin: "20260110", End: "20260103"}, {Indicator: "continued-claims", Begin: "20260103", End: "29990101"}} {
		got, err := client.Claims(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.Claims(ctx, &ClaimsRequest{Indicator: "initial-claims", Begin: "20260103", End: "20260110"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(claimsSample(t))) }))
	defer server.Close()
	client.claimsEndpoint = server.URL
	got, err = client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "19670107", End: "19670128"})
	require.NoError(t, err)
	require.Len(t, got.Observations, 4)
	for i := 0; i < 3; i++ {
		require.Nil(t, got.Observations[i].FourWeekAverage)
	}
	require.NotNil(t, got.Observations[3].FourWeekAverage)
	require.Nil(t, got.Observations[0].WeeklyChange)
}

func TestFREDMetricCacheIsolation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/pce" {
			_, _ = w.Write([]byte(pceSample(t)))
		} else {
			_, _ = w.Write([]byte(claimsSample(t)))
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.endpoint = server.URL + "/pce"
	client.claimsEndpoint = server.URL + "/claims"
	for i := 0; i < 2; i++ {
		pce, err := client.PCE(context.Background(), &PCERequest{Indicator: "pce", Begin: "202608", End: "202608"})
		require.NoError(t, err)
		require.Equal(t, 131.579, *pce.Observations[0].Index)
		claims, err := client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20261017", End: "20261017"})
		require.NoError(t, err)
		require.Equal(t, 194000.0, *claims.Observations[0].Count)
	}
	require.EqualValues(t, 2, calls.Load())
}
