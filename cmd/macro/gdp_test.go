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

func TestGDPCLI(t *testing.T) {
	defer gock.Off()
	for _, asJSON := range []bool{false, true} {
		origin := "https://data.stats.gov.cn"
		prefix := "/dg/website/publicrelease/web/external"
		mock := func(path, fixture string, params map[string]string) {
			raw, err := os.ReadFile("../../provider/nbs/testdata/gdp-" + fixture + ".json")
			require.NoError(t, err)
			gock.New(origin).Get(prefix + path).MatchParams(params).Reply(200).BodyString(string(raw))
		}
		mock("/new/queryIndexTreeAsync", "root", map[string]string{"code": "2"})
		mock("/new/queryIndexTreeAsync", "groups", map[string]string{"code": "2", "pid": "a94b8b7365a94874968cabbe392cf679"})
		mock("/new/queryIndexTreeAsync", "catalogs", map[string]string{"code": "2", "pid": "1b1ce0cfd03646dc9e15103ea4c570f6"})
		for cid, name := range map[string]string{"b676631776424600bdae363df047559f": "real", "f9b694c9b79e4ce5958bc88c6410fa67": "index", "2b0aa179980e41b8b636037ebae01d91": "qoq"} {
			mock("/new/queryIndicatorsByCid", name+"-series", map[string]string{"cid": cid})
			raw, err := os.ReadFile("../../provider/nbs/testdata/gdp-" + name + "-values.json")
			require.NoError(t, err)
			ids := map[string][]string{"real": {"b704155cd926437b8ee9c65fe058210d", "e33efbac437d4d10b5ccae6ad9d923b9"}, "index": {"170e7f00f8c24ede863c0526b42ae81f", "f5b82b2b6ad345a29a337d256a9d5ded"}, "qoq": {"88bcaf76058e49b08a45b14a204b2d3d"}}
			body := map[string]any{"cid": cid, "rootId": "1b1ce0cfd03646dc9e15103ea4c570f6", "indicatorIds": ids[name], "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{"202501SS-202603SS"}, "showType": 1}
			gock.New(origin).Post(prefix + "/stream/esData").JSON(body).Reply(200).BodyString(string(raw))
		}
		cmd := NewMacroCLI()
		args := []string{"cn", "gdp", "--begin", "2025Q1", "--end", "2026Q3", "--no-cache"}
		if asJSON {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		if asJSON {
			var result nbs.GDPResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 7)
			require.Equal(t, 4.5, *result.Observations[6].YoY)
			require.Contains(t, out.String(), `"real_base_year": null`)
		} else {
			require.Contains(t, out.String(), "2026Q3")
			require.Contains(t, out.String(), "371684.6")
			require.Contains(t, out.String(), "不变价金额按不同基期分段计算")
		}
	}
	require.True(t, gock.IsDone())
}

func TestGDPQuarterDefaults(t *testing.T) {
	cmd := newGDPCLI()
	// UTC3月31日晚已是北京时间4月1日，应使用刚结束的一季度。
	begin, end, err := gdpQuarters(cmd, time.Date(2026, 3, 31, 18, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "2024Q2", begin)
	require.Equal(t, "2026Q1", end)
	require.NoError(t, cmd.Flags().Set("end", "1992Q2"))
	begin, end, err = gdpQuarters(cmd, time.Now())
	require.NoError(t, err)
	require.Equal(t, "1992Q1", begin)
	require.Equal(t, "1992Q2", end)
	for _, args := range [][]string{{"--begin", "202501"}, {"--end", "2025Q5"}, {"--begin", "2026Q2", "--end", "2026Q1"}, {"extra"}} {
		root := NewMacroCLI()
		root.SetArgs(append([]string{"cn", "gdp"}, args...))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Error(t, root.Execute())
	}
}

func TestGDPOutputMissing(t *testing.T) {
	zero := 0.0
	var out bytes.Buffer
	require.NoError(t, printGDP(&out, &nbs.GDPResult{Observations: []nbs.GDPObservation{{Period: "1992Q1", YoY: &zero}}}))
	require.Contains(t, out.String(), "0.0")
	require.Contains(t, out.String(), "—")
}
