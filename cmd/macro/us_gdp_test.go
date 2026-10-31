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

func TestUSGDPCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/us-gdp.csv")
	require.NoError(t, err)
	for _, output := range []string{"table", "json", "standard"} {
		gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "GDPC1,A191RL1Q225SBEA").MatchHeader("User-Agent", "curl").Reply(200).BodyString(string(raw))
		cmd := NewMacroCLI()
		args := []string{"us", "gdp", "--begin", "2026Q1", "--end", "2026Q2", "--no-cache"}
		if output == "json" {
			args = append(args, "--json")
		}
		if output == "standard" {
			args = append(args, "--standard-json")
		}
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		if output == "standard" {
			var result macrodata.Dataset
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 8)
			require.Equal(t, "quarterly", result.Series[0].Frequency)
			require.True(t, result.Series[0].Annualized)
			require.True(t, result.Series[1].Annualized)
			require.False(t, result.Series[2].Annualized)
			require.Equal(t, 2017, *result.Series[0].ReferenceYear)
			require.Equal(t, 24408.011, *result.Observations[4].Value)
			require.Equal(t, 2.2, *result.Observations[5].Value)
			require.Equal(t, "2026Q1", result.Observations[6].Base.Period)
		} else if output == "json" {
			var result fred.GDPResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 2)
			require.Equal(t, 24408.011, *result.Observations[1].Level)
			require.Equal(t, 2.2, *result.Observations[1].QoQAnnualized)
			require.Equal(t, "GDPC1", result.SeriesID)
			require.Equal(t, 2017, result.ReferenceYear)
			require.Nil(t, result.PublishedAt)
			require.False(t, result.VintagesRequested)
		} else {
			require.Contains(t, out.String(), "2026Q1")
			require.Contains(t, out.String(), "24408.011")
			require.Contains(t, out.String(), "季调折年水平")
			require.Contains(t, out.String(), "环比年化")
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSGDPVintagesCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/fred/testdata/us-gdp.csv")
	require.NoError(t, err)
	workbook, err := os.ReadFile("../../provider/fred/testdata/us-gdp-vintages.xlsx")
	require.NoError(t, err)
	for _, output := range []string{"table", "json", "standard"} {
		gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(200).BodyString(string(raw))
		gock.New("https://apps.bea.gov").Get("/national/xls/gdp-gdi-vintage-history.xlsx").Reply(200).Body(bytes.NewReader(workbook))
		cmd := NewMacroCLI()
		args := []string{"us", "gdp", "--begin", "2026Q2", "--end", "2026Q2", "--vintages", "--no-cache"}
		if output == "json" {
			args = append(args, "--json")
		}
		if output == "standard" {
			args = append(args, "--standard-json")
		}
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		if output == "standard" {
			var result macrodata.Dataset
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 4, "发布档案不混入当前序列")
			var original fred.GDPResult
			require.NoError(t, json.Unmarshal(result.Original, &original))
			require.True(t, original.VintagesRequested)
			require.Len(t, original.Observations[0].Vintages, 3)
			require.Nil(t, original.Observations[0].Vintages[0].RealLevel)
			require.Equal(t, "2026-07-30", original.Observations[0].Vintages[0].ReleaseDate)
		} else if output == "json" {
			var result fred.GDPResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.True(t, result.VintagesRequested)
			require.Equal(t, "2002Q1", result.VintagesFirstPeriod)
			require.Equal(t, "2026Q2", result.VintagesLastPeriod)
			require.Contains(t, result.VintagesNominalUnit, "当期美元")
			require.Len(t, result.Observations[0].Vintages, 3)
			require.Nil(t, result.Observations[0].Vintages[0].RealLevel)
			require.Equal(t, "2026-07-30", result.Observations[0].Vintages[0].ReleaseDate)
		} else {
			require.Contains(t, out.String(), "初值（Advance）")
			require.Contains(t, out.String(), "二次估计（Second）")
			require.Contains(t, out.String(), "三次估计（Third）")
			require.Contains(t, out.String(), "未发布GDI")
			require.NotContains(t, out.String(), "GDI not published")
			require.Contains(t, out.String(), "real_level=null")
		}
	}
	require.True(t, gock.IsDone())
}

func TestUSGDPVintagesFailureIsError(t *testing.T) {
	defer gock.Off()
	gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(200).BodyString("DATE,GDPC1,A191RL1Q225SBEA\n2025-01-01,100,2\n")
	gock.New("https://apps.bea.gov").Get("/national/xls/gdp-gdi-vintage-history.xlsx").Reply(503)
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"us", "gdp", "--begin", "2025Q1", "--end", "2025Q1", "--vintages", "--no-cache", "--json"})
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorContains(t, cmd.Execute(), "503")
	require.Empty(t, out.String())
	require.True(t, gock.IsDone())
}

func TestUSGDPQuarterDefaultsAndArgs(t *testing.T) {
	// 北京已进入四月，UTC仍是三月，默认只能截至上一年Q4。
	cmd := newUSGDPCLI()
	begin, end, err := queryUSGDPQuarters(cmd, time.Date(2025, 4, 1, 0, 30, 0, 0, time.FixedZone("CST", 8*3600)))
	require.NoError(t, err)
	require.Equal(t, "2023Q1", begin)
	require.Equal(t, "2024Q4", end)
	cmd = newUSGDPCLI()
	begin, end, err = queryUSGDPQuarters(cmd, time.Date(2025, 4, 1, 0, 30, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "2023Q2", begin)
	require.Equal(t, "2025Q1", end)
	cmd = newUSGDPCLI()
	require.NoError(t, cmd.Flags().Set("end", "1947Q1"))
	begin, end, err = queryUSGDPQuarters(cmd, time.Now())
	require.NoError(t, err)
	require.Equal(t, "1947Q1", begin)
	require.Equal(t, begin, end)
	for _, args := range [][]string{{"--begin", "1946Q4"}, {"--begin", "2025Q5"}, {"--begin", "2025Q2", "--end", "2025Q1"}, {"extra"}} {
		root := NewMacroCLI()
		root.SetArgs(append([]string{"us", "gdp"}, args...))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Error(t, root.Execute())
	}
}
