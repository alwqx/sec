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

func TestUSCPICLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/bls/testdata/cpi-2024-2026.json")
	require.NoError(t, err)
	for _, indicator := range []string{"cpi", "core-cpi"} {
		for _, output := range []string{"table", "json", "standard"} {
			body := map[string]any{"seriesid": []string{"CUUR0000SA0", "CUSR0000SA0", "CUUR0000SA0L1E", "CUSR0000SA0L1E"}, "startyear": "2024", "endyear": "2026"}
			gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").JSON(body).Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			args := []string{"us", indicator, "--begin", "202510", "--end", "202609", "--no-cache"}
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
				require.Len(t, result.Observations, 48)
				require.Equal(t, "1982-1984=100", result.Series[0].IndexBase)
				require.Equal(t, "not-seasonally-adjusted", result.Series[0].SeasonalAdjustment)
				require.Equal(t, "seasonally-adjusted", result.Series[1].SeasonalAdjustment)
				require.Equal(t, "not-seasonally-adjusted", result.Series[2].SeasonalAdjustment)
				require.Equal(t, "seasonally-adjusted", result.Series[3].SeasonalAdjustment)
				require.Nil(t, result.Observations[0].Value)
				require.Equal(t, "current_value_missing", *result.Observations[2].MissingReason)
				var original bls.CPIResult
				require.NoError(t, json.Unmarshal(result.Original, &original))
				require.Equal(t, "X", original.Observations[0].NSA.Footnotes[0].Code)
				require.Equal(t, indicator, result.Series[0].Indicator)
			} else if output == "json" {
				var result bls.CPIResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				require.Equal(t, "US", result.Region)
				require.Equal(t, indicator, result.Indicator)
				require.Len(t, result.Observations, 12)
				require.Nil(t, result.Observations[0].NSA.Index)
				require.Equal(t, "X", result.Observations[0].NSA.Footnotes[0].Code)
				require.Nil(t, result.PublishedAt)
			} else {
				require.Contains(t, out.String(), "2026-09")
				require.Contains(t, out.String(), "环比基期缺失")
				require.Contains(t, out.String(), "BLS")
				if indicator == "cpi" {
					require.Contains(t, out.String(), "336.940")
				} else {
					require.Contains(t, out.String(), "339.287")
					require.Contains(t, out.String(), "剔除食品和能源")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSCPIDefaultsAndInvalidArgs(t *testing.T) {
	cmd := newUSCPICLI("cpi")
	begin, end, err := queryUSMonths(cmd, time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC), "191301")
	require.NoError(t, err)
	require.Equal(t, "202501", begin)
	require.Equal(t, "202512", end)
	require.NoError(t, cmd.Flags().Set("end", "191301"))
	begin, end, err = queryUSMonths(cmd, time.Now(), "191301")
	require.NoError(t, err)
	require.Equal(t, "191301", begin)
	require.Equal(t, "191301", end)
	for _, args := range [][]string{{"--begin", "202513"}, {"--begin", "202502", "--end", "202501"}, {"--end", "299901"}, {"--begin", "191212"}, {"extra"}} {
		root := NewMacroCLI()
		root.SetArgs(append([]string{"us", "cpi"}, args...))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Error(t, root.Execute())
	}
}
