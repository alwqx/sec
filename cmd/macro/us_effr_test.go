package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/nyfed"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSEFFRCLI(t *testing.T) {
	defer gock.Off()
	body, err := os.ReadFile("../../provider/nyfed/testdata/effr-holidays.json")
	require.NoError(t, err)
	for _, asJSON := range []bool{false, true} {
		gock.New("https://markets.newyorkfed.org").Get("/api/rates/unsecured/effr/search.json").MatchParam("startDate", "2024-12-02").MatchParam("endDate", "2025-01-06").Reply(200).BodyString(string(body))
		command := newUSEFFRCLI()
		args := []string{"--begin", "20250102", "--end", "20250106", "--no-cache", "--refresh"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		require.NoError(t, command.Execute())
		if asJSON {
			var result nyfed.EFFRResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 3)
			require.Equal(t, 4.33, result.Observations[0].RatePercent)
			require.Equal(t, "2024-12-31", result.Observations[0].PreviousDate)
			require.Nil(t, result.Observations[0].PublishedAt)
			require.Contains(t, out.String(), `"published_at": null`)
		} else {
			require.Contains(t, out.String(), "2025-01-02")
			require.Contains(t, out.String(), "4.33")
			require.Contains(t, out.String(), "基点")
			require.Contains(t, out.String(), "较前次观测")
			require.Contains(t, out.String(), "实际公布时间")
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSEFFRDatesAndArgs(t *testing.T) {
	command := newUSEFFRCLI()
	begin, end, err := queryEFFRDates(command, time.Date(2026, 1, 1, 0, 1, 0, 0, time.FixedZone("UTC+14", 14*3600)))
	require.NoError(t, err)
	require.Equal(t, "20251231", end)
	require.Equal(t, "20251202", begin)
	require.NoError(t, command.Flags().Set("end", "20000703"))
	begin, end, err = queryEFFRDates(command, time.Now())
	require.NoError(t, err)
	require.Equal(t, "20000703", begin)
	require.Equal(t, "20000703", end)
	for _, args := range [][]string{{"--begin", "20000702"}, {"--begin", "20250230"}, {"--end", "2025-01-01"}, {"--end", "29990101"}, {"--begin", "20250110", "--end", "20250101"}, {"extra"}} {
		command := newUSEFFRCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command = newUSEFFRCLI()
	command.SetArgs([]string{"--no-cache"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	require.ErrorIs(t, command.ExecuteContext(ctx), context.Canceled)
	var out bytes.Buffer
	require.NoError(t, printUSEFFR(&out, &nyfed.EFFRResult{}))
	require.Contains(t, out.String(), "没有已公布")
	require.Error(t, printUSEFFR(effrFailWriter{}, &nyfed.EFFRResult{}))
}

type effrFailWriter struct{}

func (effrFailWriter) Write(p []byte) (int, error) { return 0, errors.New("写入失败") }
