package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

func TestReverseRepoOutput(t *testing.T) {
	var out bytes.Buffer
	result := pbc.ReverseRepoResult{Events: []pbc.ReverseRepoEvent{{EffectiveDate: "20250508", PublishedAt: "2025-05-07T09:25:30+08:00", PreviousRate: 1.5, Rate: 1.4, ChangeBasisPoints: -10}}}
	require.NoError(t, printReverseRepo(&out, result))
	require.Contains(t, out.String(), "20250508")
	require.Contains(t, out.String(), "2025-05-07")
	require.Contains(t, out.String(), "-10")
	out.Reset()
	result.Events, result.PreviousEvent = nil, &pbc.ReverseRepoEvent{EffectiveDate: "20250508", Rate: 1.4}
	require.NoError(t, printReverseRepo(&out, result))
	require.Contains(t, out.String(), "区间前最近公告")
	require.Contains(t, out.String(), "未检索到")
}

func TestReverseRepoArguments(t *testing.T) {
	for _, args := range [][]string{{"cn", "reverse-repo", "--begin", "20200101"}, {"cn", "reverse-repo", "--begin", "20250230"}, {"cn", "reverse-repo", "--begin", "20250509", "--end", "20250508"}, {"cn", "reverse-repo", "extra"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
