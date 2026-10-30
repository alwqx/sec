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

func rrrSample(t *testing.T, date string) (*goquery.Document, []RRREvent) {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(sample(t, "rrr-"+date)))
	require.NoError(t, err)
	events, err := parseRRR(doc, compact(doc.Find("h2").Text()))
	require.NoError(t, err)
	return doc, events
}

func TestRRROfficialAnnouncements(t *testing.T) {
	total, broad, targeted := 0, 0, 0
	for date, count := range map[string]int{"20190104": 2, "20200101": 1, "20200313": 2, "20200403": 2, "20210709": 1, "20211206": 1, "20220415": 2, "20221125": 1, "20230317": 1, "20230914": 1, "20240124": 1, "20240927": 1, "20250507": 2} {
		t.Run(date, func(t *testing.T) {
			_, events := rrrSample(t, date)
			require.Len(t, events, count)
			for _, event := range events {
				require.NotEmpty(t, event.Scope)
				require.LessOrEqual(t, event.ChangePPLower, event.ChangePPUpper)
				require.Less(t, event.ChangePPUpper, 0.0)
				require.Equal(t, "second", event.PublishedPrecision)
				if event.ScopeType == "broad" {
					broad++
				} else {
					targeted++
				}
			}
			total += len(events)
		})
	}
	require.Equal(t, 18, total)
	require.Equal(t, 12, broad)
	require.Equal(t, 6, targeted)
	_, events := rrrSample(t, "20190104")
	require.Equal(t, []string{"20190115", "20190125"}, []string{events[0].EffectiveDate, events[1].EffectiveDate})
	require.Equal(t, -0.5, events[0].ChangePPLower)
	require.Equal(t, 2, events[0].TrancheCount)
	_, events = rrrSample(t, "20250507")
	require.Equal(t, "20250515", events[0].EffectiveDate)
	require.Equal(t, "2025-05-07T10:00:00+08:00", events[0].PublishedAt)
	require.Contains(t, events[0].Exclusions, "已执行5%")
	require.Equal(t, -0.5, events[0].ChangePPLower)
	require.Equal(t, "汽车金融公司和金融租赁公司", events[1].Scope)
	require.Equal(t, -5.0, events[1].ChangePPLower)
	require.False(t, events[1].Additional, "汽车金融公司调整不是在全面幅度上再加5个百分点")
	_, events = rrrSample(t, "20220415")
	require.Equal(t, -0.25, events[1].ChangePPLower)
	require.True(t, events[1].Additional)
	require.Equal(t, "没有跨省经营的城商行和存款准备金率高于5%的农商行", events[1].Scope)
	_, events = rrrSample(t, "20200313")
	require.Equal(t, -1.0, events[0].ChangePPLower)
	require.Equal(t, -0.5, events[0].ChangePPUpper)
	require.Equal(t, "达到考核标准的银行", events[0].Scope)
	require.True(t, events[1].Additional)
	require.Equal(t, "20200316", events[1].EffectiveDate)
	_, events = rrrSample(t, "20200403")
	require.Equal(t, "20200415", events[0].EffectiveDate)
	require.Equal(t, "20200515", events[1].EffectiveDate)
	for i, event := range events {
		require.Equal(t, -0.5, event.ChangePPLower, "每批幅度不能重复使用总降幅1个百分点")
		require.Equal(t, i+1, event.TrancheIndex)
		require.Equal(t, 2, event.TrancheCount)
	}
	_, events = rrrSample(t, "20240124")
	require.Equal(t, "20240205", events[0].EffectiveDate, "不能误用再贷款利率的1月25日生效日")
	require.Equal(t, -0.5, events[0].ChangePPLower)
}

func TestRRRInvalidAndPartialParsing(t *testing.T) {
	for _, tc := range []struct{ date, old, replacement, message string }{
		{"20200403", "存款准备金率1个百分点", "存款准备金率2个百分点", "分批降准幅度"},
		{"20200403", "5月15日", "5月35日", "生效日期无效"},
		{"20220415", "在下调存款准备金率0.25个百分点", "在下调存款准备金率0.5个百分点", "引用的全面降准幅度"},
		{"20250507", "存款准备金率5个百分点", "存款准备金率5百分比", "未完整识别"},
	} {
		doc, _ := rrrSample(t, tc.date)
		text := compact(doc.Find("#zoom").Text())
		require.Contains(t, text, tc.old)
		doc.Find("#zoom").SetText(strings.ReplaceAll(text, tc.old, tc.replacement))
		_, err := parseRRR(doc, "中国人民银行决定下调存款准备金率")
		require.ErrorContains(t, err, tc.message)
	}
	doc, _ := rrrSample(t, "20250507")
	doc.Find(`td.hui12[align="right"]`).Remove()
	events, err := parseRRR(doc, "中国人民银行决定下调存款准备金率")
	require.NoError(t, err)
	require.Equal(t, "2025-05-07", events[0].PublishedAt)
	require.Equal(t, "day", events[0].PublishedPrecision)
	require.False(t, isRRRDecision("中国人民银行决定下调金融机构外汇存款准备金率"))
	require.False(t, isRRRDecision("中国人民银行有关负责人就下调金融机构存款准备金率答记者问"))
}

func TestRRRCollection(t *testing.T) {
	row := func(date, title, href string) string {
		return fmt.Sprintf(`<table><tr><td><font class="newslist_style"><a href="%s" title="%s" istitle="true">截断标题...</a></font><span class="hui12">%s</span></td></tr></table>`, href, title, date)
	}
	files := map[string]string{
		rrrNewsPath: row("2019-01-01", "置顶旧新闻", "/pinned") + row("2025-05-07", "中国人民银行决定下调金融机构存款准备金率", "/may") + row("2023-09-01", "中国人民银行决定下调金融机构外汇存款准备金率", "/fx") + `<a tagname="/page2">下一页</a>`,
		"/page2":    row("2020-04-03", "中国人民银行决定对中小银行定向降准", "/phased") + `<a tagname="/page3">下一页</a>`,
		"/page3":    row("2017-12-31", "新闻", "/older"),
		"/may":      sample(t, "rrr-20250507"), "/phased": sample(t, "rrr-20200403"),
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
	result, err := client.scanRRR(context.Background(), RRRCoverageStart, nil)
	require.NoError(t, err)
	require.Len(t, result.Events, 4)
	require.Equal(t, "20200415", result.Events[0].EffectiveDate)
	require.Equal(t, server.URL+"/phased", result.Events[0].Source)
	require.Equal(t, 3, result.ScannedPages)
	require.Equal(t, 2, result.ScannedAnnouncements)
	require.Equal(t, int32(5), calls.Load(), "不获取外汇、问答或置顶旧新闻正文")
	_, err = client.scanRRR(context.Background(), RRRCoverageStart, nil)
	require.NoError(t, err)
	require.Equal(t, int32(8), calls.Load(), "只实时读取目录，公告正文仍复用缓存")
	result, err = client.RRR(context.Background(), RRRRequest{"20250507", "20250507"})
	require.NoError(t, err)
	require.Empty(t, result.Events, "公布日不等于生效日")
	client.Refresh = true
	_, err = client.scanRRR(context.Background(), RRRCoverageStart, nil)
	require.NoError(t, err)
	require.Equal(t, int32(13), calls.Load())
	files["/page2"] = strings.ReplaceAll(files["/page2"], "/page3", rrrNewsPath)
	_, err = client.scanRRR(context.Background(), RRRCoverageStart, nil)
	require.ErrorContains(t, err, "分页循环")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.scanRRR(ctx, RRRCoverageStart, nil)
	require.ErrorIs(t, err, context.Canceled)
	for _, req := range []RRRRequest{{"20171231", "20250101"}, {"20250230", "20250515"}, {"20250516", "20250515"}} {
		require.Error(t, req.Validate())
	}
}
