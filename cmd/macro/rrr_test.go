package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

func TestRRROutput(t *testing.T) {
	result := pbc.RRRResult{Events: []pbc.RRREvent{
		{EffectiveDate: "20200316", PublishedAt: "2020-03-13T17:00:00+08:00", ScopeType: "targeted", Scope: "达到考核标准的银行", ChangePPLower: -1, ChangePPUpper: -0.5, TrancheCount: 1},
		{EffectiveDate: "20200415", ScopeType: "targeted", Scope: "中小银行", ChangePPLower: -0.5, ChangePPUpper: -0.5, TrancheIndex: 1, TrancheCount: 2},
		{EffectiveDate: "20220425", ScopeType: "targeted", Scope: "符合条件的银行", Additional: true, ChangePPLower: -0.25, ChangePPUpper: -0.25, TrancheCount: 1},
	}}
	var out bytes.Buffer
	require.NoError(t, printRRR(&out, result))
	require.Contains(t, out.String(), "2020-03-16")
	require.Contains(t, out.String(), "2020-03-13 17:00:00")
	require.Contains(t, out.String(), "下调0.5～1")
	require.Contains(t, out.String(), "第1/2批")
	require.Contains(t, out.String(), "额外")
	out.Reset()
	require.NoError(t, printRRR(&out, pbc.RRRResult{}))
	require.Contains(t, out.String(), "未检索到")
}

func TestRRRArguments(t *testing.T) {
	for _, args := range [][]string{{"cn", "rrr", "--begin", "20171231"}, {"cn", "rrr", "--begin", "20250230"}, {"cn", "rrr", "--begin", "20250516", "--end", "20250515"}, {"cn", "rrr", "extra"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
