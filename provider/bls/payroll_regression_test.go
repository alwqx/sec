package bls

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPayrollsBatchRevisionBaselines(t *testing.T) {
	for _, mode := range []string{"revised", "missing", "failure"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body struct {
					Start string `json:"startyear"`
					End   string `json:"endyear"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				first, _ := strconv.Atoi(body.Start)
				last, _ := strconv.Atoi(body.End)
				require.LessOrEqual(t, last-first, 9)
				points := []rawPoint{{Year: "2025", Period: "M11", Value: "990"}, {Year: "2025", Period: "M12", Value: "1000"}}
				if first == 2016 {
					require.Equal(t, 2025, last)
				} else {
					require.Equal(t, 2025, first)
					require.Equal(t, 2026, last)
					if mode == "failure" {
						http.Error(w, "failed", 503)
						return
					}
					points = []rawPoint{{Year: "2026", Period: "M01", Value: "910"}}
					if mode == "revised" {
						points = append(points, rawPoint{Year: "2025", Period: "M12", Value: "900"})
					}
				}
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status": "REQUEST_SUCCEEDED", "message": []string{}, "Results": map[string]any{"series": []any{map[string]any{"seriesID": payrollSeriesID, "data": points}}}}))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.endpoint = server.URL
			_, err := client.Payrolls(context.Background(), &PayrollRequest{Begin: "201701", End: "202512"})
			require.NoError(t, err)
			require.Equal(t, 1, calls, "最后一年恰逢批次边界时不要重复请求")
			path := filepath.Join(client.CacheDir, "payrolls-observed.json")
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			got, err := client.Payrolls(context.Background(), &PayrollRequest{Begin: "201701", End: "202601"})
			require.Equal(t, 2, calls, "复用旧批次缓存，仅查询重叠年及新年份")
			if mode == "failure" {
				require.ErrorContains(t, err, "503")
				require.Nil(t, got)
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, before, after, "失败不能更新观测档案")
				return
			}
			require.NoError(t, err)
			require.Len(t, got.Observations, 109)
			dec, jan := got.Observations[107], got.Observations[108]
			require.Equal(t, "202512", dec.Period)
			require.Equal(t, 1000.0, *dec.Employment)
			require.Equal(t, 10.0, *dec.Change)
			require.Equal(t, "202601", jan.Period)
			if mode == "missing" {
				require.Nil(t, jan.Previous.Employment)
				require.Nil(t, jan.Change, "不借用另一批次的旧基期")
			} else {
				require.Equal(t, 900.0, *jan.Previous.Employment)
				require.Equal(t, 10.0, *jan.Change)
				require.Equal(t, jan.FetchedAt, jan.Previous.FetchedAt)
			}
		})
	}
}

func TestPayrollObservationWatermark(t *testing.T) {
	client := NewClient(t.TempDir())
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	observe := func(value float64, offset int) map[string][]PayrollVersion {
		history, err := client.observePayrollVersions(context.Background(), map[string]*monthlyValue{"202601": {Number: &value, RawValue: strconv.FormatFloat(value, 'f', 0, 64), Footnotes: []Footnote{}, FetchedAt: base.Add(time.Duration(offset) * time.Hour)}})
		require.NoError(t, err)
		return history
	}
	require.Len(t, observe(100, 1)["202601"], 1)
	require.Len(t, observe(100, 3)["202601"], 1, "数值不变时不新增版本")
	// 模拟较早抓取的请求晚于较新请求取得档案锁。
	late := observe(90, 2)
	require.Len(t, late["202601"], 1)
	require.Equal(t, 100.0, late["202601"][0].Employment)
	require.Len(t, observe(110, 4)["202601"], 2)
	var saved payrollHistory
	raw, err := os.ReadFile(filepath.Join(client.CacheDir, "payrolls-observed.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.Equal(t, base.Add(4*time.Hour), saved.LatestObservedAt["202601"])
}

func TestPayrollArchiveRejectsIncompleteJSON(t *testing.T) {
	for _, raw := range []string{"null", "{}", `{"schema":1,"series_id":"CES0000000001"}`, `{"schema":1,"periods":{}}`, `{"series_id":"CES0000000001","periods":{}}`, `{"schema":1,"series_id":"CES0000000001","periods":null}`} {
		t.Run(raw, func(t *testing.T) {
			client := NewClient(t.TempDir())
			path := filepath.Join(client.CacheDir, "payrolls-observed.json")
			require.NoError(t, os.WriteFile(path, []byte(raw), 0600))
			value := 100.0
			_, err := client.observePayrollVersions(context.Background(), map[string]*monthlyValue{"202601": {Number: &value, RawValue: "100", FetchedAt: time.Now().UTC()}})
			require.ErrorContains(t, err, "格式无效")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, raw, string(after))
		})
	}
}

func TestPayrollLegacyArchiveMigration(t *testing.T) {
	client := NewClient(t.TempDir())
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	legacy := payrollHistory{Schema: 1, SeriesID: payrollSeriesID, Periods: map[string][]PayrollVersion{"202601": {{Employment: 100, RawValue: "100", Footnotes: []Footnote{}, ObservedAt: base}}}}
	raw, err := json.Marshal(legacy)
	require.NoError(t, err)
	path := filepath.Join(client.CacheDir, "payrolls-observed.json")
	require.NoError(t, os.WriteFile(path, raw, 0600))
	value := 100.0
	history, err := client.observePayrollVersions(context.Background(), map[string]*monthlyValue{"202601": {Number: &value, RawValue: "100", Footnotes: []Footnote{}, FetchedAt: base.Add(time.Hour)}})
	require.NoError(t, err)
	require.Len(t, history["202601"], 1)
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	var migrated payrollHistory
	require.NoError(t, json.Unmarshal(raw, &migrated))
	require.Equal(t, base.Add(time.Hour), migrated.LatestObservedAt["202601"])
	require.Equal(t, legacy.Periods, migrated.Periods)
}

func TestPayrollConcurrentWatermarks(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	value := 100.0
	_, err := NewClient(dir).observePayrollVersions(context.Background(), map[string]*monthlyValue{"202601": {Number: &value, RawValue: "100", Footnotes: []Footnote{}, FetchedAt: base}})
	require.NoError(t, err)
	var group sync.WaitGroup
	errors := make(chan error, 2)
	start := make(chan struct{})
	for _, item := range []struct {
		value float64
		hours int
	}{{90, 1}, {100, 2}} {
		group.Add(1)
		go func(value float64, hours int) {
			defer group.Done()
			<-start
			_, err := NewClient(dir).observePayrollVersions(context.Background(), map[string]*monthlyValue{"202601": {Number: &value, RawValue: strconv.FormatFloat(value, 'f', 0, 64), Footnotes: []Footnote{}, FetchedAt: base.Add(time.Duration(hours) * time.Hour)}})
			errors <- err
		}(item.value, item.hours)
	}
	close(start)
	group.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "payrolls-observed.json"))
	require.NoError(t, err)
	var saved payrollHistory
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.NoError(t, validatePayrollHistory(&saved))
	versions := saved.Periods["202601"]
	require.Equal(t, 100.0, versions[len(versions)-1].Employment)
	require.Equal(t, base.Add(2*time.Hour), saved.LatestObservedAt["202601"])
}
