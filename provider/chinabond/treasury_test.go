package chinabond

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

func sample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	require.NoError(t, err)
	return raw
}

func TestOfficialTreasurySamples(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		count          int
		first, last    float64
	}{
		{"treasury-20261019-20261026", "20261019", "20261026", 6, 1.1813, 1.6376},
		{"treasury-20060301-20060307", "20060301", "20060307", 5, 1.68, 2.9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetchedAt := time.Now().UTC()
			rows, err := parseTreasury(sample(t, tc.name), tc.from, tc.to, "official-source", fetchedAt)
			require.NoError(t, err)
			require.Len(t, rows, tc.count)
			require.Equal(t, tc.from, rows[0].Date) // UTC前一天16点必须转为北京时间当天。
			require.Equal(t, tc.to, rows[len(rows)-1].Date)
			require.Equal(t, tc.first, *rows[0].Yields["1y"])
			require.Equal(t, tc.last, *rows[len(rows)-1].Yields["10y"])
			for _, row := range rows {
				require.Equal(t, "complete", row.Status)
				require.Empty(t, row.MissingTenors)
				require.Equal(t, "official-source", row.Source)
				require.Equal(t, fetchedAt, row.FetchedAt)
			}
		})
	}
}

func TestMissingTenorsAndZero(t *testing.T) {
	var series []rawSeries
	require.NoError(t, json.Unmarshal(sample(t, "treasury-20261019-20261026"), &series))
	series[0].Data[0][1] = json.RawMessage("0")
	series[1].Data[0][1] = json.RawMessage("null")
	series[2].Data = series[2].Data[1:]
	series = series[:3] // 10年期整条缺失，仍输出其他期限可见的所属日。
	series[0], series[2] = series[2], series[0]
	raw, err := json.Marshal(series)
	require.NoError(t, err)
	rows, err := parseTreasury(raw, "20261019", "20261026", "source", time.Now())
	require.NoError(t, err)
	require.Len(t, rows, 6)
	require.NotNil(t, rows[0].Yields["1y"])
	require.Zero(t, *rows[0].Yields["1y"])
	require.Equal(t, []string{"2y", "5y", "10y"}, rows[0].MissingTenors)
	require.Equal(t, "missing-tenors", rows[0].Status)
	encoded, err := json.Marshal(rows[0])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"1y":0`)
	require.Contains(t, string(encoded), `"2y":null`)
	rows, err = parseTreasury([]byte("[]"), "20261024", "20261025", "source", time.Now())
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestRejectInvalidTreasuryResponses(t *testing.T) {
	raw := string(sample(t, "treasury-20261019-20261026"))
	for name, body := range map[string]string{
		"HTML": "<html>busy</html>", "null": "null", "object": "{}", "missing-identity": "[{}]",
		"spot-curve":       strings.ReplaceAll(raw, "(到期)", "(即期)"),
		"other-curve":      strings.ReplaceAll(raw, curveID, "another-id"),
		"bad-yield":        strings.Replace(raw, "1.1813", `"invalid"`, 1),
		"null-timestamp":   strings.Replace(raw, "1792339200000", "null", 1),
		"timestamp-offset": strings.Replace(raw, "1792339200000", "1792339200001", 1),
		"outside-range":    strings.Replace(raw, "1792339200000", "1792252800000", 1),
		"duplicate-date":   strings.Replace(raw, "1792425600000", "1792339200000", 1),
		"duplicate-series": strings.ReplaceAll(strings.ReplaceAll(raw, "012.0", "011.0"), "(2y)", "(1y)"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTreasury([]byte(body), "20261019", "20261026", "source", time.Now())
			require.Error(t, err)
		})
	}
}

func TestTreasuryYearCacheAndFiltering(t *testing.T) {
	raw := sample(t, "treasury-20060301-20060307")
	calls := 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/cbweb-mn/yc/queryYz", r.URL.Path)
		q := r.URL.Query()
		require.Equal(t, curveID, q.Get("ycDefIds"))
		require.Equal(t, "1,1y;2,2y;5,5y;10,10y;", q.Get("dcq"))
		require.Equal(t, "2006-03-01", q.Get("startTime"))
		require.Equal(t, "2006-12-31", q.Get("endTime"))
		require.Equal(t, "0,", q.Get("qxlx"))
		require.Equal(t, "day", q.Get("par"))
		if fail {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write(raw)
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.origin = server.URL
	req := &TreasuryRequest{"20060302", "20060306"}
	result, err := client.Treasury(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 3)
	require.Equal(t, "20060302", result.Observations[0].Date)
	require.Equal(t, "20060306", result.Observations[2].Date)
	require.Equal(t, "yield-to-maturity", result.CurveType)
	require.Nil(t, result.PublishedAt)
	require.Equal(t, 1, calls)
	_, err = client.Treasury(context.Background(), &TreasuryRequest{"20060301", "20060307"})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "同年不同日期区间复用原始缓存")
	client.Refresh = true
	_, err = client.Treasury(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	fail = true
	_, err = client.Treasury(context.Background(), req)
	require.ErrorContains(t, err, "503")
	require.Equal(t, 3, calls)
	client.Refresh = false
	_, err = client.Treasury(context.Background(), req)
	require.NoError(t, err, "失败不能覆盖已有缓存")
	require.Equal(t, 3, calls)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Treasury(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 3, calls)

	path := filepath.Join(client.CacheDir, "treasury-2006.json")
	fail = false
	for _, body := range []string{"corrupt", fmt.Sprintf(`{"source":%q,"fetched_at":%q,"body":[]}`, result.Observations[0].Source, time.Now().Add(-7*time.Hour).Format(time.RFC3339))} {
		require.NoError(t, os.WriteFile(path, []byte(body), 0600))
		_, err = client.Treasury(context.Background(), req)
		require.NoError(t, err)
	}
	require.Equal(t, 5, calls, "损坏和过期缓存重新请求")
}

func TestTreasuryMultipleYearsAndFailure(t *testing.T) {
	calls := 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("startTime") == "2006-03-01" {
			w.Write(sample(t, "treasury-20060301-20060307"))
			return
		}
		if fail {
			fmt.Fprint(w, "<html>busy</html>")
			return
		}
		fmt.Fprint(w, "[]")
	}))
	defer server.Close()
	client := NewClient("")
	client.origin = server.URL
	result, err := client.Treasury(context.Background(), &TreasuryRequest{"20060301", "20070101"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 5)
	require.Equal(t, 2, calls)
	fail = true
	result, err = client.Treasury(context.Background(), &TreasuryRequest{"20060301", "20070101"})
	require.Error(t, err)
	require.Nil(t, result, "任何年份失败不能悄悄输出部分结果")
	require.Equal(t, 4, calls, "无缓存模式重新查询每年")
}

func TestTreasuryRequestValidation(t *testing.T) {
	result, err := NewClient("").Treasury(context.Background(), nil)
	require.ErrorContains(t, err, "请求不能为空")
	require.Nil(t, result)
	for _, req := range []TreasuryRequest{{"20060228", "20060301"}, {"20250230", "20250301"}, {"20250102", "20250101"}, {"2025-01-01", "20250102"}, {"20250101", "29991231"}, {"", "20250101"}} {
		require.Error(t, req.Validate())
	}
}
