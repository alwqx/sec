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

func TestUSRealPCECLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/real-pce.csv")
	require.NoError(t, err)
	for _, test := range []struct {
		component, label, id string
		level                float64
	}{{"total", "总量", "PCEC96", 16955.3}, {"goods", "商品", "DGDSRX1", 5803.7}, {"durable-goods", "耐用品", "PCEDGC96", 2219.9}, {"nondurable-goods", "非耐用品", "PCENDC96", 3598.8}, {"services", "服务", "PCESC96", 11177.8}} {
		for _, asJSON := range []bool{false, true} {
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "PCEC96,DGDSRX1,PCEDGC96,PCENDC96,PCESC96").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			args := []string{"us", "real-pce", "--begin", "202608", "--end", "202609", "--no-cache"}
			if test.component != "total" {
				args = append(args, "--component", test.component)
			}
			if asJSON {
				args = append(args, "--json")
			}
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			if asJSON {
				var got fred.RealPCEResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &got))
				require.Equal(t, test.component, got.Component)
				require.Equal(t, test.id, got.SeriesID)
				require.Len(t, got.Observations, 2)
				require.Equal(t, test.level, *got.Observations[0].Level)
				require.Nil(t, got.Observations[1].Level)
				require.True(t, got.LevelAnnualized)
				require.False(t, got.GrowthAnnualized)
				require.False(t, got.ComponentsAdditive)
			} else {
				require.Contains(t, out.String(), "2026-08")
				require.Contains(t, out.String(), test.label)
				require.Contains(t, out.String(), "2017年链式美元")
				require.Contains(t, out.String(), "季调折年率")
				require.Contains(t, out.String(), "增速未年化")
				require.Contains(t, out.String(), "当月实际消费水平缺失")
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSRealPCEDatesAndArgs(t *testing.T) {
	cmd := newUSRealPCECLI()
	require.NoError(t, cmd.Flags().Set("end", "200701"))
	begin, end, err := queryUSMonths(cmd, time.Now(), "200701")
	require.NoError(t, err)
	require.Equal(t, "200701", begin)
	require.Equal(t, "200701", end)
	for _, args := range [][]string{{"--component", "bad"}, {"--begin", "200612"}, {"--begin", "202513"}, {"--begin", "202502", "--end", "202501"}, {"extra"}} {
		root := NewMacroCLI()
		root.SetArgs(append([]string{"us", "real-pce"}, args...))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Error(t, root.Execute())
	}
}
