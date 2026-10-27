package chinabond

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func sample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	require.NoError(t, err)
	return raw
}

func TestOfficialTreasurySamples(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		count          int
		first, last    float64
	}{
		{"treasury-20261019-20261026", "20261019", "20261026", 6, 1.18, 1.64},
		{"treasury-20060301-20060307", "20060301", "20060307", 5, 1.68, 2.9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetchedAt := time.Now().UTC()
			rows, err := parseTreasury(sample(t, tc.name), tc.from, tc.to, "official-source", fetchedAt)
			require.NoError(t, err)
			require.Len(t, rows, tc.count)
			require.Equal(t, tc.from, rows[0].Date) // 直接解析接口的所属日期，不依赖时间戳。
			require.Equal(t, tc.to, rows[len(rows)-1].Date)
			require.Equal(t, tc.first, *rows[0].Yields["1y"])
			require.Equal(t, tc.last, *rows[len(rows)-1].Yields["10y"])
			for _, row := range rows {
				require.Equal(t, "complete", row.Status)
				require.Empty(t, row.MissingTenors)
				require.Equal(t, "official-source", row.Source)
				require.Equal(t, fetchedAt, row.FetchedAt)
			}
		})
	}
}

func TestMissingTenorsAndZero(t *testing.T) {
	response, err := decodeChinaBond(sample(t, "treasury-20261019-20261026"))
	require.NoError(t, err)
	// 原始数据倒序，最后一项为10月19日。
	item := response.HeList[len(response.HeList)-1]
	item.OneYear = "0"
	item.TwoYear = ""
	item.FiveYear = ""
	raw, err := json.Marshal(response)
	require.NoError(t, err)
	rows, err := parseTreasury(raw, "20261019", "20261026", "source", time.Now())
	require.NoError(t, err)
	require.Len(t, rows, 6)
	require.NotNil(t, rows[0].Yields["1y"])
	require.Zero(t, *rows[0].Yields["1y"])
	require.Equal(t, []string{"2y", "5y"}, rows[0].MissingTenors)
	require.Equal(t, "missing-tenors", rows[0].Status)
	require.Equal(t, "1.18", rows[0].Original.ThreeMonth)
	encoded, err := json.Marshal(rows[0])
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"1y":0`)
	require.Contains(t, string(encoded), `"2y":null`)
	rows, err = parseTreasury([]byte(`{"flag":"0","heList":[]}`), "20261024", "20261025", "source", time.Now())
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestRejectInvalidTreasuryResponses(t *testing.T) {
	raw := string(sample(t, "treasury-20261019-20261026"))
	for name, body := range map[string]string{
		"HTML": "<html>busy</html>", "null": "null", "object": "{}", "array": "[]",
		"failure-flag": strings.Replace(raw, `"flag":"0"`, `"flag":"1"`, 1),
		"missing-list": `{"flag":"0"}`, "null-row": `{"flag":"0","heList":[null]}`,
		"bad-yield":      strings.Replace(raw, `"oneYear":"1.18"`, `"oneYear":"invalid"`, 1),
		"nan-yield":      strings.Replace(raw, `"oneYear":"1.18"`, `"oneYear":"NaN"`, 1),
		"inf-yield":      strings.Replace(raw, `"oneYear":"1.18"`, `"oneYear":"Inf"`, 1),
		"bad-date":       strings.Replace(raw, "2026-10-26", "2026-02-30", 1),
		"outside-range":  strings.Replace(raw, "2026-10-26", "2026-10-27", 1),
		"duplicate-date": strings.Replace(raw, "2026-10-23", "2026-10-26", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseTreasury([]byte(body), "20261019", "20261026", "source", time.Now())
			require.Error(t, err)
		})
	}
}

func TestGetChinaBond(t *testing.T) {
	defer gock.Off()
	response, err := GetChinaBond(context.Background(), nil)
	require.Error(t, err)
	require.Nil(t, response)
	for _, req := range []*GetChinaBondReq{{"invalid", "2023-06-02"}, {"2023-06-01", "invalid"}, {"2023-06-02", "2023-06-01"}, {"2023-01-01", "2024-01-01"}} {
		_, err := GetChinaBond(context.Background(), req)
		require.Error(t, err)
	}
	// 恢复PR #66原测试的请求日期、参数和字符串收益率。
	body := `{"heList":[{"workTime":"2023-06-02","oneYear":"1.97","twoYear":"2.19","fiveYear":"2.45","tenYear":"2.70","fifteenYear":"","twentyYear":null,"qxmc":"中债国债收益率曲线"}],"flag":"0"}`
	gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").MatchParams(map[string]string{"startDate": "2023-06-01", "endDate": "2023-06-02", "gjqx": "0", "locale": "cn_ZH", "qxmc": "1"}).MatchHeader("Accept", `application/json, text/javascript, \*/\*; q=0.01`).Reply(200).BodyString(body)
	response, err = GetChinaBond(context.Background(), &GetChinaBondReq{"2023-06-01", "2023-06-02"})
	require.NoError(t, err)
	require.Equal(t, "0", response.Flag)
	require.Len(t, response.HeList, 1)
	require.Equal(t, "1.97", response.HeList[0].OneYear)
	require.Empty(t, response.HeList[0].FifteenYear)
	require.Empty(t, response.HeList[0].TwentyYear)
	require.True(t, gock.IsDone())
}

func TestTreasuryYearCacheAndFiltering(t *testing.T) {
	raw := sample(t, "treasury-20060301-20060307")
	calls := 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/cbweb-czb-web/czb/historyQuery", r.URL.Path)
		q := r.URL.Query()
		require.Equal(t, curveID, q.Get("qxmc"))
		require.Equal(t, "0", q.Get("gjqx"))
		require.Equal(t, "2006-03-01", q.Get("startDate"))
		require.Equal(t, "2006-12-31", q.Get("endDate"))
		require.Equal(t, "cn_ZH", q.Get("locale"))
		require.Contains(t, r.Header.Get("Accept"), "application/json")
		if fail {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write(raw)
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.origin = server.URL
	req := &TreasuryRequest{"20060302", "20060306"}
	result, err := client.Treasury(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 3)
	require.Equal(t, "20060302", result.Observations[0].Date)
	require.Equal(t, "20060306", result.Observations[2].Date)
	require.Equal(t, "yield-to-maturity", result.CurveType)
	require.Nil(t, result.PublishedAt)
	require.Equal(t, 1, calls)
	_, err = client.Treasury(context.Background(), &TreasuryRequest{"20060301", "20060307"})
	require.NoError(t, err)
	require.Equal(t, 1, calls, "同年不同日期区间复用原始缓存")
	client.Refresh = true
	_, err = client.Treasury(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	fail = true
	_, err = client.Treasury(context.Background(), req)
	require.ErrorContains(t, err, "503")
	require.Equal(t, 3, calls)
	client.Refresh = false
	_, err = client.Treasury(context.Background(), req)
	require.NoError(t, err, "失败不能覆盖已有缓存")
	require.Equal(t, 3, calls)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Treasury(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 3, calls)

	path := filepath.Join(client.CacheDir, "treasury-2006.json")
	fail = false
	for _, body := range []string{"corrupt", fmt.Sprintf(`{"source":%q,"fetched_at":%q,"body":{"flag":"0","heList":[]}}`, result.Observations[0].Source, time.Now().Add(-7*time.Hour).Format(time.RFC3339))} {
		require.NoError(t, os.WriteFile(path, []byte(body), 0600))
		_, err = client.Treasury(context.Background(), req)
		require.NoError(t, err)
	}
	require.Equal(t, 5, calls, "损坏和过期缓存重新请求")
}

func TestTreasuryMultipleYearsAndFailure(t *testing.T) {
	calls := 0
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("startDate") == "2006-03-01" {
			w.Write(sample(t, "treasury-20060301-20060307"))
			return
		}
		if fail {
			fmt.Fprint(w, "<html>busy</html>")
			return
		}
		fmt.Fprint(w, `{"flag":"0","heList":[]}`)
	}))
	defer server.Close()
	client := NewClient("")
	client.origin = server.URL
	result, err := client.Treasury(context.Background(), &TreasuryRequest{"20060301", "20070101"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 5)
	require.Equal(t, 2, calls)
	fail = true
	result, err = client.Treasury(context.Background(), &TreasuryRequest{"20060301", "20070101"})
	require.Error(t, err)
	require.Nil(t, result, "任何年份失败不能悄悄输出部分结果")
	require.Equal(t, 4, calls, "无缓存模式重新查询每年")
}

func TestTreasuryRequestValidation(t *testing.T) {
	result, err := NewClient("").Treasury(context.Background(), nil)
	require.ErrorContains(t, err, "请求不能为空")
	require.Nil(t, result)
	for _, req := range []TreasuryRequest{{"20060228", "20060301"}, {"20250230", "20250301"}, {"20250102", "20250101"}, {"2025-01-01", "20250102"}, {"20250101", "29991231"}, {"", "20250101"}} {
		require.Error(t, req.Validate())
	}
}
