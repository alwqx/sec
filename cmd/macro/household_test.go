package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestHouseholdCLI(t *testing.T) {
	defer gock.Off()
	for _, asJSON := range []bool{false, true} {
		mock := func(name, path string, params map[string]string, post bool) {
			raw, err := os.ReadFile("../../provider/nbs/testdata/household/" + name + ".json")
			require.NoError(t, err)
			request := gock.New("https://data.stats.gov.cn")
			if post {
				request = request.Post(path)
			} else {
				request = request.Get(path)
			}
			for k, v := range params {
				request = request.MatchParam(k, v)
			}
			request.Reply(200).BodyString(string(raw))
		}
		base := "/dg/website/publicrelease/web/external"
		mock("roots", base+"/new/queryIndexTreeAsync", map[string]string{"code": "2"}, false)
		mock("groups", base+"/new/queryIndexTreeAsync", map[string]string{"pid": "a94b8b7365a94874968cabbe392cf679"}, false)
		mock("catalogs", base+"/new/queryIndexTreeAsync", map[string]string{"pid": "85ee7ccd4212489d9d3b774c8558e274"}, false)
		mock("income-metadata", base+"/new/queryIndicatorsByCid", map[string]string{"cid": "ec2d57ed282f456e8d025aff035b4fad"}, false)
		mock("income-values", base+"/stream/esData", nil, true)
		mock("consumption-metadata", base+"/new/queryIndicatorsByCid", map[string]string{"cid": "71e8c15775d242b8b212edb4cab6e808"}, false)
		mock("consumption-values", base+"/stream/esData", nil, true)
		command := newHouseholdCLI()
		args := []string{"--begin", "2013Q1", "--end", "2026Q4", "--no-cache"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		require.NoError(t, command.Execute())
		if asJSON {
			var result nbs.HouseholdResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 56)
			require.Len(t, result.Series, 4)
			require.Equal(t, 4.4, *result.Observations[54].IncomeRealYTDYoY)
			require.Equal(t, 2.7, *result.Observations[54].ConsumptionRealYTDYoY)
			require.Contains(t, out.String(), "raw_values")
			require.Nil(t, result.PublishedAt)
		} else {
			for _, text := range []string{"2026前三季度", "2025全年", "34230", "22354", "4.4", "2.7", "实际同比", "—", "自产自用", "--json"} {
				require.Contains(t, out.String(), text)
			}
			require.NotContains(t, out.String(), "调查方法")
		}
		require.True(t, gock.IsDone())
	}
	root := NewMacroCLI()
	command, _, err := root.Find([]string{"cn", "household"})
	require.NoError(t, err)
	require.Equal(t, "household", command.Name())
}

func TestHouseholdQuarterDefaultsAndArguments(t *testing.T) {
	for _, test := range []struct {
		now time.Time
		end string
	}{
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "2025Q4"},
		{time.Date(2025, 12, 31, 16, 0, 0, 0, time.UTC), "2025Q4"},
		{time.Date(2025, 12, 31, 15, 59, 59, 0, time.UTC), "2025Q3"},
	} {
		begin, end, err := householdQuarters(newHouseholdCLI(), test.now)
		require.NoError(t, err)
		require.Equal(t, test.end, end)
		require.NotEmpty(t, begin)
	}
	command := newHouseholdCLI()
	require.NoError(t, command.Flags().Set("end", "2013Q1"))
	begin, end, err := householdQuarters(command, time.Now())
	require.NoError(t, err)
	require.Equal(t, "2013Q1", begin)
	require.Equal(t, begin, end)
	for _, args := range [][]string{{"extra"}, {"--end", "bad"}, {"--begin", "2012Q4", "--end", "2013Q1"}, {"--begin", "2026Q3", "--end", "2026Q2"}, {"--end", "9999Q4"}} {
		command := newHouseholdCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
}

func TestHouseholdZeroAndNegativeGrowthTable(t *testing.T) {
	zero, negative := 0.0, -5.2
	result := &nbs.HouseholdResult{Observations: []nbs.HouseholdObservation{{Period: "2026Q1", IncomeYTD: &zero, IncomeRealYTDYoY: &negative}}}
	var out bytes.Buffer
	require.NoError(t, printHousehold(&out, result))
	for _, text := range []string{"2026一季度", "0", "-5.2", "—"} {
		require.Contains(t, out.String(), text)
	}
}
