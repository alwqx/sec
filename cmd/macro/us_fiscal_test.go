package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSFiscalCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/us-fiscal.csv")
	require.NoError(t, err)
	for _, asJSON := range []bool{false, true} {
		gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "MTSR133FMS,MTSO133FMS,MTSDS133FMS").Reply(200).BodyString(string(raw))
		command := newUSFiscalCLI()
		args := []string{"--begin", "202609", "--end", "202610", "--no-cache", "--refresh"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		require.NoError(t, command.Execute())
		if asJSON {
			var result fred.FiscalResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 2)
			require.Equal(t, 26900.34710202, *result.Observations[0].Deficit)
			require.Nil(t, result.Observations[1].Receipts)
			require.Nil(t, result.PublishedAt)
			require.Nil(t, result.RevisionID)
			require.Contains(t, out.String(), "fiscal_year_to_date")
		} else {
			for _, text := range []string{"百万美元", "未季调", "未折年", "2026-09", "26900.35", "赤字（负为盈余）", "—", "--json"} {
				require.Contains(t, out.String(), text)
			}
			require.NotContains(t, out.String(), "当前快照未提供全部月度数值")
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSFiscalDatesAndArguments(t *testing.T) {
	command := newUSFiscalCLI()
	require.NoError(t, command.Flags().Set("end", "198010"))
	begin, end, err := queryUSMonths(command, time.Now(), "198010")
	require.NoError(t, err)
	require.Equal(t, "198010", begin)
	require.Equal(t, begin, end)
	for _, args := range [][]string{{"extra"}, {"--end", "bad"}, {"--begin", "198009", "--end", "198010"}, {"--begin", "202602", "--end", "202601"}, {"--end", "999912"}} {
		command := newUSFiscalCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
}

func TestUSFiscalSurplusAndZeroTable(t *testing.T) {
	zero, surplus := 0.0, -10.0
	var out bytes.Buffer
	require.NoError(t, printUSFiscal(&out, &fred.FiscalResult{Observations: []fred.FiscalObservation{{FiscalValue: fred.FiscalValue{Period: "202604", Receipts: &zero, Deficit: &surplus}}}}))
	for _, text := range []string{"0.00", "-10.00", "—", "2026-04"} {
		require.Contains(t, out.String(), text)
	}
}
