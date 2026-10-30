package fx

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/forex"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestFXDefaultsAndArguments(t *testing.T) {
	now := time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC)
	for _, kind := range []string{"quote", "history", "fixing", "reference"} {
		root := NewFXCLI()
		cmd, _, err := root.Find([]string{kind})
		require.NoError(t, err)
		req, source, err := request(cmd, kind, now)
		require.NoError(t, err)
		require.NotEmpty(t, source)
		require.NotEmpty(t, req.Pairs)
		if kind != "quote" {
			require.Equal(t, "20250101", req.Begin)
			require.Equal(t, "20260101", req.End)
		}
		if kind == "reference" {
			require.Equal(t, "16:00", req.At)
		}
	}
	for _, args := range [][]string{
		{"quote", "extra"}, {"quote", "--pairs", "CNY/CNH"}, {"quote", "--pairs", "USD/CNH,USD/CNH"},
		{"history", "--source", "unknown"}, {"history", "--source", "fred", "--pairs", "USD/CNH"},
		{"fixing", "--begin", "20050101"}, {"fixing", "--end", "20240230"},
		{"reference", "--at", "16:30"}, {"reference", "--begin", "20240102", "--end", "20240101"},
	} {
		cmd := NewFXCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute(), args)
	}
	root := NewFXCLI()
	cmd, _, err := root.Find([]string{"history"})
	require.NoError(t, err)
	require.NoError(t, cmd.Flags().Set("source", "fred"))
	req, source, err := request(cmd, "history", now)
	require.NoError(t, err)
	require.Equal(t, "fred", source)
	require.Equal(t, forex.FREDPairs, req.Pairs)
}

func TestFXCommandsJSON(t *testing.T) {
	for _, tc := range []struct {
		kind, priceType, body, host, path string
		args                              []string
	}{
		{"quote", "indicative-quote", `var hq_str_fx_susdcnh="10:45:32,6.655000,6.655100,6.655800,52,6.656000,6.657800,6.652600,6.655000,离岸人民币（香港）,-0.01,-0.0008,0.0007,,6.99,6.65,,2024-01-02";`, "https://hq.sinajs.cn", "/", []string{"--pairs", "USD/CNH"}},
		{"history", "noon-buying-rate", "observation_date,DEXCHUS,DEXHKUS\n2024-01-02,7.1,7.8\n", "https://fred.stlouisfed.org", "/graph/fredgraph.csv", []string{"--source", "fred", "--pairs", "USD/CNY", "--begin", "20240102", "--end", "20240102"}},
		{"fixing", "central-parity", `{"head":{"rep_code":"200"},"data":{"searchlist":["USD/CNY"],"total":1,"pageTotal":1,"pageNum":1,"startDate":"2024-01-02","endDate":"2024-01-02"},"records":[{"date":"2024-01-02","values":["7.1"]}]}`, "https://www.chinamoney.com.cn", "/ags/ms/cm-u-bk-ccpr/CcprHisNew", []string{"--pairs", "USD/CNY", "--begin", "20240102", "--end", "20240102"}},
		{"reference", "onshore-reference", `{"head":{"rep_code":"200"},"data":{"startDateTool":"02 Jan 2024","endDateTool":"02 Jan 2024","message":"","flag":"0"},"records":[{"dealDate":"2024-01-02","ccyPair":"USD/CNY","rateOf16hour":"7.2"}]}`, "https://www.chinamoney.com.cn", "/ags/ms/cm-u-bk-fx/RefRateHis", []string{"--pairs", "USD/CNY", "--begin", "20240102", "--end", "20240102"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			defer gock.Off()
			gock.New(tc.host).Get(tc.path).Reply(200).BodyString(tc.body)
			cmd := NewFXCLI()
			args := append([]string{tc.kind, "--json", "--no-cache"}, tc.args...)
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			var result forex.Result
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, tc.priceType, result.PriceType)
			require.Len(t, result.Observations, 1)
			require.Equal(t, "2024-01-02", result.Observations[0].Date)
			require.False(t, result.Observations[0].FetchedAt.IsZero())
			require.NotEmpty(t, result.Observations[0].Source)
			require.NotEmpty(t, result.Observations[0].RawValues)
			require.True(t, gock.IsDone())
		})
	}
}

func TestFXTerminalFormatsAndSourceNotes(t *testing.T) {
	value := 0.8488748164
	for _, kind := range []string{"indicative-quote", "provider-daily-bar", "central-parity", "onshore-reference", "noon-buying-rate"} {
		var out bytes.Buffer
		result := &forex.Result{Provider: "Sina", PriceType: kind, Timezone: "unspecified", Observations: []forex.Observation{{Date: "2024-01-02", Pair: "HKD/CNY", Value: &value, Open: &value, High: &value, Low: &value, Close: &value, Bid: &value, Ask: &value, ObservedAt: "2024-01-02T10:00:00+08:00", Note: "新浪计算值"}}, Notes: []string{"保留来源口径"}}
		require.NoError(t, printRates(&out, result))
		require.Contains(t, out.String(), "HKD/CNY")
		require.Contains(t, out.String(), "0.848875")
		require.NotContains(t, out.String(), "0.8488748164")
		require.Contains(t, out.String(), "新浪计算值")
		require.Contains(t, out.String(), "来源未注明")
	}
	var out bytes.Buffer
	require.NoError(t, printRates(&out, &forex.Result{Observations: []forex.Observation{}}))
	require.Contains(t, out.String(), "未提供汇率记录")
}

func TestFXFixingReversedPairsJSON(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.chinamoney.com.cn").Get("/ags/ms/cm-u-bk-ccpr/CcprHisNew").MatchParam("currency", "HKD/CNY,USD/CNY").Reply(200).BodyString(`{"head":{"rep_code":"200"},"data":{"searchlist":["USD/CNY","HKD/CNY"],"total":1,"pageTotal":1,"pageNum":1,"startDate":"2024-01-02","endDate":"2024-01-02"},"records":[{"date":"2024-01-02","values":["7.1","0.91"]}]}`)
	cmd := NewFXCLI()
	cmd.SetArgs([]string{"fixing", "--pairs", "HKD/CNY,USD/CNY", "--begin", "20240102", "--end", "20240102", "--json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result forex.Result
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Observations, 2)
	require.Equal(t, "HKD/CNY", result.Observations[0].Pair)
	require.Equal(t, "HKD", result.Observations[0].BaseCurrency)
	require.Equal(t, 0.91, *result.Observations[0].Value)
	require.Equal(t, "USD/CNY", result.Observations[1].Pair)
	require.Equal(t, "USD", result.Observations[1].BaseCurrency)
	require.Equal(t, 7.1, *result.Observations[1].Value)
	require.True(t, gock.IsDone())
}
