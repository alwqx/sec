package macro

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestLaborStandardErrors(t *testing.T) {
	defer gock.Off()
	for _, name := range []string{"unemployment", "participation", "earnings", "weekly-hours"} {
		for _, mode := range []string{"flags", "cancel", "http"} {
			args := []string{"us", name, "--begin", "202501", "--end", "202609", "--standard-json", "--no-cache"}
			ctx := context.Background()
			switch mode {
			case "flags":
				args = append(args, "--json")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "http":
				gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").Reply(503)
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
	raw, err := os.ReadFile("../../provider/bls/testdata/labor-force-2024-2026.json")
	require.NoError(t, err)
	gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").Reply(200).BodyString(string(raw))
	cmd := newUSLaborForceCLI("unemployment")
	cmd.SetArgs([]string{"--begin", "202501", "--end", "202609", "--standard-json", "--no-cache"})
	cmd.SetOut(failingMoneyWriter{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorContains(t, cmd.Execute(), "测试写入失败")
	require.True(t, gock.IsDone())
}
