package bls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func laborForceServer(t *testing.T, raw []byte, start, end string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		var body struct {
			IDs   []string `json:"seriesid"`
			Start string   `json:"startyear"`
			End   string   `json:"endyear"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, laborForceSeriesIDs, body.IDs)
		require.Equal(t, start, body.Start)
		require.Equal(t, end, body.End)
		_, _ = w.Write(raw)
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func TestLaborForceOfficialSampleAndCache(t *testing.T) {
	server, calls := laborForceServer(t, sample(t, "labor-force-2024-2026"), "2024", "2026")
	client := NewClient(t.TempDir())
	client.endpoint = server.URL
	for _, indicator := range []string{"unemployment", "participation"} {
		got, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: indicator, Begin: "202501", End: "202609"})
		require.NoError(t, err)
		require.Len(t, got.Observations, 21)
		require.True(t, got.SeasonallyAdjusted)
		require.Equal(t, "CPS household survey", got.Survey)
		require.Equal(t, "%", got.Unit)
		require.Equal(t, "百分点", got.ChangeUnit)
		require.Contains(t, got.Population, "16 and older")
		require.Equal(t, "latest-available", got.DataVersion)
		require.Nil(t, got.PublishedAt)
		oct, nov, jan, feb, sep := got.Observations[9], got.Observations[10], got.Observations[12], got.Observations[13], got.Observations[20]
		require.Equal(t, "202510", oct.Period)
		require.Nil(t, oct.Rate)
		require.Nil(t, oct.MoMChange)
		require.Equal(t, "-", oct.RawValue)
		require.Equal(t, "9", oct.Footnotes[0].Code)
		require.Contains(t, oct.Footnotes[0].Text, "lapse in appropriations")
		require.NotNil(t, nov.Rate)
		require.Nil(t, nov.MoMChange, "不能跨越十月缺口")
		require.NotNil(t, nov.YoYChange)
		require.Equal(t, "-", nov.MoMBase.RawValue)
		require.Equal(t, "12", jan.Footnotes[0].Code)
		require.Contains(t, jan.Footnotes[0].Text, "population controls")
		require.Equal(t, jan.Footnotes, feb.MoMBase.Footnotes)
		require.True(t, sep.Latest)
		require.Equal(t, sep.FetchedAt, sep.MoMBase.FetchedAt)
		require.Equal(t, sep.FetchedAt, sep.YoYBase.FetchedAt)
		if indicator == "unemployment" {
			require.Equal(t, "LNS14000000", got.SeriesID)
			require.Equal(t, "civilian labor force", got.Denominator)
			require.Equal(t, 4.2, *sep.Rate)
			require.InDelta(t, 0.1, *sep.MoMChange, 1e-10)
			require.InDelta(t, -0.2, *sep.YoYChange, 1e-10)
		} else {
			require.Equal(t, "LNS11300000", got.SeriesID)
			require.Equal(t, "civilian noninstitutional population", got.Denominator)
			require.Equal(t, 61.8, *sep.Rate)
			require.InDelta(t, 0.2, *sep.MoMChange, 1e-10)
			require.InDelta(t, -0.7, *sep.YoYChange, 1e-10)
		}
	}
	require.EqualValues(t, 1, calls.Load(), "两条比率共享缓存")
	client.Refresh = true
	_, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: "unemployment", Begin: "202501", End: "202609"})
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
}

func TestLaborForceStartAndValidation(t *testing.T) {
	server, _ := laborForceServer(t, sample(t, "labor-force-1948-1949"), "1948", "1949")
	client := NewClient("")
	client.endpoint = server.URL
	for _, indicator := range []string{"unemployment", "participation"} {
		got, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: indicator, Begin: "194801", End: "194901"})
		require.NoError(t, err)
		require.Len(t, got.Observations, 13)
		require.Nil(t, got.Observations[0].MoMChange)
		require.Nil(t, got.Observations[0].YoYChange)
		require.NotNil(t, got.Observations[12].YoYChange)
		if indicator == "unemployment" {
			require.Equal(t, 3.4, *got.Observations[0].Rate)
		} else {
			require.Equal(t, 58.6, *got.Observations[0].Rate)
		}
	}
	for _, req := range []*LaborForceRequest{nil, {Indicator: "bad", Begin: "202501", End: "202509"}, {Indicator: "unemployment", Begin: "194712", End: "194801"}, {Indicator: "participation", Begin: "202513", End: "202601"}, {Indicator: "unemployment", Begin: "202502", End: "202501"}, {Indicator: "participation", Begin: "202501", End: "299901"}} {
		got, err := client.LaborForce(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.LaborForce(ctx, &LaborForceRequest{Indicator: "unemployment", Begin: "202501", End: "202509"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

func laborForceResponse(points []rawPoint) map[string]any {
	return map[string]any{"status": "REQUEST_SUCCEEDED", "message": []string{}, "Results": map[string]any{"series": []any{map[string]any{"seriesID": unemploymentRateSeriesID, "data": points}, map[string]any{"seriesID": participationRateSeriesID, "data": points}}}}
}

func TestLaborForceRateBoundsBeforeCache(t *testing.T) {
	for _, value := range []string{"101", "100.1", "NaN", "-0.1"} {
		t.Run(value, func(t *testing.T) {
			raw, err := json.Marshal(laborForceResponse([]rawPoint{{Year: "2025", Period: "M01", Value: value}}))
			require.NoError(t, err)
			server, _ := laborForceServer(t, raw, "2024", "2025")
			client := NewClient(t.TempDir())
			client.endpoint = server.URL
			got, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: "participation", Begin: "202501", End: "202501"})
			require.Error(t, err)
			require.Nil(t, got)
			files, err := filepath.Glob(filepath.Join(client.CacheDir, "*.json"))
			require.NoError(t, err)
			require.Empty(t, files)
		})
	}
	raw, err := json.Marshal(laborForceResponse([]rawPoint{{Year: "2024", Period: "M01", Value: "10"}, {Year: "2024", Period: "M02", Value: "80"}, {Year: "2024", Period: "M12", Value: "5"}, {Year: "2025", Period: "M01", Value: "0"}, {Year: "2025", Period: "M02", Value: "100"}}))
	require.NoError(t, err)
	server, _ := laborForceServer(t, raw, "2024", "2025")
	client := NewClient("")
	client.endpoint = server.URL
	got, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: "unemployment", Begin: "202501", End: "202502"})
	require.NoError(t, err)
	require.Zero(t, *got.Observations[0].Rate)
	require.Equal(t, -5.0, *got.Observations[0].MoMChange)
	require.Equal(t, -10.0, *got.Observations[0].YoYChange)
	require.Equal(t, 100.0, *got.Observations[1].Rate)
	require.Equal(t, 100.0, *got.Observations[1].MoMChange, "百分点差允许零基期")
	require.Equal(t, 20.0, *got.Observations[1].YoYChange)
}

func TestLaborForceSameBatchBaselines(t *testing.T) {
	for _, mode := range []string{"revised", "missing", "failure"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Start string `json:"startyear"`
					End   string `json:"endyear"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				first, _ := strconv.Atoi(body.Start)
				last, _ := strconv.Atoi(body.End)
				require.LessOrEqual(t, last-first, 9)
				points := []rawPoint{{Year: "2023", Period: "M01", Value: "10"}, {Year: "2023", Period: "M12", Value: "10"}}
				if first == 2014 {
					require.Equal(t, 2023, last)
				} else {
					require.Equal(t, 2023, first)
					require.Equal(t, 2026, last)
					if mode == "failure" {
						http.Error(w, "failed", 503)
						return
					}
					points = []rawPoint{{Year: "2024", Period: "M01", Value: "9.9"}}
					if mode == "revised" {
						points = append(points, rawPoint{Year: "2023", Period: "M01", Value: "8"}, rawPoint{Year: "2023", Period: "M12", Value: "9"})
					}
				}
				require.NoError(t, json.NewEncoder(w).Encode(laborForceResponse(points)))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.endpoint = server.URL
			_, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: "unemployment", Begin: "201501", End: "202312"})
			require.NoError(t, err)
			got, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: "unemployment", Begin: "201501", End: "202601"})
			if mode == "failure" {
				require.ErrorContains(t, err, "503")
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got.Observations, 133)
			require.Len(t, got.Batches, 2)
			jan := got.Observations[9*12]
			require.Equal(t, "202401", jan.Period)
			if mode == "missing" {
				require.Nil(t, jan.MoMChange)
				require.Nil(t, jan.YoYChange)
			} else {
				require.InDelta(t, 0.9, *jan.MoMChange, 1e-10)
				require.InDelta(t, 1.9, *jan.YoYChange, 1e-10)
				require.Equal(t, jan.FetchedAt, jan.MoMBase.FetchedAt)
				require.Equal(t, jan.FetchedAt, jan.YoYBase.FetchedAt)
			}
		})
	}
}

func TestLaborForceThreeBatchCoverage(t *testing.T) {
	ranges := [][2]string{{"2004", "2013"}, {"2013", "2022"}, {"2022", "2026"}}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := calls.Add(1) - 1
		var body struct {
			Start string `json:"startyear"`
			End   string `json:"endyear"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Less(t, int(index), len(ranges))
		require.Equal(t, ranges[index][0], body.Start)
		require.Equal(t, ranges[index][1], body.End)
		require.NoError(t, json.NewEncoder(w).Encode(laborForceResponse([]rawPoint{})))
	}))
	defer server.Close()
	client := NewClient("")
	client.endpoint = server.URL
	got, err := client.LaborForce(context.Background(), &LaborForceRequest{Indicator: "unemployment", Begin: "200501", End: "202601"})
	require.NoError(t, err)
	require.Len(t, got.Observations, 253)
	require.Len(t, got.Batches, 3)
	for i, item := range got.Observations {
		require.Equal(t, "200501", got.Observations[0].Period)
		require.Nil(t, item.Rate)
		if i > 0 {
			require.Greater(t, item.Period, got.Observations[i-1].Period)
		}
	}
	require.Equal(t, "202601", got.Observations[252].Period)
}
