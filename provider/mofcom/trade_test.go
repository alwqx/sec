package mofcom

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestParseCNYArticle(t *testing.T) {
	raw, err := os.ReadFile("testdata/cny_monthly.html")
	require.NoError(t, err)
	row, err := parseCNYArticle(string(raw), "202412")
	require.NoError(t, err)
	require.Equal(t, 40669.6, *row.Total.Monthly)
	require.Equal(t, 438467.9, *row.Total.YearToDate)
	require.Equal(t, 24099.4, *row.Exports.Monthly)
	require.Equal(t, 16570.2, *row.Imports.Monthly)
	require.Equal(t, 7529.1, *row.Balance.Monthly)
	require.Equal(t, "24,099.4", row.RawValues["exports_monthly"])
	require.Equal(t, 8.6, *row.Exports.MonthlyMoM)
	require.Equal(t, 10.9, *row.Exports.MonthlyYoY)
	require.Equal(t, 7.1, *row.Exports.YearToDateYoY)
	require.Nil(t, row.Balance.MonthlyMoM)

	badGrowth := strings.Replace(string(raw), "<td>24,099.4</td><td>254,545.4</td><td>8.6</td>", "<td>24,099.4</td><td>254,545.4</td><td>bad</td>", 1)
	_, err = parseCNYArticle(badGrowth, "202412")
	require.ErrorContains(t, err, "出口环比字段无效")
	_, err = parseCNYArticle(string(raw), "202411")
	require.ErrorContains(t, err, "人民币贸易报告缺少进出口总值当月或累计值")
}

func TestTradeSourcesAndCache(t *testing.T) {
	defer gock.Off()
	gock.InterceptClient(fdiHTTPClient)
	defer gock.RestoreClient(fdiHTTPClient)
	read := func(name string) string {
		data, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		return string(data)
	}
	listing := `{"code":0,"total":1,"data":[{"id":123,"name":"2024年12月全国进出口总值表（人民币值）","issueDate":"2025-02-20","source":"海关总署"}]}`
	article, err := json.Marshal(articleEnvelope{Code: 0, Data: struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		IssueDate string `json:"issueDate"`
		Source    string `json:"source"`
		Content   string `json:"content"`
	}{ID: 123, Name: "2024年12月全国进出口总值表（人民币值）", IssueDate: "2025-02-20", Source: "海关总署", Content: read("cny_monthly.html")}})
	require.NoError(t, err)
	gock.New("https://data.mofcom.gov.cn").Post("/datamofcom/front/totalmonth/query").Reply(200).BodyString(read("usd_monthly.json"))
	gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").Reply(200).BodyString(listing)
	gock.New("https://fdi.mofcom.gov.cn").Get("/cipa-api/statistics/web/get").MatchParam("id", "123").Reply(200).BodyString(string(article))
	gock.New("https://data.mofcom.gov.cn").Post("/datamofcom/front/totalbycountry/query").Reply(200).BodyString(read("partners.json"))
	client := NewClient(t.TempDir())
	req := &TradeRequest{Begin: "202412", End: "202412"}
	result, err := client.Trade(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 1)
	row := result.Observations[0]
	require.Equal(t, "亿美元", row.USD.Unit)
	require.Equal(t, "亿元人民币", row.CNY.Unit)
	require.Equal(t, 3356.27, *row.USD.Exports.Monthly)
	require.Equal(t, 24099.4, *row.CNY.Exports.Monthly)
	require.Equal(t, 10.9, *row.CNY.Exports.MonthlyYoY)
	require.Empty(t, row.Note)
	require.Len(t, result.Partners, 1)
	require.Equal(t, "日本", result.Partners[0].Name)
	require.Equal(t, 3200.0, *result.Partners[0].TotalYTD)
	require.NotEmpty(t, result.Partners[0].RawValues)
	require.Equal(t, "3200.00", result.Partners[0].RawValues["total_lj_value"])
	cached, err := client.Trade(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, cached)
	require.True(t, gock.IsDone())
}

func TestCNYListingDuplicateRecords(t *testing.T) {
	read := func(name string) string {
		data, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		return string(data)
	}
	date := articleRelease{ID: 123, Name: "2024年12月全国进出口总值表（人民币值）", IssueDate: "2025-02-20", Source: "海关总署"}
	article := articleEnvelope{Code: 0}
	article.Data.ID = date.ID
	article.Data.Name = date.Name
	article.Data.IssueDate = date.IssueDate
	article.Data.Source = date.Source
	article.Data.Content = read("cny_monthly.html")
	articleJSON, err := json.Marshal(article)
	require.NoError(t, err)
	for _, conflict := range []bool{false, true} {
		name := "identical entry across pages"
		if conflict {
			name = "conflicting ID across pages"
		}
		t.Run(name, func(t *testing.T) {
			defer gock.Off()
			gock.InterceptClient(fdiHTTPClient)
			defer gock.RestoreClient(fdiHTTPClient)
			second := date
			if conflict {
				second.IssueDate = "2025-02-19"
			}
			gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").MatchType("application/json;charset=UTF-8").JSON(map[string]int{"menuId": 1, "page": 1}).Reply(200).JSON(listEnvelope{Total: 2, Data: []articleRelease{date}})
			gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").MatchType("application/json;charset=UTF-8").JSON(map[string]int{"menuId": 1, "page": 2}).Reply(200).JSON(listEnvelope{Total: 2, Data: []articleRelease{second}})
			gock.New("https://fdi.mofcom.gov.cn").Get("/cipa-api/statistics/web/get").MatchParam("id", "123").Reply(200).BodyString(string(articleJSON))
			rows, err := NewClient("").cnyRows(context.Background(), "202412", "202412")
			if conflict {
				require.ErrorContains(t, err, "分页记录 ID 冲突")
			} else {
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, 24099.4, *rows["202412"].Exports.Monthly)
			}
			require.True(t, gock.IsDone())
		})
	}

	t.Run("identical listing entry is deduplicated", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(fdiHTTPClient)
		defer gock.RestoreClient(fdiHTTPClient)
		listing := listEnvelope{Code: 0, Total: 2, Data: []articleRelease{date, date}}
		listingJSON, err := json.Marshal(listing)
		require.NoError(t, err)
		gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").Reply(200).BodyString(string(listingJSON))
		gock.New("https://fdi.mofcom.gov.cn").Get("/cipa-api/statistics/web/get").MatchParam("id", "123").Reply(200).BodyString(string(articleJSON))

		rows, err := NewClient("").cnyRows(context.Background(), "202412", "202412")
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, 24099.4, *rows["202412"].Exports.Monthly)
		require.True(t, gock.IsDone())
	})

	t.Run("conflicting duplicate ID is rejected", func(t *testing.T) {
		defer gock.Off()
		gock.InterceptClient(fdiHTTPClient)
		defer gock.RestoreClient(fdiHTTPClient)
		conflict := date
		conflict.Name = "2024年11月全国进出口总值表（人民币值）"
		listing := listEnvelope{Code: 0, Total: 2, Data: []articleRelease{date, conflict}}
		listingJSON, err := json.Marshal(listing)
		require.NoError(t, err)
		gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").Reply(200).BodyString(string(listingJSON))

		_, err = NewClient("").cnyRows(context.Background(), "202411", "202412")
		require.ErrorContains(t, err, "同页记录 ID 冲突")
		require.True(t, gock.IsDone())
	})
}

func TestTradeMissingPartners(t *testing.T) {
	for _, tc := range []struct {
		name, period, response, wantError string
		status                            int
	}{
		{"before partner coverage", "201512", `{"rows":[]}`, "", 200},
		{"unpublished partners", "202412", `{"rows":[]}`, "", 200},
		{"missing array", "202412", `{}`, "缺少有效 rows 数组", 200},
		{"null array", "202412", `{"rows":null}`, "缺少有效 rows 数组", 200},
		{"wrong period", "202412", `{"rows":[{"trade_date":"202411","type":"日本"}]}`, "所属期无效", 200},
		{"HTTP failure", "202412", `unavailable`, "HTTP 503", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer gock.Off()
			gock.InterceptClient(fdiHTTPClient)
			defer gock.RestoreClient(fdiHTTPClient)
			usd, err := os.ReadFile("testdata/usd_monthly.json")
			require.NoError(t, err)
			gock.New("https://data.mofcom.gov.cn").Post("/datamofcom/front/totalmonth/query").Reply(200).BodyString(strings.ReplaceAll(string(usd), "202412", tc.period))
			// 目录只列出更早月报，用于验证人民币缺失不会掩盖可用美元月值。
			gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").Reply(200).BodyString(`{"code":0,"total":1,"data":[{"id":1,"name":"2014年12月全国进出口总值表（人民币值）","issueDate":"2015-01-10","source":"海关总署"}]}`)
			gock.New("https://data.mofcom.gov.cn").Post("/datamofcom/front/totalbycountry/query").Reply(tc.status).BodyString(tc.response)
			client := NewClient(t.TempDir())
			result, err := client.Trade(context.Background(), &TradeRequest{Begin: tc.period, End: tc.period})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Len(t, result.Observations, 1)
				require.Equal(t, tc.period, result.Observations[0].Period)
				require.Equal(t, 3356.27, *result.Observations[0].USD.Exports.Monthly)
				require.Empty(t, result.Partners)
				require.Contains(t, result.PartnerNote, tc.period)
				require.Contains(t, result.PartnerNote, "未提供")
				cached, err := client.Trade(context.Background(), &TradeRequest{Begin: tc.period, End: tc.period})
				require.NoError(t, err)
				require.Equal(t, result, cached)
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestParseUSDMonthly(t *testing.T) {
	raw := []byte(`[[{"trade_date":"202608","total_value":4500,"total_per":"1.2","total_lj_value":43000,"export_value":2600,"export_per":"2.1","export_lj_value":25000,"import_value":1900,"import_per":"0.4","import_lj_value":18000,"imexgap_value":700,"imexgap_per":"8.0","imexgap_lj_value":7000}],{}]`)
	rows, err := parseUSD(raw, "202608", "202608")
	require.NoError(t, err)
	require.Equal(t, "2600", rawUSD(rows["202608"])["export_value"])
	require.Equal(t, 700.0, *ptrNumber(rows["202608"].Balance))
}

func TestTradeRequestValidate(t *testing.T) {
	tests := []struct {
		name, begin, end string
		wantErr          bool
	}{
		{"valid", "202501", "202508", false},
		{"invalid month", "202513", "202601", true},
		{"reverse", "202601", "202512", true},
		{"before coverage", "201412", "201501", true},
		{"future", time.Now().In(time.FixedZone("CST", 8*3600)).AddDate(0, 0, 40).Format("200601"), time.Now().In(time.FixedZone("CST", 8*3600)).AddDate(0, 0, 40).Format("200601"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&TradeRequest{Begin: tt.begin, End: tt.end}).Validate()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCNYDirectoryStopUsesReportPeriodNotIssueDate(t *testing.T) {
	items := []articleRelease{{IssueDate: "2024-11-30", Name: "2024年12月全国进出口总值表（人民币值）"}}
	require.False(t, hasOlderCNYReport(items, "202412"), "发布日期早于开始月，但报告所属期处于查询区间，不应提前停止")
	items[0].Name = "2024年11月全国进出口总值表（人民币值）"
	require.True(t, hasOlderCNYReport(items, "202412"), "目录页确认进入请求期以前的月报后才允许停止")
}

func TestCurrencyValuesKeepUnitsSeparate(t *testing.T) {
	got := TradeObservation{Period: "202608", USD: &TradeCurrencyValues{Unit: "亿美元", Total: TradeAmount{Monthly: floatPtr(4500)}}, CNY: &TradeCurrencyValues{Unit: "亿元人民币", Total: TradeAmount{Monthly: floatPtr(32000)}}}
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	var decoded TradeObservation
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, "亿美元", decoded.USD.Unit)
	require.Equal(t, "亿元人民币", decoded.CNY.Unit)
	require.Equal(t, 4500.0, *decoded.USD.Total.Monthly)
	require.Equal(t, 32000.0, *decoded.CNY.Total.Monthly)
}

func floatPtr(v float64) *float64 { return &v }
