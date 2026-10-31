package macro

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestFiscalEnterpriseStandardErrors(t *testing.T) {
	defer gock.Off()
	for _, name := range []string{"fiscal", "corporate-profits", "business-inventories", "new-orders"} {
		for _, mode := range []string{"flags", "cancel", "http"} {
			begin, end := "202608", "202609"
			if name == "corporate-profits" {
				begin, end = "2026Q2", "2026Q3"
			}
			args := []string{"us", name, "--begin", begin, "--end", end, "--standard-json", "--no-cache"}
			ctx := context.Background()
			switch mode {
			case "flags":
				args = append(args, "--json")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "http":
				gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(503)
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
	raw, err := os.ReadFile("../../provider/fred/testdata/us-fiscal.csv")
	require.NoError(t, err)
	gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(200).BodyString(string(raw))
	cmd := newUSFiscalCLI()
	cmd.SetArgs([]string{"--begin", "202609", "--end", "202610", "--standard-json", "--no-cache"})
	cmd.SetOut(failingMoneyWriter{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorContains(t, cmd.Execute(), "测试写入失败")
	require.True(t, gock.IsDone())
}
