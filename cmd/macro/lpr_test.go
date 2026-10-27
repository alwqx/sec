package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

func TestLPROutput(t *testing.T) {
	zero, rate, change := 0.0, 3.0, -10.0
	items := []pbc.LPRObservation{{QuoteDate: "20250520", PublishedAt: "2025-05-20T09:00:00+08:00", OneYearRate: &rate, OneYearChangeBP: &zero, FiveYearPlusChangeBP: &change}}
	var out bytes.Buffer
	require.NoError(t, printLPR(&out, items))
	require.Contains(t, out.String(), "20250520")
	require.Contains(t, out.String(), "-10")
	require.Contains(t, out.String(), "—")
	out.Reset()
	require.NoError(t, printLPR(&out, nil))
	require.Contains(t, out.String(), "暂无")
}

func TestLPRArguments(t *testing.T) {
	for _, args := range [][]string{{"cn", "lpr", "--begin", "201907"}, {"cn", "lpr", "--begin", "202513"}, {"cn", "lpr", "--begin", "202505", "--end", "202504"}, {"cn", "lpr", "extra"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
