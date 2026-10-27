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

func pceSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/pce-prices.csv")
	require.NoError(t, err)
	return string(raw)
}

func TestPCEOfficialDataAndCache(t *testing.T) {
	raw := pceSample(t)
	var calls atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/graph/fredgraph.csv", r.URL.Path)
		require.Equal(t, "PCEPI,PCEPILFE", r.URL.Query().Get("id"))
		require.Equal(t, "curl", r.UserAgent())
		if fail.Load() {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write([]byte(raw))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.endpoint = server.URL + "/graph/fredgraph.csv?id=PCEPI,PCEPILFE"
	req := &PCERequest{Indicator: "pce", Begin: "202501", End: "202609"}
	result, err := client.PCE(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 21)
	require.True(t, result.SeasonallyAdjusted)
	require.Equal(t, "chain-type-price-index", result.Measure)
	require.Equal(t, "PCEPI", result.SeriesID)
	require.Equal(t, "DPCERG", result.BEAAccountCode)
	require.Equal(t, "BEA", result.Producer)
	require.Equal(t, "FRED", result.Distributor)
	aug := result.Observations[19]
	require.Equal(t, "202608", aug.Period)
	require.Equal(t, 131.579, *aug.Index)
	require.Equal(t, "131.579", aug.RawIndex)
	require.InDelta(t, (131.579/127.229-1)*100, *aug.YoY, 1e-10)
	require.InDelta(t, (131.579/131.172-1)*100, *aug.MoM, 1e-10)
	require.Equal(t, "202508", aug.YoYBase.Period)
	require.Equal(t, "202607", aug.MoMBase.Period)
	require.Nil(t, result.Observations[20].Index)
	require.Nil(t, result.Observations[20].YoY)
	require.Nil(t, result.Observations[20].MoM)
	require.Nil(t, result.PublishedAt)
	require.Nil(t, result.RevisionID)
	require.False(t, result.FetchedAt.IsZero())
	core, err := client.PCE(context.Background(), &PCERequest{Indicator: "core-pce", Begin: "202608", End: "202608"})
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "两个指标和不同月份复用同一快照")
	require.Equal(t, "PCEPILFE", core.SeriesID)
	require.Equal(t, "DPCCRG", core.BEAAccountCode)
	require.Equal(t, 130.455, *core.Observations[0].Index)
	require.InDelta(t, (130.455/126.646-1)*100, *core.Observations[0].YoY, 1e-10)
	require.InDelta(t, (130.455/130.133-1)*100, *core.Observations[0].MoM, 1e-10)
	require.Equal(t, result.FetchedAt, core.FetchedAt)
	client.Refresh = true
	_, err = client.PCE(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	fail.Store(true)
	result, err = client.PCE(context.Background(), req)
	require.ErrorContains(t, err, "503")
	require.Nil(t, result)
	client.Refresh = false
	_, err = client.PCE(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 3, calls.Load())
	fail.Store(false)
	path := filepath.Join(client.CacheDir, "pce-prices.json")
	for _, cache := range []pceCache{{Source: client.endpoint, FetchedAt: time.Now().Add(-7 * time.Hour), CSV: raw}, {Source: client.endpoint, FetchedAt: time.Now(), CSV: "<html>error</html>"}} {
		data, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0600))
		_, err = client.PCE(context.Background(), req)
		require.NoError(t, err)
	}
	require.EqualValues(t, 5, calls.Load())
	client.CacheDir = ""
	_, err = client.PCE(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 6, calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.PCE(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 6, calls.Load())
}

func TestPCECSV(t *testing.T) {
	values, err := parsePCE(pceSample(t))
	require.NoError(t, err)
	require.Len(t, values["PCEPI"], 812)
	require.Len(t, values["PCEPILFE"], 812)
	require.Equal(t, 15.164, *values["PCEPI"]["195901"].Index)
	values, err = parsePCE("PCEPILFE,DATE,PCEPI\r\n.,2025-01-01,0\r\n123,2025-02-01,\r\n")
	require.NoError(t, err)
	require.Zero(t, *values["PCEPI"]["202501"].Index)
	require.Nil(t, values["PCEPI"]["202502"].Index)
	require.Nil(t, values["PCEPILFE"]["202501"].Index)
	valid := "observation_date,PCEPI,PCEPILFE\n2025-01-01,100,101\n"
	for _, raw := range []string{"<html>error</html>", "observation_date,PCEPI\n", strings.Replace(valid, "PCEPILFE", "PCE", 1), strings.Replace(valid, "PCEPILFE", "PCEPI", 1), strings.Replace(valid, "2025-01-01", "2025-01-02", 1), strings.Replace(valid, "2025-01-01", "2025-02-30", 1), strings.Replace(valid, ",100,", ",NaN,", 1), strings.Replace(valid, ",100,", ",-1,", 1), valid + "2025-01-01,102,103\n", valid + "2025-02-01,100\n", "observation_date,PCEPI,PCEPILFE\n"} {
		_, err := parsePCE(raw)
		require.Error(t, err, raw)
	}
}

func TestPCEMissingBaselines(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("observation_date,PCEPI,PCEPILFE\n2024-01-01,100,100\n2025-01-01,110,111\n2025-02-01,.,\n2025-03-01,120,121\n"))
	}))
	defer server.Close()
	client := NewClient("")
	client.endpoint = server.URL
	result, err := client.PCE(context.Background(), &PCERequest{Indicator: "pce", Begin: "202501", End: "202503"})
	require.NoError(t, err)
	require.InDelta(t, 10, *result.Observations[0].YoY, 1e-10)
	require.Nil(t, result.Observations[0].MoM)
	require.Nil(t, result.Observations[1].Index)
	require.Nil(t, result.Observations[2].MoM, "不以1月替代缺失2月")
	require.Equal(t, "202502", result.Observations[2].MoMBase.Period)
	zero, one := 0.0, 1.0
	require.Nil(t, pceChange(&one, &zero))
	require.Equal(t, -100.0, *pceChange(&zero, &one))
}

func TestPCEInvalidRequests(t *testing.T) {
	for _, req := range []*PCERequest{nil, {Indicator: "pce", Begin: "195812", End: "195901"}, {Indicator: "consumption", Begin: "202501", End: "202502"}, {Indicator: "pce", Begin: "202502", End: "202501"}, {Indicator: "pce", Begin: "202513", End: "202601"}, {Indicator: "pce", Begin: "202501", End: "299901"}} {
		result, err := NewClient("").PCE(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, result)
	}
}
