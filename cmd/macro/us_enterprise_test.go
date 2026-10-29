package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSEnterpriseCLI(t *testing.T) {
	defer gock.Off()
	for _, test := range []struct {
		metric, id, begin, end, name string
		level                        float64
	}{
		{"profit", "CPROFIT", "2026Q2", "2026Q3", "企业税前利润", 4709.453},
		{"inventory", "BUSINV", "202608", "202609", "商业库存", 2818757},
		{"new-orders", "AMTMNO", "202608", "202609", "制造业新订单", 663491},
	} {
		raw, err := os.ReadFile("../../provider/fred/testdata/us-enterprise-" + test.metric + ".csv")
		require.NoError(t, err)
		for _, asJSON := range []bool{false, true} {
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", test.id).MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			command := usEnterpriseCommand(test.metric)
			args := []string{"--begin", test.begin, "--end", test.end, "--no-cache", "--refresh"}
			if asJSON {
				args = append(args, "--json")
			}
			command.SetArgs(args)
			var out bytes.Buffer
			command.SetOut(&out)
			require.NoError(t, command.Execute())
			if asJSON {
				var result fred.EnterpriseResult
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
				require.NotNil(t, result.Observations[0].YoYBase.Level)
				require.Contains(t, out.String(), "当前快照未提供数值")
			} else {
				for _, text := range []string{test.id, test.name, "同比", "未年化", "--json", "—"} {
					require.Contains(t, out.String(), text)
				}
				require.NotContains(t, out.String(), "当前快照未提供数值")
				require.NotContains(t, out.String(), "严格上期基数缺失或非正")
				if test.metric == "profit" {
					require.Contains(t, out.String(), "季调折年")
					require.Contains(t, out.String(), "4709.453")
					require.Contains(t, out.String(), "季环比")
				} else {
					require.Contains(t, out.String(), "未折年")
					require.Contains(t, out.String(), "2026-08")
					require.Contains(t, out.String(), "月环比")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSEnterpriseDefaultsAndValidation(t *testing.T) {
	command := newUSCorporateProfitsCLI()
	begin, end, err := enterpriseQueryQuarters(command, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "2024Q1", begin)
	require.Equal(t, "2025Q4", end)
	_, end, err = enterpriseQueryQuarters(command, time.Date(2025, 12, 31, 20, 0, 0, 0, time.FixedZone("PST", -8*3600)))
	require.NoError(t, err)
	require.Equal(t, "2025Q4", end)
	require.NoError(t, command.Flags().Set("end", "1947Q1"))
	begin, end, err = enterpriseQueryQuarters(command, time.Now())
	require.NoError(t, err)
	require.Equal(t, begin, end)
	require.Equal(t, "1947Q1", begin)
	for _, metric := range []string{"profit", "inventory", "new-orders"} {
		begin, end := "199112", "199201"
		if metric == "profit" {
			begin, end = "1946Q4", "1947Q1"
		}
		cases := [][]string{{"--begin", begin, "--end", end}, {"--end", "bad"}, {"--end", "299901"}, {"extra"}}
		if metric == "profit" {
			cases = append(cases, []string{"--end", "2999Q1"}, []string{"--begin", "2026Q2", "--end", "2026Q1"})
		} else {
			cases = append(cases, []string{"--begin", "202602", "--end", "202601"})
		}
		for _, args := range cases {
			command := usEnterpriseCommand(metric)
			command.SetArgs(args)
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			require.Error(t, command.Execute())
		}
		if metric != "profit" {
			earliest := "199201"
			if metric == "new-orders" {
				earliest = "199202"
			}
			command := usEnterpriseCommand(metric)
			require.NoError(t, command.Flags().Set("end", earliest))
			begin, end, err := queryUSMonths(command, time.Now(), earliest)
			require.NoError(t, err)
			require.Equal(t, earliest, begin)
			require.Equal(t, begin, end)
		}
	}
	root := NewMacroCLI()
	for _, name := range []string{"corporate-profits", "business-inventories", "new-orders"} {
		command, _, err := root.Find([]string{"us", name})
		require.NoError(t, err)
		require.Equal(t, name, command.Name())
	}
}

func TestUSEnterpriseCompactOutput(t *testing.T) {
	zero, loss := 0.0, -10.0
	result := &fred.EnterpriseResult{Name: "测试利润", SeriesID: "CPROFIT", Frequency: "quarterly", Unit: "十亿美元", LevelAnnualized: true, Observations: []fred.EnterpriseObservation{{EnterpriseValue: fred.EnterpriseValue{Period: "2026Q1", Level: &zero}, Note: "不应出现在表格的长说明"}, {EnterpriseValue: fred.EnterpriseValue{Period: "2026Q2", Level: &loss}}, {EnterpriseValue: fred.EnterpriseValue{Period: "2026Q3"}}}}
	var out bytes.Buffer
	require.NoError(t, printUSEnterprise(&out, result))
	for _, text := range []string{"0.000", "-10.000", "—"} {
		require.Contains(t, out.String(), text)
	}
	require.NotContains(t, out.String(), result.Observations[0].Note)
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, "水平（") {
			require.NotContains(t, line, "说明")
			require.NotContains(t, line, "调查范围")
		}
	}
}
