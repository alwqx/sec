package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSEarningsHoursCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/bls/testdata/earnings-hours-2024-2026.json")
	require.NoError(t, err)
	for _, indicator := range []string{"earnings", "weekly-hours"} {
		for _, output := range []string{"table", "json", "standard"} {
			gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").JSON(map[string]any{"seriesid": []string{"CES0500000003", "CES0500000002"}, "startyear": "2024", "endyear": "2026"}).Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			args := []string{"us", indicator, "--begin", "202501", "--end", "202609", "--no-cache"}
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
				require.Len(t, got.Observations, 84)
				require.Equal(t, "CES establishment survey", got.Series[0].Survey)
				require.True(t, *got.Observations[80].Preliminary)
				require.Nil(t, got.Observations[81].Preliminary)
				require.Equal(t, "monthly_change", got.Series[1].Measure)
				require.Equal(t, "percent", got.Series[2].Unit)
				if indicator == "earnings" {
					require.Equal(t, 37.81, *got.Observations[80].Value)
					require.Equal(t, "USD_per_hour", got.Series[0].Unit)
					require.Equal(t, "nominal", got.Series[0].PriceBasis)
				} else {
					require.Equal(t, 34.4, *got.Observations[80].Value)
					require.Equal(t, "hours_per_week", got.Series[0].Unit)
					require.Equal(t, "not_applicable", got.Series[0].PriceBasis)
				}
				var original bls.EarningsHoursResult
				require.NoError(t, json.Unmarshal(got.Original, &original))
				require.True(t, original.Observations[20].Preliminary)
			} else if output == "json" {
				var got bls.EarningsHoursResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &got))
				require.Equal(t, indicator, got.Indicator)
				require.Len(t, got.Observations, 21)
				require.True(t, got.Observations[20].Preliminary)
				if indicator == "earnings" {
					require.Equal(t, 37.81, *got.Observations[20].Value)
				} else {
					require.Equal(t, 34.4, *got.Observations[20].Value)
				}
			} else {
				require.Contains(t, out.String(), "2026-09")
				require.Contains(t, out.String(), "初步（P）")
				require.Contains(t, out.String(), "私人非农部门全部雇员")
				if indicator == "earnings" {
					require.Contains(t, out.String(), "37.81")
					require.Contains(t, out.String(), "美元/小时")
				} else {
					require.Contains(t, out.String(), "34.4")
					require.Contains(t, out.String(), "小时/周")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSEarningsHoursDatesAndArgs(t *testing.T) {
	cmd := newUSEarningsHoursCLI("earnings")
	require.NoError(t, cmd.Flags().Set("end", "200603"))
	begin, end, err := queryUSMonths(cmd, time.Now(), "200603")
	require.NoError(t, err)
	require.Equal(t, "200603", begin)
	require.Equal(t, "200603", end)
	for _, indicator := range []string{"earnings", "weekly-hours"} {
		for _, args := range [][]string{{"--begin", "200602"}, {"--begin", "202513"}, {"--begin", "202502", "--end", "202501"}, {"extra"}} {
			root := NewMacroCLI()
			root.SetArgs(append([]string{"us", indicator}, args...))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			require.Error(t, root.Execute())
		}
	}
}
