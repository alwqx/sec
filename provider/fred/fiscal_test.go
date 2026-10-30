package fred

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func fiscalSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/us-fiscal.csv")
	require.NoError(t, err)
	return string(raw)
}
func mockFiscal(raw string) {
	gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "MTSR133FMS,MTSO133FMS,MTSDS133FMS").MatchHeader("User-Agent", "curl").Reply(200).BodyString(raw)
}

func TestFiscalOfficialValuesSignsAndCache(t *testing.T) {
	defer gock.Off()
	mockFiscal(fiscalSample(t))
	client := NewClient(t.TempDir())
	req := &FiscalRequest{Begin: "202509", End: "202610"}
	result, err := client.Fiscal(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 14)
	require.Len(t, result.Series, 3)
	require.Equal(t, "198010", result.FirstPeriod)
	require.Equal(t, "202609", result.LastPeriod)
	require.Equal(t, "百万美元", result.Unit)
	require.False(t, result.SeasonallyAdjusted)
	require.False(t, result.Annualized)
	require.Nil(t, result.PublishedAt)
	require.Nil(t, result.RevisionID)
	require.Equal(t, "current_visible_revision", result.DataVersion)
	first := result.Observations[0]
	require.Equal(t, 2025, first.FiscalYear)
	require.Equal(t, 12, first.FiscalMonth)
	require.Greater(t, *first.SurplusDeficit, 0.0)
	require.Equal(t, -*first.SurplusDeficit, *first.Deficit)
	october := result.Observations[1]
	require.Equal(t, 2026, october.FiscalYear)
	require.Equal(t, 1, october.FiscalMonth)
	require.Equal(t, "202510", october.FYTD.Begin)
	require.Equal(t, october.Receipts, october.FYTD.Receipts)
	recent := result.Observations[12]
	require.Equal(t, "202609", recent.Period)
	require.Equal(t, 557874.51923824, *recent.Receipts)
	require.Equal(t, 584774.86634026, *recent.Outlays)
	require.Equal(t, -26900.34710202, *recent.SurplusDeficit)
	require.Equal(t, 26900.34710202, *recent.Deficit)
	require.Equal(t, "557874.51923824", recent.RawValues["MTSR133FMS"])
	require.Equal(t, "sum_of_current_snapshot_months", recent.FYTD.Method)
	require.Equal(t, "202510", recent.FYTD.Begin)
	require.Equal(t, "202609", recent.FYTD.End)
	require.InDelta(t, 5403207.18811635, *recent.FYTD.Receipts, 0.01)
	require.NotNil(t, recent.FYTD.Outlays)
	require.Equal(t, -*recent.FYTD.SurplusDeficit, *recent.FYTD.Deficit)
	last := result.Observations[13]
	require.Nil(t, last.Receipts)
	require.Nil(t, last.FYTD.Receipts)
	require.Equal(t, 2027, last.FiscalYear)
	require.Contains(t, last.Note, "未提供")
	cached, err := client.Fiscal(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, cached)
	require.True(t, gock.IsDone())
	// 官方早期独立系列存在舍入差，不强制以两个流量替换官方盈余/赤字。
	earliest, err := client.Fiscal(context.Background(), &FiscalRequest{Begin: "198010", End: "198010"})
	require.NoError(t, err)
	v := earliest.Observations[0]
	require.Equal(t, -16921.0, *v.SurplusDeficit)
	require.Equal(t, -16920.0, *v.Receipts-*v.Outlays)
}

func TestFiscalStrictYTDAndMissingValues(t *testing.T) {
	defer gock.Off()
	raw := "DATE,MTSR133FMS,MTSO133FMS,MTSDS133FMS\n2025-10-01,0,-1,1\n2025-12-01,10,20,-10\n2026-01-01,.,30,\n"
	mockFiscal(raw)
	got, err := NewClient("").Fiscal(context.Background(), &FiscalRequest{Begin: "202510", End: "202601"})
	require.NoError(t, err)
	require.Len(t, got.Observations, 4)
	require.Zero(t, *got.Observations[0].Receipts)
	require.Equal(t, -1.0, *got.Observations[0].Outlays)
	require.Equal(t, -1.0, *got.Observations[0].Deficit)
	require.Nil(t, got.Observations[1].Receipts)
	require.Nil(t, got.Observations[2].FYTD.Receipts)
	require.Equal(t, 10.0, *got.Observations[2].Receipts)
	require.Nil(t, got.Observations[3].Receipts)
	require.Equal(t, ".", got.Observations[3].RawValues["MTSR133FMS"])
	require.Equal(t, "", got.Observations[3].RawValues["MTSDS133FMS"])
	require.Nil(t, got.Observations[3].FYTD.Outlays)
}

func TestFiscalRejectsMalformedSnapshots(t *testing.T) {
	sample, err := csv.NewReader(strings.NewReader(fiscalSample(t))).ReadAll()
	require.NoError(t, err)
	for _, mode := range []string{"unknown-column", "duplicate-column", "invalid-date", "wrong-frequency", "earlier-date", "duplicate-period", "truncated", "nan", "inf", "all-missing-column", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			rows := make([][]string, len(sample))
			for i, row := range sample {
				rows[i] = append([]string(nil), row...)
			}
			switch mode {
			case "unknown-column":
				rows[0][1] = "bad"
			case "duplicate-column":
				rows[0][1] = rows[0][2]
			case "invalid-date":
				rows[1][0] = "2026-02-30"
			case "wrong-frequency":
				rows[1][0] = "1980-10-02"
			case "earlier-date":
				rows[1][0] = "1980-09-01"
			case "duplicate-period":
				rows = append(rows, rows[1])
			case "truncated":
				rows[1] = rows[1][:2]
			case "nan":
				rows[1][1] = "NaN"
			case "inf":
				rows[1][2] = "-Inf"
			case "all-missing-column":
				for i := 1; i < len(rows); i++ {
					rows[i][3] = "."
				}
			case "overflow":
				rows[1][1] = "1e308"
				rows[2][1] = "1e308"
			}
			var out strings.Builder
			require.NoError(t, csv.NewWriter(&out).WriteAll(rows))
			_, err := parseFiscal(out.String())
			require.Error(t, err)
		})
	}
	for _, row := range sample {
		row[0], row[3] = row[3], row[0]
	}
	sample[0][3] = "\ufeffDATE"
	var out strings.Builder
	require.NoError(t, csv.NewWriter(&out).WriteAll(sample))
	_, err = parseFiscal(out.String())
	require.NoError(t, err)
}

func TestFiscalCacheValidationAndRefreshProtection(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := &FiscalRequest{Begin: "202609", End: "202609"}
	mockFiscal(fiscalSample(t))
	good, err := client.Fiscal(context.Background(), req)
	require.NoError(t, err)
	path := filepath.Join(client.CacheDir, "us-fiscal.json")
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, raw := range []string{"bad", "DATE,MTSR133FMS,MTSO133FMS,MTSDS133FMS\n2025-10-01,1e308,1,0\n2025-11-01,1e308,1,0\n"} {
		client.Refresh = true
		mockFiscal(raw)
		_, err = client.Fiscal(context.Background(), req)
		require.Error(t, err)
		after, e := os.ReadFile(path)
		require.NoError(t, e)
		require.Equal(t, saved, after)
		client.Refresh = false
		cached, e := client.Fiscal(context.Background(), req)
		require.NoError(t, e)
		require.Equal(t, good, cached)
	}
	for _, mutate := range []func(*csvCache){func(c *csvCache) { c.CSV = "bad" }, func(c *csvCache) { c.Source = "https://wrong.invalid" }, func(c *csvCache) { c.FetchedAt = time.Now().Add(-7 * time.Hour) }, func(c *csvCache) { c.FetchedAt = time.Now().Add(time.Hour) }} {
		var cache csvCache
		require.NoError(t, json.Unmarshal(saved, &cache))
		mutate(&cache)
		raw, e := json.Marshal(cache)
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(path, raw, 0600))
		mockFiscal(fiscalSample(t))
		_, e = client.Fiscal(context.Background(), req)
		require.NoError(t, e)
	}
	client.CacheDir = ""
	for i := 0; i < 2; i++ {
		mockFiscal(fiscalSample(t))
		_, err = client.Fiscal(context.Background(), req)
		require.NoError(t, err)
	}
	require.True(t, gock.IsDone())
}

func TestFiscalValidationAndCancellation(t *testing.T) {
	for _, req := range []*FiscalRequest{nil, {Begin: "198009", End: "198010"}, {Begin: "202613", End: "202613"}, {Begin: "202602", End: "202601"}, {Begin: "202601", End: "999912"}} {
		_, err := NewClient("").Fiscal(context.Background(), req)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").Fiscal(ctx, &FiscalRequest{Begin: "202609", End: "202609"})
	require.ErrorIs(t, err, context.Canceled)
}
