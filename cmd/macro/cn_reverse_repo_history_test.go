package macro

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCNReverseRepoHistoryDefaultTerminalOutputIsLimited(t *testing.T) {
	command := newCNReverseRepoHistoryCLI()
	command.SetArgs([]string{"--no-cache"})
	var output bytes.Buffer
	command.SetOut(&output)
	require.NoError(t, command.Execute())
	require.Contains(t, output.String(), "默认显示最近记录")
	require.Contains(t, output.String(), "2024-07-19")
	require.Equal(t, 20, strings.Count(output.String(), "| 7天"))
}

func TestCNReverseRepoHistoryJSONCanRequestFullArchive(t *testing.T) {
	command := newCNReverseRepoHistoryCLI()
	command.SetArgs([]string{"--no-cache", "--json", "--begin", "20150122", "--end", "20240719", "--limit", "0"})
	var output bytes.Buffer
	command.SetOut(&output)
	require.NoError(t, command.Execute())
	var result struct {
		TotalEvents int   `json:"total_events"`
		Events      []any `json:"events"`
	}
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.Equal(t, 1606, result.TotalEvents)
	require.Len(t, result.Events, 1606)
}
