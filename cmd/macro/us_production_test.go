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

func TestUSProductionCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/production.csv")
	require.NoError(t, err)
	for _, test := range []struct {
		indicator, sector, id string
		level                 float64
	}{
		{"industrial-production", "total", "INDPRO", 100.2850},
		{"industrial-production", "manufacturing", "IPMAN", 96.9553},
		{"capacity-utilization", "total", "TCU", 76.5157},
		{"capacity-utilization", "manufacturing", "MCUMFN", 76.0838},
	} {
		for _, asJSON := range []bool{false, true} {
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "INDPRO,IPMAN,TCU,MCUMFN").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			args := []string{"us", test.indicator, "--sector", test.sector, "--begin", "202402", "--end", "202402", "--no-cache"}
			if asJSON {
				args = append(args, "--json")
			}
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			if asJSON {
				var got fred.ProductionResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &got))
				require.Equal(t, test.indicator, got.Indicator)
				require.Equal(t, test.sector, got.Sector)
				require.Equal(t, test.id, got.SeriesID)
				require.Len(t, got.Observations, 1)
				require.Equal(t, test.level, *got.Observations[0].Level)
				require.Equal(t, "202401", got.Observations[0].MoMBase.Period)
				require.True(t, got.SeasonallyAdjusted)
				require.Nil(t, got.PublishedAt)
				require.Nil(t, got.RevisionID)
				if test.indicator == "industrial-production" {
					require.Equal(t, "percent", got.ChangeUnit)
					require.NotNil(t, got.Observations[0].MoM)
					require.Nil(t, got.Observations[0].MoMPercentagePoints)
				} else {
					require.Equal(t, "percentage-points", got.ChangeUnit)
					require.Nil(t, got.ReferenceYear)
					require.Nil(t, got.Observations[0].MoM)
					require.NotNil(t, got.Observations[0].MoMPercentagePoints)
				}
			} else {
				require.Contains(t, out.String(), "2024-02")
				require.Contains(t, out.String(), test.id)
				require.Contains(t, out.String(), "月度季调")
				if test.indicator == "industrial-production" {
					require.Contains(t, out.String(), "2017=100")
					require.Contains(t, out.String(), "未年化")
				} else {
					require.Contains(t, out.String(), "百分点")
					require.Contains(t, out.String(), "利用率")
				}
				if test.sector == "manufacturing" {
					require.Contains(t, out.String(), "制造业：NAICS 31–33")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSProductionDatesAndArguments(t *testing.T) {
	for _, test := range []struct{ indicator, sector, start string }{
		{"industrial-production", "total", "191901"},
		{"industrial-production", "manufacturing", "197201"},
		{"capacity-utilization", "total", "196701"},
		{"capacity-utilization", "manufacturing", "197201"},
	} {
		cmd := newUSProductionCLI(test.indicator)
		require.NoError(t, cmd.Flags().Set("end", test.start))
		req := &fred.ProductionRequest{Indicator: test.indicator, Sector: test.sector}
		start, err := req.EarliestMonth()
		require.NoError(t, err)
		begin, end, err := queryUSMonths(cmd, time.Now(), start)
		require.NoError(t, err)
		require.Equal(t, test.start, begin)
		require.Equal(t, test.start, end)
	}
	for _, test := range []struct {
		indicator string
		args      []string
	}{
		{"industrial-production", []string{"--sector", "bad"}},
		{"industrial-production", []string{"--begin", "191812", "--end", "191901"}},
		{"industrial-production", []string{"--sector", "manufacturing", "--begin", "197112", "--end", "197201"}},
		{"capacity-utilization", []string{"--begin", "196612", "--end", "196701"}},
		{"capacity-utilization", []string{"--sector", "manufacturing", "--begin", "197112", "--end", "197201"}},
		{"industrial-production", []string{"--begin", "202513"}},
		{"industrial-production", []string{"--begin", "202502", "--end", "202501"}},
		{"industrial-production", []string{"--end", "299901"}},
		{"capacity-utilization", []string{"extra"}},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(append([]string{"us", test.indicator}, test.args...))
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
	cmd := newUSProductionCLI("industrial-production")
	require.NoError(t, cmd.Flags().Set("begin", "202501"))
	require.NoError(t, cmd.Flags().Set("end", "202501"))
	require.NoError(t, cmd.Flags().Set("no-cache", "true"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorIs(t, cmd.Execute(), context.Canceled)
}

func TestUSProductionMissingRendering(t *testing.T) {
	for _, indicator := range []string{"industrial-production", "capacity-utilization"} {
		var out bytes.Buffer
		err := printUSProduction(&out, &fred.ProductionResult{Indicator: indicator, Sector: "total", Observations: []fred.ProductionObservation{{ProductionValue: fred.ProductionValue{Period: "202501"}, Note: "当月数据缺失"}}})
		require.NoError(t, err)
		require.Contains(t, out.String(), "2025-01")
		require.Contains(t, out.String(), "—")
		require.Contains(t, out.String(), "当月数据缺失")
	}
}
