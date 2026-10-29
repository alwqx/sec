package fred

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGDPSourceFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/us-gdp.csv")
	require.NoError(t, err)
	values, err := parseGDP(string(raw))
	require.NoError(t, err)
	require.Equal(t, 2182.681, *values["1947Q1"].Level)
	require.Nil(t, values["1947Q1"].QoQAnnualized)
	require.Equal(t, -1.0, *values["1947Q2"].QoQAnnualized)
	require.Equal(t, 24408.011, *values["2026Q2"].Level)
	require.Equal(t, 2.2, *values["2026Q2"].QoQAnnualized)
}

func TestGDPStrictBaselinesAndCache(t *testing.T) {
	raw := "observation_date,GDPC1,A191RL1Q225SBEA\n2024-01-01,100,-2\n2024-04-01,0,-100\n2024-10-01,120,3\n2025-01-01,130,4\n2025-04-01,140,2\n2025-07-01,.,.\n"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, raw) }))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.gdpEndpoint = server.URL
	result, err := client.GDP(context.Background(), &GDPRequest{Begin: "2024Q3", End: "2025Q3"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 5)
	require.Nil(t, result.Observations[0].Level)
	require.Nil(t, result.Observations[1].QoQ) // 缺少严格上季，不向前寻找有值季度。
	require.InDelta(t, 30, *result.Observations[2].YoY, 1e-10)
	require.Equal(t, "2024Q1", result.Observations[2].YoYBase.Period)
	require.InDelta(t, 100.0*(130.0/120-1), *result.Observations[2].QoQ, 1e-10)
	require.Equal(t, 4.0, *result.Observations[2].QoQAnnualized)
	require.Nil(t, result.Observations[3].YoY) // 零基期不能计算。
	require.Nil(t, result.Observations[4].QoQ)
	require.Nil(t, result.PublishedAt)
	require.Nil(t, result.RevisionID)
	require.Equal(t, 2017, result.ReferenceYear)
	require.True(t, result.LevelAnnualized)
	_, err = client.GDP(context.Background(), &GDPRequest{Begin: "2025Q1", End: "2025Q1"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	client.Refresh = true
	_, err = client.GDP(context.Background(), &GDPRequest{Begin: "2025Q1", End: "2025Q1"})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestGDPRejectsBadSource(t *testing.T) {
	for _, raw := range []string{
		"date,GDPC1,A191RL1Q225SBEA\n2025-01-01,1,2\n",
		"DATE,GDPC1,GDPC1\n2025-01-01,1,2\n",
		"DATE,GDPC1,A191RL1Q225SBEA\n2025-02-01,1,2\n",
		"DATE,GDPC1,A191RL1Q225SBEA\n2025-01-01,1,2\n2025-01-01,1,2\n",
		"DATE,GDPC1,A191RL1Q225SBEA\n2025-01-01,-1,2\n",
		"DATE,GDPC1,A191RL1Q225SBEA\n2025-01-01,NaN,2\n",
		"DATE,GDPC1,A191RL1Q225SBEA\n2025-01-01,1,Inf\n",
		"DATE,GDPC1,A191RL1Q225SBEA\n2025-01-01,1,.\n",
	} {
		_, err := parseGDP(raw)
		require.Error(t, err, raw)
	}
	_, err := parseGDP("\ufeffA191RL1Q225SBEA,DATE,GDPC1\n2025-01-01,1,2\n")
	require.Error(t, err)
	values, err := parseGDP("\ufeffA191RL1Q225SBEA,DATE,GDPC1\n-2,2025-01-01,100\n")
	require.NoError(t, err)
	require.Equal(t, -2.0, *values["2025Q1"].QoQAnnualized)
}

func TestGDPRequestValidation(t *testing.T) {
	for _, req := range []*GDPRequest{nil, {Begin: "1946Q4", End: "1947Q1"}, {Begin: "2025Q5", End: "2025Q5"}, {Begin: "2025q1", End: "2025Q1"}, {Begin: "2025Q2", End: "2025Q1"}, {Begin: "2025Q1", End: "9999Q1"}} {
		require.Error(t, req.Validate())
	}
	require.NoError(t, (&GDPRequest{Begin: "1947Q1", End: "2025Q1"}).Validate())
}

func TestGDPVintageOfficialFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/us-gdp-vintages.xlsx")
	require.NoError(t, err)
	values, err := parseGDPVintages(raw)
	require.NoError(t, err)
	require.Len(t, values, 98)
	require.NotEmpty(t, values["2002Q1"])
	require.NotContains(t, values, "2001Q4")
	q := values["2026Q2"]
	require.Len(t, q, 3)
	require.Equal(t, "Advance", q[0].Estimate)
	require.Equal(t, "2026-07-30", q[0].ReleaseDate)
	require.Equal(t, 1.5, *q[0].QoQAnnualized)
	require.Equal(t, 32475.2, *q[0].NominalLevel)
	require.Nil(t, q[0].RealLevel)
	require.Nil(t, q[0].PublishedAt)
	require.Equal(t, "Second", q[1].Estimate)
	require.Equal(t, "2026-08-26", q[1].ReleaseDate)
	require.Equal(t, "Third", q[2].Estimate)
	require.Equal(t, 2.2, *q[2].QoQAnnualized)
	labels := map[string]bool{}
	for _, item := range values["2025Q3"] {
		labels[item.Estimate] = true
	}
	require.True(t, labels["Initial"])
	require.True(t, labels["Updated"])
	for _, item := range values["2025Q4"] {
		if item.ReleaseDate == "2026-05-28" {
			require.Contains(t, item.Note, "GDP not open for revision")
		}
	}
	_, err = parseGDPVintages([]byte("<html>Bad Gateway</html>"))
	require.Error(t, err)
}

func TestGDPVintageCacheAndRefreshFailure(t *testing.T) {
	raw, err := os.ReadFile("testdata/us-gdp-vintages.xlsx")
	require.NoError(t, err)
	calls := 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if fail {
			http.Error(w, "failed", 503)
			return
		}
		w.Write(raw)
	}))
	defer server.Close()
	dir := t.TempDir()
	values, stamp, err := fetchGDPVintages(context.Background(), dir, false, server.URL)
	require.NoError(t, err)
	require.Len(t, values, 98)
	require.WithinDuration(t, time.Now(), stamp, time.Second)
	before, err := os.ReadFile(filepath.Join(dir, "gdp-vintages.json"))
	require.NoError(t, err)
	fail = true
	_, cached, err := fetchGDPVintages(context.Background(), dir, false, server.URL)
	require.NoError(t, err)
	require.Equal(t, stamp, cached)
	require.Equal(t, 1, calls)
	_, _, err = fetchGDPVintages(context.Background(), dir, true, server.URL)
	require.ErrorContains(t, err, "503")
	after, err := os.ReadFile(filepath.Join(dir, "gdp-vintages.json"))
	require.NoError(t, err)
	require.Equal(t, before, after)
	var saved gdpVintageCache
	require.NoError(t, json.Unmarshal(before, &saved))
	saved.FetchedAt = time.Now().Add(-7 * time.Hour)
	expired, err := json.Marshal(saved)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gdp-vintages.json"), expired, 0600))
	_, _, err = fetchGDPVintages(context.Background(), dir, false, server.URL)
	require.Error(t, err) // 不回退到过期档案。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = fetchGDPVintages(ctx, dir, false, server.URL)
	require.ErrorIs(t, err, context.Canceled)
}

func TestGDPNoVintagesByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "DATE,GDPC1,A191RL1Q225SBEA\n2025-01-01,100,2")
	}))
	defer server.Close()
	client := NewClient("")
	client.gdpEndpoint = server.URL
	result, err := client.GDP(context.Background(), &GDPRequest{Begin: "2025Q1", End: "2025Q1"})
	require.NoError(t, err)
	require.False(t, result.VintagesRequested)
	require.Empty(t, result.VintagesSource)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"vintages":null`)
	require.True(t, strings.Contains(result.Note, "当前水平"))
}
