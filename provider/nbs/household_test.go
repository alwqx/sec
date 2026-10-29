package nbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func householdFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/household/" + name + ".json")
	require.NoError(t, err)
	return raw
}

func householdServer(t *testing.T, mutate func(string, []byte) []byte) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		name := ""
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			require.Equal(t, "2", r.URL.Query().Get("code"))
			name = map[string]string{"": "roots", "a94b8b7365a94874968cabbe392cf679": "groups", "85ee7ccd4212489d9d3b774c8558e274": "catalogs"}[r.URL.Query().Get("pid")]
		case "/new/queryIndicatorsByCid":
			name = map[string]string{"ec2d57ed282f456e8d025aff035b4fad": "income-metadata", "71e8c15775d242b8b212edb4cab6e808": "consumption-metadata"}[r.URL.Query().Get("cid")]
		case "/stream/esData":
			require.Equal(t, http.MethodPost, r.Method)
			var body struct {
				CID   string   `json:"cid"`
				Root  string   `json:"rootId"`
				IDs   []string `json:"indicatorIds"`
				Dates []string `json:"dts"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "85ee7ccd4212489d9d3b774c8558e274", body.Root)
			require.Len(t, body.IDs, 2)
			name = map[string]string{"ec2d57ed282f456e8d025aff035b4fad": "income-values", "71e8c15775d242b8b212edb4cab6e808": "consumption-values"}[body.CID]
			require.NotEmpty(t, name)
			var data struct {
				Success bool       `json:"success"`
				Data    []valueRow `json:"data"`
			}
			require.NoError(t, json.Unmarshal(householdFixture(t, name), &data))
			require.Len(t, body.Dates, 1)
			bounds := strings.Split(body.Dates[0], "-")
			filtered := []valueRow{}
			for _, row := range data.Data {
				if row.Code >= bounds[0] && row.Code <= bounds[1] {
					filtered = append(filtered, row)
				}
			}
			data.Data = filtered
			raw, err := json.Marshal(data)
			require.NoError(t, err)
			if mutate != nil {
				raw = mutate(name, raw)
			}
			_, _ = w.Write(raw)
			return
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
		}
		require.NotEmpty(t, name)
		raw := householdFixture(t, name)
		if mutate != nil {
			raw = mutate(name, raw)
		}
		_, _ = w.Write(raw)
	}))
	return server, &calls
}

func TestHouseholdOfficialValuesAndCache(t *testing.T) {
	server, calls := householdServer(t, nil)
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := &HouseholdRequest{Begin: "2013Q1", End: "2026Q4"}
	got, err := client.Household(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got.Observations, 56)
	require.Len(t, got.Series, 4)
	first := got.Observations[0]
	require.Equal(t, 5006.0, *first.IncomeYTD)
	require.Equal(t, 3370.0, *first.ConsumptionYTD)
	require.Nil(t, first.IncomeRealYTDYoY)
	require.Nil(t, first.ConsumptionRealYTDYoY)
	require.Equal(t, "missing-values", first.Status)
	require.JSONEq(t, `"5006"`, string(first.RawValues["income-ytd"]))
	require.JSONEq(t, `""`, string(first.RawValues["income-real-yoy"]))
	annual := got.Observations[51]
	require.Equal(t, "2025Q4", annual.Period)
	require.Equal(t, 43377.0, *annual.IncomeYTD)
	require.Equal(t, 29476.0, *annual.ConsumptionYTD)
	require.True(t, annual.IncludesOwnProduction)
	recent := got.Observations[54]
	require.Equal(t, "2026Q3", recent.Period)
	require.Equal(t, 34230.0, *recent.IncomeYTD)
	require.Equal(t, 22354.0, *recent.ConsumptionYTD)
	require.Equal(t, 4.4, *recent.IncomeRealYTDYoY)
	require.Equal(t, 2.7, *recent.ConsumptionRealYTDYoY)
	require.False(t, recent.IncludesOwnProduction)
	require.Equal(t, "complete", recent.Status)
	require.Nil(t, got.Observations[55].IncomeYTD)
	require.Nil(t, got.PublishedAt)
	require.Nil(t, got.RevisionID)
	require.Equal(t, "year-to-date-per-capita-flow", got.Measurement)
	require.False(t, got.SeasonallyAdjusted)
	require.False(t, got.Annualized)
	for _, s := range got.Series {
		require.False(t, s.FetchedAt.IsZero())
		require.NotEmpty(t, s.Annotation)
		if strings.HasSuffix(s.Key, "yoy") {
			require.Equal(t, "real-growth", s.PriceBasis)
		}
	}
	before := *calls
	cached, err := client.Household(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, got, cached)
	require.Equal(t, before, *calls)
	client.Refresh = true
	_, err = client.Household(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, before+7, *calls)
}

func TestHouseholdValuesRejectMalformedAndPreserveMissing(t *testing.T) {
	req := &HouseholdRequest{Begin: "2026Q1", End: "2026Q3"}
	selected := []HouseholdSeries{{Key: "income-ytd", SeriesID: "value", PriceBasis: "current-prices"}, {Key: "income-real-yoy", SeriesID: "growth", PriceBasis: "real-growth"}}
	parse := func(raw string) (map[string]map[string]json.RawMessage, error) {
		var rows []valueRow
		require.NoError(t, json.Unmarshal([]byte(raw), &rows))
		return parseHousehold(rows, selected, req)
	}
	for _, bad := range []string{
		`[{"code":"202601SS","values":[{"_id":"value","da":"000000000000","value":"NaN"}]}]`,
		`[{"code":"202601SS","values":[{"_id":"growth","da":"000000000000","value":"Inf"}]}]`,
		`[{"code":"202601SS","values":[{"_id":"value","da":"000000000000","value":"-1"}]}]`,
		`[{"code":"202601SS","values":[{"_id":"value","da":"000000000000","value":"1"},{"_id":"value","da":"000000000000","value":"2"}]}]`,
		`[{"code":"202601SS"},{"code":"202601SS"}]`,
		`[{"code":"202600SS"}]`, `[{"code":"202605SS"}]`, `[{"code":"202601XX"}]`, `[{"code":"202501SS"}]`,
	} {
		_, err := parse(bad)
		require.Error(t, err, bad)
	}
	values, err := parse(`[{"code":"202601SS","values":[{"_id":"value","da":"000000000000","value":"0"},{"_id":"growth","da":"000000000000","value":"-5.2"}]},{"code":"202602SS","values":[{"_id":"value","da":"000000000000","value":""},{"_id":"growth","da":"000000000000","value":null}]}]`)
	require.NoError(t, err)
	n, err := parseNumber(values["2026Q1"]["income-ytd"])
	require.NoError(t, err)
	require.Zero(t, *n)
	n, err = parseNumber(values["2026Q1"]["income-real-yoy"])
	require.NoError(t, err)
	require.Equal(t, -5.2, *n)
	n, err = parseNumber(values["2026Q2"]["income-ytd"])
	require.NoError(t, err)
	require.Nil(t, n)
	require.NotContains(t, values, "2026Q3")
}

func TestHouseholdBadRefreshPreservesCache(t *testing.T) {
	mode := ""
	server, _ := householdServer(t, func(name string, raw []byte) []byte {
		if name == "income-values" && mode == "values" {
			return []byte(`{"success":true,"data":[{"code":"202603SS","values":[{"_id":"bb5699c5ad534b568cca7c946227225a","da":"000000000000","value":"NaN"}]}]}`)
		}
		if name == "income-metadata" && mode == "metadata" {
			return []byte(strings.ReplaceAll(string(raw), "扣除价格因素的实际增速", "名义增速"))
		}
		return raw
	})
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := &HouseholdRequest{Begin: "2026Q3", End: "2026Q3"}
	good, err := client.Household(context.Background(), req)
	require.NoError(t, err)
	files, err := filepath.Glob(filepath.Join(client.CacheDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 7)
	saved := map[string][]byte{}
	for _, file := range files {
		saved[file], err = os.ReadFile(file)
		require.NoError(t, err)
	}
	for _, bad := range []string{"values", "metadata"} {
		mode = bad
		client.Refresh = true
		_, err = client.Household(context.Background(), req)
		require.Error(t, err)
		// 成功刷新的目录有新抓取时间，仅检查坏响应对应缓存的原始正文未被覆盖。
		for _, file := range files {
			var old, current cacheEntry
			require.NoError(t, json.Unmarshal(saved[file], &old))
			raw, e := os.ReadFile(file)
			require.NoError(t, e)
			require.NoError(t, json.Unmarshal(raw, &current))
			require.JSONEq(t, string(old.Body), string(current.Body))
		}
		client.Refresh = false
		cached, e := client.Household(context.Background(), req)
		require.NoError(t, e)
		require.Equal(t, good.Observations, cached.Observations)
	}
}

func TestHouseholdValidationAndCancellation(t *testing.T) {
	for _, req := range []*HouseholdRequest{nil, {Begin: "2012Q4", End: "2013Q1"}, {Begin: "2026Q0", End: "2026Q1"}, {Begin: "2026Q3", End: "2026Q2"}, {Begin: "2026Q1", End: "9999Q4"}} {
		_, err := NewClient("").Household(context.Background(), req)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").Household(ctx, &HouseholdRequest{Begin: "2026Q3", End: "2026Q3"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestHouseholdRejectsIncompleteMetadata(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "same-id", "truncated", "empty-id"} {
		t.Run(mode, func(t *testing.T) {
			server, _ := householdServer(t, func(name string, raw []byte) []byte {
				if name != "income-metadata" {
					return raw
				}
				var response struct {
					Success bool `json:"success"`
					Data    struct {
						Total int              `json:"total"`
						List  []map[string]any `json:"list"`
					} `json:"data"`
				}
				require.NoError(t, json.Unmarshal(raw, &response))
				switch mode {
				case "missing":
					response.Data.List = response.Data.List[1:]
					response.Data.Total--
				case "duplicate":
					response.Data.List = append(response.Data.List, response.Data.List[0])
					response.Data.Total++
				case "same-id":
					response.Data.List[1]["_id"] = response.Data.List[0]["_id"]
				case "truncated":
					response.Data.Total++
				case "empty-id":
					response.Data.List[0]["_id"] = ""
				}
				data, err := json.Marshal(response)
				require.NoError(t, err)
				return data
			})
			defer server.Close()
			client := NewClient("")
			client.baseURL = server.URL
			_, err := client.Household(context.Background(), &HouseholdRequest{Begin: "2026Q3", End: "2026Q3"})
			require.Error(t, err)
		})
	}
}
