package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/federalreserve"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSTargetRateCLI(t *testing.T) {
	defer gock.Off()
	table, err := os.ReadFile("../../provider/federalreserve/testdata/openmarket.html")
	require.NoError(t, err)
	calendar, err := os.ReadFile("../../provider/federalreserve/testdata/calendar.html")
	require.NoError(t, err)
	statement, err := os.ReadFile("../../provider/federalreserve/testdata/statement-20200315.html")
	require.NoError(t, err)
	for _, asJSON := range []bool{false, true} {
		gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/openmarket.htm").Reply(200).BodyString(string(table))
		gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomccalendars.htm").Reply(200).BodyString(string(calendar))
		gock.New("https://www.federalreserve.gov").Get("/newsevents/pressreleases/monetary20200315a.htm").Reply(200).BodyString(string(statement))
		command := newUSTargetRateCLI()
		args := []string{"--begin", "20200320", "--end", "20200322", "--no-cache", "--refresh"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		require.NoError(t, command.Execute())
		if asJSON {
			var result federalreserve.TargetRateResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, "target-rate", result.Indicator)
			require.Empty(t, result.Events)
			require.Equal(t, "2020-03-16", result.Current.EffectiveDate)
			require.Equal(t, "2020-03-15", *result.Current.AnnouncedDate)
			require.Equal(t, .25, result.Current.UpperPercent)
		} else {
			require.Contains(t, out.String(), "截至 2020-03-22")
			require.Contains(t, out.String(), "0.00%–0.25%")
			require.Contains(t, out.String(), "2020-03-16")
			require.Contains(t, out.String(), "2020-03-15")
			require.Contains(t, out.String(), "17:00 EDT")
			require.Contains(t, out.String(), "区间前基准")
			require.Contains(t, out.String(), "没有目标区间调整事件")
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSTargetRateDatesAndArguments(t *testing.T) {
	command := newUSTargetRateCLI()
	begin, end, err := queryTargetRateDates(command, time.Date(2026, 1, 1, 0, 1, 0, 0, time.FixedZone("UTC+14", 14*3600)))
	require.NoError(t, err)
	require.Equal(t, "20251231", end)
	require.Equal(t, "20251202", begin)
	require.NoError(t, command.Flags().Set("end", "20081216"))
	begin, end, err = queryTargetRateDates(command, time.Now())
	require.NoError(t, err)
	require.Equal(t, "20081216", begin)
	require.Equal(t, "20081216", end)
	for _, args := range [][]string{{"--begin", "20081215"}, {"--begin", "20250230"}, {"--end", "2025-01-01"}, {"--end", "29990101"}, {"--begin", "20250110", "--end", "20250101"}, {"extra"}} {
		command := newUSTargetRateCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command = newUSTargetRateCLI()
	command.SetArgs([]string{"--no-cache"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	require.ErrorIs(t, command.ExecuteContext(ctx), context.Canceled)
	var out bytes.Buffer
	require.NoError(t, printUSTargetRate(&out, &federalreserve.TargetRateResult{}))
	require.Contains(t, out.String(), "没有可用")
	require.Error(t, printUSTargetRate(effrFailWriter{}, &federalreserve.TargetRateResult{}))
}

func TestUSTargetRateUnavailableReleaseOutput(t *testing.T) {
	var out bytes.Buffer
	event := &federalreserve.TargetRateEvent{EffectiveDate: "2008-12-16", LowerPercent: 0, UpperPercent: .25,
		ReleaseStatus: "unavailable", ReleaseNote: "公布信息不可用：官方日历未找到可核验声明"}
	result := &federalreserve.TargetRateResult{End: "20081216", Current: event, Events: []*federalreserve.TargetRateEvent{event}}
	require.NoError(t, printUSTargetRate(&out, result))
	require.Contains(t, out.String(), "截至 2008-12-16")
	require.Contains(t, out.String(), "调整事件")
	require.Contains(t, out.String(), "未取得")
	require.Contains(t, out.String(), "公布信息不可用")
	require.Error(t, printUSTargetRate(effrFailWriter{}, result))
}
