package macro

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInvalidMonths(t *testing.T) {
	for _, args := range [][]string{
		{"cn", "cpi", "--begin", "202613"},
		{"cn", "ppi", "--begin", "202609", "--end", "202601"},
		{"cn", "core-cpi", "--end", "20260901"},
		{"cn", "cpi", "unexpected"},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
