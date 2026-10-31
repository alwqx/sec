package macro

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const (
	pbcHost           = "https://www.pbc.gov.cn"
	pbcStatisticsPath = "/diaochatongjisi/116219/116319/index.html"
	pbcReportsPath    = "/diaochatongjisi/116219/116225/index.html"
)

func pbcFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../provider/pbc/testdata/" + name + ".html")
	require.NoError(t, err)
	return string(raw)
}

func mockPBCPages(pages map[string]string) {
	for path, body := range pages {
		gock.New(pbcHost).Get(path).Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(body)
	}
}

func mockFinancing(t *testing.T) {
	t.Helper()
	mockPBCPages(map[string]string{
		pbcStatisticsPath: `<a href="/y2026">2026年统计数据</a>`,
		"/y2026":          `<a href="/s2026">社会融资规模</a>`,
		"/s2026":          `<table><tr><td>社会融资规模增量统计表</td><td><a href="/f2026.htm">htm</a></td></tr><tr><td>社会融资规模存量统计表</td><td><a href="/k2026.htm">htm</a></td></tr></table>`,
		"/f2026.htm":      pbcFixture(t, "flow-2026"),
		"/k2026.htm":      pbcFixture(t, "stock-2026"),
	})
}

func mockLoans(t *testing.T) {
	t.Helper()
	mockPBCPages(map[string]string{
		pbcReportsPath:    `<a href="/r609">2026年前三季度金融统计数据报告</a><a href="/r608">2026年8月金融统计数据报告</a><a href="/r607">2026年7月金融统计数据报告</a>`,
		"/r609":           pbcFixture(t, "loan-report-202609"),
		"/r608":           pbcFixture(t, "loan-report-202608"),
		pbcStatisticsPath: `<a href="/y2026">2026年统计数据</a>`,
		"/y2026":          `<a href="/c2026">金融机构信贷收支统计</a>`,
		"/c2026":          `<table><tr><td>金融机构人民币信贷收支表</td><td><a href="/l2026.htm">htm</a></td></tr></table>`,
		"/l2026.htm":      pbcFixture(t, "loans-2026"),
	})
}

func mockDeposits(t *testing.T) {
	t.Helper()
	mockPBCPages(map[string]string{
		pbcStatisticsPath: `<a href="/y2025">2025年统计数据</a><a href="/y2026">2026年统计数据</a>`,
		"/y2025":          `<a href="/c2025">金融机构信贷收支统计</a>`,
		"/y2026":          `<a href="/c2026">金融机构信贷收支统计</a>`,
		"/c2025":          `<table><tr><td>金融机构人民币信贷收支表</td><td><a href="/d2025.htm">htm</a></td></tr></table>`,
		"/c2026":          `<table><tr><td>金融机构人民币信贷收支表</td><td><a href="/d2026.htm">htm</a></td></tr></table>`,
		pbcReportsPath:    `<a href="/r609">2026年前三季度金融统计数据报告</a><a href="/r608">2026年8月金融统计数据报告</a><a href="/r607">2026年7月金融统计数据报告</a>`,
		"/d2025.htm":      pbcFixture(t, "deposits-2025"),
		"/d2026.htm":      pbcFixture(t, "loans-2026"),
		"/r609":           pbcFixture(t, "loan-report-202609"),
		"/r608":           pbcFixture(t, "loan-report-202608"),
	})
}

func runPBCStandard(t *testing.T, args ...string) (macrodata.Dataset, map[string]macrodata.Observation) {
	t.Helper()
	result := runNBSStandard(t, args...)
	byID := map[string]macrodata.Observation{}
	for _, o := range result.Observations {
		byID[o.SeriesID+"/"+o.Period] = o
	}
	return result, byID
}

func TestFinancingStandardOutput(t *testing.T) {
	defer gock.Off()
	mockFinancing(t)
	_, byID := runPBCStandard(t, "afre", "--begin", "202609", "--end", "202609")
	require.Equal(t, 4681700.0, *byID["cn.financing.total.since-202301.level/202609"].Value)
	require.Equal(t, 7.1, *byID["cn.financing.total.since-202301.yoy_percent/202609"].Value)
	require.Equal(t, 35476.0, *byID["cn.financing.total.since-202301.monthly_flow/202609"].Value)
	require.Equal(t, 274589.0, *byID["cn.financing.total.since-202301.ytd_flow/202609"].Value)
	require.Equal(t, "202601", byID["cn.financing.total.since-202301.ytd_flow/202609"].PeriodBegin)
}

func TestLoansStandardOutput(t *testing.T) {
	defer gock.Off()
	mockLoans(t)
	result, byID := runPBCStandard(t, "loans", "--begin", "202609", "--end", "202609")
	require.Equal(t, 2834357.83, *byID["cn.loans.total.since-202301.level/202609"].Value)
	require.Equal(t, 2834400.0, *byID["cn.loans.total.since-202301.reported_level/202609"].Value)
	require.Equal(t, 4.8, *byID["cn.loans.total.since-202301.yoy_percent/202609"].Value)
	require.Equal(t, 10800.0, *byID["cn.loans.total.since-202301.monthly_flow_derived/202609"].Value)
	require.Equal(t, "2026-10-14", *byID["cn.loans.total.since-202301.ytd_flow/202609"].PublishedDate)
	indicators := map[string]bool{}
	for _, s := range result.Series {
		indicators[s.Indicator] = true
	}
	require.Len(t, indicators, 8, "总额和七个分项各自成指标")
}

func TestDepositsStandardOutput(t *testing.T) {
	defer gock.Off()
	mockDeposits(t)
	_, byID := runPBCStandard(t, "deposits", "--begin", "202609", "--end", "202610")
	require.Equal(t, 1752979.27, *byID["cn.deposits.households.since-202301.level/202609"].Value)
	require.NotNil(t, byID["cn.deposits.households.since-202301.level/202609"].RawValue)
	require.Equal(t, "202509", byID["cn.deposits.households.since-202301.yoy_percent/202609"].Base.Period)
	require.Equal(t, 24000.0, *byID["cn.deposits.households.since-202301.monthly_flow_derived/202609"].Value)
	require.Equal(t, "source_value_missing", *byID["cn.deposits.households.since-202301.reported_yoy_percent/202609"].MissingReason)
	require.Nil(t, byID["cn.deposits.households.since-202301.level/202610"].Value, "未发布月份不补值")
	require.Equal(t, "current_value_missing", *byID["cn.deposits.households.since-202301.yoy_percent/202610"].MissingReason)
	require.Equal(t, "report_not_found", *byID["cn.deposits.households.since-202301.monthly_flow/202610"].MissingReason)
}

func TestPBCCreditStandardErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		mock func(*testing.T)
		path string
	}{
		{[]string{"financing", "--begin", "202609", "--end", "202609"}, mockFinancing, pbcStatisticsPath},
		{[]string{"loans", "--begin", "202609", "--end", "202609"}, mockLoans, pbcReportsPath},
		{[]string{"deposits", "--begin", "202609", "--end", "202610"}, mockDeposits, pbcReportsPath},
	} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			t.Run(test.args[0]+"/"+mode, func(t *testing.T) {
				defer gock.Off()
				args := append(append([]string{"cn"}, test.args...), "--standard-json", "--no-cache")
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "http":
					gock.New(pbcHost).Get(test.path).Reply(503)
				case "writer":
					test.mock(t)
				}
				cmd := NewMacroCLI()
				cmd.SetArgs(args)
				var out bytes.Buffer
				cmd.SetOut(&out)
				if mode == "writer" {
					cmd.SetOut(failingMoneyWriter{})
				}
				cmd.SetErr(&bytes.Buffer{})
				cmd.SilenceUsage = true
				err := cmd.ExecuteContext(ctx)
				require.Error(t, err)
				switch mode {
				case "flags":
					require.ErrorContains(t, err, "none of the others can be")
				case "cancel":
					require.ErrorIs(t, err, context.Canceled)
				case "http":
					require.ErrorContains(t, err, "503")
				case "writer":
					require.ErrorContains(t, err, "测试写入失败")
				}
				if mode != "writer" {
					require.Empty(t, out.String())
				}
			})
		}
	}
}
