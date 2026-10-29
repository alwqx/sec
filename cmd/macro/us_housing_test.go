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

func TestUSHousingCLI(t *testing.T) {
	defer gock.Off()
	for _, component := range []string{"all", "new-sales", "existing-sales"} {
		for _, asJSON := range []bool{false, true} {
			raw, err := os.ReadFile("../../provider/fred/testdata/us-housing-sales.csv")
			require.NoError(t, err)
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "HSN1F,EXHOSLUSM495S").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
			command := newUSHousingCLI()
			args := []string{"--begin", "202608", "--end", "202609", "--component", component, "--no-cache", "--refresh"}
			if asJSON {
				args = append(args, "--json")
			}
			command.SetArgs(args)
			var out bytes.Buffer
			command.SetOut(&out)
			require.NoError(t, command.Execute())
			if asJSON {
				var result fred.HousingResult
				require.NoError(t, json.Unmarshal(out.Bytes(), &result))
				count := 2
				if component == "all" {
					count = 4
				}
				require.Len(t, result.Observations, count)
				require.Nil(t, result.PublishedAt)
				require.Nil(t, result.RevisionID)
				require.Equal(t, "monthly", result.Frequency)
				require.Contains(t, out.String(), `"growth_annualized": false`)
				if component != "new-sales" {
					require.Contains(t, out.String(), `"first_period": "202509"`)
					require.Contains(t, out.String(), `"unit": "套"`)
					require.Contains(t, out.String(), "National Association of Realtors")
				}
			} else {
				for _, text := range []string{"2026-09", "季调折年", "FRED", "月环比", "严格日历基期"} {
					require.Contains(t, out.String(), text)
				}
				if component != "existing-sales" {
					require.Contains(t, out.String(), "新建单户住宅销售")
					require.Contains(t, out.String(), "676.00")
					require.Contains(t, out.String(), "千套")
				}
				if component != "new-sales" {
					require.Contains(t, out.String(), "成屋销售")
					require.Contains(t, out.String(), "3990000.00")
					require.Contains(t, out.String(), "成屋版权归")
				}
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSHousePriceCLI(t *testing.T) {
	defer gock.Off()
	for _, asJSON := range []bool{false, true} {
		raw, err := os.ReadFile("../../provider/fred/testdata/us-housing-price.csv")
		require.NoError(t, err)
		gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "USSTHPI").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
		command := newUSHousePriceCLI()
		args := []string{"--begin", "2026Q2", "--end", "2026Q3", "--no-cache"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		require.NoError(t, command.Execute())
		if asJSON {
			var result fred.HousingResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, "quarterly", result.Frequency)
			require.Len(t, result.Observations, 2)
			require.Equal(t, 719.87, *result.Observations[0].Value)
			require.Nil(t, result.Observations[1].Value)
			require.False(t, result.Series[0].SeasonallyAdjusted)
			require.False(t, result.Series[0].LevelAnnualized)
		} else {
			for _, text := range []string{"2026Q2", "719.87", "1980Q1=100", "未季调季环比", "再融资评估值", "—"} {
				require.Contains(t, out.String(), text)
			}
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSHousingDatesAndArguments(t *testing.T) {
	command := newUSHousePriceCLI()
	begin, end, err := housingQuarters(command, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "2024Q1", begin)
	require.Equal(t, "2025Q4", end)
	_, end, err = housingQuarters(command, time.Date(2025, 12, 31, 20, 0, 0, 0, time.FixedZone("PST", -8*3600)))
	require.NoError(t, err)
	require.Equal(t, "2025Q4", end)
	require.NoError(t, command.Flags().Set("end", "1975Q1"))
	begin, end, err = housingQuarters(command, time.Now())
	require.NoError(t, err)
	require.Equal(t, "1975Q1", begin)
	require.Equal(t, begin, end)
	for _, price := range []bool{false, true} {
		badDates := []string{"196212", "2026Q1", "202613", "299901"}
		if price {
			badDates = []string{"1974Q4", "202601", "2026Q0", "2999Q1"}
		}
		for _, date := range badDates {
			command := housingCommand(price)
			command.SetArgs([]string{"--end", date})
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			require.Error(t, command.Execute())
		}
		command := housingCommand(price)
		command.SetArgs([]string{"extra"})
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
	command = newUSHousingCLI()
	command.SetArgs([]string{"--component", "other"})
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	require.Error(t, command.Execute())
}

func TestUSHousingOutputZeroAndMissing(t *testing.T) {
	zero := 0.0
	var out bytes.Buffer
	require.NoError(t, housingPrint(&out, &fred.HousingResult{Frequency: "monthly", Observations: []fred.HousingObservation{{HousingBase: fred.HousingBase{Period: "202601", Value: &zero}, Component: "new-sales", Unit: "千套"}, {HousingBase: fred.HousingBase{Period: "202602"}, Component: "new-sales", Unit: "千套"}}}))
	require.Contains(t, out.String(), "0.00")
	require.Contains(t, out.String(), "—")
	require.Contains(t, out.String(), "2026-01")
}
