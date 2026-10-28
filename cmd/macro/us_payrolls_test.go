package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/bls"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSPayrollsCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/bls/testdata/payrolls-2024-2026.json")
	require.NoError(t, err)
	gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").Reply(200).BodyString(string(raw))
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"us", "payrolls", "--begin", "202501", "--end", "202609", "--no-cache", "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result bls.PayrollResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Observations, 21)
	require.False(t, result.HistoryEnabled)
	require.Equal(t, 159044.0, *result.Observations[20].Employment)
	require.Equal(t, 29.0, *result.Observations[20].Change)
	require.Empty(t, result.Observations[20].ObservedVersions)
	require.True(t, gock.IsDone())
}
