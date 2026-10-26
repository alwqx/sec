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

func TestNonManufacturingPMI(t *testing.T) {
	var calls atomic.Int32
	files := map[string][]byte{}
	for _, name := range []string{"root", "categories", "pmi-catalogs", "nonmanufacturing-series", "nonmanufacturing-values"} {
		files[name] = fixture(t, name)
	}
	_, selected := surveySamples(t, "nonmanufacturing", nonManufacturingSurvey)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			name := map[string]string{"": "root", "fc982599aa684be7969d7b90b1bd0e84": "categories", "8f108f3406d7436b82498390d9e08da7": "pmi-catalogs"}[r.URL.Query().Get("pid")]
			if name == "" {
				http.Error(w, "错误目录", 400)
				return
			}
			w.Write(files[name])
		case "/new/queryIndicatorsByCid":
			if r.URL.Query().Get("cid") != "7a64a6e25aec4a8e9dde044ecd9e2cce" {
				http.Error(w, "未选中非制造业目录", 400)
				return
			}
			w.Write(files["nonmanufacturing-series"])
		case "/stream/esData":
			var body struct {
				IDs     []string `json:"indicatorIds"`
				Dates   []string `json:"dts"`
				Catalog string   `json:"cid"`
			}
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.IDs) != 12 || len(body.Dates) != 1 || body.Dates[0] != "202607MM-202609MM" || body.Catalog != "7a64a6e25aec4a8e9dde044ecd9e2cce" {
				http.Error(w, "错误请求", 400)
				return
			}
			for i, s := range selected {
				if body.IDs[i] != s.ID {
					http.Error(w, "错误分项", 400)
					return
				}
			}
			w.Write(files["nonmanufacturing-values"])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := PMIRequest{"202607", "202609"}
	items, err := client.NonManufacturingPMI(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 36)
	require.Equal(t, "202607", items[0].Period)
	require.Equal(t, 49.0, *items[0].Index)
	require.Equal(t, 49.3, *items[1].Index)
	require.Equal(t, 47.0, *items[2].Index)
	require.Equal(t, 50.2, *items[24].Index)
	require.Equal(t, 50.2, *items[25].Index)
	require.Equal(t, 50.3, *items[26].Index)
	require.Equal(t, 55.5, *items[35].Index)
	for _, item := range items {
		require.Equal(t, "nonmanufacturing", item.Survey)
		require.Equal(t, "%", item.Unit)
		require.True(t, item.SeasonallyAdjusted)
	}
	require.Equal(t, int32(5), calls.Load())
	cached, err := client.NonManufacturingPMI(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, items, cached)
	require.Equal(t, int32(5), calls.Load())
	client.Refresh = true
	_, err = client.NonManufacturingPMI(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(10), calls.Load())
}

func TestNonManufacturingMissingComponents(t *testing.T) {
	rows, selected := surveySamples(t, "nonmanufacturing", nonManufacturingSurvey)
	// 服务业缺失时不能用总指数替代；真实零值不能变为空值。
	for i := range rows[0].Values {
		if rows[0].Values[i].ID == selected[1].ID {
			rows[0].Values[i].Value = json.RawMessage(`null`)
		}
		if rows[0].Values[i].ID == selected[2].ID {
			rows[0].Values[i].Value = json.RawMessage(`0`)
		}
	}
	req := PMIRequest{"202607", "202609"}
	items, err := parseSurvey(rows, req, "catalog", selected, time.Time{}, nonManufacturingSurvey)
	require.NoError(t, err)
	require.Equal(t, 50.2, *items[24].Index)
	require.Nil(t, items[25].Index)
	require.Equal(t, 0.0, *items[26].Index)
	rows[0].Values = append(rows[0].Values, rows[0].Values[0])
	_, err = parseSurvey(rows, req, "catalog", selected, time.Time{}, nonManufacturingSurvey)
	require.ErrorContains(t, err, "重复观测值")
	_, err = parseSurvey(rows, req, "catalog", selected[:5], time.Time{}, nonManufacturingSurvey)
	require.ErrorContains(t, err, "元数据不完整")
	_, err = NewClient("").NonManufacturingPMI(context.Background(), PMIRequest{"202609", "202601"})
	require.ErrorContains(t, err, "不能晚于")
}
