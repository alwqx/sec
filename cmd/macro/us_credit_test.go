package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSCreditCLI(t *testing.T) {
	defer gock.Off()
	for _, test := range []struct {
		metric, id, begin, end string
		level                  float64
	}{
		{"bank", "LOANINV", "202609", "202610", 19882.6426},
		{"standards", "DRTSCILM", "2026Q3", "2026Q4", 0},
		{"debt-service", "TDSP", "2026Q2", "2026Q3", 11.111439},
	} {
		raw, err := os.ReadFile("../../provider/fred/testdata/us-credit-" + test.metric + ".csv")
		require.NoError(t, err)
		for _, output := range []string{"table", "json", "standard"} {
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", test.id).MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			command := usCreditCommand(test.metric)
			args := []string{"--begin", test.begin, "--end", test.end, "--no-cache", "--refresh"}
			if output == "json" {
				args = append(args, "--json")
			}
			if output == "standard" {
				args = append(args, "--standard-json")
			}
			command.SetArgs(args)
			var out bytes.Buffer
			command.SetOut(&out)
			require.NoError(t, command.Execute())
			if output == "standard" {
				var result macrodata.Dataset
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				require.Len(t, result.Series, 3)
				require.Len(t, result.Observations, 6)
				require.Equal(t, test.level, *result.Observations[0].Value)
				require.Equal(t, test.id, result.Observations[0].SourceSeriesID)
				require.NotNil(t, result.Observations[0].RawValue)
				require.Nil(t, result.Observations[3].Value)
				require.Equal(t, "source_value_missing", *result.Observations[3].MissingReason)
				require.Equal(t, "current_value_missing", *result.Observations[4].MissingReason)
				require.Nil(t, result.Observations[0].PublishedAt)
				require.False(t, result.Series[1].Annualized)
				require.Equal(t, "derived", result.Series[1].ValueOrigin)
				require.False(t, result.Series[0].Annualized)
				if test.metric == "bank" {
					require.Equal(t, "USD_billion", result.Series[0].Unit)
					require.Equal(t, "pro_rata_average_of_weekly_levels", result.Series[0].PeriodBasis)
					require.Equal(t, "level_ratio_not_break_adjusted", result.Series[1].ChangeMethod)
					require.Equal(t, "mom_percent", result.Series[2].Measure)
				} else {
					require.Equal(t, "percent", result.Series[0].Unit)
					require.Equal(t, "percentage_points", result.Series[1].Unit)
					require.Equal(t, "qoq_percentage_points", result.Series[2].Measure)
					if test.metric == "standards" {
						require.Equal(t, "survey_quarter", result.Series[0].PeriodBasis)
						require.Equal(t, "not-seasonally-adjusted", result.Series[0].SeasonalAdjustment)
						require.Equal(t, "available", result.Observations[0].Status)
					} else {
						require.Equal(t, "credit_bureau_based_dsr", result.Series[0].Methodology)
					}
				}
				var original fred.CreditResult
				require.NoError(t, json.Unmarshal(result.Original, &original))
				require.Equal(t, test.id, original.SeriesID)
				require.Len(t, original.Observations, 2)
			} else if output == "json" {
				var result fred.CreditResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				require.Equal(t, test.id, result.SeriesID)
				require.Len(t, result.Observations, 2)
				require.Equal(t, test.level, *result.Observations[0].Level)
				require.Nil(t, result.Observations[1].Level)
				require.Nil(t, result.PublishedAt)
				require.Nil(t, result.RevisionID)
				require.NotEmpty(t, result.Definition)
				require.NotEmpty(t, result.MethodologyNotes)
				require.NotNil(t, result.Observations[0].PreviousBase.Level)
				if test.metric == "bank" {
					require.NotNil(t, result.Observations[0].PreviousChangePercent)
					require.Nil(t, result.Observations[0].PreviousChangePP)
				} else {
					require.NotNil(t, result.Observations[0].PreviousChangePP)
					require.Nil(t, result.Observations[0].PreviousChangePercent)
				}
				require.Contains(t, out.String(), "当前快照未提供数值")
			} else {
				for _, text := range []string{test.id, "未折年", "—", "--json"} {
					require.Contains(t, out.String(), text)
				}
				require.NotContains(t, out.String(), "当前快照未提供数值")
				if test.metric == "bank" {
					for _, text := range []string{"月平均余额", "2026-09", "同比（%）", "月环比（%）", "未另作断点调整", "1947-01"} {
						require.Contains(t, out.String(), text)
					}
					require.NotContains(t, out.String(), "百分点")
				} else {
					require.Contains(t, out.String(), "百分点")
					require.NotContains(t, out.String(), "同比（%）")
					if test.metric == "standards" {
						for _, text := range []string{"未季调", "调查季度", "负值净放松", "0.0000"} {
							require.Contains(t, out.String(), text)
						}
					} else {
						require.Contains(t, out.String(), "征信数据方法")
						require.Contains(t, out.String(), "2005Q1")
					}
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSCreditDatesAndArguments(t *testing.T) {
	for _, test := range []struct{ metric, earliest string }{{"standards", "1990Q2"}, {"debt-service", "2005Q1"}} {
		command := usCreditCommand(test.metric)
		begin, end, err := creditQueryQuarters(command, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), test.metric)
		require.NoError(t, err)
		require.Equal(t, "2024Q1", begin)
		require.Equal(t, "2025Q4", end)
		_, end, err = creditQueryQuarters(command, time.Date(2025, 12, 31, 20, 0, 0, 0, time.FixedZone("PST", -8*3600)), test.metric)
		require.NoError(t, err)
		require.Equal(t, "2025Q4", end)
		require.NoError(t, command.Flags().Set("end", test.earliest))
		begin, end, err = creditQueryQuarters(command, time.Now(), test.metric)
		require.NoError(t, err)
		require.Equal(t, test.earliest, begin)
		require.Equal(t, begin, end)
	}
	command := newUSBankCreditCLI()
	require.NoError(t, command.Flags().Set("end", "194701"))
	begin, end, err := queryUSMonths(command, time.Now(), "194701")
	require.NoError(t, err)
	require.Equal(t, "194701", begin)
	require.Equal(t, begin, end)
	for _, metric := range []string{"bank", "standards", "debt-service"} {
		bad := [][]string{{"--end", "bad"}, {"extra"}}
		if metric == "bank" {
			bad = append(bad, []string{"--begin", "194612", "--end", "194701"}, []string{"--begin", "202602", "--end", "202601"}, []string{"--end", "299901"})
		} else {
			earliest, before := "1990Q2", "1990Q1"
			if metric == "debt-service" {
				earliest, before = "2005Q1", "2004Q4"
			}
			bad = append(bad, []string{"--begin", before, "--end", earliest}, []string{"--begin", "2026Q2", "--end", "2026Q1"}, []string{"--end", "2999Q1"}, []string{"--end", "2026Q0"})
		}
		for _, args := range bad {
			command := usCreditCommand(metric)
			command.SetArgs(args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			require.Error(t, command.Execute())
		}
	}
	root := NewMacroCLI()
	for _, name := range []string{"bank-credit", "lending-standards", "debt-service"} {
		command, _, err := root.Find([]string{"us", name})
		require.NoError(t, err)
		require.Equal(t, name, command.Name())
	}
}

func TestUSCreditZeroNegativeAndCompactTable(t *testing.T) {
	zero, negative, change := 0.0, -10.0, -5.0
	result := &fred.CreditResult{Metric: "standards", Frequency: "quarterly", ChangeUnit: "percentage_points", Unit: "%", Observations: []fred.CreditObservation{{CreditValue: fred.CreditValue{Period: "2026Q1", Level: &zero}, PreviousChangePP: &change, Note: "不要将长说明放入表格"}, {CreditValue: fred.CreditValue{Period: "2026Q2", Level: &negative}}, {CreditValue: fred.CreditValue{Period: "2026Q3"}}}}
	var out bytes.Buffer
	require.NoError(t, printUSCredit(&out, result))
	for _, text := range []string{"0.0000", "-10.0000", "-5.00", "—", "百分点"} {
		require.Contains(t, out.String(), text)
	}
	require.NotContains(t, out.String(), result.Observations[0].Note)
}
