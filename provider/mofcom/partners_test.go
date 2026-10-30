package mofcom

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func cnyPartnersSample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/cny-partners-202608.html")
	require.NoError(t, err)
	return string(raw)
}

func TestCNYPartnersOfficialTable(t *testing.T) {
	result, err := parseCNYPartners(cnyPartnersSample(t), "202608")
	require.NoError(t, err)
	require.Len(t, result.Rows, 30)
	byName := map[string]TradePartner{}
	for _, row := range result.Rows {
		byName[row.Name] = row
		require.Equal(t, "CNY", row.Currency)
		require.Equal(t, "亿元人民币", row.Unit)
		require.Equal(t, "202608", row.Period)
	}
	require.Equal(t, 46455.2, *byName["总值"].TotalMonthly)
	require.Equal(t, 347752.7, *byName["总值"].TotalYTD)
	us := byName["美国"]
	require.Equal(t, 3789.1, *us.TotalMonthly)
	require.Equal(t, 27637.1, *us.TotalYTD)
	require.Equal(t, 2886.1, *us.ExportsMonthly)
	require.Equal(t, 20710.2, *us.ExportsYTD)
	require.Equal(t, 903.0, *us.ImportsMonthly)
	require.Equal(t, 6927.0, *us.ImportsYTD)
	require.Equal(t, -0.7, *us.ImportsYTDYoY)
	require.Equal(t, "3,789.1", us.RawValues["total_monthly"])
	require.Contains(t, byName, "欧洲联盟")
	require.Contains(t, byName, "其中：德国")
	require.Contains(t, result.Scope, "东帝汶")
	require.Contains(t, result.Scope, "欧洲联盟包括")
	require.Contains(t, result.Note, "不能加总")
}

func TestCNYPartnersRejectsChangedTables(t *testing.T) {
	for name, change := range map[string]func(*goquery.Document){
		"missing unit":      func(d *goquery.Document) { d.Find("tr").First().Remove() },
		"reordered columns": func(d *goquery.Document) { d.Find("tr").Eq(1).Find("td").Eq(1).SetText("出口") },
		"wrong month":       func(d *goquery.Document) { d.Find("tr").Eq(2).Find("td").Eq(1).SetText("1至7月") },
		"missing column":    func(d *goquery.Document) { d.Find("tr").Eq(4).Find("td").Last().Remove() },
		"duplicate row":     func(d *goquery.Document) { d.Find("table").AppendSelection(d.Find("tr").Eq(4).Clone()) },
		"invalid value":     func(d *goquery.Document) { d.Find("tr").Eq(4).Find("td").Eq(1).SetText("NaN") },
		"negative level":    func(d *goquery.Document) { d.Find("tr").Eq(4).Find("td").Eq(2).SetText("-1") },
		"no total":          func(d *goquery.Document) { d.Find("tr").Eq(3).Remove() },
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(cnyPartnersSample(t)))
			require.NoError(t, err)
			change(doc)
			html, err := doc.Html()
			require.NoError(t, err)
			_, err = parseCNYPartners(html, "202608")
			require.Error(t, err)
		})
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(cnyPartnersSample(t)))
	require.NoError(t, err)
	doc.Find("tr").Eq(4).Find("td").Eq(1).SetText("—")
	doc.Find("tr").Eq(4).Find("td").Eq(3).SetText("0")
	html, err := doc.Html()
	require.NoError(t, err)
	result, err := parseCNYPartners(html, "202608")
	require.NoError(t, err)
	require.Nil(t, result.Rows[1].TotalMonthly)
	require.Equal(t, 0.0, *result.Rows[1].ExportsMonthly)
}

func mockPartnerArticle(t *testing.T, release articleRelease, wrongMetadata bool) string {
	t.Helper()
	var article articleEnvelope
	article.Data.ID, article.Data.Name, article.Data.IssueDate, article.Data.Source, article.Data.Content = release.ID, release.Name, release.IssueDate, "海关总署", cnyPartnersSample(t)
	if wrongMetadata {
		article.Data.Source = "其他机构"
	}
	raw, err := json.Marshal(article)
	require.NoError(t, err)
	return string(raw)
}

func TestCNYPartnersDiscoveryAndCache(t *testing.T) {
	defer gock.Off()
	gock.InterceptClient(fdiHTTPClient)
	defer gock.RestoreClient(fdiHTTPClient)
	release := articleRelease{ID: 16991, Name: "2026年8月进出口商品主要国别（地区）总值表（人民币值）", IssueDate: "2026-10-13", Source: "海关总署"}
	// 总量月报已进入较早月份，也不能据此停止寻找伙伴月报。
	other := articleRelease{ID: 100, Name: "2026年7月全国进出口总值表（人民币值）", IssueDate: "2026-10-14", Source: "海关总署"}
	for i, r := range []articleRelease{other, release} {
		gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").MatchType("application/json;charset=UTF-8").JSON(map[string]int{"menuId": 1, "page": i + 1}).Reply(200).JSON(listEnvelope{Total: 2, Data: []articleRelease{r}})
	}
	gock.New("https://fdi.mofcom.gov.cn").Get("/cipa-api/statistics/web/get").MatchParam("id", "16991").Reply(200).BodyString(mockPartnerArticle(t, release, false))
	client := NewClient(t.TempDir())
	result, err := client.cnyPartners(context.Background(), "202608")
	require.NoError(t, err)
	require.Len(t, result.Rows, 30)
	require.Equal(t, "2026-10-13", result.Rows[0].PublishedDate)
	require.Contains(t, result.Source, "16991")
	require.False(t, result.FetchedAt.IsZero())
	again, err := client.cnyPartners(context.Background(), "202608")
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.True(t, gock.IsDone())
}

func TestCNYPartnersRejectsArticleMismatchAndKeepsEmptyCatalog(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(map[bool]string{false: "no partner report", true: "wrong article source"}[bad], func(t *testing.T) {
			defer gock.Off()
			gock.InterceptClient(fdiHTTPClient)
			defer gock.RestoreClient(fdiHTTPClient)
			release := articleRelease{ID: 16991, Name: "2026年8月进出口商品主要国别（地区）总值表（人民币值）", IssueDate: "2026-10-13", Source: "海关总署"}
			if !bad {
				release.Name = "2026年7月进出口商品主要国别（地区）总值表（人民币值）"
			}
			gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").Reply(200).JSON(listEnvelope{Total: 1, Data: []articleRelease{release}})
			if bad {
				gock.New("https://fdi.mofcom.gov.cn").Get("/cipa-api/statistics/web/get").Reply(200).BodyString(mockPartnerArticle(t, release, true))
			}
			result, err := NewClient("").cnyPartners(context.Background(), "202608")
			if bad {
				require.ErrorContains(t, err, "元数据")
			} else {
				require.NoError(t, err)
				require.Empty(t, result.Rows)
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestTradeSelectsCNYPartners(t *testing.T) {
	defer gock.Off()
	gock.InterceptClient(fdiHTTPClient)
	defer gock.RestoreClient(fdiHTTPClient)
	monthly := articleRelease{ID: 123, Name: "2026年8月全国进出口总值表（人民币值）", IssueDate: "2026-10-13", Source: "海关总署"}
	partner := articleRelease{ID: 16991, Name: "2026年8月进出口商品主要国别（地区）总值表（人民币值）", IssueDate: "2026-10-13", Source: "海关总署"}
	listing := listEnvelope{Total: 2, Data: []articleRelease{monthly, partner}}
	gock.New("https://fdi.mofcom.gov.cn").Post("/cipa-api/statistics/web/list").Times(2).Reply(200).JSON(listing)
	// 月度人民币总量与伙伴表各取自己的报告，美元伙伴接口不应被请求。
	var article articleEnvelope
	article.Data.ID, article.Data.Name, article.Data.IssueDate, article.Data.Source = monthly.ID, monthly.Name, monthly.IssueDate, monthly.Source
	raw, err := os.ReadFile("testdata/cny_monthly.html")
	require.NoError(t, err)
	article.Data.Content = strings.ReplaceAll(string(raw), "12月", "8月")
	gock.New("https://fdi.mofcom.gov.cn").Get("/cipa-api/statistics/web/get").MatchParam("id", "123").Reply(200).JSON(article)
	gock.New("https://fdi.mofcom.gov.cn").Get("/cipa-api/statistics/web/get").MatchParam("id", "16991").Reply(200).BodyString(mockPartnerArticle(t, partner, false))
	raw, err = os.ReadFile("testdata/usd_monthly.json")
	require.NoError(t, err)
	gock.New("https://data.mofcom.gov.cn").Post("/datamofcom/front/totalmonth/query").Reply(200).BodyString(strings.ReplaceAll(string(raw), "202412", "202608"))
	result, err := NewClient("").Trade(context.Background(), &TradeRequest{Begin: "202608", End: "202608", PartnerCurrency: "cny"})
	require.NoError(t, err)
	require.Equal(t, "CNY", result.PartnerCurrency)
	require.NotNil(t, result.Observations[0].USD)
	require.NotNil(t, result.Observations[0].CNY)
	require.Equal(t, "CNY", result.Partners[0].Currency)
	require.NotEmpty(t, result.PartnerScope)
	require.Contains(t, result.Note, "不能相加")
	require.True(t, gock.IsDone())
	require.Error(t, (&TradeRequest{Begin: "202608", End: "202608", PartnerCurrency: "eur"}).Validate())
}
