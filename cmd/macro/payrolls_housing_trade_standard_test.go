package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const (
	housingSalesIDs = "HSN1F,EXHOSLUSM495S"
	nominalTradeIDs = "BOPTEXP,BOPTIMP,BOPGSTB,BOPGEXP,BOPGIMP,BOPGTB,BOPSEXP,BOPSIMP,BOPSTB"
	realTradeIDs    = "EXPGSC1,IMPGSC1,NETEXC,A253RX1Q020SBEA,A255RX1Q020SBEA,A646RX1Q020SBEA,B656RX1Q020SBEA"
)

func TestHousingTradeStandardOutput(t *testing.T) {
	for _, test := range []struct {
		args                 []string
		file, ids, firstID   string
		series, observations int
		unit                 string
	}{
		{[]string{"housing", "--begin", "202608", "--end", "202609"}, "us-housing-sales", housingSalesIDs, "us.housing.new-sales.level", 6, 12, "housing_units_thousand"},
		{[]string{"housing", "--begin", "202608", "--end", "202609", "--component", "existing-sales"}, "us-housing-sales", housingSalesIDs, "us.housing.existing-sales.level", 3, 6, "housing_units"},
		{[]string{"house-price", "--begin", "2026Q1", "--end", "2026Q2"}, "us-housing-price", "USSTHPI", "us.house-price.index", 3, 6, "index"},
		{[]string{"trade", "--begin", "202608", "--end", "202609"}, "us-trade-monthly", nominalTradeIDs, "us.exports.total.level", 9, 18, "USD_million"},
		{[]string{"real-trade", "--begin", "2026Q2", "--end", "2026Q3"}, "us-trade-quarterly", realTradeIDs, "us.real-exports.total.level", 9, 18, "USD_billion"},
		{[]string{"real-trade", "--begin", "2026Q2", "--end", "2026Q3", "--component", "goods"}, "us-trade-quarterly", realTradeIDs, "us.real-exports.goods.level", 6, 12, "USD_billion"},
	} {
		t.Run(strings.Join(test.args, "_"), func(t *testing.T) {
			defer gock.Off()
			raw, err := os.ReadFile("../../provider/fred/testdata/" + test.file + ".csv")
			require.NoError(t, err)
			gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", test.ids).Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			cmd.SetArgs(append(append([]string{"us"}, test.args...), "--standard-json", "--no-cache"))
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			var result macrodata.Dataset
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Series, test.series)
			require.Len(t, result.Observations, test.observations)
			require.Equal(t, test.firstID, result.Series[0].ID)
			require.Equal(t, test.unit, result.Series[0].Unit)
			require.Equal(t, test.firstID, result.Observations[0].SeriesID)
			require.NotNil(t, result.Observations[0].Value)
			require.NotNil(t, result.Observations[0].RawValue)
			require.Nil(t, result.Observations[0].PublishedAt)
			require.NotEmpty(t, result.Original)
			last := result.Observations[len(result.Observations)-1]
			require.Equal(t, result.Series[len(result.Series)-1].ID, last.SeriesID)
			require.True(t, gock.IsDone())
		})
	}
}

func TestPayrollsStandardOutput(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/bls/testdata/payrolls-2024-2026.json")
	require.NoError(t, err)
	gock.New("https://api.bls.gov").Post("/publicAPI/v1/timeseries/data/").Reply(200).BodyString(string(raw))
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"us", "nfp", "--begin", "202501", "--end", "202609", "--no-cache", "--standard-json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Series, 2)
	require.Len(t, result.Observations, 42)
	require.Equal(t, "jobs_thousand", result.Series[1].Unit)
	require.Equal(t, 159044.0, *result.Observations[40].Value)
	require.Equal(t, 29.0, *result.Observations[41].Value)
	require.Equal(t, "CES0000000001", result.Observations[41].SourceSeriesID)
	require.True(t, gock.IsDone())
}

func TestPayrollsHousingTradeStandardErrors(t *testing.T) {
	type target struct {
		args           []string
		host, file, id string
	}
	bls := "https://api.bls.gov"
	fred := "https://fred.stlouisfed.org"
	for _, test := range []target{
		{[]string{"payrolls", "--begin", "202501", "--end", "202609"}, bls, "../../provider/bls/testdata/payrolls-2024-2026.json", ""},
		{[]string{"housing", "--begin", "202608", "--end", "202609"}, fred, "../../provider/fred/testdata/us-housing-sales.csv", housingSalesIDs},
		{[]string{"house-price", "--begin", "2026Q1", "--end", "2026Q2"}, fred, "../../provider/fred/testdata/us-housing-price.csv", "USSTHPI"},
		{[]string{"trade", "--begin", "202608", "--end", "202609"}, fred, "../../provider/fred/testdata/us-trade-monthly.csv", nominalTradeIDs},
		{[]string{"real-trade", "--begin", "2026Q2", "--end", "2026Q3"}, fred, "../../provider/fred/testdata/us-trade-quarterly.csv", realTradeIDs},
	} {
		mock := func() *gock.Request {
			if test.host == bls {
				return gock.New(bls).Post("/publicAPI/v1/timeseries/data/")
			}
			return gock.New(fred).Get("/graph/fredgraph.csv").MatchParam("id", test.id)
		}
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			t.Run(test.args[0]+"/"+mode, func(t *testing.T) {
				defer gock.Off()
				args := append(append([]string{"us"}, test.args...), "--standard-json", "--no-cache")
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "http":
					mock().Reply(503)
				case "writer":
					raw, err := os.ReadFile(test.file)
					require.NoError(t, err)
					mock().Reply(200).BodyString(string(raw))
				}
				cmd := NewMacroCLI()
				cmd.SetArgs(args)
				var out bytes.Buffer
				cmd.SetOut(&out)
				if mode == "writer" {
					cmd.SetOut(failingMoneyWriter{})
				}
				cmd.SetErr(&bytes.Buffer{})
				cmd.SilenceUsage = true
				err := cmd.ExecuteContext(ctx)
				require.Error(t, err)
				switch mode {
				case "flags":
					require.ErrorContains(t, err, "none of the others can be")
				case "cancel":
					require.ErrorIs(t, err, context.Canceled)
				case "http":
					require.ErrorContains(t, err, "503")
				case "writer":
					require.ErrorContains(t, err, "测试写入失败")
				}
				if mode != "writer" {
					require.Empty(t, out.String())
				}
				require.True(t, gock.IsDone())
			})
		}
	}
}
