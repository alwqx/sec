package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSMoneyCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/money.csv")
	require.NoError(t, err)
	for _, asJSON := range []bool{false, true} {
		gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "M2SL").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
		cmd := newUSMoneyCLI()
		args := []string{"--begin", "202609", "--end", "202610", "--no-cache", "--refresh"}
		if asJSON {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		if asJSON {
			var result fred.MoneyResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, "M2SL", result.SeriesID)
			require.Equal(t, "monthly-average", result.PeriodBasis)
			require.Len(t, result.Observations, 2)
			require.Equal(t, 23413.9, *result.Observations[0].Level)
			require.Nil(t, result.Observations[1].Level)
			require.Nil(t, result.PublishedAt)
			require.False(t, result.LevelAnnualized)
			require.False(t, result.GrowthAnnualized)
		} else {
			for _, text := range []string{"2026-09", "23413.9", "M2SL", "月平均名义余额", "水平不折年", "增速未年化", "当月M2余额缺失"} {
				require.Contains(t, out.String(), text)
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSMoneyDatesAndArguments(t *testing.T) {
	cmd := newUSMoneyCLI()
	require.Equal(t, []string{"m2"}, cmd.Aliases)
	require.NoError(t, cmd.Flags().Set("end", "195901"))
	begin, end, err := queryUSMonths(cmd, time.Now(), "195901")
	require.NoError(t, err)
	require.Equal(t, "195901", begin)
	require.Equal(t, "195901", end)
	for _, args := range [][]string{{"--begin", "195812"}, {"--begin", "202513"}, {"--begin", "202502", "--end", "202501"}, {"--end", "299901"}, {"extra"}} {
		cmd := newUSMoneyCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd = newUSMoneyCLI()
	cmd.SetArgs([]string{"--no-cache"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorIs(t, cmd.ExecuteContext(ctx), context.Canceled)
}
