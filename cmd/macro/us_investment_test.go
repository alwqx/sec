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

func TestUSInvestmentCLI(t *testing.T) {
	defer gock.Off()
	for _, test := range []struct {
		option, id string
		quarterly  bool
		level      float64
	}{{"housing-starts", "HOUST", false, 1221}, {"building-permits", "PERMIT", false, 1398}, {"real", "Y033RX1Q020SBEA", true, 1598.303}, {"nominal", "Y033RC1Q027SBEA", true, 1922.516}} {
		file, ids, begin, end, flag := "housing", "HOUST,PERMIT", "202609", "202610", "--component"
		if test.quarterly {
			file, ids, begin, end, flag = "equipment", "Y033RC1Q027SBEA,Y033RX1Q020SBEA", "2026Q2", "2026Q3", "--price-basis"
		}
		raw, err := os.ReadFile("../../provider/fred/testdata/us-investment-" + file + ".csv")
		require.NoError(t, err)
		for _, asJSON := range []bool{false, true} {
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", ids).MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			cmd := newUSInvestmentCLI()
			if test.quarterly {
				cmd = newUSEquipmentInvestmentCLI()
			}
			args := []string{"--begin", begin, "--end", end, flag, test.option, "--no-cache", "--refresh"}
			if asJSON {
				args = append(args, "--json")
			}
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			if asJSON {
				var result fred.InvestmentResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				require.Equal(t, test.id, result.SeriesID)
				require.Len(t, result.Observations, 2)
				require.Equal(t, test.level, *result.Observations[0].Level)
				require.Nil(t, result.Observations[1].Level)
				require.Nil(t, result.PublishedAt)
				require.Nil(t, result.RevisionID)
				require.True(t, result.LevelAnnualized)
				require.False(t, result.GrowthAnnualized)
				require.NotNil(t, result.Observations[0].PreviousBase.Level)
			} else {
				for _, text := range []string{test.id, "季调折年", "增速未年化", "严格基期", "当期水平缺失", "—"} {
					require.Contains(t, out.String(), text)
				}
				if test.quarterly {
					require.Contains(t, out.String(), "2026Q2")
					require.Contains(t, out.String(), "十亿美元")
					if test.option == "real" {
						require.Contains(t, out.String(), "2017年链式美元")
					}
				} else {
					require.Contains(t, out.String(), "2026-09")
					require.Contains(t, out.String(), "千套")
					require.Contains(t, out.String(), "住房套数不是建筑物数量或投资金额")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSInvestmentDefaultsClampsAndArguments(t *testing.T) {
	for _, test := range []struct{ basis, end string }{{"real", "2007Q1"}, {"nominal", "1947Q1"}} {
		cmd := newUSEquipmentInvestmentCLI()
		require.NoError(t, cmd.Flags().Set("end", test.end))
		begin, end, err := investmentQueryQuarters(cmd, time.Now(), test.basis)
		require.NoError(t, err)
		require.Equal(t, test.end, begin)
		require.Equal(t, begin, end)
	}
	cmd := newUSEquipmentInvestmentCLI()
	begin, end, err := investmentQueryQuarters(cmd, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "real")
	require.NoError(t, err)
	require.Equal(t, "2024Q1", begin)
	require.Equal(t, "2025Q4", end)
	_, end, err = investmentQueryQuarters(cmd, time.Date(2025, 12, 31, 20, 0, 0, 0, time.FixedZone("PST", -8*3600)), "real")
	require.NoError(t, err)
	require.Equal(t, "2025Q4", end)
	for _, earliest := range []string{"195901", "196001"} {
		cmd := newUSInvestmentCLI()
		require.NoError(t, cmd.Flags().Set("end", earliest))
		begin, end, err := queryUSMonths(cmd, time.Now(), earliest)
		require.NoError(t, err)
		require.Equal(t, earliest, begin)
		require.Equal(t, begin, end)
	}
	for _, quarterly := range []bool{false, true} {
		bad := [][]string{{"--end", "2026Q0"}, {"--end", "2006Q4"}, {"--end", "2999Q1"}, {"--price-basis", "other"}, {"--begin", "2026Q2", "--end", "2026Q1"}, {"extra"}}
		if !quarterly {
			bad = [][]string{{"--end", "202613"}, {"--begin", "195812", "--end", "195901"}, {"--end", "299901"}, {"--component", "equipment"}, {"--begin", "195912", "--end", "196001", "--component", "building-permits"}, {"--begin", "202602", "--end", "202601"}, {"extra"}}
		}
		for _, args := range bad {
			cmd := investmentCommand(quarterly)
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.Execute())
		}
	}
}
