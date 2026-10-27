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

func TestLPRAnnouncement(t *testing.T) {
	for _, tc := range []struct {
		month, date, published string
		one, five              float64
	}{
		{"201908", "20190820", "2019-08-20T09:30:00+08:00", 4.25, 4.85},
		{"202407", "20240722", "2024-07-22T09:00:00+08:00", 3.35, 3.85},
		{"202504", "20250421", "2025-04-21T09:00:00+08:00", 3.1, 3.6},
		{"202505", "20250520", "2025-05-20T09:00:00+08:00", 3, 3.5},
	} {
		t.Run(tc.month, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(sample(t, "lpr-"+tc.month)))
			require.NoError(t, err)
			match := lprTitleDate.FindStringSubmatch(compact(doc.Find("h2").Text()))
			require.NotNil(t, match, "需兼容同时调整发布时间的扩展标题")
			titleDate, err := lprDate(match)
			require.NoError(t, err)
			require.Equal(t, tc.date, titleDate)
			report, err := parseLPR(doc, tc.date)
			require.NoError(t, err)
			require.Equal(t, tc.date, report.QuoteDate)
			require.Equal(t, tc.published, report.PublishedAt)
			require.Equal(t, tc.one, *report.OneYearRate)
			require.Equal(t, tc.five, *report.FiveYearPlusRate)
			require.Equal(t, "until-next-release", report.Validity)
		})
	}
	raw := sample(t, "lpr-202505")
	for _, tc := range []struct{ old, replacement, message string }{
		{"5年期以上LPR", "5年期LPR", "缺少唯一"},
		{"1年期LPR为3.0%", "1年期LPR为3..0%", "无效"},
		{"2025年5月20日贷款市场", "2025年5月21日贷款市场", "不一致"},
		{"2025-05-20 09:00:00", "2025-05-21 09:00:00", "不一致"},
	} {
		require.Contains(t, raw, tc.old)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(strings.ReplaceAll(raw, tc.old, tc.replacement)))
		require.NoError(t, err)
		_, err = parseLPR(doc, "20250520")
		require.ErrorContains(t, err, tc.message)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	require.NoError(t, err)
	doc.Find("#shijian").Remove()
	report, err := parseLPR(doc, "20250520")
	require.NoError(t, err)
	require.Equal(t, "2025-05-20", report.PublishedAt)
	require.Equal(t, "day", report.PublishedPrecision, "不能补入常规发布时间")
}

func TestLPRCollection(t *testing.T) {
	link := func(date, href string) string {
		return fmt.Sprintf(`<a href="%s" title="%s全国银行间同业拆借中心受权公布贷款市场报价利率（LPR）公告">标题显示被截断...</a>`, href, date)
	}
	files := map[string]string{
		lprPath:  link("2025年5月20日", "/may") + `<a href="/banks">全国银行间同业拆借中心受权发布贷款市场报价利率报价行名单</a><a tagname="/page2">下一页</a>`,
		"/page2": link("2025年4月21日", "/apr") + link("2024年12月20日", "/old"),
		"/may":   sample(t, "lpr-202505"), "/apr": sample(t, "lpr-202504"),
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
	req := LPRRequest{"202505", "202505"}
	items, err := client.LPR(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 1, "区间外上月只作为基准，不混入结果")
	require.Equal(t, -10.0, *items[0].OneYearChangeBP)
	require.Equal(t, -10.0, *items[0].FiveYearPlusChangeBP)
	require.Equal(t, "changed", items[0].ChangeStatus)
	require.Equal(t, "20250421", items[0].PreviousQuoteDate)
	require.Equal(t, server.URL+"/apr", items[0].PreviousSource)
	require.Equal(t, int32(4), calls.Load())
	_, err = client.LPR(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(4), calls.Load())
	items, err = client.LPR(context.Background(), LPRRequest{"202502", "202505"})
	require.NoError(t, err)
	require.Len(t, items, 4)
	require.Nil(t, items[0].OneYearRate)
	require.Nil(t, items[0].FetchedAt)
	require.Equal(t, "missing-release", items[0].ChangeStatus)
	require.Nil(t, items[2].OneYearChangeBP, "上月缺失时不能跨缺口作差")
	require.Equal(t, "unknown", items[2].ChangeStatus)
	client.Refresh = true
	files["/may"] = strings.ReplaceAll(files["/may"], "1年期LPR为3.0%", "1年期LPR为3.1%")
	items, err = client.LPR(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, 0.0, *items[0].OneYearChangeBP, "单个期限持平须保留真实零值")
	require.Equal(t, -10.0, *items[0].FiveYearPlusChangeBP)
	files["/may"] = strings.ReplaceAll(files["/may"], "5年期以上LPR为3.5%", "5年期以上LPR为3.6%")
	items, err = client.LPR(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "unchanged", items[0].ChangeStatus)
	oldList := files[lprPath]
	files[lprPath] = link("2025年1月20日", "/jan") + link("2024年12月20日", "/dec") + link("2024年11月20日", "/old")
	files["/jan"] = strings.NewReplacer("2025年5月20日", "2025年1月20日", "2025-05-20", "2025-01-20").Replace(sample(t, "lpr-202505"))
	files["/dec"] = strings.NewReplacer("2025年4月21日", "2024年12月20日", "2025-04-21", "2024-12-20").Replace(sample(t, "lpr-202504"))
	items, err = client.LPR(context.Background(), LPRRequest{"202501", "202501"})
	require.NoError(t, err)
	require.Equal(t, "20241220", items[0].PreviousQuoteDate)
	require.Equal(t, -10.0, *items[0].OneYearChangeBP, "跨年仍以相邻上月比较")
	files[lprPath] = oldList
	files["/conflict"] = sample(t, "lpr-202505")
	files[lprPath] += link("2025年5月20日", "/conflict")
	_, err = client.LPR(context.Background(), req)
	require.ErrorContains(t, err, "同月公告冲突")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.LPR(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLPRReformStartAndTerminalPage(t *testing.T) {
	raw := sample(t, "lpr-201908")
	terminal := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == lprPath {
			fmt.Fprint(w, `<a href="/aug">2019年8月20日全国银行间同业拆借中心受权公布贷款市场报价利率（LPR）公告</a>`)
			if terminal {
				fmt.Fprint(w, `当前页:1/1`)
			}
		} else {
			fmt.Fprint(w, raw)
		}
	}))
	defer server.Close()
	client := NewClient("")
	client.origin = server.URL
	items, err := client.LPR(context.Background(), LPRRequest{"201908", "201908"})
	require.NoError(t, err)
	require.Equal(t, 4.25, *items[0].OneYearRate)
	require.Nil(t, items[0].OneYearChangeBP, "改革首月不能与旧机制比较")
	terminal = false
	_, err = client.LPR(context.Background(), LPRRequest{"201908", "201908"})
	require.ErrorContains(t, err, "无法确认已到末页")
	for _, req := range []LPRRequest{{"201907", "201908"}, {"202513", "202601"}, {"202505", "202504"}} {
		require.Error(t, req.Validate())
	}
}
