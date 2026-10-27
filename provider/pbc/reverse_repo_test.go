package pbc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

func TestReverseRepoAnnouncement(t *testing.T) {
	for _, tc := range []struct {
		name, effective, published, basis string
		before, after, bp                 float64
	}{
		{"20240722", "20240722", "2024-07-22T08:00:00+08:00", "immediate-on-publication-date", 1.8, 1.7, -10},
		{"20240927", "20240927", "2024-09-27T08:00:00+08:00", "explicit-date", 1.7, 1.5, -20},
		{"20250508", "20250508", "2025-05-07T09:25:30+08:00", "explicit-date", 1.5, 1.4, -10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(sample(t, "reverse-repo-"+tc.name)))
			require.NoError(t, err)
			event, err := parseReverseRepoEvent(doc)
			require.NoError(t, err)
			require.NotNil(t, event)
			require.Equal(t, tc.effective, event.EffectiveDate)
			require.Equal(t, tc.published, event.PublishedAt)
			require.Equal(t, tc.basis, event.EffectiveDateBasis)
			require.Equal(t, tc.before, event.PreviousRate)
			require.Equal(t, tc.after, event.Rate)
			require.Equal(t, tc.bp, event.ChangeBasisPoints)
		})
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(sample(t, "reverse-repo-temporary")))
	require.NoError(t, err)
	event, err := parseReverseRepoEvent(doc)
	require.NoError(t, err)
	require.Nil(t, event, "临时隔夜工具加减点不是七天政策利率调整")
	for _, tc := range []struct{ old, replacement, message string }{
		{"5月8日起", "2月30日起", "无效利率生效日期"},
		{"从2025年5月8日起", "经研究", "生效日期"},
		{"1.50%调整为1.40%", "1.50%改为1.40%", "格式无法识别"},
		{"2025-05-07 09:25:30", "2025-05-08 09:25:30", "不一致"},
	} {
		raw := sample(t, "reverse-repo-20250508")
		require.Contains(t, raw, tc.old)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(strings.ReplaceAll(raw, tc.old, tc.replacement)))
		require.NoError(t, err)
		_, err = parseReverseRepoEvent(doc)
		require.ErrorContains(t, err, tc.message)
	}
}

func TestReverseRepoCollection(t *testing.T) {
	files := map[string]string{
		reverseRepoPath: `<table><tr><td><a href="/temporary">中国人民银行公开市场业务公告［2026］第2号</a><span>2026-06-17</span></td></tr><tr><td><a href="/may">中国人民银行公开市场业务公告［2025］第1号</a><span>2025-05-07</span></td></tr></table><a tagname="/page2">下一页</a>`,
		"/page2":        `<table><tr><td><a href="/sep">公开市场业务公告［2024］第5号</a><span>2024-09-27</span></td></tr><tr><td><a href="/jul">公开市场业务公告［2024］第4号</a><span>2024-07-22</span></td></tr><tr><td><a href="/old">公开市场业务公告［2024］第3号</a><span>2024-07-08</span></td></tr></table>`,
		"/temporary":    sample(t, "reverse-repo-temporary"), "/may": sample(t, "reverse-repo-20250508"),
		"/sep": sample(t, "reverse-repo-20240927"), "/jul": sample(t, "reverse-repo-20240722"),
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if body, ok := files[r.URL.Path]; ok {
			fmt.Fprint(w, body)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.origin = server.URL
	req := ReverseRepoRequest{"20250101", "20251231"}
	result, err := client.ReverseRepo(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	require.Equal(t, "20250508", result.Events[0].EffectiveDate)
	require.Equal(t, server.URL+"/may", result.Events[0].Source)
	require.Equal(t, "20240927", result.PreviousEvent.EffectiveDate)
	require.Equal(t, int32(6), calls.Load())
	_, err = client.ReverseRepo(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(6), calls.Load(), "缓存命中不重复请求")
	result, err = client.ReverseRepo(context.Background(), ReverseRepoRequest{"20250507", "20250507"})
	require.NoError(t, err)
	require.Empty(t, result.Events, "按生效日过滤，不能混入仅已公布但尚未生效的调整")
	require.Equal(t, "20240927", result.PreviousEvent.EffectiveDate)
	client.Refresh = true
	_, err = client.ReverseRepo(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(12), calls.Load())
	files["/may"] = strings.ReplaceAll(files["/may"], "1.50%", "1.60%")
	_, err = client.ReverseRepo(context.Background(), req)
	require.ErrorContains(t, err, "不连续")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.ReverseRepo(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestReverseRepoRequest(t *testing.T) {
	for _, req := range []ReverseRepoRequest{{"202407", "20250508"}, {"20240230", "20250508"}, {"20200101", "20250508"}, {"20250509", "20250508"}} {
		require.Error(t, req.Validate())
	}
}
