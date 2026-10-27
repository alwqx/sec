package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func gdpRows(t *testing.T, name string) []valueRow {
	t.Helper()
	var response struct {
		Data []valueRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "gdp-"+name+"-values"), &response))
	return response.Data
}

func TestGDPCollection(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	catalogNames := map[string]string{"b676631776424600bdae363df047559f": "real", "f9b694c9b79e4ce5958bc88c6410fa67": "index", "2b0aa179980e41b8b636037ebae01d91": "qoq"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			http.Error(w, "failed", 503)
			return
		}
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			require.Equal(t, "2", r.URL.Query().Get("code"))
			name := map[string]string{"": "root", "a94b8b7365a94874968cabbe392cf679": "groups", "1b1ce0cfd03646dc9e15103ea4c570f6": "catalogs"}[r.URL.Query().Get("pid")]
			if name == "" {
				http.Error(w, "bad directory", 400)
				return
			}
			w.Write(fixture(t, "gdp-"+name))
		case "/new/queryIndicatorsByCid":
			name := catalogNames[r.URL.Query().Get("cid")]
			if name == "" {
				http.Error(w, "bad catalog", 400)
				return
			}
			w.Write(fixture(t, "gdp-"+name+"-series"))
		case "/stream/esData":
			require.Equal(t, http.MethodPost, r.Method)
			var body struct {
				Catalog string              `json:"cid"`
				Root    string              `json:"rootId"`
				IDs     []string            `json:"indicatorIds"`
				Dates   []string            `json:"dts"`
				Areas   []map[string]string `json:"das"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "1b1ce0cfd03646dc9e15103ea4c570f6", body.Root)
			require.Equal(t, []string{"199201SS-202603SS"}, body.Dates)
			require.Equal(t, "000000000000", body.Areas[0]["value"])
			name := catalogNames[body.Catalog]
			want := map[string][]string{"real": {"b704155cd926437b8ee9c65fe058210d", "e33efbac437d4d10b5ccae6ad9d923b9"}, "index": {"170e7f00f8c24ede863c0526b42ae81f", "f5b82b2b6ad345a29a337d256a9d5ded"}, "qoq": {"88bcaf76058e49b08a45b14a204b2d3d"}}
			require.Equal(t, want[name], body.IDs)
			w.Write(fixture(t, "gdp-"+name+"-values"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := &GDPRequest{Begin: "1992Q1", End: "2026Q3"}
	result, err := client.GDP(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 139)
	require.Len(t, result.Series, 5)
	require.Nil(t, result.PublishedAt)
	require.Nil(t, result.RevisionID)
	require.Nil(t, result.RealBaseYear)
	require.Equal(t, "latest-available", result.DataVersion)
	require.Nil(t, result.Observations[0].YoY)
	require.Nil(t, result.Observations[0].RealValue)
	require.Nil(t, result.Observations[0].QoQ)
	require.Equal(t, "missing-values", result.Observations[0].Status)
	latest := result.Observations[len(result.Observations)-1]
	require.Equal(t, "2026Q3", latest.Period)
	require.Equal(t, 371684.6, *latest.RealValue)
	require.Equal(t, 1058656.0, *latest.RealYTDValue)
	require.Equal(t, 104.5, *latest.YoYIndex)
	require.Equal(t, 4.5, *latest.YoY)
	require.Equal(t, 4.6, *latest.YTDYoY)
	require.Equal(t, 1.2, *latest.QoQ)
	require.Equal(t, "complete", latest.Status)
	require.Contains(t, result.Series[0].Annotation, "不同基期")
	require.False(t, result.Series[0].SeasonallyAdjusted)
	require.True(t, result.Series[4].SeasonallyAdjusted)
	require.Equal(t, "亿元", result.Series[0].Unit)
	require.Equal(t, "上年同期=100", result.Series[2].Unit)
	require.EqualValues(t, 9, calls.Load())
	_, err = client.GDP(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 9, calls.Load(), "复用共享NBS缓存")
	client.Refresh = true
	_, err = client.GDP(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 18, calls.Load())
	client.CacheDir = ""
	client.Refresh = false
	_, err = client.GDP(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 27, calls.Load())
	fail.Store(true)
	result, err = client.GDP(context.Background(), req)
	require.ErrorContains(t, err, "503")
	require.Nil(t, result)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.GDP(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestGDPParsing(t *testing.T) {
	selected := []GDPSeries{{Key: "real-value", SeriesID: "b704155cd926437b8ee9c65fe058210d"}, {Key: "real-ytd-value", SeriesID: "e33efbac437d4d10b5ccae6ad9d923b9"}}
	for _, tc := range []string{"normal", "zero-and-missing", "bad-quarter", "duplicate-quarter", "duplicate-value", "invalid-value", "negative-level"} {
		t.Run(tc, func(t *testing.T) {
			rows := gdpRows(t, "real")
			rows = rows[:1] // 官方2026Q3
			switch tc {
			case "zero-and-missing":
				rows[0].Values[0].Value = json.RawMessage(`"0"`)
				rows[0].Values[1].Value = json.RawMessage(`null`)
			case "bad-quarter":
				rows[0].Code = "202605SS"
			case "duplicate-quarter":
				rows = append(rows, rows[0])
			case "duplicate-value":
				rows[0].Values = append(rows[0].Values, rows[0].Values[0])
			case "invalid-value":
				rows[0].Values[0].Value = json.RawMessage(`"NaN"`)
			case "negative-level":
				rows[0].Values[0].Value = json.RawMessage(`"-1"`)
			}
			item := &GDPObservation{Period: "2026Q3"}
			err := mergeGDP(rows, selected, map[string]*GDPObservation{"2026Q3": item})
			if strings.Contains(tc, "bad-") || strings.Contains(tc, "duplicate-") || tc == "invalid-value" || tc == "negative-level" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc == "zero-and-missing" {
				require.NotNil(t, item.RealValue)
				require.Zero(t, *item.RealValue)
				require.Nil(t, item.RealYTDValue)
			} else {
				require.Equal(t, 371684.6, *item.RealValue)
			}
		})
	}
	rows := gdpRows(t, "qoq")[:1]
	rows[0].Values[0].Value = json.RawMessage(`"-0.7"`)
	item := &GDPObservation{}
	require.NoError(t, mergeGDP(rows, []GDPSeries{{Key: "qoq", SeriesID: "88bcaf76058e49b08a45b14a204b2d3d"}}, map[string]*GDPObservation{"2026Q3": item}))
	require.Equal(t, -0.7, *item.QoQ)
}

func TestGDPInvalidRequests(t *testing.T) {
	for _, req := range []*GDPRequest{nil, {Begin: "202501", End: "2026Q3"}, {Begin: "2025Q0", End: "2026Q3"}, {Begin: "2026Q4", End: "2026Q3"}, {Begin: "1991Q4", End: "1992Q1"}, {Begin: "+025Q1", End: "2026Q1"}} {
		result, err := NewClient("").GDP(context.Background(), req)
		require.Error(t, err, fmt.Sprint(req))
		require.Nil(t, result)
	}
}
