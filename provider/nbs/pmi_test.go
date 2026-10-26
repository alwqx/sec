package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func pmiSamples(t *testing.T) ([]valueRow, []series) {
	return surveySamples(t, "pmi", manufacturingSurvey)
}

func surveySamples(t *testing.T, prefix string, survey pmiSurvey) ([]valueRow, []series) {
	t.Helper()
	var response struct {
		Data []valueRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, prefix+"-values"), &response))
	var metadata struct {
		Data struct {
			List []series `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, prefix+"-series"), &metadata))
	var selected []series
	for _, component := range survey.components {
		for _, s := range metadata.Data.List {
			if compact(s.Name) == component.name {
				selected = append(selected, s)
			}
		}
	}
	require.Len(t, selected, len(survey.components))
	return response.Data, selected
}

func TestManufacturingPMI(t *testing.T) {
	var calls atomic.Int32
	files := map[string][]byte{}
	for _, name := range []string{"root", "categories", "pmi-catalogs", "pmi-series", "pmi-values"} {
		files[name] = fixture(t, name)
	}
	_, selected := pmiSamples(t)
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
			if r.URL.Query().Get("cid") != "93ffbb1aa85740d3aa2618371508b606" {
				http.Error(w, "未选中制造业目录", 400)
				return
			}
			w.Write(files["pmi-series"])
		case "/stream/esData":
			var body struct {
				IDs      []string `json:"indicatorIds"`
				Dates    []string `json:"dts"`
				Root     string   `json:"rootId"`
				ShowType int      `json:"showType"`
			}
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil || len(body.IDs) != 5 || len(body.Dates) != 1 || body.Dates[0] != "202607MM-202609MM" || body.Root != "8f108f3406d7436b82498390d9e08da7" || body.ShowType != 1 {
				http.Error(w, "错误查询参数", 400)
				return
			}
			for i, s := range selected {
				if s.ID != body.IDs[i] {
					http.Error(w, "错误分项", 400)
					return
				}
			}
			w.Write(files["pmi-values"])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.baseURL = server.URL
	req := PMIRequest{"202607", "202609"}
	items, err := client.ManufacturingPMI(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 15)
	require.Equal(t, "202607", items[0].Period)
	require.Equal(t, "manufacturing-pmi", items[0].Indicator)
	require.Equal(t, "manufacturing", items[0].Survey)
	require.Equal(t, 49.2, *items[0].Index, "PMI 不应减 100")
	require.Equal(t, 50.1, *items[10].Index)
	require.Equal(t, 48.4, *items[14].Index)
	require.True(t, items[0].SeasonallyAdjusted)
	require.Equal(t, "%", items[0].Unit)
	require.Equal(t, int32(5), calls.Load())
	cached, err := client.ManufacturingPMI(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, items, cached)
	require.Equal(t, int32(5), calls.Load())
	client.Refresh = true
	_, err = client.ManufacturingPMI(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(10), calls.Load())
}

func TestPMIMissingAndInvalidValues(t *testing.T) {
	req := PMIRequest{"202607", "202609"}
	for _, test := range []struct {
		name, value string
		fail        bool
	}{
		{"missing", `"--"`, false}, {"null", `null`, false}, {"zero", `0`, false},
		{"invalid", `"invalid"`, true}, {"nan", `"NaN"`, true}, {"negative", `-1`, true}, {"over100", `101`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows, selected := pmiSamples(t)
			rows[0].Values[0].Value = json.RawMessage(test.value)
			items, err := parsePMI(rows, req, "catalog", selected, time.Time{})
			if test.fail {
				require.Error(t, err)
				require.Nil(t, items)
				return
			}
			require.NoError(t, err)
			if test.name == "zero" {
				require.Equal(t, 0.0, *items[10].Index)
			} else {
				require.Nil(t, items[10].Index)
			}
		})
	}
	rows, selected := pmiSamples(t)
	rows[0].Values = rows[0].Values[1:]
	items, err := parsePMI(rows, req, "catalog", selected, time.Time{})
	require.NoError(t, err)
	require.Nil(t, items[10].Index)
	rows, selected = pmiSamples(t)
	rows[0].Values = append(rows[0].Values, rows[0].Values[0])
	_, err = parsePMI(rows, req, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "重复观测值")
	rows, selected = pmiSamples(t)
	rows = append(rows, rows[0])
	_, err = parsePMI(rows, req, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "重复月份")
	rows, selected = pmiSamples(t)
	rows[0].Code = "202613MM"
	_, err = parsePMI(rows, req, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "无效")
	rows, selected = pmiSamples(t)
	items, err = parsePMI(rows, PMIRequest{"202608", "202608"}, "catalog", selected, time.Time{})
	require.NoError(t, err)
	require.Len(t, items, 5)
}

func TestPMIMetadataErrors(t *testing.T) {
	for _, mode := range []string{"missing", "truncated", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/new/queryIndexTreeAsync":
					fmt.Fprint(w, `{"success":true,"data":[{"_id":"any","_name":"月度数据"},{"_id":"any","_name":"采购经理指数"},{"_id":"any","_name":"制造业采购经理指数"}]}`)
				case "/new/queryIndicatorsByCid":
					switch mode {
					case "missing":
						fmt.Fprint(w, `{"success":true,"data":{"total":0,"list":[]}}`)
					case "truncated":
						fmt.Fprint(w, `{"success":true,"data":{"total":1,"list":[]}}`)
					case "duplicate":
						fmt.Fprint(w, `{"success":true,"data":{"total":2,"list":[{"_id":"a","i_showname":"制造业采购经理指数 (%)"},{"_id":"b","i_showname":"制造业采购经理指数 (%)"}]}}`)
					}
				default:
					http.Error(w, "不应请求数据", 400)
				}
			}))
			defer server.Close()
			client := NewClient("")
			client.baseURL = server.URL
			items, err := client.ManufacturingPMI(context.Background(), PMIRequest{"202601", "202609"})
			require.Error(t, err)
			require.Nil(t, items)
		})
	}
	client := NewClient("")
	_, err := client.ManufacturingPMI(context.Background(), PMIRequest{"202613", "202609"})
	require.ErrorContains(t, err, "无效月份")
	_, err = client.ManufacturingPMI(context.Background(), PMIRequest{"202609", "202601"})
	require.ErrorContains(t, err, "不能晚于")
}
