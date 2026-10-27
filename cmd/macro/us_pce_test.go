package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSPCECLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/pce-prices.csv")
	require.NoError(t, err)
	for _, indicator := range []string{"pce", "core-pce"} {
		for _, asJSON := range []bool{false, true} {
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "PCEPI,PCEPILFE").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			args := []string{"us", indicator, "--begin", "202608", "--end", "202609", "--no-cache"}
			if asJSON {
				args = append(args, "--json")
			}
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			if asJSON {
				var result fred.PCEResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				require.Len(t, result.Observations, 2)
				require.True(t, result.SeasonallyAdjusted)
				require.Nil(t, result.Observations[1].Index)
				if indicator == "pce" {
					require.Equal(t, "PCEPI", result.SeriesID)
				} else {
					require.Equal(t, "PCEPILFE", result.SeriesID)
				}
			} else {
				require.Contains(t, out.String(), "2026-08")
				require.Contains(t, out.String(), "2026-09")
				require.Contains(t, out.String(), "当月指数缺失")
				if indicator == "pce" {
					require.Contains(t, out.String(), "131.579")
				} else {
					require.Contains(t, out.String(), "130.455")
					require.Contains(t, out.String(), "剔除食品和能源")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSPCEDatesAndArgs(t *testing.T) {
	cmd := newUSPCECLI("pce")
	require.NoError(t, cmd.Flags().Set("end", "195901"))
	begin, end, err := queryUSMonths(cmd, time.Now(), "195901")
	require.NoError(t, err)
	require.Equal(t, "195901", begin)
	require.Equal(t, "195901", end)
	for _, args := range [][]string{{"--begin", "195812"}, {"--begin", "202513"}, {"--begin", "202502", "--end", "202501"}, {"extra"}} {
		root := NewMacroCLI()
		root.SetArgs(append([]string{"us", "pce"}, args...))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Error(t, root.Execute())
	}
}
