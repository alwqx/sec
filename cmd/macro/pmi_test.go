package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func TestPMIOutput(t *testing.T) {
	var output bytes.Buffer
	value := 50.1
	zero := 0.0
	err := printPMI(&output, []nbs.PMIObservation{
		{Period: "202609", Indicator: "manufacturing-pmi", Index: &value},
		{Period: "202609", Indicator: "new-orders", Index: &zero},
		{Period: "202609", Indicator: "employment", Index: nil},
	})
	require.NoError(t, err)
	for _, text := range []string{"202609", "制造业 PMI", "新出口订单", "从业人员", "50.1", "0.0", "—", "季调扩散指数"} {
		require.Contains(t, output.String(), text)
	}
	output.Reset()
	require.NoError(t, printPMI(&output, nil))
	require.Contains(t, output.String(), "暂无")
}

func TestPMIArguments(t *testing.T) {
	for _, args := range [][]string{
		{"cn", "pmi", "--begin", "202613"},
		{"cn", "pmi", "--end", "20260101"},
		{"cn", "pmi", "--begin", "202609", "--end", "202601"},
		{"cn", "pmi", "extra"},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
