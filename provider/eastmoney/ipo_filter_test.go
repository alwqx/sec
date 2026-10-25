package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPOFilterBeforePagination(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		page, _ := strconv.Atoi(r.URL.Query().Get("pn"))
		rows := []map[string]any{}
		start, end := 0, 500
		if page == 2 {
			start, end = 500, 540
		}
		for index := start; index < end; index++ {
			date := 20250101
			if index >= 500 {
				date = 20240115
			}
			rows = append(rows, map[string]any{"f12": fmt.Sprintf("%06d", 600000+index), "f14": "新股", "f26": date})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"total": 540, "diff": rows}})
	}))
	defer server.Close()
	previous := ipoURL
	ipoURL = server.URL
	defer func() { ipoURL = previous }()
	for page, want := range map[int]int{1: 30, 2: 10, 3: 0} {
		rows, total, err := ListIPOFiltered(context.Background(), "2024-01-15", "2024-01-15", page, 30)
		require.NoError(t, err)
		require.Equal(t, 40, total)
		require.Len(t, rows, want)
	}
	require.Equal(t, 6, requests)
}

func TestIPOFilterOpenBoundsAndUnknownDates(t *testing.T) {
	server := newTestServer(t, `{"data":{"total":4,"diff":[{"f12":"600001","f14":"A","f26":20240101},{"f12":"600002","f14":"B","f26":20240201},{"f12":"600003","f14":"C","f26":"-"},{"f12":"600004","f14":"D","f26":20240301}]}}`)
	defer server.Close()
	previous := ipoURL
	ipoURL = server.URL
	defer func() { ipoURL = previous }()
	for _, tc := range []struct {
		since, until string
		want         int
	}{
		{"2024-02-01", "", 2}, {"", "2024-02-01", 2}, {"2025-01-01", "", 0},
	} {
		rows, total, err := ListIPOFiltered(context.Background(), tc.since, tc.until, 1, 30)
		require.NoError(t, err)
		require.Equal(t, tc.want, total)
		require.Len(t, rows, tc.want)
	}
}

func TestIPODatesValidation(t *testing.T) {
	for _, pair := range [][2]string{{"2024-02-30", ""}, {"", "20240101"}, {"2025-01-01", "2024-01-01"}} {
		require.Error(t, ValidateIPODates(pair[0], pair[1]))
	}
	require.NoError(t, ValidateIPODates("", ""))
}

func TestIPOFilterRejectsPartialSource(t *testing.T) {
	server := newTestServer(t, `{"data":{"total":600,"diff":[{"f12":"600001","f14":"A","f26":20240101}]}}`)
	defer server.Close()
	previous := ipoURL
	ipoURL = server.URL
	defer func() { ipoURL = previous }()
	_, _, err := ListIPOFiltered(context.Background(), "2024-01-01", "", 1, 30)
	require.Error(t, err)
}
