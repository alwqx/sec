package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const usTreasuryHost = "https://home.treasury.gov"

const usTreasuryFeed = `<feed xmlns="http://www.w3.org/2005/Atom"><entry><content><m:properties xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata" xmlns:d="http://schemas.microsoft.com/ado/2007/08/dataservices"><d:NEW_DATE>2026-01-02T00:00:00</d:NEW_DATE><d:BC_1MONTH m:null="true"/><d:BC_3MONTH>3.65</d:BC_3MONTH><d:BC_6MONTH>3.60</d:BC_6MONTH><d:BC_1YEAR>3.50</d:BC_1YEAR><d:BC_2YEAR>3.45</d:BC_2YEAR><d:BC_3YEAR>3.55</d:BC_3YEAR><d:BC_5YEAR>3.70</d:BC_5YEAR><d:BC_7YEAR>3.90</d:BC_7YEAR><d:BC_10YEAR>4.15</d:BC_10YEAR><d:BC_20YEAR>4.75</d:BC_20YEAR><d:BC_30YEAR>4.80</d:BC_30YEAR></m:properties></content></entry></feed>`

func mockUSTreasuryYear(year string, status int, body string) {
	gock.New(usTreasuryHost).Get("/resource-center/data-chart-center/interest-rates/pages/xml").MatchParam("field_tdr_date_value", year).Reply(status).BodyString(body)
}

func runUSTreasury(args ...string) (string, error) {
	cmd := NewMacroCLI()
	cmd.SetArgs(append([]string{"us", "treasury"}, args...))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true
	err := cmd.Execute()
	return out.String(), err
}

func TestUSTreasuryStandardOutput(t *testing.T) {
	defer gock.Off()
	mockUSTreasuryYear("2026", 200, usTreasuryFeed)
	out, err := runUSTreasury("--begin", "20260101", "--end", "20260131", "--standard-json")
	require.NoError(t, err)
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	require.Equal(t, "US", result.Region)
	require.Len(t, result.Series, 11)
	require.Len(t, result.Observations, 11)
	byID := map[string]macrodata.Observation{}
	for _, o := range result.Observations {
		byID[o.SeriesID] = o
	}
	require.Equal(t, 4.15, *byID["us.treasury.10y.yield"].Value)
	require.Equal(t, "20260102", byID["us.treasury.10y.yield"].Period)
	require.NotNil(t, byID["us.treasury.10y.yield"].Source)
	require.NotNil(t, byID["us.treasury.10y.yield"].FetchedAt)
	require.Nil(t, byID["us.treasury.1m.yield"].Value)
	require.True(t, gock.IsDone())
}

func TestUSTreasuryTableAndJSON(t *testing.T) {
	defer gock.Off()
	mockUSTreasuryYear("2026", 200, usTreasuryFeed)
	out, err := runUSTreasury("--begin", "20260101", "--end", "20260131")
	require.NoError(t, err)
	require.Contains(t, out, "2026-01-02")
	require.Contains(t, out, "—", "缺失期限不显示为0")
	require.Contains(t, out, "4.15")
	require.NotContains(t, out, "0.00")

	mockUSTreasuryYear("2026", 200, usTreasuryFeed)
	out, err = runUSTreasury("--begin", "20260101", "--end", "20260131", "--json")
	require.NoError(t, err)
	var days []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &days))
	require.Len(t, days, 1)
	require.Equal(t, 4.15, days[0]["10y"])
	value, present := days[0]["1m"]
	require.True(t, present)
	require.Nil(t, value, "未公布的期限是null，不是0")
	require.NotContains(t, days[0], "YClose")
}

func TestUSTreasuryEmptyWindow(t *testing.T) {
	defer gock.Off()
	mockUSTreasuryYear("2026", 200, usTreasuryFeed)
	out, err := runUSTreasury("--begin", "20260103", "--end", "20260104")
	require.NoError(t, err)
	require.Contains(t, out, "未返回美国国债收益率数据")
}

// 跨年查询任一年度失败时不输出部分结果。
func TestUSTreasuryFailedYearReturnsNoPartialData(t *testing.T) {
	defer gock.Off()
	mockUSTreasuryYear("2025", 503, "unavailable")
	mockUSTreasuryYear("2026", 200, usTreasuryFeed)
	out, err := runUSTreasury("--begin", "20251201", "--end", "20260131", "--standard-json")
	require.ErrorContains(t, err, "503")
	require.Empty(t, out)
}

// 上游返回异常内容时报错，不把它当成缺失或无记录。
func TestUSTreasuryRejectsBadFeed(t *testing.T) {
	entry := func(date, value string) string {
		return `<entry><content><m:properties xmlns:m="m" xmlns:d="d"><d:NEW_DATE>` + date + `</d:NEW_DATE><d:BC_10YEAR>` + value + `</d:BC_10YEAR></m:properties></content></entry>`
	}
	feed := func(entries ...string) string {
		return `<feed xmlns="http://www.w3.org/2005/Atom">` + strings.Join(entries, "") + `</feed>`
	}
	for name, test := range map[string]struct{ year, begin, end, body, message string }{
		"bad value":  {"2025", "20250101", "20250131", feed(entry("2025-01-02T00:00:00", "N/A")), "is not a number"},
		"bad date":   {"2025", "20250101", "20250131", feed(entry("not-a-date", "4.1")), "parse treasury date"},
		"duplicate":  {"2025", "20250101", "20250131", feed(entry("2025-01-02T00:00:00", "4.1"), entry("2025-01-02T00:00:00", "4.2")), "duplicate treasury date"},
		"empty past": {"2025", "20250101", "20250131", feed(), "no records for a past year"},
	} {
		t.Run(name, func(t *testing.T) {
			defer gock.Off()
			mockUSTreasuryYear(test.year, 200, test.body)
			out, err := runUSTreasury("--begin", test.begin, "--end", test.end, "--standard-json")
			require.ErrorContains(t, err, test.message)
			require.Empty(t, out)
		})
	}
}

// 当前年度可能还没有记录，例如1月初，此时不算错误。
func TestUSTreasuryCurrentYearMayBeEmpty(t *testing.T) {
	defer gock.Off()
	now := time.Now().UTC()
	mockUSTreasuryYear(now.Format("2006"), 200, `<feed xmlns="http://www.w3.org/2005/Atom"></feed>`)
	first := now.Format("2006") + "0101"
	out, err := runUSTreasury("--begin", first, "--end", first)
	require.NoError(t, err)
	require.Contains(t, out, "未返回美国国债收益率数据")
}

func TestUSTreasuryErrors(t *testing.T) {
	t.Run("flags", func(t *testing.T) {
		out, err := runUSTreasury("--begin", "20260101", "--end", "20260131", "--standard-json", "--json")
		require.ErrorContains(t, err, "none of the others can be")
		require.Empty(t, out)
	})
	t.Run("cancel", func(t *testing.T) {
		defer gock.Off()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		cmd := NewMacroCLI()
		cmd.SetArgs([]string{"us", "treasury", "--begin", "20260101", "--end", "20260131", "--standard-json"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SilenceUsage = true
		require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
		require.Empty(t, out.String())
	})
	t.Run("writer", func(t *testing.T) {
		defer gock.Off()
		mockUSTreasuryYear("2026", 200, usTreasuryFeed)
		cmd := NewMacroCLI()
		cmd.SetArgs([]string{"us", "treasury", "--begin", "20260101", "--end", "20260131", "--standard-json"})
		cmd.SetOut(failingMoneyWriter{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SilenceUsage = true
		require.ErrorContains(t, cmd.Execute(), "测试写入失败")
	})
}

func TestUSTreasuryDates(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	run := func(begin, end string) (string, string, error) {
		cmd := &cobra.Command{}
		cmd.Flags().String("begin", begin, "")
		cmd.Flags().String("end", end, "")
		return usTreasuryDates(cmd, now)
	}
	begin, end, err := run("", "")
	require.NoError(t, err)
	require.Equal(t, "20261003", begin)
	require.Equal(t, "20261101", end)
	begin, _, err = run("", "19900110")
	require.NoError(t, err)
	require.Equal(t, usTreasuryCoverageStart, begin, "默认起点不早于覆盖起点")
	for _, test := range []struct{ begin, end, message string }{
		{"", "2026-01-01", "无效截至日期"},
		{"", "19891231", "支持自19900102起"},
		{"20260230", "20260301", "无效开始日期"},
		{"20260102", "20260101", "不能晚于截至日期"},
		{"19891231", "20260101", "支持自19900102起"},
		{"20261101", "20261102", "不能晚于UTC今天"},
	} {
		_, _, err := run(test.begin, test.end)
		require.ErrorContains(t, err, test.message)
	}
}

func TestUSHousingStartsKeepsOldName(t *testing.T) {
	cmd := NewMacroCLI()
	for _, args := range [][]string{{"us", "housing-starts"}, {"us", "investment"}} {
		found, _, err := cmd.Find(args)
		require.NoError(t, err)
		require.Equal(t, "housing-starts", found.Name())
	}
	found, _, err := cmd.Find([]string{"cn", "investment"})
	require.NoError(t, err)
	require.Contains(t, found.Short, "固定资产投资", "中国命令不受影响")
}
