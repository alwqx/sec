package bls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPayrollsLevelsChangesAndObservedVersions(t *testing.T) {
	raw := sample(t, "payrolls-2024-2026")
	var response rawResponse
	require.NoError(t, json.Unmarshal(raw, &response))
	points := response.Results.Series[0].Data[:0]
	for _, point := range response.Results.Series[0].Data {
		if point.Year != "2024" {
			points = append(points, point)
		}
	}
	response.Results.Series[0].Data = points
	raw, err := json.Marshal(response)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(raw) }))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.endpoint = server.URL
	req := &PayrollRequest{Begin: "202601", End: "202609"}
	got, err := client.Payrolls(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, payrollSeriesID, got.SeriesID)
	require.True(t, got.HistoryEnabled)
	require.False(t, got.OfficialVintageArchiveAvailable)
	require.Nil(t, got.PublishedAt)
	require.Len(t, got.Observations, 9)
	require.Equal(t, 159044.0, *got.Observations[8].Employment)
	require.Equal(t, 159015.0, *got.Observations[8].Previous.Employment)
	require.Equal(t, 29.0, *got.Observations[8].Change)
	require.True(t, got.Observations[8].Preliminary)
	require.Len(t, got.Observations[8].ObservedVersions, 1)
	require.Equal(t, 133.0, *got.Observations[7].Change)
	require.Equal(t, -10.0, *got.Observations[6].Change)
	require.Equal(t, -156.0, *got.Observations[1].Change)

	got, err = client.Payrolls(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got.Observations[8].ObservedVersions, 1, "读取缓存时不得重复记录版本")

	require.NoError(t, json.Unmarshal(raw, &response))
	for i := range response.Results.Series[0].Data {
		if response.Results.Series[0].Data[i].Year == "2026" && response.Results.Series[0].Data[i].Period == "M08" {
			response.Results.Series[0].Data[i].Value = "159000"
			response.Results.Series[0].Data[i].Footnotes = nil
		}
	}
	updated, err := json.Marshal(response)
	require.NoError(t, err)
	client.Refresh = true
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(updated) })
	got, err = client.Payrolls(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 44.0, *got.Observations[8].Change)
	require.Len(t, got.Observations[7].ObservedVersions, 2)
	require.False(t, got.Observations[7].Preliminary)
	require.Equal(t, 159000.0, got.Observations[7].ObservedVersions[1].Employment)
}

func TestPayrollsNoCacheAndValidation(t *testing.T) {
	require.Error(t, (*PayrollRequest)(nil).Validate())
	require.Error(t, (&PayrollRequest{Begin: "193812", End: "193901"}).Validate())
	require.Error(t, (&PayrollRequest{Begin: "202701", End: "202612"}).Validate())
	client := NewClient("")
	client.endpoint = "http://127.0.0.1:1"
	_, err := client.Payrolls(context.Background(), &PayrollRequest{Begin: "202609", End: "202609"})
	require.Error(t, err)
	_, err = os.Stat(filepath.Join(t.TempDir(), "payrolls-observed.json"))
	require.True(t, os.IsNotExist(err))
}

func TestObservePayrollVersionsPreservesCorruptArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payrolls-observed.json")
	require.NoError(t, os.WriteFile(path, []byte("corrupt"), 0600))
	client := NewClient(dir)
	n := 100.0
	_, err := client.observePayrollVersions(context.Background(), map[string]*monthlyValue{"202601": {Number: &n, FetchedAt: time.Now().UTC()}})
	require.ErrorContains(t, err, "损坏")
	contents, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, "corrupt", string(contents))
}
