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

func TestMoneySupplyOutputAndArguments(t *testing.T) {
	m1, m1yoy, m2, m2yoy := 1167600.0, 3.2, 3596300.0, 7.2
	items := []pbc.MoneySupplyObservation{{Period: "202609", M1Balance: &m1, M1YoY: &m1yoy, M1Definition: "revised-2025", M2Balance: &m2, M2YoY: &m2yoy}}
	var out bytes.Buffer
	require.NoError(t, printMoneySupply(&out, items))
	require.Contains(t, out.String(), "revised-2025")
	require.Contains(t, out.String(), "1167600")
	require.Contains(t, out.String(), "—")
	for _, args := range [][]string{{"cn", "money", "--begin", "202613"}, {"cn", "money", "--begin", "202609", "--end", "202601"}, {"cn", "money", "extra"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
