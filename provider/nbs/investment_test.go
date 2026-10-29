package nbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func investmentFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "investment-"+name))
	require.NoError(t, err)
	return string(raw)
}

func TestInvestmentOfficialReleases(t *testing.T) {
	for _, test := range []struct {
		name, sector, period string
		amount, growth       float64
		hasAmount            bool
		scope                string
	}{
		{"202609.html", "total", "202609", 334115, -7.2, true, ""},
		{"202509.html", "total", "202509", 371535, -0.5, true, ""},
		{"202512.html", "total", "202512", 485186, -3.8, true, ""},
		{"202602.html", "total", "202602", 52721, 1.8, true, ""},
		{"202609.html", "manufacturing", "202609", 0, -2.1, false, ""},
		{"202509.html", "infrastructure", "202509", 0, 1.1, false, "excluding_electricity_heat_gas_water"},
		{"202512.html", "infrastructure", "202512", 0, -2.2, false, "excluding_electricity_heat_gas_water"},
		{"202602.html", "infrastructure", "202602", 0, 11.4, false, "including_electricity_heat_gas_water"},
		{"202609.html", "infrastructure", "202609", 0, -4.2, false, "including_electricity_heat_gas_water"},
		{"property-202609.html", "real-estate", "202609", 53364, -20.6, true, ""},
	} {
		t.Run(test.sector+test.period, func(t *testing.T) {
			item, err := parseInvestmentRelease(investmentFixture(t, test.name), test.sector, test.period)
			require.NoError(t, err)
			require.Equal(t, test.growth, *item.YTDGrowth)
			if test.hasAmount {
				require.Equal(t, test.amount, *item.YTDAmount)
			} else {
				require.Nil(t, item.YTDAmount)
			}
			require.Equal(t, test.scope, item.InfrastructureScope)
			require.Equal(t, "nominal", item.PriceBasis)
			require.False(t, item.SeasonallyAdjusted)
			require.NotNil(t, item.PublishedAt)
			require.Nil(t, item.HistoricalVersion)
			require.Contains(t, item.Note, "累计同比不相减")
			if strings.HasSuffix(test.period, "02") {
				require.Contains(t, item.Note, "不是2月单月值")
			}
		})
	}
	item, err := parseInvestmentRelease(investmentFixture(t, "202609.html"), "total", "202609")
	require.NoError(t, err)
	require.Equal(t, "2026-10-19T10:00:00+08:00", item.PublishedAt.Format(time.RFC3339), "取页面公布时间，目录时间10:05不是实际公布时间")
	require.Contains(t, item.BasisNote, "可比口径")
}

func TestInvestmentParsingRejectsChangedScopeAndMetadata(t *testing.T) {
	html := investmentFixture(t, "202609.html")
	for _, test := range []struct{ name, html, sector, period string }{
		{"month", html, "total", "202608"},
		{"category", html, "real-estate", "202609"},
		{"unit", strings.ReplaceAll(html, "亿元", "万元"), "total", "202609"},
		{"title", strings.ReplaceAll(html, `name="ArticleTitle"`, `name="wrong"`), "total", "202609"},
		{"unknown-infrastructure", strings.ReplaceAll(html, "基础设施投资包括电力", "基础设施投资包括未知行业"), "infrastructure", "202609"},
		{"invalid-time", strings.ReplaceAll(html, "2026/10/19 10:00", "unknown"), "total", "202609"},
	} {
		t.Run(test.name, func(t *testing.T) {
			item, err := parseInvestmentRelease(test.html, test.sector, test.period)
			require.Error(t, err)
			require.Nil(t, item)
		})
	}
	zeroHTML := strings.ReplaceAll(investmentFixture(t, "202609.html"), ">-2.1<", ">0.0<")
	item, err := parseInvestmentRelease(zeroHTML, "manufacturing", "202609")
	require.NoError(t, err)
	require.Equal(t, 0.0, *item.YTDGrowth)
	property := strings.ReplaceAll(investmentFixture(t, "property-202609.html"), ">53364<", ">-1<")
	_, err = parseInvestmentRelease(property, "real-estate", "202609")
	require.ErrorContains(t, err, "累计金额无效")
}

func mockInvestmentFeed(t *testing.T, page, fixture string) {
	t.Helper()
	gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", page).MatchParam("pageSize", "100").Reply(200).BodyString(investmentFixture(t, fixture))
}

func TestInvestmentCacheRefreshAndFuture(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := &InvestmentRequest{Begin: "202609", End: "202609", Sector: "total"}
	mock := func() {
		mockInvestmentFeed(t, "1", "feed1.json")
		gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965616.html").Reply(200).BodyString(investmentFixture(t, "202609.html"))
	}
	mock()
	result, err := client.Investment(context.Background(), req)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Len(t, result.Observations, 1)
	require.Equal(t, 334115.0, *result.Observations[0].YTDAmount)
	require.Equal(t, "202509", result.ArchiveStart)
	cached, err := client.Investment(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, cached)
	client.Refresh = true
	mock()
	_, err = client.Investment(context.Background(), req)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	client.Refresh = false
	mockInvestmentFeed(t, "2", "feed2.json")
	mockInvestmentFeed(t, "3", "feed3.json")
	req.End = "202610"
	future, err := client.Investment(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, future.Observations, 2)
	require.Equal(t, "not_published", future.Observations[1].Status)
	require.Nil(t, future.Observations[1].YTDAmount)
	require.Nil(t, future.Observations[1].PublishedAt)
	require.Empty(t, future.Observations[1].Source)
	require.True(t, gock.IsDone())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Investment(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestInvestmentJanuaryNoRequests(t *testing.T) {
	defer gock.Off()
	result, err := NewClient("").Investment(context.Background(), &InvestmentRequest{Begin: "202601", End: "202601", Sector: "manufacturing"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 1)
	require.Equal(t, "january_exempt", result.Observations[0].Status)
	require.Nil(t, result.Observations[0].YTDGrowth)
	require.Nil(t, result.Observations[0].FetchedAt)
	require.Contains(t, result.Observations[0].Note, "合并")
}

func TestInvestmentPagingAndArchiveGap(t *testing.T) {
	for _, test := range []struct{ name, response, message string }{
		{"empty", `{"success":true,"data":[]}`, "目录为空"},
		{"unrelated", `{"success":true,"data":[{"title":"其他月报"}]}`, "未检索到"},
		{"failure", `{"success":false,"message":"服务错误"}`, "查询失败"},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer gock.Off()
			gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").Reply(200).BodyString(test.response)
			_, err := NewClient("").Investment(context.Background(), &InvestmentRequest{Begin: "202609", End: "202609", Sector: "total"})
			require.ErrorContains(t, err, test.message)
		})
	}
	t.Run("gap", func(t *testing.T) {
		defer gock.Off()
		var feed struct {
			Data []industrialRelease `json:"data"`
		}
		require.NoError(t, json.Unmarshal([]byte(investmentFixture(t, "feed1.json")), &feed))
		for i := range feed.Data {
			if investmentReleasePeriod(feed.Data[i].Title, "total") == "202608" {
				feed.Data[i].Title = "已移除的月报"
			}
		}
		raw, err := json.Marshal(map[string]any{"success": true, "data": feed.Data})
		require.NoError(t, err)
		gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", "1").Reply(200).BodyString(string(raw))
		mockInvestmentFeed(t, "2", "feed2.json")
		mockInvestmentFeed(t, "3", "feed3.json")
		_, err = NewClient("").Investment(context.Background(), &InvestmentRequest{Begin: "202608", End: "202608", Sector: "total"})
		require.ErrorContains(t, err, "档案缺口")
		require.True(t, gock.IsDone())
	})
	t.Run("repeated-page", func(t *testing.T) {
		defer gock.Off()
		mockInvestmentFeed(t, "1", "feed1.json")
		mockInvestmentFeed(t, "2", "feed1.json")
		_, err := NewClient("").Investment(context.Background(), &InvestmentRequest{Begin: "202510", End: "202510", Sector: "total"})
		require.ErrorContains(t, err, "重复分页")
		require.True(t, gock.IsDone())
	})
}

func TestInvestmentReleaseCacheValidation(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	release := industrialRelease{URL: "https://www.stats.gov.cn/sj/zxfb/202610/t20261019_1965616.html"}
	key := sha256.Sum256([]byte("investment-release\n" + release.URL))
	path := filepath.Join(client.CacheDir, hex.EncodeToString(key[:])+".json")
	body, _ := json.Marshal(investmentFixture(t, "202609.html"))
	for _, age := range []time.Duration{7 * time.Hour, -time.Hour} {
		require.NoError(t, client.saveCache(path, cacheEntry{FetchedAt: time.Now().Add(-age), Body: body}))
		gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965616.html").Reply(503)
		_, _, err := client.investmentRelease(context.Background(), release, "total", "202609")
		require.ErrorContains(t, err, "503")
	}
	invalid, _ := json.Marshal("<html>坏缓存</html>")
	require.NoError(t, client.saveCache(path, cacheEntry{FetchedAt: time.Now(), Body: invalid}))
	gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965616.html").Reply(200).BodyString(investmentFixture(t, "202609.html"))
	_, _, err := client.investmentRelease(context.Background(), release, "total", "202609")
	require.NoError(t, err)
	client.Refresh = true
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965616.html").Reply(200).BodyString("<html>错误页</html>")
	_, _, err = client.investmentRelease(context.Background(), release, "total", "202609")
	require.Error(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after, "错误页不能覆盖有效缓存")
	for _, bad := range []string{"http://www.stats.gov.cn/sj/x.html", "https://evil.example/sj/x.html", "https://www.stats.gov.cn@evil.example/sj/x.html"} {
		_, _, err := client.investmentRelease(context.Background(), industrialRelease{URL: bad}, "total", "202609")
		require.ErrorContains(t, err, "无效")
	}
	require.True(t, gock.IsDone())
}

func TestInvestmentValidation(t *testing.T) {
	for _, req := range []*InvestmentRequest{nil, {Begin: "202508", End: "202609", Sector: "total"}, {Begin: "202509", End: "202609", Sector: "real-estate"}, {Begin: "202609", End: "202601", Sector: "total"}, {Begin: "202613", End: "202613", Sector: "total"}, {Begin: "202609", End: "202609", Sector: "bad"}} {
		_, err := NewClient("").Investment(context.Background(), req)
		require.Error(t, err)
	}
	require.Equal(t, "202512", investmentReleasePeriod("2025年全国固定资产投资基本情况", "total"))
	require.Equal(t, "", investmentReleasePeriod("2026年1—13月份全国固定资产投资基本情况", "total"))
	require.Equal(t, "", investmentReleasePeriod("2026年1—9月份全国房地产市场基本情况", "total"))
}

func investmentFeedCachePath(client *Client, page int) string {
	params := url.Values{"code": {"zxfb"}, "pagenum": {strconv.Itoa(page)}, "pageSize": {"100"}}
	key := sha256.Sum256([]byte(client.baseURL + "/new/queryCMSArticles?" + params.Encode() + "\n"))
	return filepath.Join(client.CacheDir, hex.EncodeToString(key[:])+".json")
}

func TestInvestmentDirectoryRefreshPreservesValidCache(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := &InvestmentRequest{Begin: "202510", End: "202510", Sector: "total"}
	needed := map[string]bool{"202510": true}
	mockInvestmentFeed(t, "1", "feed1.json")
	mockInvestmentFeed(t, "2", "feed2.json")
	_, _, err := client.investmentReleases(context.Background(), req, needed)
	require.NoError(t, err)
	page1, page2 := investmentFeedCachePath(client, 1), investmentFeedCachePath(client, 2)
	before1, err := os.ReadFile(page1)
	require.NoError(t, err)
	before2, err := os.ReadFile(page2)
	require.NoError(t, err)
	client.Refresh = true

	gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", "1").Reply(200).BodyString(`{"success":true,"data":[]}`)
	_, _, err = client.investmentReleases(context.Background(), req, needed)
	require.ErrorContains(t, err, "目录为空")
	after, err := os.ReadFile(page1)
	require.NoError(t, err)
	require.Equal(t, before1, after, "空首页不能覆盖原有效目录")

	mockInvestmentFeed(t, "1", "feed1.json")
	mockInvestmentFeed(t, "2", "feed1.json")
	_, _, err = client.investmentReleases(context.Background(), req, needed)
	require.ErrorContains(t, err, "重复分页")
	after, err = os.ReadFile(page2)
	require.NoError(t, err)
	require.Equal(t, before2, after, "重复分页不能覆盖该页原有效目录")

	var conflicting investmentReleaseFeed
	require.NoError(t, json.Unmarshal([]byte(investmentFixture(t, "feed2.json")), &conflicting))
	conflicting.Data = append(conflicting.Data, industrialRelease{Title: "2025年1—10月份全国固定资产投资基本情况", URL: "https://www.stats.gov.cn/sj/zxfb/202511/conflict.html"})
	raw, err := json.Marshal(conflicting)
	require.NoError(t, err)
	mockInvestmentFeed(t, "1", "feed1.json")
	gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", "2").Reply(200).BodyString(string(raw))
	_, _, err = client.investmentReleases(context.Background(), req, needed)
	require.ErrorContains(t, err, "多个版本")
	after, err = os.ReadFile(page2)
	require.NoError(t, err)
	require.Equal(t, before2, after, "同月版本冲突不能覆盖该页原有效目录")
	require.True(t, gock.IsDone())
}

func TestInvestmentDirectoryBadCacheRecovery(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := &InvestmentRequest{Begin: "202510", End: "202510", Sector: "total"}
	needed := map[string]bool{"202510": true}
	mockInvestmentFeed(t, "1", "feed1.json")
	mockInvestmentFeed(t, "2", "feed2.json")
	_, _, err := client.investmentReleases(context.Background(), req, needed)
	require.NoError(t, err)
	page1, page2 := investmentFeedCachePath(client, 1), investmentFeedCachePath(client, 2)

	for _, bad := range []string{
		`{"success":true,"data":[]}`,
		strings.Replace(investmentFixture(t, "feed1.json"), `"success":true`, `"success":false`, 1),
		strings.Replace(investmentFixture(t, "feed1.json"), `"success":true`, `"ignored":true`, 1),
	} {
		require.NoError(t, client.saveCache(page1, cacheEntry{FetchedAt: time.Now(), Body: json.RawMessage(bad)}))
		mockInvestmentFeed(t, "1", "feed1.json")
		found, _, err := client.investmentReleases(context.Background(), req, needed)
		require.NoError(t, err)
		require.Contains(t, found, "202510")
		raw, err := os.ReadFile(page1)
		require.NoError(t, err)
		var entry cacheEntry
		require.NoError(t, json.Unmarshal(raw, &entry))
		require.JSONEq(t, investmentFixture(t, "feed1.json"), string(entry.Body))
		require.True(t, gock.IsDone(), "坏缓存须请求网络恢复，不能被当作有效命中")
	}

	// 第二页缓存重复第一页：先校验缓存失败，再校验网络；两次回调不能污染分页状态。
	require.NoError(t, client.saveCache(page2, cacheEntry{FetchedAt: time.Now(), Body: json.RawMessage(investmentFixture(t, "feed1.json"))}))
	mockInvestmentFeed(t, "2", "feed2.json")
	found, _, err := client.investmentReleases(context.Background(), req, needed)
	require.NoError(t, err)
	require.Contains(t, found, "202510")
	raw, err := os.ReadFile(page2)
	require.NoError(t, err)
	var entry cacheEntry
	require.NoError(t, json.Unmarshal(raw, &entry))
	require.JSONEq(t, investmentFixture(t, "feed2.json"), string(entry.Body))
	require.True(t, gock.IsDone())
}

func TestInvestmentTerminalDirectoryValidationPreservesCache(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := &InvestmentRequest{Begin: "202610", End: "202610", Sector: "total"}
	needed := map[string]bool{"202610": true}
	for i := 1; i <= 3; i++ {
		mockInvestmentFeed(t, strconv.Itoa(i), "feed"+strconv.Itoa(i)+".json")
	}
	_, _, err := client.investmentReleases(context.Background(), req, needed)
	require.NoError(t, err)
	terminalPath := investmentFeedCachePath(client, 3)
	before, err := os.ReadFile(terminalPath)
	require.NoError(t, err)
	client.Refresh = true
	for i := 1; i <= 2; i++ {
		var feed investmentReleaseFeed
		require.NoError(t, json.Unmarshal([]byte(investmentFixture(t, "feed"+strconv.Itoa(i)+".json")), &feed))
		for j := range feed.Data {
			feed.Data[j].Title = "其他月报"
		}
		raw, err := json.Marshal(feed)
		require.NoError(t, err)
		gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryCMSArticles").MatchParam("pagenum", strconv.Itoa(i)).Reply(200).BodyString(string(raw))
	}
	mockInvestmentFeed(t, "3", "feed3.json")
	_, _, err = client.investmentReleases(context.Background(), req, needed)
	require.ErrorContains(t, err, "未检索到")
	after, err := os.ReadFile(terminalPath)
	require.NoError(t, err)
	require.Equal(t, before, after, "最终未检索到对应月报时不能覆盖原末页缓存")
	require.True(t, gock.IsDone())
}
