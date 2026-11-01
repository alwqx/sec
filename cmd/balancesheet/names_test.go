package balancesheet

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCommandNamesKeepOldAliases(t *testing.T) {
	root := &cobra.Command{Use: "sec"}
	root.AddCommand(NewBalanceSheetCLI(), NewBalanceSheetDownloadCLI())
	for want, names := range map[string][]string{
		"financials":    {"financials", "balance-sheet", "bs"},
		"annual-report": {"annual-report", "balance-sheet-download", "bsd"},
	} {
		for _, name := range names {
			found, _, err := root.Find([]string{name})
			require.NoError(t, err)
			require.Equal(t, want, found.Name())
		}
	}
}
