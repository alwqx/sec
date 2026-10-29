package nbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func propertySamples(t *testing.T, component string) ([]valueRow, [2]propertySeries) {
	t.Helper()
	var response struct {
		Data []valueRow `json:"data"`
	}
	var metadata struct {
		Data struct {
			List []propertySeries `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "property-"+component+"-values"), &response))
	require.NoError(t, json.Unmarshal(fixture(t, "property-"+component+"-series"), &metadata))
	spec := propertySpecs[component]
	var selected [2]propertySeries
	for _, s := range metadata.Data.List {
		if compact(s.Name) == spec.amountName {
			selected[0] = s
		}
		if compact(s.Name) == spec.growthName {
			selected[1] = s
		}
	}
	return response.Data, selected
}

func TestPropertyOfficialSamplesAndCache(t *testing.T) {
	var calls atomic.Int32
	files := map[string][]byte{}
	for _, name := range []string{"root", "categories", "property-catalogs", "property-sales-area-series", "property-sales-area-values", "property-sales-value-series", "property-sales-value-values", "property-starts-area-series", "property-starts-area-values"} {
		files[name] = fixture(t, name)
	}
	components := map[string]string{"0ae633cdb85f4a8397650831b2b27e50": "sales-area", "5530aa75038249fab2e5e10a9997c18b": "sales-value", "cac0766314e045ea82f69886aabd31b0": "starts-area"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			name := map[string]string{"": "root", "fc982599aa684be7969d7b90b1bd0e84": "categories", "a7d36f218969426daaeeddd1c6c5823a": "property-catalogs"}[r.URL.Query().Get("pid")]
			if name == "" {
				http.Error(w, "错误目录", http.StatusBadRequest)
				return
			}
			w.Write(files[name])
		case "/new/queryIndicatorsByCid":
			component := components[r.URL.Query().Get("cid")]
			if component == "" || r.URL.Query().Get("pageSize") != "1000" {
				http.Error(w, "错误指标目录", http.StatusBadRequest)
				return
			}
			w.Write(files["property-"+component+"-series"])
		case "/stream/esData":
			var body struct {
				Catalog string              `json:"cid"`
				Root    string              `json:"rootId"`
				IDs     []string            `json:"indicatorIds"`
				Dates   []string            `json:"dts"`
				Areas   []map[string]string `json:"das"`
			}
			if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil || components[body.Catalog] == "" {
				http.Error(w, "错误查询参数", http.StatusBadRequest)
				return
			}
			component := components[body.Catalog]
			_, selected := propertySamples(t, component)
			if body.Root != "a7d36f218969426daaeeddd1c6c5823a" || len(body.IDs) != 2 || body.IDs[0] != selected[0].ID || body.IDs[1] != selected[1].ID || len(body.Dates) != 1 || body.Dates[0] != "202601MM-202609MM" || len(body.Areas) != 1 || body.Areas[0]["value"] != "000000000000" {
				http.Error(w, "应查询全国总量的累计值和累计同比", http.StatusBadRequest)
				return
			}
			w.Write(files["property-"+component+"-values"])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := &PropertyRequest{Begin: "202601", End: "202609"}
	result, err := client.Property(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 27)
	require.Equal(t, int32(9), calls.Load())
	for _, item := range result.Observations[:3] {
		require.Equal(t, "202601", item.Period)
		require.Nil(t, item.YTDAmount)
		require.Nil(t, item.YTDGrowth)
		require.Contains(t, item.Note, "1月不拆出")
	}
	for _, item := range result.Observations[3:6] {
		require.Equal(t, "202601-202602", item.YTDPeriod)
		require.Contains(t, item.Note, "1—2月合并累计")
	}
	expected := map[string][2]float64{"sales-area": {56482.90, -13.2}, "sales-value": {53798.77, -13.9}, "starts-area": {34031.30, -24.9}}
	for _, item := range result.Observations[24:] {
		require.Equal(t, "202609", item.Period)
		require.Equal(t, expected[item.Component][0], *item.YTDAmount)
		require.Equal(t, expected[item.Component][1], *item.YTDGrowth)
		require.Nil(t, item.PublishedAt)
		require.False(t, item.SeasonallyAdjusted)
		require.Equal(t, "%", item.GrowthUnit)
		require.NotEmpty(t, item.Definition)
		require.NotEmpty(t, item.OriginalMetricName)
		require.NotEmpty(t, item.CoverageStart)
	}
	cached, err := client.Property(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, cached)
	require.Equal(t, int32(9), calls.Load())
	client.Refresh = true
	_, err = client.Property(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(18), calls.Load())
	client.CacheDir = ""
	client.Refresh = false
	_, err = client.Property(context.Background(), &PropertyRequest{Begin: "202601", End: "202609", Component: "starts-area"})
	require.NoError(t, err)
	require.Equal(t, int32(23), calls.Load())
	_, err = client.Property(context.Background(), &PropertyRequest{Begin: "202601", End: "202609", Component: "starts-area"})
	require.NoError(t, err)
	require.Equal(t, int32(28), calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Property(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, int32(28), calls.Load())
}

func TestPropertyParseMissingZeroAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]valueRow) []valueRow
		err  string
	}{
		{"zero", func(rows []valueRow) []valueRow { rows[0].Values[0].Value = json.RawMessage(`"0"`); return rows }, ""},
		{"missing", func(rows []valueRow) []valueRow { rows[0].Values[0].Value = json.RawMessage(`null`); return rows }, ""},
		{"other-area", func(rows []valueRow) []valueRow { rows[0].Values[0].Area = "110000000000"; return rows }, ""},
		{"negative", func(rows []valueRow) []valueRow { rows[0].Values[0].Value = json.RawMessage(`"-1"`); return rows }, "不能为负"},
		{"non-finite", func(rows []valueRow) []valueRow { rows[0].Values[0].Value = json.RawMessage(`"NaN"`); return rows }, "无效数值"},
		{"bad-month", func(rows []valueRow) []valueRow { rows[0].Code = "202613MM"; return rows }, "无效的房地产月份"},
		{"duplicate-month", func(rows []valueRow) []valueRow { return append(rows, rows[0]) }, "重复月份"},
		{"duplicate-value", func(rows []valueRow) []valueRow {
			rows[0].Values = append(rows[0].Values, rows[0].Values[0])
			return rows
		}, "重复全国观测"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, selected := propertySamples(t, "sales-area")
			items, err := parseProperty(tc.edit(rows), &PropertyRequest{Begin: "202601", End: "202609"}, "sales-area", node{ID: selected[0].CatalogID}, selected, time.Now())
			if tc.err != "" {
				require.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			if tc.name == "zero" {
				require.NotNil(t, items[0].YTDAmount)
				require.Zero(t, *items[0].YTDAmount)
			} else {
				require.Nil(t, items[0].YTDAmount)
			}
		})
	}
}

func TestPropertyHistoricalScope(t *testing.T) {
	var response struct {
		Data []valueRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "property-sales-area-history-values"), &response))
	_, selected := propertySamples(t, "sales-area")
	items, err := parseProperty(response.Data, &PropertyRequest{Begin: "200001", End: "200509"}, "sales-area", node{}, selected, time.Now())
	require.NoError(t, err)
	require.Len(t, items, 69)
	byMonth := map[string]PropertyObservation{}
	for _, item := range items {
		byMonth[item.Period] = item
	}
	require.Nil(t, byMonth["200001"].YTDAmount)
	require.Equal(t, 636.1, *byMonth["200002"].YTDAmount)
	require.Equal(t, "before_200508_actual_sales", byMonth["200507"].ScopeVersion)
	require.Equal(t, "200508_transition_unspecified", byMonth["200508"].ScopeVersion)
	require.Equal(t, "after_200508_presales_and_completed_sales", byMonth["200509"].ScopeVersion)
	require.Contains(t, byMonth["200508"].Note, "未明确")
}

func TestPropertyMetadataValidation(t *testing.T) {
	_, original := propertySamples(t, "sales-area")
	for _, tc := range []struct {
		name string
		edit func([]propertySeries) []propertySeries
	}{
		{"duplicate", func(s []propertySeries) []propertySeries { return append(s, s[0]) }},
		{"unit", func(s []propertySeries) []propertySeries { s[0].Unit = "平方米"; return s }},
		{"wrong-catalog", func(s []propertySeries) []propertySeries { s[0].CatalogID = "residential"; return s }},
		{"missing", func(s []propertySeries) []propertySeries { return s[:1] }},
		{"empty-id", func(s []propertySeries) []propertySeries { s[0].ID = ""; return s }},
		{"duplicate-id", func(s []propertySeries) []propertySeries { s[1].ID = s[0].ID; return s }},
		{"residential", func(s []propertySeries) []propertySeries {
			s[0].Name = "商品住宅销售面积累计值(万平方米)"
			return s
		}},
		{"truncated", func(s []propertySeries) []propertySeries { return s }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			list := tc.edit(append([]propertySeries{}, original[:]...))
			total := len(list)
			if tc.name == "truncated" {
				total++
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"list": list, "total": total}})
			}))
			defer server.Close()
			client := NewClient("")
			client.baseURL = server.URL
			_, err := client.propertyMetadata(context.Background(), node{ID: original[0].CatalogID}, propertySpecs["sales-area"])
			require.Error(t, err)
		})
	}
	_, err := propertyNode([]node{{ID: "a", Name: "房地产"}, {ID: "b", Name: "房地产"}}, "房地产")
	require.ErrorContains(t, err, "目录重复")
}

func TestPropertyRequestValidation(t *testing.T) {
	for _, req := range []*PropertyRequest{nil, {Begin: "199912", End: "200001"}, {Begin: "202601", End: "202613"}, {Begin: "202609", End: "202601"}, {Begin: "202601", End: "202609", Component: "residential"}} {
		require.Error(t, req.Validate())
	}
	for _, component := range []string{"", "all", "sales-area", "sales-value", "starts-area"} {
		require.NoError(t, (&PropertyRequest{Begin: "200001", End: "202609", Component: component}).Validate())
	}
}

// 通过原始响应定位缓存，不依赖请求缓存文件名的生成方式。
func propertyCachedResponsePath(t *testing.T, dir string, response []byte) string {
	t.Helper()
	var expected any
	require.NoError(t, json.Unmarshal(response, &expected))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, file := range entries {
		path := filepath.Join(dir, file.Name())
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		var entry cacheEntry
		require.NoError(t, json.Unmarshal(raw, &entry))
		var body any
		require.NoError(t, json.Unmarshal(entry.Body, &body))
		if reflect.DeepEqual(expected, body) {
			return path
		}
	}
	t.Fatal("未找到对应的房地产响应缓存")
	return ""
}

func TestPropertyInvalidRefreshPreservesCacheAndRecovers(t *testing.T) {
	metadataList := func(response map[string]any) []any { return response["data"].(map[string]any)["list"].([]any) }
	valueRows := func(response map[string]any) []any { return response["data"].([]any) }
	firstValue := func(response map[string]any) map[string]any {
		return valueRows(response)[0].(map[string]any)["values"].([]any)[0].(map[string]any)
	}
	for _, tc := range []struct {
		name, target string
		edit         func(map[string]any)
	}{
		{"missing-metadata", "metadata", func(r map[string]any) { r["data"] = map[string]any{"total": 0, "list": []any{}} }},
		{"wrong-unit", "metadata", func(r map[string]any) { metadataList(r)[0].(map[string]any)["du_name"] = "平方米" }},
		{"duplicate-id", "metadata", func(r map[string]any) {
			metadataList(r)[1].(map[string]any)["_id"] = metadataList(r)[0].(map[string]any)["_id"]
		}},
		{"failed-metadata", "metadata", func(r map[string]any) { r["success"] = false }},
		{"missing-metadata-success", "metadata", func(r map[string]any) { delete(r, "success") }},
		{"negative-amount", "values", func(r map[string]any) { firstValue(r)["value"] = "-1" }},
		{"non-finite-value", "values", func(r map[string]any) { firstValue(r)["value"] = "NaN" }},
		{"duplicate-month", "values", func(r map[string]any) { r["data"] = append(valueRows(r), valueRows(r)[0]) }},
		{"invalid-month", "values", func(r map[string]any) { valueRows(r)[0].(map[string]any)["code"] = "202613MM" }},
		{"failed-values", "values", func(r map[string]any) { r["success"] = false }},
		{"missing-values-success", "values", func(r map[string]any) { delete(r, "success") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{}
			for _, name := range []string{"root", "categories", "property-catalogs", "property-sales-area-series", "property-sales-area-values"} {
				files[name] = fixture(t, name)
			}
			targetFile := "property-sales-area-series"
			if tc.target == "values" {
				targetFile = "property-sales-area-values"
			}
			validRaw := files[targetFile]
			var invalid map[string]any
			require.NoError(t, json.Unmarshal(validRaw, &invalid))
			tc.edit(invalid)
			invalidRaw, err := json.Marshal(invalid)
			require.NoError(t, err)
			var response atomic.Value
			response.Store(validRaw)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				name := ""
				switch r.URL.Path {
				case "/new/queryIndexTreeAsync":
					name = map[string]string{"": "root", "fc982599aa684be7969d7b90b1bd0e84": "categories", "a7d36f218969426daaeeddd1c6c5823a": "property-catalogs"}[r.URL.Query().Get("pid")]
				case "/new/queryIndicatorsByCid":
					name = "property-sales-area-series"
				case "/stream/esData":
					name = "property-sales-area-values"
				}
				if name == "" {
					http.NotFound(w, r)
					return
				}
				if name == targetFile {
					w.Write(response.Load().([]byte))
				} else {
					w.Write(files[name])
				}
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.baseURL = server.URL
			req := &PropertyRequest{Begin: "202601", End: "202609", Component: "sales-area"}
			original, err := client.Property(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, original.Observations, 9)
			path := propertyCachedResponsePath(t, client.CacheDir, validRaw)
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			client.Refresh = true
			response.Store(invalidRaw)
			_, err = client.Property(context.Background(), req)
			require.Error(t, err)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, before, after, "无效刷新不能覆盖该响应的有效缓存")

			client.Refresh = false
			response.Store(validRaw)
			previousCalls := calls.Load()
			cached, err := client.Property(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, original, cached)
			require.Equal(t, previousCalls, calls.Load(), "刷新失败后普通查询仍能复用有效缓存")

			// 模拟旧版本写入的坏缓存：即使尚未过期，也必须重新采集恢复。
			require.NoError(t, client.saveCache(path, cacheEntry{FetchedAt: time.Now().UTC(), Body: invalidRaw}))
			recovered, err := client.Property(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, recovered.Observations, 9)
			require.Equal(t, 56482.9, *recovered.Observations[8].YTDAmount)
			require.Equal(t, previousCalls+1, calls.Load(), "只需重新采集失效的响应")
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			var entry cacheEntry
			require.NoError(t, json.Unmarshal(raw, &entry))
			require.JSONEq(t, string(validRaw), string(entry.Body))
		})
	}
}
