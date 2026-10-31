package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestPropertyCLI(t *testing.T) {
	defer gock.Off()
	for _, output := range []string{"table", "json", "standard"} {
		origin := "https://data.stats.gov.cn"
		prefix := "/dg/website/publicrelease/web/external"
		mock := func(path, file string, params map[string]string) {
			raw, err := os.ReadFile("../../provider/nbs/testdata/" + file + ".json")
			require.NoError(t, err)
			gock.New(origin).Get(prefix + path).MatchParams(params).Reply(200).BodyString(string(raw))
		}
		mock("/new/queryIndexTreeAsync", "root", map[string]string{"code": "1"})
		mock("/new/queryIndexTreeAsync", "categories", map[string]string{"code": "1", "pid": "fc982599aa684be7969d7b90b1bd0e84"})
		mock("/new/queryIndexTreeAsync", "property-catalogs", map[string]string{"code": "1", "pid": "a7d36f218969426daaeeddd1c6c5823a"})
		mock("/new/queryIndicatorsByCid", "property-starts-area-series", map[string]string{"cid": "cac0766314e045ea82f69886aabd31b0", "pageSize": "1000"})
		raw, err := os.ReadFile("../../provider/nbs/testdata/property-starts-area-values.json")
		require.NoError(t, err)
		body := map[string]any{"cid": "cac0766314e045ea82f69886aabd31b0", "rootId": "a7d36f218969426daaeeddd1c6c5823a", "indicatorIds": []string{"17660a75e3494e389bad3658ff124995", "d0bfd7e4b56a4bb98cea7cfd141475d9"}, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{"202510MM-202609MM"}, "showType": 1}
		gock.New(origin).Post(prefix + "/stream/esData").JSON(body).Reply(200).BodyString(string(raw))
		command := newPropertyCLI()
		args := []string{"--end", "202609", "--component", "starts-area", "--no-cache", "--refresh"}
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
			require.Len(t, result.Observations, 18)
			require.Equal(t, "square_meters_10000", result.Series[0].Unit)
			require.Equal(t, "year-to-date", result.Series[0].PeriodBasis)
			require.Equal(t, "202601", result.Observations[16].PeriodBegin)
			require.Equal(t, "202609", result.Observations[16].PeriodEnd)
			require.Equal(t, 34031.30, *result.Observations[16].Value)
			require.Equal(t, -24.9, *result.Observations[17].Value)
			require.Contains(t, out.String(), `"published_at": null`)
			require.NotEmpty(t, result.Observations[16].SourceSeriesID)
		} else if output == "json" {
			var result nbs.PropertyResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 9)
			require.Equal(t, "starts-area", result.Observations[8].Component)
			require.Equal(t, 34031.30, *result.Observations[8].YTDAmount)
			require.Equal(t, -24.9, *result.Observations[8].YTDGrowth)
			require.Contains(t, out.String(), `"published_at": null`)
			require.Contains(t, out.String(), `"ytd_amount": null`)
		} else {
			require.Contains(t, out.String(), "2026-01 至 2026-09")
			require.Contains(t, out.String(), "房屋新开工面积")
			require.Contains(t, out.String(), "34031.30")
			require.Contains(t, out.String(), "-24.9")
			require.Contains(t, out.String(), "1—2月合并累计")
			require.NotContains(t, out.String(), "202601-202609")
		}
	}
	require.True(t, gock.IsDone())
}

func TestPropertyOutputMissingZero(t *testing.T) {
	zero := 0.0
	var out bytes.Buffer
	require.NoError(t, printProperty(&out, &nbs.PropertyResult{Observations: []nbs.PropertyObservation{{Component: "sales-value", YTDPeriod: "202601-202602", YTDAmount: &zero, YTDGrowth: &zero, AmountUnit: "亿元"}, {Component: "sales-area", YTDPeriod: "202601-202601", AmountUnit: "万平方米"}}}))
	for _, text := range []string{"新建商品房销售额", "新建商品房销售面积", "2026-01 至 2026-02", "亿元", "万平方米", "0.00", "0.0", "—", "累计同比"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	require.NoError(t, printProperty(&out, nil))
	require.Contains(t, out.String(), "暂无")
}

func TestPropertyCLIInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"--begin", "202613"}, {"--begin", "202609", "--end", "202601"}, {"--begin", "199912", "--end", "200001"}, {"--component", "residential"}, {"extra"}} {
		command := newPropertyCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
}
