package pbc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRRREmbeddedHistory(t *testing.T) {
	history, err := loadRRRHistory()
	require.NoError(t, err)
	require.Equal(t, 12, history.AnnouncementCount)
	require.Len(t, history.Events, 16)
	for _, event := range history.Events {
		_, parsed := rrrSample(t, strings.ReplaceAll(event.PublishedAt[:10], "-", ""))
		matched := false
		for _, candidate := range parsed {
			if rrrEventKey(candidate) == rrrEventKey(event) {
				candidate.Source, candidate.FetchedAt = event.Source, event.FetchedAt
				require.Equal(t, event, candidate, "快照字段需与官方样本一致")
				matched = true
			}
		}
		require.True(t, matched)
	}
	client := NewClient("")
	client.origin = "http://127.0.0.1:1" // 历史查询不能触发任何网络请求。
	result, err := client.RRR(context.Background(), RRRRequest{"20200501", "20200531"})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, "20200515", result.Events[0].EffectiveDate)
	require.Equal(t, "history-only", result.UpdateStatus)
	require.Equal(t, 0, result.ScannedPages)
	require.Nil(t, result.ListFetchedAt)
}

func TestRRRIncrementalAppend(t *testing.T) {
	row := func(date, href string) string {
		return fmt.Sprintf(`<table><tr><td><font class="newslist_style"><a href="%s" title="中国人民银行决定下调金融机构存款准备金率">公告</a></font><span class="hui12">%s</span></td></tr></table>`, href, date)
	}
	root := row("2025-05-07", "/may") + `<a tagname="/older">下一页</a>`
	files := map[string]string{"/may": sample(t, "rrr-20250507"), "/older": row("2025-04-30", "/unused")}
	var calls, bodyCalls atomic.Int32
	failed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if failed {
			http.Error(w, "unavailable", 503)
		} else if r.URL.Path == rrrNewsPath {
			fmt.Fprint(w, root)
		} else if body, ok := files[r.URL.Path]; ok {
			if r.URL.Path == "/may" || r.URL.Path == "/new" {
				bodyCalls.Add(1)
			}
			fmt.Fprint(w, body)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, historical := rrrSample(t, "20200101")
	historical[0].Source = server.URL + "/known"
	verified, err := time.Parse(time.RFC3339, "2025-05-01T00:00:00+08:00")
	require.NoError(t, err)
	history := rrrHistory{VerifiedThrough: "20250501", VerifiedAt: verified, AnnouncementCount: 1, Events: historical}
	client := NewClient(t.TempDir())
	client.origin = server.URL
	today := time.Now().In(time.FixedZone("CST", 8*60*60))
	req := RRRRequest{RRRCoverageStart, today.AddDate(0, 0, 1).Format("20060102")}
	result, err := client.rrrWithHistory(context.Background(), req, history)
	require.NoError(t, err)
	require.Len(t, result.Events, 3)
	require.Equal(t, 2, result.ScannedPages)
	require.Equal(t, 1, result.ScannedAnnouncements)
	require.Equal(t, int32(3), calls.Load())
	require.Equal(t, int32(1), bodyCalls.Load())
	result, err = client.rrrWithHistory(context.Background(), req, history)
	require.NoError(t, err)
	require.Len(t, result.Events, 3, "已持久化的增量不能在推进检查点后消失")
	require.Equal(t, 2, result.CachedUpdateCount)
	require.Equal(t, 1, result.ScannedPages)
	require.Equal(t, int32(4), calls.Load())
	require.Equal(t, int32(1), bodyCalls.Load(), "不重复抓取已有公告")
	// 同一检查日稍后发布的新公告必须继续发现，且未来生效事件也需要持久化。
	tomorrow := today.AddDate(0, 0, 1)
	files["/new"] = strings.NewReplacer("2024-09-27", today.Format("2006-01-02"), "2024年9月27日", fmt.Sprintf("%d年%d月%d日", tomorrow.Year(), tomorrow.Month(), tomorrow.Day())).Replace(sample(t, "rrr-20240927"))
	root = row(today.Format("2006-01-02"), "/new") + root
	result, err = client.rrrWithHistory(context.Background(), RRRRequest{RRRCoverageStart, today.Format("20060102")}, history)
	require.NoError(t, err)
	require.Len(t, result.Events, 3, "未来生效事件尚不混入区间内")
	require.Equal(t, int32(2), bodyCalls.Load())
	result, err = client.rrrWithHistory(context.Background(), req, history)
	require.NoError(t, err)
	require.Len(t, result.Events, 4)
	require.Equal(t, tomorrow.Format("20060102"), result.Events[3].EffectiveDate)
	require.Equal(t, int32(2), bodyCalls.Load(), "同日重扫只发现新URL，不重复追加")
	before := calls.Load()
	result, err = client.rrrWithHistory(context.Background(), RRRRequest{RRRCoverageStart, "20250531"}, history)
	require.NoError(t, err)
	require.Len(t, result.Events, 3)
	require.Equal(t, before, calls.Load(), "已覆盖的历史区间直接合并内置及本地增量")
	client.Refresh = true
	result, err = client.rrrWithHistory(context.Background(), RRRRequest{RRRCoverageStart, "20250531"}, history)
	require.NoError(t, err)
	require.Equal(t, "checked-latest", result.UpdateStatus)
	statePath := filepath.Join(client.CacheDir, "rrr-updates.json")
	stateBefore, err := os.ReadFile(statePath)
	require.NoError(t, err)
	failed = true
	_, err = client.rrrWithHistory(context.Background(), req, history)
	require.ErrorContains(t, err, "503")
	stateAfter, err := os.ReadFile(statePath)
	require.NoError(t, err)
	require.Equal(t, stateBefore, stateAfter, "失败时不能推进检查点")
	failed = false
	require.NoError(t, os.WriteFile(statePath, []byte("broken"), 0600))
	result, err = client.rrrWithHistory(context.Background(), req, history)
	require.NoError(t, err)
	require.Len(t, result.Events, 4, "损坏增量从内置检查点重新发现")
	uncached := NewClient("")
	uncached.origin = server.URL
	result, err = uncached.rrrWithHistory(context.Background(), req, history)
	require.NoError(t, err)
	require.Len(t, result.Events, 4)
	require.Zero(t, result.CachedUpdateCount)
}
