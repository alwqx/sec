package nyfed

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
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return body
}

func TestEFFRRealResponse(t *testing.T) {
	body := sample(t, "effr-holidays.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "2024-12-02", r.URL.Query().Get("startDate"))
		require.Equal(t, "2025-01-06", r.URL.Query().Get("endDate"))
		require.NotEmpty(t, r.Header.Get("User-Agent"))
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := NewClient("")
	client.endpoint = server.URL
	result, err := client.EFFR(context.Background(), &EFFRRequest{Begin: "20250102", End: "20250106"})
	require.NoError(t, err)
	require.Equal(t, "%", result.Unit)
	require.Len(t, result.Observations, 3)
	require.Equal(t, "2025-01-02", result.Observations[0].EffectiveDate)
	require.Equal(t, "2024-12-31", result.Observations[0].PreviousDate)
	require.Equal(t, 4.33, result.Observations[0].RatePercent)
	require.Equal(t, 0.0, *result.Observations[0].ChangeBasisPoints)
	require.Nil(t, result.Observations[0].PublishedAt)
	require.False(t, result.Observations[0].FetchedAt.IsZero())
	require.Contains(t, string(result.Observations[0].Raw), "volumeInBillions")
	require.Equal(t, "2025-01-06", result.Observations[2].EffectiveDate)
}

func TestEFFRMethodologyAndBasisPoints(t *testing.T) {
	body := sample(t, "effr-methodology.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }))
	defer server.Close()
	client := NewClient("")
	client.endpoint = server.URL
	result, err := client.EFFR(context.Background(), &EFFRRequest{Begin: "20160229", End: "20160304"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 5)
	require.Equal(t, -8.0, *result.Observations[0].ChangeBasisPoints)
	require.Contains(t, result.Observations[0].Methodology, "旧口径")
	require.Equal(t, "2016-02-29", result.Observations[1].PreviousDate)
	require.Nil(t, result.Observations[1].ChangeBasisPoints)
	require.Contains(t, result.Observations[1].Methodology, "中位数")
	require.Equal(t, 1.0, *result.Observations[2].ChangeBasisPoints)
}

func TestEFFREarlyAndRevision(t *testing.T) {
	body := sample(t, "effr-early.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "2000-07-03", r.URL.Query().Get("startDate"))
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := NewClient("")
	client.endpoint = server.URL
	result, err := client.EFFR(context.Background(), &EFFRRequest{Begin: "20000703", End: "20000710"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 5)
	require.Nil(t, result.Observations[0].ChangeBasisPoints)
	require.Empty(t, result.Observations[0].PreviousDate)
	require.Equal(t, "r", result.Observations[4].RevisionIndicator)
	require.Contains(t, string(result.Observations[4].Raw), "intraDayLow")
}

func TestEFFRWalkBackForPrevious(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			fmt.Fprint(w, `{"refRates":[{"effectiveDate":"2025-01-06","type":"EFFR","percentRate":4.33}]}`)
		case 2:
			require.Equal(t, "2023-12-06", r.URL.Query().Get("startDate"))
			require.Equal(t, "2024-12-05", r.URL.Query().Get("endDate"))
			fmt.Fprint(w, `{"refRates":[{"effectiveDate":"2024-12-05","type":"EFFR","percentRate":4.58}]}`)
		default:
			t.Fatal("不应继续查询")
		}
	}))
	defer server.Close()
	client := NewClient("")
	client.endpoint = server.URL
	result, err := client.EFFR(context.Background(), &EFFRRequest{Begin: "20250106", End: "20250106"})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, "2024-12-05", result.Observations[0].PreviousDate)
	require.Equal(t, -25.0, *result.Observations[0].ChangeBasisPoints)
}

func TestEFFRCache(t *testing.T) {
	dir := t.TempDir()
	body := sample(t, "effr-holidays.json")
	calls := 0
	broken := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if broken {
			fmt.Fprint(w, `{"refRates":[{"effectiveDate":"2025-01-02","type":"EFFR"}]}`)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()
	client := NewClient(dir)
	client.endpoint = server.URL
	req := &EFFRRequest{Begin: "20250102", End: "20250106"}
	first, err := client.EFFR(context.Background(), req)
	require.NoError(t, err)
	second, err := client.EFFR(context.Background(), req)
	require.NoError(t, err)
	firstJSON, err := json.Marshal(first)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second)
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(secondJSON))
	require.Equal(t, 1, calls)
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	require.NoError(t, err)
	require.Len(t, paths, 1)
	original, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	broken = true
	client.Refresh = true
	_, err = client.EFFR(context.Background(), req)
	require.Error(t, err)
	unchanged, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	require.Equal(t, original, unchanged)
	broken = false
	_, err = client.EFFR(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 3, calls)
	client.Refresh = false
	for _, age := range []time.Duration{7 * time.Hour, -time.Hour} {
		cache := cachedResponse{FetchedAt: time.Now().Add(-age), Body: body}
		raw, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(paths[0], raw, 0600))
		_, err = client.EFFR(context.Background(), req)
		require.NoError(t, err)
	}
	require.Equal(t, 5, calls)
	require.NoError(t, os.WriteFile(paths[0], []byte(`broken`), 0600))
	_, err = client.EFFR(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 6, calls)
	client.CacheDir = ""
	_, err = client.EFFR(context.Background(), req)
	require.NoError(t, err)
	_, err = client.EFFR(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 8, calls)
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
}

func TestEFFRValidationErrorsAndCancellation(t *testing.T) {
	var req *EFFRRequest
	require.Error(t, req.Validate())
	for _, r := range []EFFRRequest{{"20000702", "20000710"}, {"20250230", "20250301"}, {"2025-01-01", "20250102"}, {"20250106", "20250105"}, {"20250101", "29990101"}} {
		require.Error(t, r.Validate())
	}
	begin, _ := time.Parse("20060102", "20250101")
	end, _ := time.Parse("20060102", "20250110")
	for _, body := range []string{`not json`, `{}`, `{"refRates":null}`, `{"refRates":[{"effectiveDate":"2025-01-02","type":"OBFR","percentRate":4.3}]}`, `{"refRates":[{"effectiveDate":"2025-01-02","type":"EFFR"}]}`, `{"refRates":[{"effectiveDate":"2024-12-31","type":"EFFR","percentRate":4.3}]}`, `{"refRates":[{"effectiveDate":"2025-01-02","type":"EFFR","percentRate":4.3},{"effectiveDate":"2025-01-02","type":"EFFR","percentRate":4.3}]}`} {
		_, err := parseResponse([]byte(body), begin, end)
		require.Error(t, err, body)
	}
	points, err := parseResponse([]byte(`{"refRates":[]}`), begin, end)
	require.NoError(t, err)
	require.Empty(t, points)
	client := NewClient("")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.EFFR(ctx, &EFFRRequest{Begin: "20250101", End: "20250110"})
	require.ErrorIs(t, err, context.Canceled)
	for _, status := range []int{400, 429, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		client.endpoint = server.URL
		_, err = client.EFFR(context.Background(), &EFFRRequest{Begin: "20250101", End: "20250110"})
		require.ErrorContains(t, err, fmt.Sprint(status))
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client.endpoint = server.URL
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = client.EFFR(ctx, &EFFRRequest{Begin: "20250101", End: "20250110"})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, strings.Contains(err.Error(), "缓存"))
}
