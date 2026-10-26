package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

func TestLoansOutput(t *testing.T) {
	value, zero := 10800.0, 0.0
	items := []pbc.LoanObservation{{Period: "202609", MonthlyFlow: &value, MonthlyFlowApproximate: true, Components: []pbc.LoanComponent{{Name: "住户短期贷款", MonthlyFlow: &zero, MonthlyFlowApproximate: true}}}}
	var out bytes.Buffer
	require.NoError(t, printLoans(&out, items, false))
	require.Contains(t, out.String(), "≈10800")
	require.Contains(t, out.String(), "—")
	require.NotContains(t, out.String(), "住户短期贷款")
	out.Reset()
	require.NoError(t, printLoans(&out, items, true))
	require.Contains(t, out.String(), "住户短期贷款")
	require.Contains(t, out.String(), "≈0")
	out.Reset()
	require.NoError(t, printLoans(&out, nil, false))
	require.Contains(t, out.String(), "暂无")
}

func TestLoansArguments(t *testing.T) {
	for _, args := range [][]string{{"cn", "loans", "--begin", "202613"}, {"cn", "loans", "--begin", "202609", "--end", "202601"}, {"cn", "loans", "extra"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
