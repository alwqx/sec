package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/mofcom"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestCNServicesCommandJSON(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/mofcom/testdata/services-monthly.json")
	require.NoError(t, err)
	gock.New("https://data.mofcom.gov.cn").Get("/datamofcom/front/fwmy/mouthly").MatchParam("start_date", "202607").MatchParam("end_date", "202608").MatchParam("pageNumber", "1").Reply(200).BodyString(string(raw))
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"cn", "services-trade", "--begin", "202607", "--end", "202608", "--json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result mofcom.ServicesResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "services_trade", result.Indicator)
	require.Equal(t, "monthly", result.PeriodBasis)
	require.False(t, result.LevelAnnualized)
	require.Len(t, result.Observations, 2)
	require.Equal(t, "202607", result.Observations[0].Period)
	require.Equal(t, -218.5, *result.Observations[1].USD.Balance)
	require.Nil(t, result.Observations[1].PublishedAt)
	require.NotEmpty(t, result.Observations[1].RawValues)
	require.True(t, gock.IsDone())
}

func TestPrintCNServicesMissingAndZero(t *testing.T) {
	zero, balance := 0.0, -10.2
	result := &mofcom.ServicesResult{Observations: []mofcom.ServicesObservation{{Period: "202601"}, {Period: "202602", CNY: &mofcom.ServicesValues{Exports: &zero, Balance: &balance}}}}
	var out bytes.Buffer
	require.NoError(t, printCNServices(&out, result))
	require.Contains(t, out.String(), "2026-01")
	require.Contains(t, out.String(), "—")
	require.Contains(t, out.String(), "0.00")
	require.Contains(t, out.String(), "-10.20")
	require.Contains(t, out.String(), "亿元")
	require.Contains(t, out.String(), "亿美元")
	require.Contains(t, out.String(), "不从累计值推算单月")
}

func TestCNTradeNewArgumentsRejectBeforeFetching(t *testing.T) {
	for _, args := range [][]string{
		{"cn", "services-trade", "extra"},
		{"cn", "services-trade", "--begin", "201512", "--end", "201601"},
		{"cn", "services-trade", "--begin", "202608", "--end", "202607"},
		{"cn", "trade", "--partners-currency", "eur"},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute(), args)
	}
}
