package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestDepositsCLI(t *testing.T) {
	defer gock.Off()
	for _, asJSON := range []bool{false, true} {
		pages := map[string]string{
			"/diaochatongjisi/116219/116319/index.html": `<a href="/y2025">2025年统计数据</a><a href="/y2026">2026年统计数据</a>`,
			"/y2025": `<a href="/c2025">金融机构信贷收支统计</a>`, "/y2026": `<a href="/c2026">金融机构信贷收支统计</a>`,
			"/c2025": `<table><tr><td>金融机构人民币信贷收支表</td><td><a href="/d2025.htm">htm</a></td></tr></table>`,
			"/c2026": `<table><tr><td>金融机构人民币信贷收支表</td><td><a href="/d2026.htm">htm</a></td></tr></table>`,
			"/diaochatongjisi/116219/116225/index.html": `<a href="/r609">2026年前三季度金融统计数据报告</a><a href="/r608">2026年8月金融统计数据报告</a><a href="/r607">2026年7月金融统计数据报告</a>`,
		}
		for path, name := range map[string]string{"/d2025.htm": "deposits-2025", "/d2026.htm": "loans-2026", "/r609": "loan-report-202609", "/r608": "loan-report-202608"} {
			raw, err := os.ReadFile("../../provider/pbc/testdata/" + name + ".html")
			require.NoError(t, err)
			pages[path] = string(raw)
		}
		for path, raw := range pages {
			gock.New("https://www.pbc.gov.cn").Get(path).Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(raw)
		}
		command := newDepositsCLI()
		args := []string{"--begin", "202609", "--end", "202610", "--no-cache"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		require.NoError(t, command.Execute())
		require.True(t, gock.IsDone())
		if asJSON {
			var result pbc.DepositResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 2)
			require.Equal(t, 1752979.27, *result.Observations[0].Balance)
			require.Equal(t, 24000.0, *result.Observations[0].MonthlyFlow)
			require.Nil(t, result.Observations[0].ReportedHouseholdYoY)
			require.Nil(t, result.Observations[1].Balance)
			require.Contains(t, out.String(), "raw_report_text")
			require.Contains(t, out.String(), "year_ago_base")
		} else {
			for _, text := range []string{"2026-09", "2026-10", "1752979.27", "计算同比", "≈24000.00", "93900.00", "—", "--json"} {
				require.Contains(t, out.String(), text)
			}
			require.NotContains(t, out.String(), "报告未提供可用月度值")
		}
	}
	root := NewMacroCLI()
	cmd, _, err := root.Find([]string{"cn", "deposits"})
	require.NoError(t, err)
	require.Equal(t, "deposits", cmd.Name())
}

func TestDepositMonthsAndArguments(t *testing.T) {
	for _, test := range []struct {
		now time.Time
		end string
	}{
		{time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC), "202512"},
		{time.Date(2025, 12, 31, 16, 0, 0, 0, time.UTC), "202512"},
		{time.Date(2025, 12, 31, 15, 59, 59, 0, time.UTC), "202511"},
	} {
		_, end, err := depositMonths(newDepositsCLI(), test.now)
		require.NoError(t, err)
		require.Equal(t, test.end, end)
	}
	cmd := newDepositsCLI()
	require.NoError(t, cmd.Flags().Set("end", "202101"))
	begin, end, err := depositMonths(cmd, time.Now())
	require.NoError(t, err)
	require.Equal(t, "202101", begin)
	require.Equal(t, begin, end)
	for _, args := range [][]string{{"extra"}, {"--end", "bad"}, {"--begin", "202012", "--end", "202101"}, {"--begin", "202609", "--end", "202608"}, {"--end", "999901"}} {
		cmd := newDepositsCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
