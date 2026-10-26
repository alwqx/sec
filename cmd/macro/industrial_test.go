package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func TestIndustrialOutput(t *testing.T) {
	ytd, mom, zero := 6.3, 0.76, 0.0
	items := []nbs.IndustrialObservation{
		{Period: "202602", YTDPeriod: "202601-202602", YTD: &ytd, MoM: &mom, Note: "1—2月合并累计，非2月单月同比"},
		{Period: "202603", YTDPeriod: "202601-202603", YoY: &zero},
	}
	var out bytes.Buffer
	require.NoError(t, printIndustrial(&out, items))
	for _, text := range []string{"单月同比", "累计同比", "季调环比", "202601-202602", "6.3", "0.76", "0.0", "—", "非2月单月同比"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	require.NoError(t, printIndustrial(&out, nil))
	require.Contains(t, out.String(), "暂无")
}

func TestIndustrialArguments(t *testing.T) {
	for _, args := range [][]string{
		{"cn", "industrial", "--begin", "202613"},
		{"cn", "ip", "--begin", "202609", "--end", "202601"},
		{"cn", "industrial", "extra"},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
