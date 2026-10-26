package nbs

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const industrialReleaseURL = "https://www.stats.gov.cn/sj/zxfb/202610/t20261019_1965617.html"

func industrialSamples(t *testing.T) ([]valueRow, []series) {
	t.Helper()
	var response struct {
		Data []valueRow `json:"data"`
	}
	var metadata struct {
		Data struct {
			List []series `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "industrial-values"), &response))
	require.NoError(t, json.Unmarshal(fixture(t, "industrial-series"), &metadata))
	return response.Data, metadata.Data.List
}

func TestIndustrialProduction(t *testing.T) {
	defer gock.Off()
	html, err := os.ReadFile("testdata/industrial-release.html")
	require.NoError(t, err)
	mock := func() {
		for _, pair := range []struct{ pid, file string }{{"", "root"}, {"fc982599aa684be7969d7b90b1bd0e84", "categories"}, {"f484811113a5470892cb52b00c2b35c2", "industrial-catalogs"}} {
			req := gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryIndexTreeAsync")
			if pair.pid != "" {
				req.MatchParam("pid", pair.pid)
			}
			req.Reply(200).BodyString(string(fixture(t, pair.file)))
		}
		gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryIndicatorsByCid").MatchParam("cid", "3f2e14f0542348ed9fe02476eca3450b").Reply(200).BodyString(string(fixture(t, "industrial-series")))
		gock.New("https://data.stats.gov.cn").Post("/dg/website/publicrelease/web/external/stream/esData").MatchType("json").Reply(200).BodyString(string(fixture(t, "industrial-values")))
		gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", "1").Reply(200).BodyString(string(fixture(t, "industrial-releases")))
		gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965617.html").Reply(200).BodyString(string(html))
	}
	client := NewClient(t.TempDir())
	req := IndustrialRequest{"202601", "202609"}
	mock()
	items, err := client.IndustrialProduction(context.Background(), req)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Len(t, items, 9)
	require.Nil(t, items[0].YoY)
	require.Nil(t, items[0].YTD)
	require.Equal(t, 0.26, *items[0].MoM)
	require.Nil(t, items[1].YoY)
	require.Equal(t, 6.3, *items[1].YTD)
	require.Equal(t, 0.76, *items[1].MoM)
	require.Equal(t, "202601-202602", items[1].YTDPeriod)
	require.Contains(t, items[1].Note, "非2月单月同比")
	require.Equal(t, 5.5, *items[8].YoY)
	require.Equal(t, 5.3, *items[8].YTD)
	require.Equal(t, 0.61, *items[8].MoM)
	require.Equal(t, industrialReleaseURL, items[8].MoMSource)
	require.Equal(t, "2026-10-19T10:06:00+08:00", items[8].MoMPublishedAt.Format(time.RFC3339))
	require.NotNil(t, items[8].MoMFetchedAt)
	require.Equal(t, "real", items[8].PriceBasis)
	require.True(t, items[8].MoMSeasonallyAdjusted)
	cached, err := client.IndustrialProduction(context.Background(), req)
	require.NoError(t, err)
	// JSON 时间重新解码后时区表示可能不同，比较时间所指的时刻。
	require.Equal(t, items[8].MoM, cached[8].MoM)
	require.True(t, items[8].MoMFetchedAt.Equal(*cached[8].MoMFetchedAt))
	client.Refresh = true
	mock()
	_, err = client.IndustrialProduction(context.Background(), req)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
}

func TestIndustrialValues(t *testing.T) {
	rows, selected := industrialSamples(t)
	rows[0].Values[0].Value = json.RawMessage(`"-13.5"`)
	rows[0].Values[1].Value = json.RawMessage(`"0"`)
	items, err := parseIndustrial(rows, IndustrialRequest{"202609", "202609"}, "catalog", selected, time.Time{})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, -13.5, *items[0].YoY)
	require.Equal(t, 0.0, *items[0].YTD)
	rows[0].Values[0].Value = json.RawMessage(`"NaN"`)
	_, err = parseIndustrial(rows, IndustrialRequest{"202601", "202609"}, "catalog", selected, time.Time{})
	require.Error(t, err)
	rows, selected = industrialSamples(t)
	rows = append(rows, rows[0])
	_, err = parseIndustrial(rows, IndustrialRequest{"202601", "202609"}, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "重复月份")
	rows, selected = industrialSamples(t)
	rows[0].Values = append(rows[0].Values, rows[0].Values[0])
	_, err = parseIndustrial(rows, IndustrialRequest{"202601", "202609"}, "catalog", selected, time.Time{})
	require.ErrorContains(t, err, "重复观测值")
	_, err = NewClient("").IndustrialProduction(context.Background(), IndustrialRequest{"202609", "202601"})
	require.ErrorContains(t, err, "不能晚于")
}

func TestIndustrialMoMTable(t *testing.T) {
	html, err := os.ReadFile("testdata/industrial-release.html")
	require.NoError(t, err)
	rates, err := parseIndustrialMoM(string(html))
	require.NoError(t, err)
	require.Len(t, rates, 13)
	require.Equal(t, 0.65, rates["202509"])
	require.Equal(t, 0.45, rates["202512"])
	require.Equal(t, 0.26, rates["202601"])
	for _, test := range []struct {
		html string
		fail bool
	}{
		{`<table><tr><td>年份</td><td>月份</td><td>环比增速</td></tr><tr><td>2026年</td><td>1月</td><td>0</td></tr><tr><td>2月</td><td>-0.50</td></tr></table>`, false},
		{`<html>访问验证</html>`, true},
		{`<table><tr><td>环比增速</td></tr><tr><td>1月</td><td>0.5</td></tr></table>`, true},
		{`<table><tr><td>环比增速</td></tr><tr><td>2026年</td><td>1月</td><td>NaN</td></tr></table>`, true},
		{`<table><tr><td>环比增速</td></tr><tr><td>2026年</td><td>1月</td><td>0.5</td></tr><tr><td>1月</td><td>0.6</td></tr></table>`, true},
	} {
		values, err := parseIndustrialMoM(test.html)
		if test.fail {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, 0.0, values["202601"])
			require.Equal(t, -0.5, values["202602"])
		}
	}
	require.Equal(t, "202602", releasePeriod("2026年1—2月份规模以上工业增加值增长6.3%"))
	require.Equal(t, "202604", releasePeriod("2026年1—4月份规模以上工业增加值增长5.6%"))
	require.Empty(t, releasePeriod("2026年工业企业利润"))
}

func TestIndustrialMoMPaginationAndMissing(t *testing.T) {
	defer gock.Off()
	var first []industrialRelease
	for i := 0; i < 100; i++ {
		first = append(first, industrialRelease{Title: "其他数据"})
	}
	page, _ := json.Marshal(map[string]any{"success": true, "data": first})
	gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", "1").Reply(200).BodyString(string(page))
	page, _ = json.Marshal(map[string]any{"success": true, "data": []industrialRelease{{Title: "2026年1—2月份规模以上工业增加值增长6.3%", URL: industrialReleaseURL, Published: "2026-03-16 10:00"}}})
	gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", "2").Reply(200).BodyString(string(page))
	gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965617.html").Reply(200).BodyString(`<table><tr><td>环比增速</td></tr><tr><td>2026年</td><td>1月</td><td>0</td></tr></table>`)
	items := []IndustrialObservation{{Period: "202601"}, {Period: "202602"}}
	require.NoError(t, NewClient("").supplementIndustrialMoM(context.Background(), items))
	require.True(t, gock.IsDone())
	require.Equal(t, 0.0, *items[0].MoM)
	require.Nil(t, items[1].MoM)
	require.Contains(t, items[1].Note, "未提供")
	_, _, err := NewClient("").industrialReleaseRates(context.Background(), "https://example.com/report.html")
	require.ErrorContains(t, err, "链接")
}

func TestIndustrialMoMNewestRevision(t *testing.T) {
	defer gock.Off()
	olderURL := "https://www.stats.gov.cn/sj/zxfb/202609/old.html"
	page, _ := json.Marshal(map[string]any{"success": true, "data": []industrialRelease{
		{Title: "2026年8月份规模以上工业增加值增长5.2%", URL: olderURL, Published: "2026-09-15 10:00"},
		{Title: "2026年9月份规模以上工业增加值增长5.5%", URL: industrialReleaseURL, Published: "2026-10-19 10:06"},
	}})
	gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").Reply(200).BodyString(string(page))
	gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965617.html").Reply(200).BodyString(`<table><tr><td>环比增速</td></tr><tr><td>2026年</td><td>8月</td><td>0.54</td></tr></table>`)
	gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202609/old.html").Reply(200).BodyString(`<table><tr><td>环比增速</td></tr><tr><td>2026年</td><td>7月</td><td>0.10</td></tr><tr><td>8月</td><td>0.50</td></tr></table>`)
	items := []IndustrialObservation{{Period: "202607"}, {Period: "202608"}}
	require.NoError(t, NewClient("").supplementIndustrialMoM(context.Background(), items))
	require.True(t, gock.IsDone())
	require.Equal(t, 0.54, *items[1].MoM)
	require.Equal(t, industrialReleaseURL, items[1].MoMSource)
	require.Equal(t, 0.10, *items[0].MoM)
	require.Equal(t, olderURL, items[0].MoMSource)
}
