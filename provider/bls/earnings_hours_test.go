package bls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func earningsHoursServer(t *testing.T, raw []byte, expectedStart, expectedEnd string) (*httptest.Server, *atomic.Int32) {
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
		require.Equal(t, earningsHoursSeriesIDs, body.IDs)
		require.Equal(t, expectedStart, body.Start)
		require.Equal(t, expectedEnd, body.End)
		_, _ = w.Write(raw)
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func TestEarningsHoursOfficialSampleAndSharedCache(t *testing.T) {
	server, calls := earningsHoursServer(t, sample(t, "earnings-hours-2024-2026"), "2024", "2026")
	client := NewClient(t.TempDir())
	client.endpoint = server.URL
	for _, indicator := range []string{"earnings", "weekly-hours"} {
		got, err := client.EarningsHours(context.Background(), &EarningsHoursRequest{Indicator: indicator, Begin: "202501", End: "202609"})
		require.NoError(t, err)
		require.Len(t, got.Observations, 21)
		require.True(t, got.SeasonallyAdjusted)
		require.Equal(t, "monthly", got.Frequency)
		require.Contains(t, got.Population, "private nonfarm")
		require.Equal(t, "latest-available", got.DataVersion)
		require.Nil(t, got.PublishedAt)
		latest := got.Observations[20]
		require.Equal(t, "202609", latest.Period)
		require.True(t, latest.Preliminary)
		require.True(t, latest.Latest)
		require.Equal(t, "P", latest.Footnotes[0].Code)
		require.Equal(t, "202608", latest.MoMBase.Period)
		require.Equal(t, "202509", latest.YoYBase.Period)
		require.Equal(t, latest.FetchedAt, latest.MoMBase.FetchedAt)
		require.Equal(t, latest.FetchedAt, latest.YoYBase.FetchedAt)
		if indicator == "earnings" {
			require.Equal(t, "CES0500000003", got.SeriesID)
			require.Equal(t, "美元/小时", got.Unit)
			require.Equal(t, "current-dollars", got.PriceBasis)
			require.Equal(t, "37.81", latest.RawValue)
			require.Equal(t, 37.81, *latest.Value)
			require.InDelta(t, 0.05, *latest.Change, 1e-10)
			require.InDelta(t, (37.81/37.76-1)*100, *latest.MoM, 1e-10)
			require.InDelta(t, (37.81/36.70-1)*100, *latest.YoY, 1e-10)
		} else {
			require.Equal(t, "CES0500000002", got.SeriesID)
			require.Equal(t, "小时/周", got.Unit)
			require.Empty(t, got.PriceBasis)
			require.Equal(t, 34.4, *latest.Value)
			require.Zero(t, *latest.Change)
			require.Zero(t, *latest.MoM)
			require.InDelta(t, (34.4/34.2-1)*100, *latest.YoY, 1e-10)
		}
	}
	require.EqualValues(t, 1, calls.Load(), "两个命令应共享两条系列的原始响应缓存")
	client.Refresh = true
	_, err := client.EarningsHours(context.Background(), &EarningsHoursRequest{Indicator: "earnings", Begin: "202501", End: "202609"})
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
}

func TestEarningsHoursSeriesStart(t *testing.T) {
	server, _ := earningsHoursServer(t, sample(t, "earnings-hours-2006-2007"), "2006", "2007")
	client := NewClient("")
	client.endpoint = server.URL
	for _, indicator := range []string{"earnings", "weekly-hours"} {
		got, err := client.EarningsHours(context.Background(), &EarningsHoursRequest{Indicator: indicator, Begin: "200603", End: "200703"})
		require.NoError(t, err)
		require.Len(t, got.Observations, 13)
		first := got.Observations[0]
		require.NotNil(t, first.Value)
		require.Nil(t, first.Change)
		require.Nil(t, first.MoM)
		require.Nil(t, first.YoY)
		require.Equal(t, "200602", first.MoMBase.Period)
		require.NotNil(t, got.Observations[12].YoY)
	}
}

func earningsHoursResponse(points []rawPoint) map[string]any {
	return map[string]any{"status": "REQUEST_SUCCEEDED", "message": []string{}, "Results": map[string]any{"series": []any{map[string]any{"seriesID": earningsHoursSeriesIDs[0], "data": points}, map[string]any{"seriesID": earningsHoursSeriesIDs[1], "data": []rawPoint{}}}}}
}

func TestEarningsHoursMissingAndZero(t *testing.T) {
	raw, err := json.Marshal(earningsHoursResponse([]rawPoint{{Year: "2024", Period: "M01", Value: "20"}, {Year: "2024", Period: "M12", Value: "5"}, {Year: "2025", Period: "M01", Value: "0"}, {Year: "2025", Period: "M02", Value: "10"}, {Year: "2025", Period: "M10", Value: "-", Footnotes: []Footnote{{Code: "X", Text: "missing"}}}, {Year: "2025", Period: "M11", Value: "12"}, {Year: "2026", Period: "M01", Value: "13"}}))
	require.NoError(t, err)
	server, _ := earningsHoursServer(t, raw, "2024", "2026")
	client := NewClient("")
	client.endpoint = server.URL
	got, err := client.EarningsHours(context.Background(), &EarningsHoursRequest{Indicator: "earnings", Begin: "202501", End: "202601"})
	require.NoError(t, err)
	require.Zero(t, *got.Observations[0].Value)
	require.Equal(t, -5.0, *got.Observations[0].Change)
	require.Equal(t, -100.0, *got.Observations[0].MoM)
	require.Equal(t, -100.0, *got.Observations[0].YoY)
	require.Equal(t, 10.0, *got.Observations[1].Change)
	require.Nil(t, got.Observations[1].MoM, "零基期不执行除法")
	require.Nil(t, got.Observations[9].Value)
	require.Equal(t, "X", got.Observations[9].Footnotes[0].Code)
	require.Nil(t, got.Observations[10].MoM, "不跨越十月缺口")
	require.Equal(t, "-", got.Observations[10].MoMBase.RawValue)
	require.Nil(t, got.Observations[12].MoM, "不借用十一月替代缺失的十二月")
	require.Nil(t, got.Observations[12].YoY, "上年同月为零")
	got, err = client.EarningsHours(context.Background(), &EarningsHoursRequest{Indicator: "weekly-hours", Begin: "202501", End: "202601"})
	require.NoError(t, err)
	for _, item := range got.Observations {
		require.Nil(t, item.Value)
		require.Nil(t, item.Change)
		require.Nil(t, item.MoM)
		require.Nil(t, item.YoY)
	}
}

func TestEarningsHoursBatchBaselinesAndFailure(t *testing.T) {
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
				points := []rawPoint{{Year: "2023", Period: "M01", Value: "100"}, {Year: "2023", Period: "M12", Value: "100"}}
				if first == 2014 {
					require.Equal(t, 2023, last)
				} else {
					require.Equal(t, 2023, first)
					require.Equal(t, 2026, last)
					if mode == "failure" {
						http.Error(w, "failed", 503)
						return
					}
					points = []rawPoint{{Year: "2024", Period: "M01", Value: "99"}}
					if mode == "revised" {
						points = append(points, rawPoint{Year: "2023", Period: "M01", Value: "80"}, rawPoint{Year: "2023", Period: "M12", Value: "90"})
					}
				}
				require.NoError(t, json.NewEncoder(w).Encode(earningsHoursResponse(points)))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.endpoint = server.URL
			_, err := client.EarningsHours(context.Background(), &EarningsHoursRequest{Indicator: "earnings", Begin: "201501", End: "202312"})
			require.NoError(t, err)
			got, err := client.EarningsHours(context.Background(), &EarningsHoursRequest{Indicator: "earnings", Begin: "201501", End: "202601"})
			if mode == "failure" {
				require.ErrorContains(t, err, "503")
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got.Batches, 2)
			jan := got.Observations[9*12]
			require.Equal(t, "202401", jan.Period)
			if mode == "missing" {
				require.Nil(t, jan.Change)
				require.Nil(t, jan.MoM)
				require.Nil(t, jan.YoY)
			} else {
				require.Equal(t, 9.0, *jan.Change)
				require.InDelta(t, 10.0, *jan.MoM, 1e-10)
				require.InDelta(t, 23.75, *jan.YoY, 1e-10)
				require.Equal(t, jan.FetchedAt, jan.MoMBase.FetchedAt)
				require.Equal(t, jan.FetchedAt, jan.YoYBase.FetchedAt)
			}
		})
	}
}

func TestEarningsHoursRequestValidationAndCancellation(t *testing.T) {
	client := NewClient("")
	for _, req := range []*EarningsHoursRequest{nil, {Indicator: "bad", Begin: "202501", End: "202509"}, {Indicator: "earnings", Begin: "200602", End: "200603"}, {Indicator: "weekly-hours", Begin: "202513", End: "202601"}, {Indicator: "earnings", Begin: "202502", End: "202501"}, {Indicator: "earnings", Begin: "202501", End: "299901"}} {
		got, err := client.EarningsHours(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.EarningsHours(ctx, &EarningsHoursRequest{Indicator: "earnings", Begin: "202501", End: "202509"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}
