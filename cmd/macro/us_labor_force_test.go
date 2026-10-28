package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSLaborForceCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/bls/testdata/labor-force-2024-2026.json")
	require.NoError(t, err)
	for _, indicator := range []string{"unemployment", "participation"} {
		for _, asJSON := range []bool{false, true} {
			gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").JSON(map[string]any{"seriesid": []string{"LNS14000000", "LNS11300000"}, "startyear": "2024", "endyear": "2026"}).Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			args := []string{"us", indicator, "--begin", "202501", "--end", "202609", "--no-cache"}
			if asJSON {
				args = append(args, "--json")
			}
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			if asJSON {
				var got bls.LaborForceResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &got))
				require.Equal(t, indicator, got.Indicator)
				require.Len(t, got.Observations, 21)
				require.Equal(t, "百分点", got.ChangeUnit)
				require.Nil(t, got.Observations[9].Rate)
				require.Equal(t, "12", got.Observations[12].Footnotes[0].Code)
				if indicator == "unemployment" {
					require.Equal(t, 4.2, *got.Observations[20].Rate)
				} else {
					require.Equal(t, 61.8, *got.Observations[20].Rate)
				}
			} else {
				require.Contains(t, out.String(), "2026-09")
				require.Contains(t, out.String(), "百分点")
				require.Contains(t, out.String(), "当月:12")
				require.Contains(t, out.String(), "当月比率缺失")
				require.Contains(t, out.String(), "CPS住户调查")
				if indicator == "unemployment" {
					require.Contains(t, out.String(), "失业率")
				} else {
					require.Contains(t, out.String(), "劳动参与率")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSLaborForceDatesAndArgs(t *testing.T) {
	cmd := newUSLaborForceCLI("unemployment")
	require.NoError(t, cmd.Flags().Set("end", "194801"))
	begin, end, err := queryUSMonths(cmd, time.Now(), "194801")
	require.NoError(t, err)
	require.Equal(t, "194801", begin)
	require.Equal(t, "194801", end)
	for _, indicator := range []string{"unemployment", "participation"} {
		for _, args := range [][]string{{"--begin", "194712"}, {"--begin", "202513"}, {"--begin", "202502", "--end", "202501"}, {"extra"}} {
			root := NewMacroCLI()
			root.SetArgs(append([]string{"us", indicator}, args...))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			require.Error(t, root.Execute())
		}
	}
}
