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

func realPCESample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/real-pce.csv")
	require.NoError(t, err)
	return string(raw)
}

func TestRealPCEOfficialComponentsAndCache(t *testing.T) {
	raw := realPCESample(t)
	var calls atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "PCEC96,DGDSRX1,PCEDGC96,PCENDC96,PCESC96", r.URL.Query().Get("id"))
		require.Equal(t, "curl", r.UserAgent())
		if fail.Load() {
			http.Error(w, "failed", 503)
			return
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.realPCEEndpoint = server.URL + "?id=PCEC96,DGDSRX1,PCEDGC96,PCENDC96,PCESC96"
	for _, test := range []struct {
		component, id, code          string
		level, previous, yearEarlier float64
	}{
		{"total", "PCEC96", "DPCERX", 16955.3, 16862.5, 16529.7},
		{"goods", "DGDSRX1", "DGDSRX", 5803.7, 5727.3, 5648.8},
		{"durable-goods", "PCEDGC96", "DDURRX", 2219.9, 2177.9, 2122.0},
		{"nondurable-goods", "PCENDC96", "DNDGRX", 3598.8, 3562.6, 3536.7},
		{"services", "PCESC96", "DSERRX", 11177.8, 11155.6, 10905.4},
	} {
		got, err := client.RealPCE(context.Background(), &RealPCERequest{Component: test.component, Begin: "202608", End: "202609"})
		require.NoError(t, err)
		require.Len(t, got.Observations, 2)
		require.Equal(t, test.id, got.SeriesID)
		require.Equal(t, test.code, got.BEAAccountCode)
		require.Equal(t, "real-pce", got.Indicator)
		require.Equal(t, 2017, got.ReferenceYear)
		require.Equal(t, "十亿美元（2017年链式美元）", got.Unit)
		require.True(t, got.SeasonallyAdjusted)
		require.True(t, got.LevelAnnualized)
		require.False(t, got.GrowthAnnualized)
		require.False(t, got.ComponentsAdditive)
		require.Nil(t, got.PublishedAt)
		require.Nil(t, got.RevisionID)
		require.Equal(t, "latest-available", got.DataVersion)
		aug := got.Observations[0]
		require.Equal(t, test.level, *aug.Level)
		require.Equal(t, test.previous, *aug.MoMBase.Level)
		require.Equal(t, "202607", aug.MoMBase.Period)
		require.Equal(t, test.yearEarlier, *aug.YoYBase.Level)
		require.Equal(t, "202508", aug.YoYBase.Period)
		require.InDelta(t, (test.level/test.previous-1)*100, *aug.MoM, 1e-10)
		require.InDelta(t, (test.level/test.yearEarlier-1)*100, *aug.YoY, 1e-10)
		require.Nil(t, got.Observations[1].Level)
		require.Nil(t, got.Observations[1].MoM)
	}
	require.EqualValues(t, 1, calls.Load(), "五个分项与不同范围共享快照")
	got, err := client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "200701", End: "200701"})
	require.NoError(t, err)
	require.Equal(t, 11181.0, *got.Observations[0].Level)
	require.Nil(t, got.Observations[0].MoM)
	require.Nil(t, got.Observations[0].YoY)
	require.EqualValues(t, 1, calls.Load())
	client.Refresh = true
	_, err = client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "202608", End: "202608"})
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	path := filepath.Join(client.CacheDir, "real-pce.json")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	fail.Store(true)
	got, err = client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "202608", End: "202608"})
	require.ErrorContains(t, err, "503")
	require.Nil(t, got)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	fail.Store(false)
	client.Refresh = false
	for _, cache := range []csvCache{{Source: client.realPCEEndpoint, FetchedAt: time.Now().Add(-7 * time.Hour), CSV: raw}, {Source: client.realPCEEndpoint, FetchedAt: time.Now(), CSV: "invalid"}} {
		data, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0600))
		_, err = client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "202608", End: "202608"})
		require.NoError(t, err)
	}
	client.CacheDir = ""
	_, err = client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "202608", End: "202608"})
	require.NoError(t, err)
	require.EqualValues(t, 6, calls.Load())
}

func TestRealPCEGapsZeroAndComponentIndependence(t *testing.T) {
	raw := "DATE,PCESC96,PCENDC96,PCEDGC96,DGDSRX1,PCEC96\n2024-01-01,50,20,20,40,100\n2025-01-01,60,20,20,50,0\n2025-02-01,70,20,20,.,110\n2025-03-01,80,20,20,60,120\n2025-05-01,90,20,20,60,130\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer server.Close()
	client := NewClient("")
	client.realPCEEndpoint = server.URL
	got, err := client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "202501", End: "202505"})
	require.NoError(t, err)
	require.Zero(t, *got.Observations[0].Level)
	require.Equal(t, -100.0, *got.Observations[0].YoY)
	require.Nil(t, got.Observations[1].MoM, "零基期不能计算百分比")
	require.NotNil(t, got.Observations[2].MoM)
	require.Nil(t, got.Observations[3].Level)
	require.Nil(t, got.Observations[4].MoM, "不跨越缺失的四月")
	goods, err := client.RealPCE(context.Background(), &RealPCERequest{Component: "goods", Begin: "202501", End: "202503"})
	require.NoError(t, err)
	require.Equal(t, 50.0, *goods.Observations[0].Level, "商品直接取官方序列，不相加耐用品与非耐用品")
	require.Nil(t, goods.Observations[1].Level)
	require.Nil(t, goods.Observations[2].MoM, "不以分项和填补缺口")
}

func TestRealPCECSVAndRequests(t *testing.T) {
	values, err := parseRealPCE(realPCESample(t))
	require.NoError(t, err)
	require.Len(t, values["PCEC96"], 236)
	valid := "observation_date,PCEC96,DGDSRX1,PCEDGC96,PCENDC96,PCESC96\n2025-01-01,100,40,10,30,60\n"
	for _, raw := range []string{"<html>error</html>", "observation_date,PCEC96\n", strings.Replace(valid, "PCEC96", "PCEPI", 1), strings.Replace(valid, "PCESC96", "PCEC96", 1), strings.Replace(valid, "2025-01-01", "2025-01-02", 1), strings.Replace(valid, "2025-01-01", "2006-12-01", 1), strings.Replace(valid, ",100,", ",NaN,", 1), strings.Replace(valid, ",100,", ",-1,", 1), valid + "2025-01-01,100,40,10,30,60\n", valid + "2025-02-01,100\n", strings.Split(valid, "\n")[0] + "\n"} {
		_, err := parseRealPCE(raw)
		require.Error(t, err, raw)
	}
	client := NewClient("")
	for _, req := range []*RealPCERequest{nil, {Component: "bad", Begin: "202501", End: "202502"}, {Component: "total", Begin: "200612", End: "200701"}, {Component: "total", Begin: "202513", End: "202601"}, {Component: "total", Begin: "202502", End: "202501"}, {Component: "goods", Begin: "202501", End: "299901"}} {
		got, err := client.RealPCE(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.RealPCE(ctx, &RealPCERequest{Component: "total", Begin: "202501", End: "202502"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

func TestRealPCECacheSeparateFromPricesAndClaims(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/prices":
			_, _ = w.Write([]byte(pceSample(t)))
		case "/claims":
			_, _ = w.Write([]byte(claimsSample(t)))
		default:
			_, _ = w.Write([]byte(realPCESample(t)))
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.endpoint = server.URL + "/prices"
	client.claimsEndpoint = server.URL + "/claims"
	client.realPCEEndpoint = server.URL + "/real"
	for i := 0; i < 2; i++ {
		_, err := client.PCE(context.Background(), &PCERequest{Indicator: "pce", Begin: "202608", End: "202608"})
		require.NoError(t, err)
		_, err = client.Claims(context.Background(), &ClaimsRequest{Indicator: "initial-claims", Begin: "20261017", End: "20261017"})
		require.NoError(t, err)
		got, err := client.RealPCE(context.Background(), &RealPCERequest{Component: "total", Begin: "202608", End: "202608"})
		require.NoError(t, err)
		require.Equal(t, 16955.3, *got.Observations[0].Level)
	}
	require.EqualValues(t, 3, calls.Load())
}
