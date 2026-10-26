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

func reportSample(t *testing.T, period string) loanReport {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(sample(t, "loan-report-"+period)))
	require.NoError(t, err)
	report, err := parseLoanReport(doc, period)
	require.NoError(t, err)
	return report
}

func TestRMBLoans(t *testing.T) {
	files := map[string]string{
		statisticsPath:       `<a href="/year2026.html">2026年统计数据</a>`,
		"/year2026.html":     `<a href="/credit.html">金融机构信贷收支统计 Sources and Uses</a>`,
		"/credit.html":       `<table><tr><td>金融机构人民币信贷收支表<br>RMB</td><td><a href="/loans.htm">htm</a></td></tr><tr><td>存款类金融机构人民币信贷收支表</td><td><a href="/wrong.htm">htm</a></td></tr></table>`,
		"/loans.htm":         sample(t, "loans-2026"),
		financialReportsPath: `<a href="/r202609.html">2026年前三季度金融统计数据报告</a><a href="/r202608.html">2026年8月金融统计数据报告</a><a href="/old.html">2026年7月金融统计数据报告</a>`,
		"/r202609.html":      sample(t, "loan-report-202609"), "/r202608.html": sample(t, "loan-report-202608"),
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
	req := LoanRequest{"202609", "202609"}
	items, err := client.RMBLoans(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, items, 1)
	item := items[0]
	require.Equal(t, 2834357.83, *item.Balance)
	require.Equal(t, 2834400.0, *item.ReportedBalance, "不要覆盖精确余额表为报告四舍五入值")
	require.Equal(t, 4.8, *item.YoY)
	require.Equal(t, 10800.0, *item.MonthlyFlow)
	require.Equal(t, 115200.0, *item.YTDFlow)
	require.Equal(t, "difference-of-rounded-ytd-releases", item.MonthlyFlowMethod)
	require.True(t, item.MonthlyFlowApproximate)
	require.Equal(t, server.URL+"/r202608.html", item.PreviousReportSource)
	require.Equal(t, "2026-10-14", item.ReportPublishedDate)
	require.Len(t, item.Components, 7)
	for _, part := range item.Components {
		if part.Key == "households" {
			require.Equal(t, 822369.11, *part.Balance)
			require.Equal(t, -10300.0, *part.YTDFlow)
			require.Equal(t, 0.0, *part.MonthlyFlow)
		}
		if part.Key == "households-short" {
			require.Equal(t, -900.0, *part.MonthlyFlow)
		}
		if part.Key == "enterprises-long" {
			require.Equal(t, 8900.0, *part.MonthlyFlow)
		}
	}
	require.Equal(t, int32(7), calls.Load())
	cached, err := client.RMBLoans(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, item.Balance, cached[0].Balance)
	require.Equal(t, item.ReportFetchedAt, cached[0].ReportFetchedAt)
	require.Equal(t, int32(7), calls.Load())
	client.Refresh = true
	_, err = client.RMBLoans(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int32(14), calls.Load())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.RMBLoans(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
}

func TestLoanReportPeriods(t *testing.T) {
	september := reportSample(t, "202609")
	require.Equal(t, 2834400.0, *september.Balance)
	require.Equal(t, 115200.0, *september.YTD["total"], "不能选到社融的11.50万亿元")
	require.Equal(t, -11400.0, *september.YTD["households-short"])
	november := reportSample(t, "202111")
	require.Equal(t, 12700.0, *november.Monthly["total"])
	require.Nil(t, november.YTD["total"], "月份后的逗号不能导致单月值被当作累计")
	december := reportSample(t, "202112")
	require.Equal(t, 199500.0, *december.YTD["total"])
	require.Equal(t, 11300.0, *december.Monthly["total"], "年报另列的本月新增应优先保留")
	require.Equal(t, 120200.0, *december.YTD["enterprises"])
	require.Nil(t, december.Monthly["enterprises"])
	n, method, approx := monthlyLoanFlow("202112", "total", december, november)
	require.Equal(t, 11300.0, *n)
	require.Equal(t, "published-monthly", method)
	require.False(t, approx)
	n, _, _ = monthlyLoanFlow("202112", "enterprises", december, november)
	require.Nil(t, n, "没有相邻累计分项时不能作差")
	april := reportSample(t, "202204")
	require.Equal(t, 6454.0, *april.Monthly["total"])
	require.Equal(t, -2170.0, *april.Monthly["households"])
	require.Nil(t, april.Monthly["households-short"], "住房/消费/经营贷款不能替代短期/中长期分组")
	require.Equal(t, 2652.0, *april.Monthly["enterprises-long"])
	february := reportSample(t, "202602")
	require.Equal(t, 56100.0, *february.YTD["total"])
	n, _, _ = monthlyLoanFlow("202602", "total", february, loanReport{})
	require.Nil(t, n)
	jan := loanReport{Period: "202601", YTD: map[string]*float64{"total": february.YTD["total"]}}
	n, method, approx = monthlyLoanFlow("202601", "total", jan, loanReport{})
	require.Equal(t, 56100.0, *n)
	require.Equal(t, "january-published-ytd", method)
	require.False(t, approx)
	require.Equal(t, "202609", financialReportPeriod("2026年前三季度金融统计数据报告"))
	require.Equal(t, "202612", financialReportPeriod("2026年金融统计数据报告"))
	require.Empty(t, financialReportPeriod("2026年三季度小额贷款公司统计数据报告"))
}

func TestLoanBalanceAndUnknownScope(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(sample(t, "loans-2026")))
	require.NoError(t, err)
	balances, err := parseLoanBalances(doc, 2026)
	require.NoError(t, err)
	require.Equal(t, 837279.32, *balances.Values["202601"]["households"])
	require.Equal(t, 205060.9, *balances.Values["202601"]["households-short"])
	require.Equal(t, 492689.19, *balances.Values["202601"]["enterprises-short"], "相同短期贷款标签需按部门区分")
	require.Nil(t, balances.Values["202610"]["total"])
	body := `<html><body><div id="zoom"><p>月末人民币贷款余额100万亿元，同比增长5%。</p><p>未知期间人民币贷款增加2万亿元。分部门看，住户贷款增加100亿元，其中短期贷款增加20亿元；企（事）业单位贷款增加200亿元。</p></div></body></html>`
	doc, err = goquery.NewDocumentFromReader(strings.NewReader(body))
	require.NoError(t, err)
	_, err = parseLoanReport(doc, "202609")
	require.ErrorContains(t, err, "所属期间")
	body = strings.Replace(body, "未知期间", "1—9月", 1)
	doc, err = goquery.NewDocumentFromReader(strings.NewReader(body))
	require.NoError(t, err)
	report, err := parseLoanReport(doc, "202609")
	require.NoError(t, err)
	require.Equal(t, 20000.0, *report.YTD["total"])
	require.Nil(t, report.Monthly["total"], "累计日期尾部的9月不能误作单月发布值")
	_, err = NewClient("").RMBLoans(context.Background(), LoanRequest{"202609", "202601"})
	require.ErrorContains(t, err, "不能晚于")
}

func TestLoanReportPagination(t *testing.T) {
	report := sample(t, "loan-report-202609")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case financialReportsPath:
			fmt.Fprint(w, `<a href="/" tagname="/page2.html">下一页</a>`)
		case "/page2.html":
			fmt.Fprint(w, `<a href="/report.html">2026年前三季度金融统计数据报告</a>`)
		case "/report.html":
			fmt.Fprint(w, report)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := NewClient("")
	client.origin = server.URL
	reports, err := client.loanReports(context.Background(), LoanRequest{"202609", "202609"})
	require.NoError(t, err)
	require.Len(t, reports, 1)
	require.Equal(t, 115200.0, *reports["202609"].YTD["total"])
}
