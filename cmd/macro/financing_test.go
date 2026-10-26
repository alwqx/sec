package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

func TestFinancingOutput(t *testing.T) {
	stock, flow, zero, negative := 4681700.0, 35476.0, 0.0, -393.0
	items := []pbc.FinancingObservation{{Period: "202609", Stock: &stock, Flow: &flow, YTDFlow: &zero, Components: []pbc.FinancingComponent{{Name: "企业债券", Flow: &negative}}}}
	var out bytes.Buffer
	require.NoError(t, printFinancing(&out, items, false))
	for _, text := range []string{"4681700", "35476", "计算值", "亿元", "—"} {
		require.Contains(t, out.String(), text)
	}
	require.NotContains(t, out.String(), "企业债券")
	out.Reset()
	require.NoError(t, printFinancing(&out, items, true))
	require.Contains(t, out.String(), "企业债券")
	require.Contains(t, out.String(), "-393")
	out.Reset()
	require.NoError(t, printFinancing(&out, nil, false))
	require.Contains(t, out.String(), "暂无")
}

func TestFinancingArguments(t *testing.T) {
	for _, args := range [][]string{{"cn", "financing", "--begin", "202613"}, {"cn", "afre", "--begin", "202609", "--end", "202601"}, {"cn", "financing", "extra"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
