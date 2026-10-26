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

func unemploymentSamples(t *testing.T) ([]valueRow, []series) {
	t.Helper()
	var response struct {
		Data []valueRow `json:"data"`
	}
	var metadata struct {
		Data struct {
			List []series `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "unemployment-values"), &response))
	require.NoError(t, json.Unmarshal(fixture(t, "unemployment-series"), &metadata))
	var selected []series
	for _, component := range unemploymentComponents {
		for _, s := range metadata.Data.List {
			if compact(s.Name) == component.name {
				selected = append(selected, s)
			}
		}
	}
	require.Len(t, selected, 5)
	return response.Data, selected
}

func TestUnemployment(t *testing.T) {
	var calls atomic.Int32
	files := map[string][]byte{}
	for _, name := range []string{"root", "categories", "unemployment-catalogs", "unemployment-series", "unemployment-values"} {
		files[name] = fixture(t, name)
	}
	_, selected := unemploymentSamples(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			name := map[string]string{"": "root", "fc982599aa684be7969d7b90b1bd0e84": "categories", "e69d8b62991649d4b278e0445bcc0cd0": "unemployment-catalogs"}[r.URL.Query().Get("pid")]
			if name == "" {
				http.Error(w, "错误目录", 400)
				return
			}
			w.Write(files[name])
		case "/new/queryIndicatorsByCid":
			if r.URL.Query().Get("cid") != "ee3b7046b390415b9b7745e3d16f6052" {
				http.Error(w, "错误失业率目录", 400)
				return
			}
			w.Write(files["unemployment-series"])
		case "/stream/esData":
			var body struct {
				IDs   []string `json:"indicatorIds"`
				Dates []string `json:"dts"`
				Root  string   `json:"rootId"`
			}
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || (len(body.IDs) != 1 && len(body.IDs) != 5) || len(body.Dates) != 1 || body.Dates[0] != "202301MM-202409MM" || body.Root != "e69d8b62991649d4b278e0445bcc0cd0" {
				http.Error(w, "错误查询参数", 400)
				return
			}
			for i, id := range body.IDs {
				if id != selected[i].ID {
					http.Error(w, "错误失业率分组", 400)
					return
				}
			}
			w.Write(files["unemployment-values"])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := UnemploymentRequest{Begin: "202301", End: "202409"}
	items, err := client.Unemployment(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 6)
	require.Equal(t, 5.2, *items[0].Rate, "不能混入31个大城市数据")
	require.Equal(t, "national-surveyed", items[0].DefinitionVersion)
	require.Equal(t, int32(5), calls.Load())
	req.AgeGroups = true
	items, err = client.Unemployment(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 30)
	require.Equal(t, int32(6), calls.Load(), "年龄选项应只新增数据请求，元数据复用缓存")
	require.Equal(t, 21.3, *items[1].Rate)
	require.Equal(t, "legacy-including-students", items[1].DefinitionVersion)
	require.Equal(t, "included-if-in-labour-force", items[1].StudentScope)
	require.Nil(t, items[6].Rate)
	require.Equal(t, "publication-paused-202307-202311", items[6].DefinitionVersion)
	require.Equal(t, 14.9, *items[16].Rate)
	require.Equal(t, "excluding-students-since-202312", items[16].DefinitionVersion)
	require.NotEqual(t, items[1].SeriesKey, items[16].SeriesKey, "官方 ID 相同也不能将口径变化两侧拼接")
	require.Equal(t, items[1].SeriesID, items[16].SeriesID)
	require.Equal(t, "excluded", items[16].StudentScope)
	require.Equal(t, 6.1, *items[17].Rate)
	require.Equal(t, 3.9, *items[18].Rate)
	require.Nil(t, items[19].Rate)
	cached, err := client.Unemployment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, items, cached)
	require.Equal(t, int32(6), calls.Load())
	client.Refresh = true
	_, err = client.Unemployment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(11), calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Unemployment(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestUnemploymentValues(t *testing.T) {
	req := UnemploymentRequest{Begin: "202409", End: "202409"}
	for _, test := range []struct {
		raw     string
		fail    bool
		missing bool
	}{
		{`0`, false, false}, {`"--"`, false, true}, {`null`, false, true}, {`-1`, true, false}, {`101`, true, false}, {`"NaN"`, true, false},
	} {
		rows, selected := unemploymentSamples(t)
		rows[0].Values[0].Value = json.RawMessage(test.raw)
		items, err := parseUnemployment(rows, req, "catalog", selected[:1], time.Time{})
		if test.fail {
			require.Error(t, err)
			require.Nil(t, items)
			continue
		}
		require.NoError(t, err)
		require.Len(t, items, 1)
		if test.missing {
			require.Nil(t, items[0].Rate)
		} else {
			require.Equal(t, 0.0, *items[0].Rate)
		}
	}
	rows, selected := unemploymentSamples(t)
	rows = append(rows, rows[0])
	_, err := parseUnemployment(rows, req, "catalog", selected[:1], time.Time{})
	require.ErrorContains(t, err, "重复月份")
	rows, selected = unemploymentSamples(t)
	rows[0].Values = append(rows[0].Values, rows[0].Values[0])
	_, err = parseUnemployment(rows, req, "catalog", selected[:1], time.Time{})
	require.ErrorContains(t, err, "重复观测值")
	rows, selected = unemploymentSamples(t)
	rows[0].Values = rows[0].Values[1:]
	items, err := parseUnemployment(rows, req, "catalog", selected[:1], time.Time{})
	require.NoError(t, err)
	require.Nil(t, items[0].Rate, "不能用城市或户籍分组补值")
	_, err = NewClient("").Unemployment(context.Background(), UnemploymentRequest{Begin: "202613", End: "202609"})
	require.ErrorContains(t, err, "无效月份")
}

func TestUnemploymentMetadataErrors(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			var metadata struct {
				Data struct {
					Total int      `json:"total"`
					List  []series `json:"list"`
				} `json:"data"`
				Success bool `json:"success"`
			}
			require.NoError(t, json.Unmarshal(fixture(t, "unemployment-series"), &metadata))
			switch mode {
			case "missing":
				metadata.Data.List = metadata.Data.List[1:]
				metadata.Data.Total--
			case "duplicate":
				metadata.Data.List = append(metadata.Data.List, metadata.Data.List[0])
				metadata.Data.Total++
			case "truncated":
				metadata.Data.Total++
			}
			raw, err := json.Marshal(metadata)
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/new/queryIndexTreeAsync":
					w.Write([]byte(`{"success":true,"data":[{"_id":"any","_name":"月度数据"},{"_id":"any","_name":"城镇调查失业率"}]}`))
				case "/new/queryIndicatorsByCid":
					w.Write(raw)
				default:
					http.Error(w, "不应发起数据请求", 400)
				}
			}))
			defer server.Close()
			client := NewClient("")
			client.baseURL = server.URL
			items, err := client.Unemployment(context.Background(), UnemploymentRequest{Begin: "202601", End: "202609"})
			require.Error(t, err)
			require.Nil(t, items)
		})
	}
}
