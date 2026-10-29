package macro

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func TestInvestmentOutput(t *testing.T) {
	amount, growth, zero := 334115.0, -7.2, 0.0
	result := &nbs.InvestmentResult{Sector: "infrastructure", Observations: []nbs.InvestmentObservation{
		{Sector: "infrastructure", Period: "202509", YTDGrowth: &zero, Scope: "不含电力、热力、燃气及水生产和供应业"},
		{Sector: "infrastructure", Period: "202609", YTDAmount: &amount, YTDGrowth: &growth, Scope: "含电力、热力、燃气及水生产和供应业"},
	}}
	var out bytes.Buffer
	require.NoError(t, printInvestment(&out, result))
	for _, text := range []string{"月份", "累计金额", "累计同比", "2025-09", "2026-09", "2026-01至2026-09", "334115.0", "-7.2", "0.0", "—", "不含电力", "含电力", "未季调", "1月免报"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	require.NoError(t, printInvestment(&out, nil))
	require.Contains(t, out.String(), "暂无")
}

func TestInvestmentCommandJanuaryJSON(t *testing.T) {
	command := newInvestmentCLI()
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"--begin", "202601", "--end", "202601", "--sector", "manufacturing", "--no-cache", "--json"})
	require.NoError(t, command.Execute())
	var result nbs.InvestmentResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "manufacturing", result.Sector)
	require.Len(t, result.Observations, 1)
	require.Equal(t, "january_exempt", result.Observations[0].Status)
	require.Contains(t, out.String(), `"ytd_amount": null`)
	require.Contains(t, out.String(), `"published_at": null`)
	require.Contains(t, out.String(), `"historical_version": null`)
	for _, flag := range []string{"begin", "end", "sector", "json", "refresh", "no-cache"} {
		require.NotNil(t, command.Flags().Lookup(flag))
	}
}

func TestInvestmentArguments(t *testing.T) {
	for _, args := range [][]string{
		{"--begin", "202508", "--end", "202609"},
		{"--begin", "202509", "--end", "202609", "--sector", "real-estate"},
		{"--begin", "202610", "--end", "202601"},
		{"--end", "202613"},
		{"--begin", "202609", "--end", "202609", "--sector", "private"},
		{"extra"},
	} {
		command := newInvestmentCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
}
