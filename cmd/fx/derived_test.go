package fx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/forex"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestDerivedArguments(t *testing.T) {
	for _, args := range [][]string{
		{"history", "--source", "hkma"},
		{"cross", "--source", "hkma"}, {"cross", "--source", "fred", "--pairs", "CNH/CNY"},
		{"cross", "--source", "fred", "--max-skew", "1s"}, {"cross", "--source", "fred", "--at", "16:00"},
		{"cross", "--source", "sina", "--begin", "20240101"}, {"cross", "--source", "sina", "--max-skew", "61s"},
		{"cross", "--source", "cfets", "--at", "16:30"}, {"cross", "--pairs", "HKD/CNY,HKD/CNY"},
		{"spread", "--max-skew", "-1s"}, {"spread", "--max-age", "0s"}, {"spread", "--max-age", "25h"}, {"spread", "extra"},
	} {
		cmd := NewFXCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute(), args)
	}
}

func TestCrossFREDJSON(t *testing.T) {
	defer gock.Off()
	gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(200).BodyString("observation_date,DEXCHUS,DEXHKUS\n2024-01-02,7.2,7.8\n2024-01-03,.,7.8\n")
	cmd := NewFXCLI()
	cmd.SetArgs([]string{"cross", "--pairs", "HKD/CNY,CNY/HKD", "--begin", "20240102", "--end", "20240103", "--json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result forex.Result
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Observations, 4)
	require.InDelta(t, 7.2/7.8, *result.Observations[0].Value, 1e-12)
	require.InDelta(t, 7.8/7.2, *result.Observations[1].Value, 1e-12)
	require.Nil(t, result.Observations[2].Value)
	require.Equal(t, "missing-value", result.Observations[2].Alignment.Status)
	require.True(t, result.Observations[0].IsDerived)
	require.Len(t, result.Observations[0].Inputs, 2)
	require.True(t, gock.IsDone())
}

func TestCrossCFETSDirection(t *testing.T) {
	defer gock.Off()
	for _, input := range []struct {
		code, pair string
		value      float64
	}{{"USD.CNY", "USD/CNY", 7.2}, {"HKD.CNY", "HKD/CNY", 0.9}} {
		gock.New("https://www.chinamoney.com.cn").Get("/ags/ms/cm-u-bk-fx/RefRateHis").MatchParam("currencyCode", input.code).Reply(200).BodyString(fmt.Sprintf(`{"head":{"rep_code":"200"},"data":{"startDateTool":"02 Jan 2024","endDateTool":"02 Jan 2024","message":"","flag":"0"},"records":[{"dealDate":"2024-01-02","ccyPair":"%s","rateOf16hour":"%g"}]}`, input.pair, input.value))
	}
	cmd := NewFXCLI()
	cmd.SetArgs([]string{"cross", "--source", "cfets", "--pairs", "USD/HKD", "--begin", "20240102", "--end", "20240102", "--json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result forex.Result
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Observations, 1)
	require.InDelta(t, 8, *result.Observations[0].Value, 1e-12)
	require.Equal(t, "16:00", result.ObservationTime)
	require.Equal(t, "same-fixed-time", result.Observations[0].Alignment.Status)
	require.True(t, gock.IsDone())
}

func quoteLine(symbol string, value float64, at time.Time) string {
	return fmt.Sprintf("var hq_str_fx_s%s=\"%s,%g,%g,%g,0,%g,%g,%g,%g,参考行情,0,0,0,,0,0,,%s\";\n", symbol, at.Format("15:04:05"), value, value+0.0001, value, value, value+0.1, value-0.1, value, at.Format("2006-01-02"))
}

func TestSpreadStrictAndExplicitTolerance(t *testing.T) {
	for _, approximate := range []bool{false, true} {
		t.Run(fmt.Sprint(approximate), func(t *testing.T) {
			defer gock.Off()
			now := time.Now().In(forex.Beijing).Truncate(time.Second)
			body := quoteLine("usdcny", 7.2, now.Add(-2*time.Second)) + quoteLine("usdcnh", 7.21, now.Add(-time.Second))
			gock.New("https://hq.sinajs.cn").Get("/").MatchParam("list", "fx_susdcny,fx_susdcnh").Reply(200).BodyString(body)
			cmd := NewFXCLI()
			args := []string{"spread", "--json", "--no-cache"}
			if approximate {
				args = append(args, "--max-skew", "2s")
			}
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			var result forex.SpreadResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			if approximate {
				require.InDelta(t, 0.01, *result.Difference, 1e-12)
				require.Equal(t, "within-tolerance", result.Alignment.Status)
			} else {
				require.Nil(t, result.Difference)
				require.Equal(t, "timestamp-mismatch", result.Alignment.Status)
			}
			require.Len(t, result.Inputs, 2)
			require.Equal(t, 1.0, *result.Alignment.SkewSeconds)
			require.True(t, gock.IsDone())
		})
	}
}

func TestCrossSina(t *testing.T) {
	t.Run("Sina cross", func(t *testing.T) {
		defer gock.Off()
		now := time.Now().In(forex.Beijing).Truncate(time.Second)
		gock.New("https://hq.sinajs.cn").Get("/").Reply(200).BodyString(quoteLine("usdcny", 7.2, now) + quoteLine("usdcnh", 7.21, now) + quoteLine("usdhkd", 7.8, now))
		cmd := NewFXCLI()
		cmd.SetArgs([]string{"cross", "--source", "sina", "--pairs", "HKD/CNH", "--json", "--no-cache"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		var result forex.Result
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		require.InDelta(t, 7.21/7.8, *result.Observations[0].Value, 1e-12)
		require.Equal(t, "synchronized", result.Observations[0].Alignment.Status)
		require.True(t, gock.IsDone())
	})
}

func TestSpreadTerminalMissingValue(t *testing.T) {
	var out bytes.Buffer
	result := &forex.SpreadResult{IsDerived: true, Alignment: forex.Alignment{Status: "timestamp-mismatch", Note: "时点不一致，未计算"}, MaxAgeSeconds: 900}
	require.NoError(t, printSpread(&out, result))
	require.Contains(t, out.String(), "时点不一致")
	require.Contains(t, out.String(), "推导值")
	require.Contains(t, out.String(), "—")
}

func TestSpreadTerminalFormatsBeijingQuoteTimes(t *testing.T) {
	value := 7.2
	result := &forex.SpreadResult{IsDerived: true, Inputs: []forex.Observation{
		{Pair: "USD/CNY", Value: &value, ObservedAt: "2024-01-02T08:00:00Z"},
		{Pair: "USD/CNH", Value: &value, ObservedAt: "2024-01-02T16:00:01+08:00"},
	}, Alignment: forex.Alignment{Status: "timestamp-mismatch"}}
	var out bytes.Buffer
	require.NoError(t, printSpread(&out, result))
	require.Contains(t, out.String(), "报价时间（北京时间）")
	require.Contains(t, out.String(), "2024-01-02 16:00:00")
	require.Contains(t, out.String(), "2024-01-02 16:00:01")
	require.NotContains(t, out.String(), "2024-01-02T")
	require.Equal(t, "2024-01-02T08:00:00Z", result.Inputs[0].ObservedAt, "格式化不能修改JSON使用的原始时间")
}
