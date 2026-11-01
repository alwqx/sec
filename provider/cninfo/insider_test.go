package cninfo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func mockInsiderPage(t *testing.T, keyword string, hasMore bool, items ...map[string]any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"hasMore": hasMore, "announcements": items})
	require.NoError(t, err)
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").MatchParam("searchkey", keyword).Reply(200).BodyString(string(body))
}

func insiderItem(id, title string, at int64) map[string]any {
	return map[string]any{"announcementId": id, "announcementTitle": title, "announcementTime": at}
}

func TestQueryInsiderAnnouncements(t *testing.T) {
	defer gock.Off()
	both := insiderItem("both", "关于不<em>减持</em>并择机<em>增持</em>的公告", 300)
	mockInsiderPage(t, "增持", true, insiderItem("a", "关于<em>增持</em>股份&amp;计划的公告", 100), both, insiderItem("fuzzy", "关于<em>增</em>加<em>持</em>有基金的公告", 500))
	mockInsiderPage(t, "增持", false, map[string]any{"announcementId": "gone", "announcementTitle": "增持作废", "invalidationFlag": 1, "announcementTime": 900})
	mockInsiderPage(t, "减持", false, both, insiderItem("b", "股东<em>减持</em>计划", 200))
	items, truncated, err := QueryInsiderAnnouncements(context.Background(), "600036,org")
	require.NoError(t, err)
	require.False(t, truncated)
	titles := []string{}
	for _, item := range items {
		titles = append(titles, CleanTitle(item.Title))
	}
	require.Equal(t, []string{"关于不减持并择机增持的公告", "股东减持计划", "关于增持股份&计划的公告"}, titles, "按时间倒序、去重、严格匹配并清理标题")
	require.True(t, gock.IsDone())
}

func TestQueryInsiderAnnouncementsTruncatesLongResults(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").MatchParam("searchkey", "增持").Times(insiderMaxPages).Reply(200).BodyString(`{"hasMore":true,"announcements":[{"announcementId":"a","announcementTitle":"增持"}]}`)
	mockInsiderPage(t, "减持", false)
	items, truncated, err := QueryInsiderAnnouncements(context.Background(), "600036,org")
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, items, 1)
	require.True(t, gock.IsDone())
}

// 空页说明没有更多结果，不继续翻页，也不算截断。
func TestQueryInsiderAnnouncementsStopsOnEmptyPage(t *testing.T) {
	defer gock.Off()
	gock.CleanUnmatchedRequest()
	for _, keyword := range insiderKeywords {
		mockInsiderPage(t, keyword, true)
	}
	items, truncated, err := QueryInsiderAnnouncements(context.Background(), "600036,org")
	require.NoError(t, err)
	require.False(t, truncated)
	require.Empty(t, items)
	require.True(t, gock.IsDone())
	require.False(t, gock.HasUnmatchedRequest())
}

func TestQueryInsiderAnnouncementsFailsWithoutPartialResults(t *testing.T) {
	defer gock.Off()
	mockInsiderPage(t, "增持", false, insiderItem("a", "增持", 1))
	gock.New("https://www.cninfo.com.cn").Post("/new/hisAnnouncement/query").MatchParam("searchkey", "减持").Reply(500)
	items, _, err := QueryInsiderAnnouncements(context.Background(), "600036,org")
	require.Error(t, err)
	require.Nil(t, items)
}

func TestCleanTitle(t *testing.T) {
	require.Equal(t, "关于增持&计划", CleanTitle("关于<em>增持</em>&amp;计划"))
	require.Equal(t, "持股<5%的股东", CleanTitle("持股&lt;5%的股东"), "转义的文字不是标签，只清理一次")
}
