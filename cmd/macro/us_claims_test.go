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

func TestUSClaimsCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/jobless-claims.csv")
	require.NoError(t, err)
	for _, indicator := range []string{"initial-claims", "continued-claims"} {
		for _, output := range []string{"table", "json", "standard"} {
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "ICSA,CCSA").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			args := []string{"us", indicator, "--begin", "20261001", "--end", "20261024", "--no-cache"}
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
				var result macrodata.Dataset
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				require.Len(t, result.Observations, 12)
				require.Equal(t, "weekly", result.Series[0].Frequency)
				require.Equal(t, "20261003", result.Observations[0].Period)
				require.Len(t, result.Observations[2].Inputs, 4)
				require.Equal(t, "20260926", result.Observations[1].Base.Period)
				require.Nil(t, result.Observations[9].Value)
				require.Nil(t, result.Observations[0].PublishedAt)
				if indicator == "initial-claims" {
					require.Equal(t, "claims", result.Series[0].Unit)
					require.Equal(t, 194000.0, *result.Observations[6].Value)
					require.Equal(t, 197750.0, *result.Observations[8].Value)
				} else {
					require.Equal(t, "persons", result.Series[0].Unit)
					require.Nil(t, result.Observations[6].Value, "续请所属周不前移")
				}
			} else if output == "json" {
				var got fred.ClaimsResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &got))
				require.Len(t, got.Observations, 4)
				require.True(t, got.SeasonallyAdjusted)
				require.Nil(t, got.Observations[3].Count)
				if indicator == "initial-claims" {
					require.Equal(t, "ICSA", got.SeriesID)
					require.Equal(t, 194000.0, *got.Observations[2].Count)
				} else {
					require.Equal(t, "CCSA", got.SeriesID)
					require.Nil(t, got.Observations[2].Count)
				}
			} else {
				require.Contains(t, out.String(), "2026-10-17")
				require.Contains(t, out.String(), "四周均值")
				require.Contains(t, out.String(), "尚未发布")
				if indicator == "initial-claims" {
					require.Contains(t, out.String(), "194000")
					require.Contains(t, out.String(), "197750.00")
				} else {
					require.Contains(t, out.String(), "1728000")
					require.Contains(t, out.String(), "1711000.00")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSClaimsDatesAndArgs(t *testing.T) {
	command := newUSClaimsCLI("initial-claims")
	begin, end, err := queryUSWeeks(command, time.Date(2026, 1, 4, 0, 1, 0, 0, time.FixedZone("UTC+14", 14*60*60)))
	require.NoError(t, err)
	require.Equal(t, "20260103", end)
	require.Equal(t, "20251018", begin)
	require.NoError(t, command.Flags().Set("end", "19670107"))
	begin, end, err = queryUSWeeks(command, time.Now())
	require.NoError(t, err)
	require.Equal(t, "19670107", begin)
	require.Equal(t, "19670107", end)
	for _, indicator := range []string{"initial-claims", "continued-claims"} {
		for _, args := range [][]string{{"--begin", "19670106"}, {"--begin", "20260230"}, {"--begin", "20260110", "--end", "20260103"}, {"--end", "2026-01-03"}, {"--end", "29990101"}, {"extra"}} {
			root := NewMacroCLI()
			root.SetArgs(append([]string{"us", indicator}, args...))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			require.Error(t, root.Execute())
		}
	}
}
