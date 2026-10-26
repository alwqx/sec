package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func TestRetailOutput(t *testing.T) {
	amount, growth, zero := 86078.9, 2.8, 0.0
	items := []nbs.RetailObservation{
		{Period: "202602", YTDPeriod: "202601-202602", YTDAmount: &amount, YTDGrowth: &growth, Note: "1—2月合并累计，不是2月单月值"},
		{Period: "202603", Amount: &zero, YoY: &zero},
	}
	var out bytes.Buffer
	require.NoError(t, printRetail(&out, items))
	for _, text := range []string{"当月金额", "亿元", "累计金额", "累计同比", "86078.9", "2.8", "0.0", "—", "202601-202602", "名义同比", "不是2月单月值"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	require.NoError(t, printRetail(&out, nil))
	require.Contains(t, out.String(), "暂无")
}

func TestRetailArguments(t *testing.T) {
	for _, args := range [][]string{
		{"cn", "retail", "--begin", "202613"},
		{"cn", "retail", "--begin", "202609", "--end", "202601"},
		{"cn", "retail", "--end", "20260101"},
		{"cn", "retail", "extra"},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
