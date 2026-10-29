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

func TestUSTradeCLI(t *testing.T) {
	defer gock.Off()
	for _, real := range []bool{false, true} {
		name, begin, end, ids := "us-trade-monthly.csv", "202608", "202609", "BOPTEXP,BOPTIMP,BOPGSTB,BOPGEXP,BOPGIMP,BOPGTB,BOPSEXP,BOPSIMP,BOPSTB"
		if real {
			name, begin, end, ids = "us-trade-quarterly.csv", "2026Q2", "2026Q3", "EXPGSC1,IMPGSC1,NETEXC,A253RX1Q020SBEA,A255RX1Q020SBEA,A646RX1Q020SBEA,B656RX1Q020SBEA"
		}
		raw, err := os.ReadFile("../../provider/fred/testdata/" + name)
		require.NoError(t, err)
		for _, component := range []string{"total", "goods", "services"} {
			for _, asJSON := range []bool{false, true} {
				gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", ids).MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
				cmd := newUSTradeCommand(real)
				args := []string{"--begin", begin, "--end", end, "--component", component, "--no-cache", "--refresh"}
				if asJSON {
					args = append(args, "--json")
				}
				cmd.SetArgs(args)
				var out bytes.Buffer
				cmd.SetOut(&out)
				require.NoError(t, cmd.Execute())
				if asJSON {
					var got fred.TradeResult
					require.NoError(t, json.Unmarshal(out.Bytes(), &got))
					require.Len(t, got.Observations, 2)
					require.Equal(t, component, got.Component)
					require.Nil(t, got.PublishedAt)
					require.Nil(t, got.RevisionID)
					require.Nil(t, got.Observations[1].Exports)
					require.NotNil(t, got.Observations[0].Exports)
					if real && component != "total" {
						require.Nil(t, got.Observations[0].Balance)
					} else {
						require.NotNil(t, got.Observations[0].Balance)
					}
				} else {
					for _, text := range []string{"出口", "进口", "余额较上期变化", "空值", "FRED", "—"} {
						require.Contains(t, out.String(), text)
					}
					if real {
						require.Contains(t, out.String(), "2017年链式美元")
						require.Contains(t, out.String(), "季调折年")
						require.Contains(t, out.String(), "2026Q2")
					} else {
						require.Contains(t, out.String(), "2026-08")
						require.Contains(t, out.String(), "不折年")
					}
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSTradeDatesAndArguments(t *testing.T) {
	cmd := newUSRealTradeCLI()
	begin, end, err := queryUSRealTradeQuarters(cmd, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "2024Q1", begin)
	require.Equal(t, "2025Q4", end)
	// UTC边界，洛杉矶2025-12-31已进入UTC第一季度。
	_, end, err = queryUSRealTradeQuarters(cmd, time.Date(2025, 12, 31, 20, 0, 0, 0, time.FixedZone("PST", -8*3600)))
	require.NoError(t, err)
	require.Equal(t, "2025Q4", end)
	require.NoError(t, cmd.Flags().Set("end", "1947Q1"))
	begin, end, err = queryUSRealTradeQuarters(cmd, time.Now())
	require.NoError(t, err)
	require.Equal(t, "1947Q1", begin)
	require.Equal(t, begin, end)
	for _, real := range []bool{false, true} {
		badDates := []string{"2026Q0", "202601", "1946Q4", "2999Q1"}
		if !real {
			badDates = []string{"2026Q1", "202613", "199112", "299901"}
		}
		for _, date := range badDates {
			cmd := newUSTradeCommand(real)
			cmd.SetArgs([]string{"--end", date})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.Execute())
		}
		for _, args := range [][]string{{"--component", "other"}, {"extra"}} {
			cmd := newUSTradeCommand(real)
			cmd.SetArgs(args)
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			require.Error(t, cmd.Execute())
		}
	}
}
