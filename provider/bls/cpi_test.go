package bls

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	require.NoError(t, err)
	return raw
}

func TestCPIOfficialDataAndCache(t *testing.T) {
	raw := sample(t, "cpi-2024-2026")
	var calls atomic.Int32
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/publicAPI/v1/timeseries/data/", r.URL.Path)
		var body struct {
			IDs        []string `json:"seriesid"`
			Start, End string
		}
		var params map[string]json.RawMessage
		require.NoError(t, json.NewDecoder(r.Body).Decode(&params))
		require.NoError(t, json.Unmarshal(params["seriesid"], &body.IDs))
		require.NoError(t, json.Unmarshal(params["startyear"], &body.Start))
		require.NoError(t, json.Unmarshal(params["endyear"], &body.End))
		require.Equal(t, cpiSeriesIDs, body.IDs)
		require.Equal(t, "2024", body.Start)
		require.Equal(t, "2026", body.End)
		if fail.Load() {
			http.Error(w, "quota", 429)
			return
		}
		w.Write(raw)
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.endpoint = server.URL + "/publicAPI/v1/timeseries/data/"
	req := &CPIRequest{Indicator: "cpi", Begin: "202510", End: "202609"}
	result, err := client.CPI(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 12)
	require.Len(t, result.Series, 2)
	require.Len(t, result.Batches, 1)
	first := result.Observations[0]
	require.Equal(t, "202510", first.Period)
	require.NotNil(t, first.NSA)
	require.Nil(t, first.NSA.Index)
	require.Nil(t, first.SA.Index)
	require.Nil(t, first.YoY)
	require.Nil(t, first.MoM)
	require.Equal(t, "-", first.NSA.RawValue)
	require.Equal(t, "X", first.NSA.Footnotes[0].Code)
	require.Contains(t, first.NSA.Footnotes[0].Text, "2025 lapse")
	require.NotNil(t, first.YoYBase.Index)
	// 缺失10月不能使用9月替代11月环比基期。
	require.Nil(t, result.Observations[1].MoM)
	require.NotNil(t, result.Observations[1].SA.Index)
	last := result.Observations[11]
	require.Equal(t, 336.940, *last.NSA.Index)
	require.Equal(t, 336.298, *last.SA.Index)
	require.InDelta(t, (336.940/324.800-1)*100, *last.YoY, 1e-10)
	require.InDelta(t, (336.298/334.131-1)*100, *last.MoM, 1e-10)
	require.Equal(t, "202509", last.YoYBase.Period)
	require.Equal(t, "202608", last.MoMBase.Period)
	require.True(t, last.NSA.Latest)
	require.Empty(t, last.NSA.Footnotes)
	require.False(t, last.NSA.FetchedAt.IsZero())
	require.Nil(t, result.PublishedAt)
	require.Equal(t, "latest-available", result.DataVersion)
	core, err := client.CPI(context.Background(), &CPIRequest{Indicator: "core-cpi", Begin: req.Begin, End: req.End})
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load(), "CPI与核心CPI共享四序列缓存")
	require.Equal(t, 339.287, *core.Observations[11].NSA.Index)
	require.InDelta(t, (339.287/330.804-1)*100, *core.Observations[11].YoY, 1e-10)
	require.InDelta(t, (338.896/337.765-1)*100, *core.Observations[11].MoM, 1e-10)
	require.Equal(t, result.Batches[0].FetchedAt, core.Batches[0].FetchedAt)
	client.Refresh = true
	_, err = client.CPI(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	fail.Store(true)
	result, err = client.CPI(context.Background(), req)
	require.ErrorContains(t, err, "429")
	require.Nil(t, result)
	client.Refresh = false
	_, err = client.CPI(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 3, calls.Load(), "失败不覆盖已有缓存")
	fail.Store(false)
	paths, err := filepath.Glob(filepath.Join(client.CacheDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, paths, 1)
	require.NoError(t, os.WriteFile(paths[0], []byte("invalid"), 0600))
	_, err = client.CPI(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 4, calls.Load())
	for _, cache := range []cachedResponse{
		{FetchedAt: time.Now().Add(-7 * time.Hour), Body: raw},
		{FetchedAt: time.Now(), Body: json.RawMessage(`{"status":"REQUEST_SUCCEEDED","message":[],"Results":{"series":[]}}`)},
	} {
		data, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(paths[0], data, 0600))
		_, err = client.CPI(context.Background(), req)
		require.NoError(t, err)
	}
	require.EqualValues(t, 6, calls.Load(), "过期或缺少系列的缓存必须重新请求")
	client.CacheDir = ""
	_, err = client.CPI(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 7, calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.CPI(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 7, calls.Load())
}

func TestCPIEarlySeries(t *testing.T) {
	response, err := decodeMonthlyBatch(sample(t, "cpi-1913-1914"), cpiSeriesIDs, 1913, 1914)
	require.NoError(t, err)
	require.Len(t, response.Messages, 6)
	require.Len(t, response.Results.Series[0].Data, 24)
	for _, series := range response.Results.Series[1:] {
		require.Empty(t, series.Data)
	}
}

func TestCPIInvalidData(t *testing.T) {
	for _, name := range []string{"missing-series", "duplicate-series", "missing-array", "duplicate-month", "bad-month", "invalid-number", "negative-number", "out-of-range", "quota", "warning", "annual-average"} {
		t.Run(name, func(t *testing.T) {
			var response rawResponse
			require.NoError(t, json.Unmarshal(sample(t, "cpi-2024-2026"), &response))
			switch name {
			case "missing-series":
				response.Results.Series = response.Results.Series[:3]
			case "duplicate-series":
				response.Results.Series[1].ID = response.Results.Series[0].ID
			case "missing-array":
				response.Results.Series[0].Data = nil
			case "duplicate-month":
				response.Results.Series[0].Data = append(response.Results.Series[0].Data, response.Results.Series[0].Data[0])
			case "bad-month":
				response.Results.Series[0].Data[0].Period = "M+1"
			case "invalid-number":
				response.Results.Series[0].Data[0].Value = "NaN"
			case "negative-number":
				response.Results.Series[0].Data[0].Value = "-2"
			case "out-of-range":
				response.Results.Series[0].Data[0].Year = "2027"
			case "quota":
				response.Status = "REQUEST_NOT_PROCESSED"
				response.Messages = []string{"quota exceeded"}
			case "warning":
				response.Messages = []string{"Year range has been reduced"}
			case "annual-average":
				response.Results.Series[0].Data = append(response.Results.Series[0].Data, rawPoint{Year: "2025", Period: "M13", Value: "900"})
			}
			raw, err := json.Marshal(response)
			require.NoError(t, err)
			_, err = decodeMonthlyBatch(raw, cpiSeriesIDs, 2024, 2026)
			if name == "annual-average" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	_, err := decodeResponse([]byte("<html>busy</html>"))
	require.Error(t, err)
}

func TestCPIBatchBoundaries(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			Start string `json:"startyear"`
			End   string `json:"endyear"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		first, _ := strconv.Atoi(body.Start)
		last, _ := strconv.Atoi(body.End)
		require.LessOrEqual(t, last-first, 9)
		if first == 2014 {
			require.Equal(t, 2023, last)
		} else {
			require.Equal(t, 2024, first)
			require.Equal(t, 2026, last)
			if fail.Load() {
				http.Error(w, "failed", 503)
				return
			}
		}
		response := rawResponse{Status: "REQUEST_SUCCEEDED", Messages: []string{}}
		require.NoError(t, json.Unmarshal([]byte(`{"Results":{"series":[{"seriesID":"CUUR0000SA0","data":[]},{"seriesID":"CUSR0000SA0","data":[]},{"seriesID":"CUUR0000SA0L1E","data":[]},{"seriesID":"CUSR0000SA0L1E","data":[]}]}}`), &response))
		for i := range response.Results.Series {
			if first == 2014 {
				response.Results.Series[i].Data = []rawPoint{{Year: "2023", Period: "M01", Value: "100"}, {Year: "2023", Period: "M12", Value: "100"}}
			} else {
				value := "150"
				if i%2 == 1 {
					value = "200"
				}
				response.Results.Series[i].Data = []rawPoint{{Year: "2024", Period: "M01", Value: value}}
			}
		}
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	defer server.Close()
	client := NewClient("")
	client.endpoint = server.URL
	req := &CPIRequest{Indicator: "cpi", Begin: "201501", End: "202601"}
	result, err := client.CPI(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Batches, 2)
	require.EqualValues(t, 2, calls.Load())
	jan2024 := result.Observations[9*12]
	require.Equal(t, "202401", jan2024.Period)
	require.InDelta(t, 50, *jan2024.YoY, 1e-10)
	require.InDelta(t, 100, *jan2024.MoM, 1e-10)
	fail.Store(true)
	result, err = client.CPI(context.Background(), req)
	require.ErrorContains(t, err, "503")
	require.Nil(t, result)
}

func TestCPIRequestAndZero(t *testing.T) {
	for _, req := range []*CPIRequest{nil, {Indicator: "ppi", Begin: "202501", End: "202509"}, {Indicator: "cpi", Begin: "202513", End: "202601"}, {Indicator: "cpi", Begin: "202502", End: "202501"}, {Indicator: "cpi", Begin: "191212", End: "191301"}, {Indicator: "cpi", Begin: "202501", End: "299901"}} {
		result, err := NewClient("").CPI(context.Background(), req)
		require.Error(t, err, fmt.Sprint(req))
		require.Nil(t, result)
	}
	zero, one := 0.0, 1.0
	require.Nil(t, percentChange(&CPIValue{Index: &one}, &CPIValue{Index: &zero}))
	require.Equal(t, -100.0, *percentChange(&CPIValue{Index: &zero}, &CPIValue{Index: &one}))
}
