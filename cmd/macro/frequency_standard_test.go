package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestEFFRStandardMethodologyBoundary(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/nyfed/testdata/effr-methodology.json")
	require.NoError(t, err)
	gock.New("https://markets.newyorkfed.org").Get("/api/rates/unsecured/effr/search.json").MatchParam("startDate", "2016-01-29").MatchParam("endDate", "2016-03-04").Reply(200).BodyString(string(raw))
	cmd := newUSEFFRCLI()
	cmd.SetArgs([]string{"--begin", "20160229", "--end", "20160304", "--standard-json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var d macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &d))
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 10)
	require.Equal(t, "20160229", d.Observations[0].Period)
	require.Equal(t, "20160301", d.Observations[2].Period)
	require.NotEqual(t, d.Observations[0].SeriesID, d.Observations[2].SeriesID)
	require.Nil(t, d.Observations[3].Value)
	require.Equal(t, "methodology_break", *d.Observations[3].MissingReason)
	require.Equal(t, 1.0, *d.Observations[5].Value)
	require.True(t, gock.IsDone())
}

func TestFrequencyStandardCLIErrors(t *testing.T) {
	defer gock.Off()
	for _, name := range []string{"gdp", "effr"} {
		for _, mode := range []string{"flags", "cancel", "http"} {
			args := []string{"us", name, "--begin", "2026Q1", "--end", "2026Q2", "--standard-json", "--no-cache"}
			if name == "effr" {
				args[3], args[5] = "20250102", "20250106"
			}
			ctx := context.Background()
			switch mode {
			case "flags":
				args = append(args, "--json")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "http":
				if name == "gdp" {
					gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(503)
				} else {
					gock.New("https://markets.newyorkfed.org").Get("/api/rates/unsecured/effr/search.json").Reply(503)
				}
			}
			cmd := NewMacroCLI()
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SilenceUsage = true
			err := cmd.ExecuteContext(ctx)
			require.Error(t, err, name+"/"+mode)
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Empty(t, out.String())
			require.True(t, gock.IsDone())
		}
	}
}
