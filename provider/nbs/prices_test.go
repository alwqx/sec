package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// 固定官方目录和指标元数据，避免单测依赖实时网络或猜测旧版指标编码。
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	require.NoError(t, err)
	return raw
}

func TestPrices(t *testing.T) {
	var calls atomic.Int32
	var missing atomic.Bool
	var broken atomic.Bool
	files := map[string][]byte{}
	for _, name := range []string{"root", "categories", "prices", "cpi-yoy-catalogs", "cpi-mom-catalogs", "ppi-catalogs", "5c7452825c7c4dcba391db5ca7f335c5", "b4fad2cf9e0e4af7815b7e9e2e95c5c7", "60e8b361f11c4a878c652a6487a25561", "677cfbb4f06941af8c1761c4804e58cf"} {
		files[name] = fixture(t, name)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if broken.Load() {
			fmt.Fprint(w, `{"success":false,"message":"维护中"}`)
			return
		}
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			name := map[string]string{"": "root", "fc982599aa684be7969d7b90b1bd0e84": "categories", "3c9c459384c74f578f3541b2198aac70": "prices", "5b434e4d5e634a39b27a95f8251e9aae": "cpi-yoy-catalogs", "7318f3bae40b4b4badbf519bcd2c79c9": "cpi-mom-catalogs", "55de73673b3d440585cff051fb2e8694": "ppi-catalogs"}[r.URL.Query().Get("pid")]
			if name == "" {
				http.Error(w, "未知目录", 400)
				return
			}
			w.Write(files[name])
		case "/new/queryIndicatorsByCid":
			raw := files[r.URL.Query().Get("cid")]
			if raw == nil {
				http.Error(w, "未知指标目录", 400)
				return
			}
			w.Write(raw)
		case "/stream/esData":
			var body struct {
				IDs      []string `json:"indicatorIds"`
				Dates    []string `json:"dts"`
				ShowType int      `json:"showType"`
			}
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.IDs) != 1 || len(body.Dates) != 1 || body.Dates[0] != "202601MM-202609MM" || body.ShowType != 1 {
				http.Error(w, "请求参数错误", 400)
				return
			}
			value := `"100.9"`
			if missing.Load() {
				value = `"--"`
			}
			fmt.Fprintf(w, `{"success":true,"data":[{"code":"202609MM","values":[{"_id":%q,"da":"000000000000","value":%s}]},{"code":"202608MM","values":[{"_id":%q,"da":"000000000000","value":"100.0"}]}]}`, body.IDs[0], value, body.IDs[0])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	for _, indicator := range []string{"cpi", "core-cpi", "ppi"} {
		t.Run(indicator, func(t *testing.T) {
			req := PriceRequest{indicator, "202601", "202609"}
			items, err := client.Prices(context.Background(), req)
			require.NoError(t, err)
			require.Len(t, items, 4)
			require.Equal(t, "202608", items[0].Period)
			require.Equal(t, 0.0, *items[0].Change)
			require.Equal(t, 100.9, *items[2].Index)
			require.Equal(t, 0.9, *items[2].Change)
			require.False(t, items[0].FetchedAt.IsZero())
			if indicator == "core-cpi" {
				require.Contains(t, items[0].SeriesName, "不包括食品和能源")
			}
			count := calls.Load()
			again, err := client.Prices(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, count, calls.Load(), "缓存应避免再次请求")
			require.Equal(t, items[0].FetchedAt, again[0].FetchedAt)
		})
	}
	missing.Store(true)
	client.Refresh = true
	items, err := client.Prices(context.Background(), PriceRequest{"cpi", "202601", "202609"})
	require.NoError(t, err)
	require.Nil(t, items[2].Index)
	require.Nil(t, items[2].Change)
	broken.Store(true)
	_, err = client.Prices(context.Background(), PriceRequest{"cpi", "202601", "202609"})
	require.ErrorContains(t, err, "维护中")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Prices(ctx, PriceRequest{"cpi", "202601", "202609"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestOfficialValueFixture(t *testing.T) {
	var response struct {
		Data []valueRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "values"), &response))
	require.Len(t, response.Data, 2)
	value, err := parseNumber(response.Data[0].Values[0].Value)
	require.NoError(t, err)
	require.Equal(t, 100.9, *value)
}

func TestUnavailableCoreHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"success":true,"data":{"total":0,"list":[]}}`)
	}))
	defer server.Close()
	client := NewClient("")
	client.baseURL = server.URL
	items, err := client.queryCatalog(context.Background(), PriceRequest{"core-cpi", "202001", "202002"}, "上年同月=100", "root", node{ID: "old", Name: "历史目录"})
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Nil(t, items[0].Index)
	require.Nil(t, items[0].Change)
	require.Contains(t, items[0].Note, "未提供核心 CPI")
	_, err = client.queryCatalog(context.Background(), PriceRequest{"cpi", "202001", "202002"}, "上年同月=100", "root", node{ID: "old", Name: "历史目录"})
	require.ErrorContains(t, err, "缺少")
}

func TestUpstreamErrors(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"forbidden", "禁止访问", 403, "HTTP 403"},
		{"html", "<html>维护中</html>", 200, "有效 JSON"},
		{"failed", `{"success":false,"message":"维护中"}`, 200, "维护中"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); fmt.Fprint(w, test.body) }))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.baseURL = server.URL
			_, err := client.tree(context.Background(), "")
			require.ErrorContains(t, err, test.want)
			files, err := os.ReadDir(client.CacheDir)
			require.NoError(t, err)
			require.Empty(t, files, "失败响应不能缓存")
		})
	}
}

func TestValidationAndPartitions(t *testing.T) {
	for _, req := range []PriceRequest{{"unknown", "202601", "202609"}, {"cpi", "202613", "202609"}, {"cpi", "20260101", "202609"}, {"cpi", "202609", "202601"}} {
		require.Error(t, req.Validate())
	}
	require.True(t, overlaps("全国指数 (2021-2025)", "202501", "202609"))
	require.False(t, overlaps("全国指数 (2021-2025)", "202601", "202609"))
	require.False(t, overlaps("全国指数 (2026-)", "202001", "202512"))
	require.True(t, overlaps("全国指数 (-2015)", "201501", "201512"))
	for _, raw := range []string{`null`, `""`, `"--"`, `"—"`} {
		v, err := parseNumber(json.RawMessage(raw))
		require.NoError(t, err)
		require.Nil(t, v)
	}
	for _, raw := range []string{`"NaN"`, `"Inf"`, `"未知"`} {
		_, err := parseNumber(json.RawMessage(raw))
		require.Error(t, err)
	}
	v, err := parseNumber(json.RawMessage(`0`))
	require.NoError(t, err)
	require.Equal(t, 0.0, *v)
	_, err = findNode([]node{{Name: "其他"}}, "月度数据")
	require.True(t, strings.Contains(err.Error(), "月度数据"))
}
