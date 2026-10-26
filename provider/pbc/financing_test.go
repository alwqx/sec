package pbc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func sample(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".html")
	require.NoError(t, err)
	return string(raw)
}

func parseSample(t *testing.T, year int, kind string) financingTable {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(sample(t, fmt.Sprintf("%s-%d", kind, year))))
	require.NoError(t, err)
	table, err := parseFinancing(doc, year, kind)
	require.NoError(t, err)
	return table
}

func TestSocialFinancing(t *testing.T) {
	files := map[string]string{statisticsPath: `<a href="/year2025.html">2025年统计数据</a><a href="/year2026.html">2026年统计数据</a>`}
	for _, year := range []int{2025, 2026} {
		files[fmt.Sprintf("/year%d.html", year)] = fmt.Sprintf(`<a href="/social%d.html">社会融资规模</a>`, year)
		files[fmt.Sprintf("/social%d.html", year)] = fmt.Sprintf(`<table><tr><td><table><tr><td>社会融资规模增量统计表<br>Flow</td><td><a href="/flow%d.htm">htm</a></td><td><a href="/unused.xlsx">xls</a></td></tr></table><table><tr><td>社会融资规模存量统计表<br>Stock</td><td><a href="/stock%d.htm">htm</a></td></tr></table><table><tr><td>地区社会融资规模增量统计表</td><td><a href="/province.htm">htm</a></td></tr></table></td></tr></table>`, year, year)
		for _, kind := range []string{"flow", "stock"} {
			files[fmt.Sprintf("/%s%d.htm", kind, year)] = sample(t, fmt.Sprintf("%s-%d", kind, year))
		}
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if body, ok := files[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, body)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.origin = server.URL
	req := FinancingRequest{"202609", "202609"}
	items, err := client.SocialFinancing(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 1)
	item := items[0]
	require.Equal(t, 4681700.0, *item.Stock)
	require.Equal(t, 7.1, *item.StockYoY)
	require.Equal(t, 35476.0, *item.Flow)
	require.Equal(t, 274589.0, *item.YTDFlow, "只查询9月也必须读取1—8月才能累计")
	require.Equal(t, "亿元", item.Unit)
	require.Equal(t, "万亿元", item.StockSourceUnit)
	require.Equal(t, "sum-of-published-monthly-flows", item.YTDMethod)
	require.Len(t, item.Components, 10)
	for _, part := range item.Components {
		if part.Key == "corporate-bonds" {
			require.Equal(t, -393.0, *part.Flow)
		}
		if part.Key == "rmb-loans" {
			require.Equal(t, 2799000.0, *part.Stock)
		}
	}
	require.Equal(t, int32(5), calls.Load())
	require.NotEmpty(t, item.StockNotes)
	cached, err := client.SocialFinancing(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, items, cached)
	require.Equal(t, int32(5), calls.Load())
	cross, err := client.SocialFinancing(context.Background(), FinancingRequest{"202511", "202609"})
	require.NoError(t, err)
	require.Len(t, cross, 11)
	require.Equal(t, "202511", cross[0].Period)
	require.Equal(t, "202609", cross[10].Period)
	require.Equal(t, 72185.0, *cross[2].Flow)
	require.Equal(t, 72185.0, *cross[2].YTDFlow, "跨年累计应重置")
	require.Equal(t, int32(9), calls.Load())
	client.Refresh = true
	_, err = client.SocialFinancing(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(14), calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.SocialFinancing(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestAnnualTables(t *testing.T) {
	for _, year := range []int{2020, 2025, 2026} {
		for _, kind := range []string{"flow", "stock"} {
			table := parseSample(t, year, kind)
			require.Len(t, table.Values, 12)
			require.Contains(t, table.Names, "total")
		}
	}
	flow := parseSample(t, 2026, "flow")
	stock := parseSample(t, 2026, "stock")
	require.Equal(t, 35476.0, *flow.Values["202609"]["total"].Amount)
	require.Equal(t, 468.17, *stock.Values["202609"]["total"].Amount)
	require.Nil(t, flow.Values["202610"]["total"].Amount, "未来模板空行不能变为零")
	require.Equal(t, 274589.0, *yearToDate(flow, 2026, 9, "total"))
	delete(flow.Values, "202602")
	require.Nil(t, yearToDate(flow, 2026, 9, "total"), "缺失月份不能跳过求和")
	require.NotEmpty(t, parseSample(t, 2020, "stock").Notes, "编号口径注释不能作为数据行，也不能丢弃")
}

func TestMalformedTables(t *testing.T) {
	valid := sample(t, "flow-2026")
	for _, test := range []struct{ name, body string }{
		{"wrong-title", strings.ReplaceAll(valid, "社会融资规模增量统计表", "地区统计表")},
		{"missing-unit", strings.ReplaceAll(valid, "单位：亿元人民币", "金额")},
		{"invalid-value", strings.ReplaceAll(valid, "72185", "NaN")},
		{"missing-total", strings.ReplaceAll(valid, "社会融资规模增量</td>", "其他总量</td>")},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(test.body))
			require.NoError(t, err)
			_, err = parseFinancing(doc, 2026, "flow")
			require.Error(t, err)
		})
	}
	for _, text := range []string{"NaN", "Inf", "未知"} {
		_, err := number(text)
		require.Error(t, err)
	}
	zero, err := number("0")
	require.NoError(t, err)
	require.Equal(t, 0.0, *zero)
	missing, err := number("--")
	require.NoError(t, err)
	require.Nil(t, missing)
	negative, err := number("-393")
	require.NoError(t, err)
	require.Equal(t, -393.0, *negative)
	_, err = NewClient("").SocialFinancing(context.Background(), FinancingRequest{"202609", "202601"})
	require.ErrorContains(t, err, "不能晚于")
	_, err = NewClient("").resolve("https://www.pbc.gov.cn/", "https://example.com/file.html")
	require.Error(t, err)
}

func TestGBKAndGrid(t *testing.T) {
	html := `<html><meta charset="gb2312"><table><tr><td rowspan="2">年份</td><td colspan="2">社会融资规模</td></tr><tr><td>存量</td><td>增速</td></tr></table></html>`
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(html))
	require.NoError(t, err)
	doc, err := decodePage(cachedPage{Body: encoded, ContentType: "text/html; charset=gb2312"})
	require.NoError(t, err)
	grid, err := tableGrid(doc.Find("table"))
	require.NoError(t, err)
	require.Equal(t, []string{"年份", "社会融资规模", "社会融资规模"}, grid[0])
	require.Equal(t, []string{"年份", "存量", "增速"}, grid[1])
}
