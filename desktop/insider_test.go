//go:build windows || darwin

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const cninfoHost = "https://www.cninfo.com.cn"

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func mockCninfoStock() {
	gock.New(cninfoHost).Get("/new/data/szse_stock.json").Reply(200).BodyString(`{"stockList":[{"code":"600036","orgId":"org1","zwjc":"招商银行"}]}`)
}

func cninfoAnnouncement(id, title string, at int64) string {
	return fmt.Sprintf(`{"announcementId":%q,"announcementTitle":%q,"announcementTime":%d,"secCode":"600036","secName":"招商银行","adjunctUrl":"finalpage/2026-01-01/%s.PDF"}`, id, title, at, id)
}

type announcementPage struct {
	Results []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"results"`
	Total     int  `json:"total"`
	HasMore   bool `json:"has_more"`
	Truncated bool `json:"truncated"`
}

func decodeAnnouncements(t *testing.T, path string) announcementPage {
	t.Helper()
	rr := macroRequest(t, path)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var page announcementPage
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &page))
	return page
}

func TestInsiderAnnouncements(t *testing.T) {
	defer gock.Off()
	isolateHome(t)
	mockCninfoStock()
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("searchkey", "增持").MatchParam("stock", "600036,org1").Reply(200).BodyString(`{"hasMore":false,"announcements":[` + cninfoAnnouncement("a", "关于<em>增持</em>计划", 100) + `,` + cninfoAnnouncement("c", "关于<em>增</em>加<em>持</em>有基金", 300) + `]}`)
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("searchkey", "减持").Reply(200).BodyString(`{"hasMore":false,"announcements":[` + cninfoAnnouncement("b", "股东<em>减持</em>计划", 200) + `]}`)
	page := decodeAnnouncements(t, "/api/v1/announcements?code=SH600036&type=insider")
	require.Equal(t, 2, page.Total, "模糊匹配但标题不含增减持的公告不计入")
	require.Equal(t, "股东减持计划", page.Results[0].Title, "按时间倒序，标题已清理")
	require.Equal(t, "关于增持计划", page.Results[1].Title)
	require.False(t, page.HasMore)
	require.False(t, page.Truncated)
}

func TestInsiderAnnouncementsFilterAndPaginate(t *testing.T) {
	defer gock.Off()
	isolateHome(t)
	items := make([]string, 0, 35)
	for i := 0; i < 35; i++ {
		items = append(items, cninfoAnnouncement(fmt.Sprintf("a%d", i), fmt.Sprintf("增持计划%02d", i), int64(1000-i)))
	}
	response := `{"hasMore":false,"announcements":[` + strings.Join(items, ",") + `]}`
	gock.New(cninfoHost).Get("/new/data/szse_stock.json").Persist().Reply(200).BodyString(`{"stockList":[{"code":"600036","orgId":"org1"}]}`)
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("searchkey", "增持").Persist().Reply(200).BodyString(response)
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("searchkey", "减持").Persist().Reply(200).BodyString(`{"hasMore":false,"announcements":[]}`)

	first := decodeAnnouncements(t, "/api/v1/announcements?code=SH600036&type=insider")
	require.Equal(t, 35, first.Total)
	require.Len(t, first.Results, 30)
	require.True(t, first.HasMore)
	second := decodeAnnouncements(t, "/api/v1/announcements?code=SH600036&type=insider&page=2")
	require.Len(t, second.Results, 5)
	require.False(t, second.HasMore)
	beyond := decodeAnnouncements(t, "/api/v1/announcements?code=SH600036&type=insider&page=9")
	require.Empty(t, beyond.Results)
	filtered := decodeAnnouncements(t, "/api/v1/announcements?code=SH600036&type=insider&key=%E8%AE%A1%E5%88%9207")
	require.Equal(t, 1, filtered.Total)
	spaced := decodeAnnouncements(t, "/api/v1/announcements?code=SH600036&type=insider&key=%E5%A2%9E%E6%8C%81%20%E8%AE%A1%E5%88%9207")
	require.Equal(t, 1, spaced.Total, "空格分隔的词都要命中")
}

func TestInsiderAnnouncementsKeywordIgnoresCaseAndKeepsEscapedText(t *testing.T) {
	defer gock.Off()
	isolateHome(t)
	mockCninfoStock()
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("searchkey", "增持").Reply(200).BodyString(`{"hasMore":false,"announcements":[` + cninfoAnnouncement("a", "*ST某某持股&lt;5%股东<em>增持</em>", 100) + `]}`)
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("searchkey", "减持").Reply(200).BodyString(`{"hasMore":false,"announcements":[]}`)
	page := decodeAnnouncements(t, "/api/v1/announcements?code=SH600036&type=insider&key=st")
	require.Equal(t, 1, page.Total)
	require.Equal(t, "*ST某某持股<5%股东增持", page.Results[0].Title, "转义的文字不被当成标签删掉")
}

func TestInsiderAnnouncementsErrors(t *testing.T) {
	defer gock.Off()
	isolateHome(t)
	rr := macroRequest(t, "/api/v1/announcements?type=insider")
	require.Equal(t, http.StatusBadRequest, rr.Code, "增减持公告必须指定证券")
	mockCninfoStock()
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").Reply(500)
	rr = macroRequest(t, "/api/v1/announcements?code=SH600036&type=insider")
	require.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestIPOAnnouncements(t *testing.T) {
	defer gock.Off()
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("category", "category_sf_szsh").MatchParam("pageNum", "2").Reply(200).BodyString(`{"totalAnnouncement":31,"hasMore":false,"announcements":[` + cninfoAnnouncement("a", "<em>首次公开发行</em>公告", 1000) + `]}`)
	page := decodeAnnouncements(t, "/api/v1/ipo/announcements?page=2")
	require.Equal(t, 31, page.Total)
	require.Equal(t, "首次公开发行公告", page.Results[0].Title)
}

// CNINFO 只有起止日期都提供时才筛选，只填一端也要生效。
func TestIPOAnnouncementsFillOpenDateRange(t *testing.T) {
	defer gock.Off()
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("seDate", "1990-01-01~2026-10-30").Reply(200).BodyString(`{"announcements":[]}`)
	decodeAnnouncements(t, "/api/v1/ipo/announcements?until=2026-10-30")
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("seDate", "2026-10-01~2026-10-30").Reply(200).BodyString(`{"announcements":[]}`)
	decodeAnnouncements(t, "/api/v1/ipo/announcements?since=2026-10-01&until=2026-10-30")
	gock.New(cninfoHost).Post("/new/hisAnnouncement/query").MatchParam("seDate", "^2026-10-01~20").Reply(200).BodyString(`{"announcements":[]}`)
	decodeAnnouncements(t, "/api/v1/ipo/announcements?since=2026-10-01")
	require.True(t, gock.IsDone())
}

func TestIPOAnnouncementsValidation(t *testing.T) {
	for _, query := range []string{"since=2026-13-01", "until=20260101", "since=2026-02-01&until=2026-01-01", "page=-1", "since=2999-01-01", "until=1985-01-01"} {
		rr := macroRequest(t, "/api/v1/ipo/announcements?"+query)
		require.Equal(t, http.StatusBadRequest, rr.Code, query)
	}
}
