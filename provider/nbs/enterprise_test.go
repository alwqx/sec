package nbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func enterpriseFixture(t *testing.T, name string) []byte {
	t.Helper()
	return fixture(t, "enterprise-"+name)
}

func enterpriseSamples(t *testing.T, sample, metric string) (enterpriseValuesResponse, []EnterpriseMetric) {
	t.Helper()
	var metadata enterpriseMetadataResponse
	var response enterpriseValuesResponse
	require.NoError(t, json.Unmarshal(enterpriseFixture(t, "series"), &metadata))
	require.NoError(t, json.Unmarshal(enterpriseFixture(t, sample), &response))
	selected, err := selectEnterpriseSeries(metadata, "d760d189206642ce8afad57640408e09", metric)
	require.NoError(t, err)
	return response, selected
}

type enterpriseTestSource struct {
	Calls    atomic.Int32
	Metadata atomic.Value
	Values   atomic.Value
	Payload  atomic.Value
	IDs      atomic.Value
}

func enterpriseTestClient(t *testing.T, cacheDir string) (*Client, *enterpriseTestSource) {
	t.Helper()
	source := &enterpriseTestSource{}
	source.Metadata.Store(string(enterpriseFixture(t, "series")))
	source.Values.Store(string(enterpriseFixture(t, "current-values")))
	files := map[string][]byte{}
	for _, name := range []string{"root", "categories", "catalogs"} {
		files[name] = enterpriseFixture(t, name)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		source.Calls.Add(1)
		switch r.URL.Path {
		case "/new/queryIndexTreeAsync":
			name := map[string]string{"": "root", "fc982599aa684be7969d7b90b1bd0e84": "categories", "f484811113a5470892cb52b00c2b35c2": "catalogs"}[r.URL.Query().Get("pid")]
			if name == "" {
				http.Error(w, "错误目录", 400)
				return
			}
			w.Write(files[name])
		case "/new/queryIndicatorsByCid":
			if r.URL.Query().Get("cid") != "d760d189206642ce8afad57640408e09" || r.URL.Query().Get("pageSize") != "1000" {
				http.Error(w, "错误元数据目录", 400)
				return
			}
			io.WriteString(w, source.Metadata.Load().(string))
		case "/stream/esData":
			raw, err := io.ReadAll(r.Body)
			var body struct {
				Catalog string              `json:"cid"`
				Root    string              `json:"rootId"`
				IDs     []string            `json:"indicatorIds"`
				Dates   []string            `json:"dts"`
				Areas   []map[string]string `json:"das"`
			}
			if err != nil || json.Unmarshal(raw, &body) != nil || r.Method != http.MethodPost || body.Catalog != "d760d189206642ce8afad57640408e09" || body.Root != "f484811113a5470892cb52b00c2b35c2" || len(body.Dates) != 1 || body.Dates[0] != "202601MM-202609MM" || len(body.Areas) != 1 || body.Areas[0]["value"] != "000000000000" {
				http.Error(w, "查询应为全国工业总计及指定月份", 400)
				return
			}
			source.Payload.Store(string(raw))
			source.IDs.Store(body.IDs)
			io.WriteString(w, source.Values.Load().(string))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := NewClient(cacheDir)
	client.baseURL = server.URL
	return client, source
}

func enterpriseItem(t *testing.T, items []EnterpriseObservation, month, metric string) EnterpriseObservation {
	t.Helper()
	for _, item := range items {
		if item.Period == month && item.Metric == metric {
			return item
		}
	}
	t.Fatalf("缺少观测 %s %s", month, metric)
	return EnterpriseObservation{}
}

func TestEnterpriseOfficialValuesCacheAndFilter(t *testing.T) {
	client, source := enterpriseTestClient(t, t.TempDir())
	req := &EnterpriseRequest{Begin: "202601", End: "202609", Metric: "all"}
	result, err := client.Enterprise(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(5), source.Calls.Load())
	require.Len(t, result.Metrics, 4)
	require.Len(t, result.Observations, 36)
	require.Equal(t, []string{"5d98d24036dd4dbebf7f5c0a80753df6", "7f30595d047445e0a3880ab928795892", "9e0537fd94264be0a0f0c1d239dac6fd", "9b524e0c28224feaad7f1f008890a0dc", "f67df29d7067437f88bcc2dbce9ef677", "e00c23ba6ffc45ae86757ce81f1c1997", "46196614c8a64cfdbde9c2e5661083fe", "01d4fd4cd8ec4be5bf836d89cb2ebe0e"}, source.IDs.Load())
	for _, metric := range result.Metrics {
		require.NotEmpty(t, metric.ValueSeries.Definition)
		require.Contains(t, metric.ValueSeries.Annotation, "2011")
		require.Nil(t, metric.ValueSeries.CoverageStart)
	}
	profit := enterpriseItem(t, result.Observations, "202608", "profit")
	require.Equal(t, 52719.8, *profit.Value)
	require.Equal(t, 15.7, *profit.YoY)
	require.Equal(t, "202601-202608", *profit.YTDPeriod)
	require.Nil(t, profit.AsOfDate)
	for metric, expected := range map[string][2]float64{"inventory": {196992.3, 13.9}, "finished-goods": {73633.8, 11.0}, "receivables": {294844.9, 9.0}} {
		item := enterpriseItem(t, result.Observations, "202608", metric)
		require.Equal(t, expected[0], *item.Value)
		require.Equal(t, expected[1], *item.YoY)
		require.Nil(t, item.YTDPeriod)
		require.Equal(t, "2026-08-31", *item.AsOfDate)
		require.Equal(t, "month_end_comparable_scope", item.YoYBasis)
	}
	for _, metric := range enterpriseOrder {
		january := enterpriseItem(t, result.Observations, "202601", metric)
		require.Equal(t, "january_exempt", january.Status)
		require.Nil(t, january.Value)
		require.Nil(t, january.YoY)
		september := enterpriseItem(t, result.Observations, "202609", metric)
		require.Equal(t, "no_value", september.Status)
		require.Nil(t, september.Value)
		require.Contains(t, september.Note, "不等同于官方未发布")
		require.Nil(t, september.PublishedAt)
	}
	feb := enterpriseItem(t, result.Observations, "202602", "profit")
	require.Equal(t, 10245.6, *feb.Value)
	require.Contains(t, feb.Note, "1—2月合并累计利润")
	feb = enterpriseItem(t, result.Observations, "202602", "inventory")
	require.Equal(t, "2026-02-28", *feb.AsOfDate)
	require.Contains(t, feb.Note, "不是1—2月累计")
	require.Nil(t, result.HistoricalVersion)
	require.False(t, result.SeasonallyAdjusted)
	cached, err := client.Enterprise(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, cached)
	require.Equal(t, int32(5), source.Calls.Load())
	client.Refresh = true
	_, err = client.Enterprise(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(10), source.Calls.Load())
	client.Refresh = false
	req.Metric = "profit"
	filtered, err := client.Enterprise(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, filtered.Metrics, 1)
	require.Len(t, filtered.Observations, 9)
	require.Equal(t, []string{"5d98d24036dd4dbebf7f5c0a80753df6", "7f30595d047445e0a3880ab928795892"}, source.IDs.Load())
	client.CacheDir = ""
	before := source.Calls.Load()
	for i := 0; i < 2; i++ {
		_, err = client.Enterprise(context.Background(), req)
		require.NoError(t, err)
	}
	require.Equal(t, before+10, source.Calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Enterprise(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestEnterpriseHistoricalScopeAndMissingSurvey(t *testing.T) {
	for _, test := range []struct{ sample, begin, end string }{{"scope2007-values", "200612", "200705"}, {"scope2011-values", "201011", "201102"}} {
		response, selected := enterpriseSamples(t, test.sample, "profit")
		items, err := parseEnterprise(response, &EnterpriseRequest{Begin: test.begin, End: test.end, Metric: "profit"}, selected)
		require.NoError(t, err)
		if test.begin == "200612" {
			require.Equal(t, "all_state_owned_and_nonstate_revenue_5m", items[0].ScopeStandard)
			feb := enterpriseItem(t, items, "200702", "profit")
			require.Equal(t, "industrial_legal_entities_revenue_5m", feb.ScopeStandard)
			require.Equal(t, 2932.07, *feb.Value)
			march := enterpriseItem(t, items, "200703", "profit")
			require.Equal(t, "not_surveyed", march.Status)
			require.Nil(t, march.Value)
			require.Contains(t, march.Note, "不插值")
		} else {
			dec := enterpriseItem(t, items, "201012", "profit")
			require.Equal(t, "not_surveyed", dec.Status)
			feb := enterpriseItem(t, items, "201102", "profit")
			require.Equal(t, "industrial_legal_entities_revenue_20m", feb.ScopeStandard)
			require.Equal(t, 6454.58, *feb.Value)
			require.Equal(t, "monthly_except_january", feb.SurveyFrequency)
			require.Equal(t, "2011", feb.ComparisonScopeYear)
		}
	}
	response, selected := enterpriseSamples(t, "receivables-values", "receivables")
	items, err := parseEnterprise(response, &EnterpriseRequest{Begin: "201812", End: "202002", Metric: "receivables"}, selected)
	require.NoError(t, err)
	require.Equal(t, 143418.2, *items[0].Value)
	feb2019 := enterpriseItem(t, items, "201902", "receivables")
	require.Nil(t, feb2019.Value)
	require.Nil(t, feb2019.YoY)
	require.Equal(t, "definition_suspended", feb2019.Status)
	require.Contains(t, feb2019.Note, "不用合并口径填")
	feb2020 := enterpriseItem(t, items, "202002", "receivables")
	require.Equal(t, 137973.6, *feb2020.Value)
	require.Equal(t, "2020-02-29", *feb2020.AsOfDate)
	require.Equal(t, "separate_accounts_receivable_since_2020", feb2020.AccountingScope)
	response, selected = enterpriseSamples(t, "earliest-values", "all")
	items, err = parseEnterprise(response, &EnterpriseRequest{Begin: "199901", End: "199903"}, selected)
	require.NoError(t, err)
	require.Equal(t, 112.7, *enterpriseItem(t, items, "199902", "profit").Value)
	inventory := enterpriseItem(t, items, "199902", "inventory")
	require.Nil(t, inventory.Value)
	require.Contains(t, inventory.Note, "201002")
}

func TestEnterpriseValuesRejectCorruptionAndKeepLosses(t *testing.T) {
	for _, name := range []string{"negative-profit", "zero", "missing", "profit-note", "negative-stock", "not-a-number", "duplicate-month", "duplicate-cell", "missing-cell", "invalid-month", "missing-month", "outside-range", "failure", "foreign-region"} {
		t.Run(name, func(t *testing.T) {
			response, selected := enterpriseSamples(t, "current-values", "all")
			row := &response.Data[1] // 官方样本倒序；202608 为非空所属月。
			var profit, profitGrowth, inventory int
			for i, v := range row.Values {
				switch v.ID {
				case selected[0].ValueSeries.ID:
					profit = i
				case selected[0].GrowthSeries.ID:
					profitGrowth = i
				case selected[1].ValueSeries.ID:
					inventory = i
				}
			}
			fail := false
			switch name {
			case "negative-profit":
				row.Values[profit].Value = json.RawMessage(`"-12.5"`)
				row.Values[profitGrowth].Value = json.RawMessage(`"-140.0"`)
			case "zero":
				row.Values[profit].Value = json.RawMessage(`0`)
				row.Values[profitGrowth].Value = json.RawMessage(`0`)
			case "missing":
				row.Values[profitGrowth].Value = json.RawMessage(`"--"`)
			case "profit-note":
				row.Values[profitGrowth].Value = json.RawMessage(`"（注1）"`)
			case "negative-stock":
				row.Values[inventory].Value = json.RawMessage(`-1`)
				fail = true
			case "not-a-number":
				row.Values[profitGrowth].Value = json.RawMessage(`"NaN"`)
				fail = true
			case "duplicate-month":
				response.Data = append(response.Data, response.Data[0])
				fail = true
			case "duplicate-cell":
				row.Values = append(row.Values, row.Values[profit])
				fail = true
			case "missing-cell":
				row.Values = append(row.Values[:profit], row.Values[profit+1:]...)
				fail = true
			case "invalid-month":
				row.Code = "202613MM"
				fail = true
			case "missing-month":
				response.Data = response.Data[1:]
				fail = true
			case "outside-range":
				row.Code = "202610MM"
				fail = true
			case "failure":
				response.Success = false
				fail = true
			case "foreign-region":
				foreign := row.Values[inventory]
				foreign.Area = "110000000000"
				foreign.Value = json.RawMessage(`-999`)
				row.Values = append(row.Values, foreign)
			}
			items, err := parseEnterprise(response, &EnterpriseRequest{Begin: "202601", End: "202609"}, selected)
			if fail {
				require.Error(t, err)
				require.Nil(t, items)
				return
			}
			require.NoError(t, err)
			item := enterpriseItem(t, items, "202608", "profit")
			switch name {
			case "negative-profit":
				require.Equal(t, -12.5, *item.Value)
				require.Equal(t, -140.0, *item.YoY)
			case "zero":
				require.Equal(t, 0.0, *item.Value)
				require.Equal(t, 0.0, *item.YoY)
			case "missing", "profit-note":
				require.Nil(t, item.YoY)
				require.Equal(t, "partial", item.Status)
			}
		})
	}
}

func TestEnterpriseMetadataRejectsWrongSeries(t *testing.T) {
	for _, mode := range []string{"missing", "duplicate", "duplicate-id", "truncated", "unit", "period-code", "catalog", "definition", "success"} {
		t.Run(mode, func(t *testing.T) {
			var metadata enterpriseMetadataResponse
			require.NoError(t, json.Unmarshal(enterpriseFixture(t, "series"), &metadata))
			index := 0
			for i, s := range metadata.Data.List {
				if compact(s.Name) == "利润总额累计值(亿元)" {
					index = i
				}
			}
			switch mode {
			case "missing":
				metadata.Data.List = append(metadata.Data.List[:index], metadata.Data.List[index+1:]...)
				metadata.Data.Total--
			case "duplicate":
				metadata.Data.List = append(metadata.Data.List, metadata.Data.List[index])
				metadata.Data.Total++
			case "duplicate-id":
				for i := range metadata.Data.List {
					if compact(metadata.Data.List[i].Name) == "利润总额累计增长(%)" {
						metadata.Data.List[i].ID = metadata.Data.List[index].ID
					}
				}
			case "truncated":
				metadata.Data.Total++
			case "unit":
				metadata.Data.List[index].Unit = "万元"
			case "period-code":
				metadata.Data.List[index].PeriodCode = "1"
			case "catalog":
				metadata.Data.List[index].CatalogID = "other"
			case "definition":
				metadata.Data.List[index].Definition = ""
			case "success":
				metadata.Success = false
			}
			selected, err := selectEnterpriseSeries(metadata, "d760d189206642ce8afad57640408e09", "all")
			require.Error(t, err)
			require.Nil(t, selected)
		})
	}
}

func enterpriseCachePath(client *Client, path string, params url.Values, payload string) string {
	endpoint := client.baseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	key := sha256.Sum256([]byte(endpoint + "\n" + payload))
	return filepath.Join(client.CacheDir, hex.EncodeToString(key[:])+".json")
}

func TestEnterpriseInvalidResponsesPreserveCacheAndRecover(t *testing.T) {
	client, source := enterpriseTestClient(t, t.TempDir())
	req := &EnterpriseRequest{Begin: "202601", End: "202609", Metric: "all"}
	_, err := client.Enterprise(context.Background(), req)
	require.NoError(t, err)
	metadataPath := enterpriseCachePath(client, "/new/queryIndicatorsByCid", url.Values{"cid": {"d760d189206642ce8afad57640408e09"}, "pageSize": {"1000"}}, "")
	valuesPath := enterpriseCachePath(client, "/stream/esData", nil, source.Payload.Load().(string))
	beforeMeta, err := os.ReadFile(metadataPath)
	require.NoError(t, err)
	beforeValues, err := os.ReadFile(valuesPath)
	require.NoError(t, err)
	client.Refresh = true
	source.Metadata.Store(`{"success":true,"data":{"total":0,"list":[]}}`)
	_, err = client.Enterprise(context.Background(), req)
	require.Error(t, err)
	after, err := os.ReadFile(metadataPath)
	require.NoError(t, err)
	require.Equal(t, beforeMeta, after)
	source.Metadata.Store(string(enterpriseFixture(t, "series")))
	source.Values.Store(`{"success":true,"data":[]}`)
	_, err = client.Enterprise(context.Background(), req)
	require.ErrorContains(t, err, "缺少月份行")
	after, err = os.ReadFile(valuesPath)
	require.NoError(t, err)
	require.Equal(t, beforeValues, after)
	source.Values.Store(string(enterpriseFixture(t, "current-values")))
	client.Refresh = false
	for _, path := range []string{metadataPath, valuesPath} {
		before := source.Calls.Load()
		require.NoError(t, client.saveCache(path, cacheEntry{FetchedAt: time.Now(), Body: json.RawMessage(`{"success":false,"data":[]}`)}))
		_, err = client.Enterprise(context.Background(), req)
		require.NoError(t, err)
		require.Equal(t, before+1, source.Calls.Load(), "坏缓存应仅重采受影响的响应")
	}
	// 时间过期后必须请求；上游异常不能用旧值静默降级。
	body := enterpriseFixture(t, "current-values")
	require.NoError(t, client.saveCache(valuesPath, cacheEntry{FetchedAt: time.Now().Add(-7 * time.Hour), Body: body}))
	source.Values.Store(`{"success":true,"data":[]}`)
	_, err = client.Enterprise(context.Background(), req)
	require.ErrorContains(t, err, "缺少月份行")
	_, err = os.Stat(valuesPath)
	require.NoError(t, err)
}

func TestEnterpriseValidationAndCatalog(t *testing.T) {
	for _, req := range []*EnterpriseRequest{nil, {Begin: "199812", End: "199901"}, {Begin: "202613", End: "202613"}, {Begin: "202609", End: "202601"}, {Begin: "202601", End: "202609", Metric: "bills"}} {
		_, err := NewClient("").Enterprise(context.Background(), req)
		require.Error(t, err)
	}
	for _, nodes := range [][]node{{}, {{Name: "工业", ID: ""}}, {{Name: "工业", ID: "a"}, {Name: "工业", ID: "b"}}} {
		_, err := enterpriseNode(nodes, "工业")
		require.Error(t, err)
	}
	_, _, err := enterpriseNumber(json.RawMessage(`"NaN"`), true)
	require.Error(t, err)
	_, marked, err := enterpriseNumber(json.RawMessage(`"注"`), true)
	require.NoError(t, err)
	require.True(t, marked)
	require.NotContains(t, strings.Join(enterpriseOrder, ","), "bills")
}
