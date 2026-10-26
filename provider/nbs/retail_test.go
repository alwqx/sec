package nbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func retailSamples(t *testing.T) ([]valueRow, []series) {
	t.Helper()
	var response struct {
		Data []valueRow `json:"data"`
	}
	var metadata struct {
		Data struct {
			List []series `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "retail-values"), &response))
	require.NoError(t, json.Unmarshal(fixture(t, "retail-series"), &metadata))
	return response.Data, metadata.Data.List[:4]
}

func TestRetailSales(t *testing.T) {
	var calls atomic.Int32
	files := map[string][]byte{}
	for _, name := range []string{"root", "categories", "retail-catalogs", "retail-series", "retail-values"} {
		files[name] = fixture(t, name)
	}
	_, selected := retailSamples(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			name := map[string]string{"": "root", "fc982599aa684be7969d7b90b1bd0e84": "categories", "3913ce1309d04eb1bdf7d7b622b1d07c": "retail-catalogs"}[r.URL.Query().Get("pid")]
			if name == "" {
				http.Error(w, "错误目录", 400)
				return
			}
			w.Write(files[name])
		case "/new/queryIndicatorsByCid":
			if r.URL.Query().Get("cid") != "d0cb882c7f27443ab6b3ef9421901961" {
				http.Error(w, "错误社零目录", 400)
				return
			}
			w.Write(files["retail-series"])
		case "/stream/esData":
			var body struct {
				IDs     []string `json:"indicatorIds"`
				Dates   []string `json:"dts"`
				Catalog string   `json:"cid"`
				Root    string   `json:"rootId"`
			}
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.IDs) != 4 || len(body.Dates) != 1 || body.Dates[0] != "202601MM-202609MM" || body.Catalog != "d0cb882c7f27443ab6b3ef9421901961" || body.Root != "3913ce1309d04eb1bdf7d7b622b1d07c" {
				http.Error(w, "错误查询参数", 400)
				return
			}
			for i, s := range selected {
				if body.IDs[i] != s.ID {
					http.Error(w, "不应查询限额以上单位系列", 400)
					return
				}
			}
			w.Write(files["retail-values"])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := RetailRequest{"202601", "202609"}
	items, err := client.RetailSales(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 9)
	require.Equal(t, "202601", items[0].Period)
	require.Nil(t, items[0].Amount)
	require.Nil(t, items[0].YTDAmount)
	require.Nil(t, items[0].YoY)
	require.Nil(t, items[0].YTDGrowth)
	require.Nil(t, items[1].Amount)
	require.Nil(t, items[1].YoY)
	require.Equal(t, 86078.9, *items[1].YTDAmount)
	require.Equal(t, 2.8, *items[1].YTDGrowth)
	require.Equal(t, "202601-202602", items[1].YTDPeriod)
	require.Contains(t, items[1].Note, "1—2月合并累计")
	require.Equal(t, 42432.5, *items[8].Amount)
	require.Equal(t, 370001.3, *items[8].YTDAmount)
	require.Equal(t, 1.1, *items[8].YoY)
	require.Equal(t, 1.1, *items[8].YTDGrowth)
	require.Equal(t, "nominal", items[8].PriceBasis)
	require.Equal(t, "亿元", items[8].AmountUnit)
	require.Equal(t, "%", items[8].GrowthUnit)
	require.False(t, items[8].SeasonallyAdjusted)
	require.Equal(t, int32(5), calls.Load())
	cached, err := client.RetailSales(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, items, cached)
	require.Equal(t, int32(5), calls.Load())
	client.Refresh = true
	_, err = client.RetailSales(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(10), calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.RetailSales(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRetailValues(t *testing.T) {
	req := RetailRequest{"202609", "202609"}
	for _, test := range []struct {
		name, raw string
		field     int
		fail      bool
	}{
		{"zero-amount", `0`, 0, false}, {"missing-amount", `"--"`, 0, false},
		{"negative-growth", `"-20.5"`, 2, false}, {"negative-amount", `-1`, 0, true},
		{"invalid", `"NaN"`, 3, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, selected := retailSamples(t)
			rows[0].Values[test.field].Value = json.RawMessage(test.raw)
			items, err := parseRetail(rows, req, "catalog", selected, time.Time{})
			if test.fail {
				require.Error(t, err)
				require.Nil(t, items)
				return
			}
			require.NoError(t, err)
			require.Len(t, items, 1)
			switch test.name {
			case "zero-amount":
				require.Equal(t, 0.0, *items[0].Amount)
			case "missing-amount":
				require.Nil(t, items[0].Amount)
			case "negative-growth":
				require.Equal(t, -20.5, *items[0].YoY)
			}
		})
	}
	rows, selected := retailSamples(t)
	rows[0].Values = rows[0].Values[1:]
	items, err := parseRetail(rows, req, "catalog", selected, time.Time{})
	require.NoError(t, err)
	require.Nil(t, items[0].Amount, "不能用限额以上金额或累计金额补值")
	rows, selected = retailSamples(t)
	rows = append(rows, rows[0])
	_, err = parseRetail(rows, req, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "重复月份")
	rows, selected = retailSamples(t)
	rows[0].Values = append(rows[0].Values, rows[0].Values[0])
	_, err = parseRetail(rows, req, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "重复观测值")
	rows, selected = retailSamples(t)
	rows[0].Code = "202613MM"
	_, err = parseRetail(rows, req, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "无效")
	_, err = NewClient("").RetailSales(context.Background(), RetailRequest{"202609", "202601"})
	require.ErrorContains(t, err, "不能晚于")
}

func TestRetailMetadataErrors(t *testing.T) {
	for _, mode := range []string{"missing", "truncated", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			var metadata struct {
				Data struct {
					Total int      `json:"total"`
					List  []series `json:"list"`
				} `json:"data"`
				Success bool `json:"success"`
			}
			require.NoError(t, json.Unmarshal(fixture(t, "retail-series"), &metadata))
			switch mode {
			case "missing":
				metadata.Data.List = metadata.Data.List[1:]
				metadata.Data.Total = len(metadata.Data.List)
			case "truncated":
				metadata.Data.Total++
			case "duplicate":
				metadata.Data.List = append(metadata.Data.List, metadata.Data.List[0])
				metadata.Data.Total++
			}
			raw, err := json.Marshal(metadata)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/new/queryIndexTreeAsync":
					w.Write([]byte(`{"success":true,"data":[{"_id":"any","_name":"月度数据"},{"_id":"any","_name":"国内贸易"},{"_id":"any","_name":"社会消费品零售总额"}]}`))
				case "/new/queryIndicatorsByCid":
					w.Write(raw)
				default:
					http.Error(w, "不应发起数据请求", 400)
				}
			}))
			defer server.Close()
			client := NewClient("")
			client.baseURL = server.URL
			items, err := client.RetailSales(context.Background(), RetailRequest{"202601", "202609"})
			require.Error(t, err)
			require.Nil(t, items)
		})
	}
}
