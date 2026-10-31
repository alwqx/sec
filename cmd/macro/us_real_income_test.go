package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSRealIncomeCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/real-income.csv")
	require.NoError(t, err)
	for _, output := range []string{"table", "json", "standard"} {
		gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "DSPIC96").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
		cmd := newUSRealIncomeCLI()
		args := []string{"--begin", "202608", "--end", "202609", "--no-cache"}
		if output == "json" {
			args = append(args, "--json")
		}
		if output == "standard" {
			args = append(args, "--standard-json")
		}
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		if output == "standard" {
			var got macrodata.Dataset
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			require.Len(t, got.Observations, 6)
			require.Equal(t, "us.real-income.level", got.Series[0].ID)
			require.Equal(t, "DSPIC96", got.Observations[0].SourceSeriesID)
			require.Equal(t, 18410.5, *got.Observations[0].Value)
			require.True(t, got.Series[0].Annualized)
			require.False(t, got.Series[1].Annualized)
			require.Equal(t, 2017, *got.Series[0].ReferenceYear)
			require.Contains(t, got.Series[0].Coverage, "非人均")
			require.Nil(t, got.Observations[3].Value)
			require.Equal(t, "current_value_missing", *got.Observations[4].MissingReason)
			var original fred.RealIncomeResult
			require.NoError(t, json.Unmarshal(got.Original, &original))
			require.Len(t, original.Observations, 2)
		} else if output == "json" {
			var got fred.RealIncomeResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			require.Equal(t, "DSPIC96", got.SeriesID)
			require.Len(t, got.Observations, 2)
			require.Equal(t, 18410.5, *got.Observations[0].Level)
			require.Nil(t, got.Observations[1].Level)
			require.True(t, got.LevelAnnualized)
			require.False(t, got.GrowthAnnualized)
			require.Nil(t, got.PublishedAt)
		} else {
			for _, text := range []string{"2026-08", "18410.5", "实际可支配", "2017年链式美元", "季调折年率", "增速未年化", "当月实际可支配收入水平缺失"} {
				require.Contains(t, out.String(), text)
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSRealIncomeDatesAndArgs(t *testing.T) {
	cmd := newUSRealIncomeCLI()
	require.NoError(t, cmd.Flags().Set("end", "195901"))
	begin, end, err := queryUSMonths(cmd, time.Now(), "195901")
	require.NoError(t, err)
	require.Equal(t, "195901", begin)
	require.Equal(t, "195901", end)
	cmd = newUSRealIncomeCLI()
	begin, end, err = queryUSMonths(cmd, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), "195901")
	require.NoError(t, err)
	require.Equal(t, "202510", begin)
	require.Equal(t, "202609", end)
	for _, args := range [][]string{{"--begin", "195812"}, {"--begin", "202513"}, {"--begin", "202502", "--end", "202501"}, {"--end", "299901"}, {"extra"}} {
		cmd := newUSRealIncomeCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
