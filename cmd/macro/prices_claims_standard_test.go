package macro

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestPricesClaimsStandardErrors(t *testing.T) {
	defer gock.Off()
	for _, name := range []string{"cpi", "core-cpi", "initial-claims", "continued-claims"} {
		for _, mode := range []string{"flags", "cancel", "http"} {
			args := []string{"us", name, "--begin", "202601", "--end", "202609", "--standard-json", "--no-cache"}
			claims := name == "initial-claims" || name == "continued-claims"
			if claims {
				args[3], args[5] = "20261001", "20261024"
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
				if claims {
					gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(503)
				} else {
					gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").Reply(503)
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
	raw, err := os.ReadFile("../../provider/fred/testdata/jobless-claims.csv")
	require.NoError(t, err)
	gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(200).BodyString(string(raw))
	cmd := newUSClaimsCLI("initial-claims")
	cmd.SetArgs([]string{"--begin", "20261001", "--end", "20261024", "--no-cache", "--standard-json"})
	cmd.SetOut(failingMoneyWriter{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorContains(t, cmd.Execute(), "测试写入失败")
	require.True(t, gock.IsDone())
}
