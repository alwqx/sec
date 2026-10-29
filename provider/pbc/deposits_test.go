package pbc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
)

func depositDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return doc
}

func TestDepositBalancesOfficial(t *testing.T) {
	for _, year := range []int{2020, 2025, 2026} {
		name := fmt.Sprintf("deposits-%d", year)
		if year == 2026 {
			name = "loans-2026"
		}
		values, err := parseDepositBalances(depositDoc(t, sample(t, name)), year)
		require.NoError(t, err)
		require.Len(t, values, 12)
		value := values[fmt.Sprintf("%d09", year)]
		require.NotNil(t, value.Balance)
		require.NotNil(t, value.Demand)
		require.NotNil(t, value.TimeAndOther)
		require.NotEmpty(t, value.RawValues)
		require.NotEmpty(t, value.Notes)
		if year == 2026 {
			require.Equal(t, 1752979.27, *value.Balance)
			require.Equal(t, 445096.87, *value.Demand)
			require.Equal(t, 1307882.4, *value.TimeAndOther)
			require.Nil(t, values["202610"].Balance)
		}
	}
}

func TestDepositReportsSeparateCurrenciesAndPeriods(t *testing.T) {
	for _, test := range []struct {
		period       string
		monthly, ytd *float64
	}{
		{"202609", nil, depositPointer(93900)}, {"202608", nil, depositPointer(69900)}, {"202602", nil, depositPointer(52400)},
		{"202112", nil, depositPointer(99000)}, {"202111", depositPointer(7308), nil},
	} {
		report, err := parseDepositReport(depositDoc(t, sample(t, "loan-report-"+test.period)), test.period)
		require.NoError(t, err)
		require.Equal(t, test.monthly, report.Monthly)
		require.Equal(t, test.ytd, report.YTD)
		require.Nil(t, report.YoY, "不能把全部人民币存款同比赋给住户")
	}
	for _, test := range []struct {
		prefix, period, household string
		monthly, ytd              *float64
	}{
		{"1月", "202601", "增加1.2万亿元", depositPointer(12000), depositPointer(12000)},
		{"前两个月", "202602", "减少2亿元", nil, depositPointer(-2)},
		{"本月", "202602", "增加0亿元", depositPointer(0), nil},
	} {
		html := `<html><a href="/source">source</a><div id="zoom"><p>月末人民币存款余额100万亿元，同比增长7.5%。</p><p>` + test.prefix + `人民币存款增加3万亿元。其中，住户存款` + test.household + `。</p></div></html>`
		r, err := parseDepositReport(depositDoc(t, html), test.period)
		require.NoError(t, err)
		require.Equal(t, test.monthly, r.Monthly)
		require.Equal(t, test.ytd, r.YTD)
	}
	_, err := parseDepositReport(depositDoc(t, `<body><p>月末人民币存款余额100万亿元。</p><p>1月人民币存款增加1万亿元。其中，住户存款增加1亿元。</p></body>`), "202602")
	require.Error(t, err)
}
func depositPointer(v float64) *float64 { return &v }

func TestRMBDepositsStrictBasesAndCache(t *testing.T) {
	files := map[string]string{
		statisticsPath: `<a href="/y2025">2025年统计数据</a><a href="/y2026">2026年统计数据</a>`,
		"/y2025":       `<a href="/credit2025">金融机构信贷收支统计</a>`, "/y2026": `<a href="/credit2026">金融机构信贷收支统计</a>`,
		"/credit2025":       `<table><tr><td>金融机构人民币信贷收支表</td><td><a href="/deposits2025.htm">htm</a></td></tr></table>`,
		"/credit2026":       `<table><tr><td>金融机构人民币信贷收支表</td><td><a href="/deposits2026.htm">htm</a></td></tr><tr><td>存款类金融机构人民币信贷收支表</td><td><a href="/wrong.htm">htm</a></td></tr></table>`,
		"/deposits2025.htm": sample(t, "deposits-2025"), "/deposits2026.htm": sample(t, "loans-2026"),
		financialReportsPath: `<a href="/r609">2026年前三季度金融统计数据报告</a><a href="/r608">2026年8月金融统计数据报告</a><a href="/r607">2026年7月金融统计数据报告</a>`,
		"/r609":              sample(t, "loan-report-202609"), "/r608": sample(t, "loan-report-202608"),
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		html, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, html)
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.origin = server.URL
	req := &DepositRequest{"202609", "202610"}
	result, err := client.RMBDeposits(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
	item := result.Observations[0]
	require.Equal(t, 1752979.27, *item.Balance)
	require.Equal(t, 1639847.95, *item.YearAgoBase.Balance)
	require.InDelta(t, (1752979.27/1639847.95-1)*100, *item.ComputedYoY, 1e-9)
	require.Nil(t, item.ReportedHouseholdYoY)
	require.Equal(t, 24000.0, *item.MonthlyFlow)
	require.Equal(t, 93900.0, *item.YTDFlow)
	require.True(t, item.MonthlyFlowApproximate)
	require.Equal(t, "difference-of-rounded-ytd-releases", item.MonthlyFlowMethod)
	require.Equal(t, server.URL+"/r608", item.PreviousReportSource)
	require.Equal(t, "2026-10-14", item.ReportPublishedDate)
	require.Nil(t, result.BalancePublishedAt)
	require.Nil(t, result.RevisionID)
	require.False(t, result.Annualized)
	require.Nil(t, result.Observations[1].Balance)
	require.Nil(t, result.Observations[1].MonthlyFlow)
	before := calls.Load()
	cached, err := client.RMBDeposits(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, cached)
	require.Equal(t, before, calls.Load())
	address := server.URL + "/deposits2026.htm"
	key := sha256.Sum256([]byte(address))
	path := filepath.Join(client.CacheDir, hex.EncodeToString(key[:])+".json")
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	files["/deposits2026.htm"] = strings.Replace(files["/deposits2026.htm"], "1752979.27", "NaN", 1)
	client.Refresh = true
	_, err = client.RMBDeposits(context.Background(), req)
	require.Error(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, saved, after)
	client.Refresh = false
	_, err = client.RMBDeposits(context.Background(), req)
	require.NoError(t, err)
	files["/deposits2026.htm"] = sample(t, "loans-2026")
	var page cachedPage
	require.NoError(t, json.Unmarshal(saved, &page))
	page.Body = []byte(`<table><tr><td>broken</td></tr></table>`)
	corrupt, err := json.Marshal(page)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, corrupt, 0600))
	before = calls.Load()
	_, err = client.RMBDeposits(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, before+1, calls.Load())
	// 严格基期缺失时不跨到最近月份；累计报告缺失也不使用余额差冒充新增。
	files["/deposits2025.htm"] = strings.Replace(sample(t, "deposits-2025"), "1639847.95", "", 1)
	files[financialReportsPath] = `<a href="/r609">2026年前三季度金融统计数据报告</a><a href="/r607">2026年7月金融统计数据报告</a>`
	client.CacheDir = ""
	result, err = client.RMBDeposits(context.Background(), req)
	require.NoError(t, err)
	require.Nil(t, result.Observations[0].ComputedYoY)
	require.Nil(t, result.Observations[0].MonthlyFlow)
}

func TestDepositsRejectBadBalancesAndRequests(t *testing.T) {
	original := sample(t, "loans-2026")
	for _, raw := range []string{
		strings.Replace(original, "单位：亿元", "单位：万亿元", 1),
		strings.Replace(original, "1752979.27", "-1", 1), strings.Replace(original, "1752979.27", "Inf", 1),
		strings.Replace(original, "住户存款", "unknown", 1), strings.Replace(original, "2026.09", "2026.08", 1),
		strings.Replace(original, "2026.09", "2025.09", 1),
	} {
		_, err := parseDepositBalances(depositDoc(t, raw), 2026)
		require.Error(t, err)
	}
	for _, req := range []*DepositRequest{nil, {"202001", "202101"}, {"202613", "202613"}, {"202609", "202608"}, {"202601", "999901"}} {
		_, err := NewClient("").RMBDeposits(context.Background(), req)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").RMBDeposits(ctx, &DepositRequest{"202609", "202609"})
	require.ErrorIs(t, err, context.Canceled)
}
