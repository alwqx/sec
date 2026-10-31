package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const (
	nbsHost    = "https://data.stats.gov.cn"
	nbsAPIPath = "/dg/website/publicrelease/web/external"
)

type nbsFixture struct{ pid, catalogs, cid, series, values string }

var (
	pmiFixture             = nbsFixture{"8f108f3406d7436b82498390d9e08da7", "pmi-catalogs", "93ffbb1aa85740d3aa2618371508b606", "pmi-series", "pmi-values"}
	nonManufacturingLookup = nbsFixture{"8f108f3406d7436b82498390d9e08da7", "pmi-catalogs", "7a64a6e25aec4a8e9dde044ecd9e2cce", "nonmanufacturing-series", "nonmanufacturing-values"}
	unemploymentFixture    = nbsFixture{"e69d8b62991649d4b278e0445bcc0cd0", "unemployment-catalogs", "ee3b7046b390415b9b7745e3d16f6052", "unemployment-series", "unemployment-values"}
	industrialFixture      = nbsFixture{"f484811113a5470892cb52b00c2b35c2", "industrial-catalogs", "3f2e14f0542348ed9fe02476eca3450b", "industrial-series", "industrial-values"}
)

func nbsTestdata(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../provider/nbs/testdata/" + name)
	require.NoError(t, err)
	return string(raw)
}

func mockNBSQuery(t *testing.T, fixture nbsFixture) {
	t.Helper()
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("code", "1").Reply(200).BodyString(nbsTestdata(t, "root.json"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("pid", "fc982599aa684be7969d7b90b1bd0e84").Reply(200).BodyString(nbsTestdata(t, "categories.json"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("pid", fixture.pid).Reply(200).BodyString(nbsTestdata(t, fixture.catalogs+".json"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndicatorsByCid").MatchParam("cid", fixture.cid).Reply(200).BodyString(nbsTestdata(t, fixture.series+".json"))
	gock.New(nbsHost).Post(nbsAPIPath + "/stream/esData").Reply(200).BodyString(nbsTestdata(t, fixture.values+".json"))
}

func mockIndustrialRelease(t *testing.T) {
	t.Helper()
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryCMSArticles").MatchParam("pagenum", "1").Reply(200).BodyString(nbsTestdata(t, "industrial-releases.json"))
	gock.New("https://www.stats.gov.cn").Get("/sj/zxfb/202610/t20261019_1965617.html").Reply(200).BodyString(nbsTestdata(t, "industrial-release.html"))
}

func runNBSStandard(t *testing.T, args ...string) macrodata.Dataset {
	t.Helper()
	cmd := NewMacroCLI()
	cmd.SetArgs(append(append([]string{"cn"}, args...), "--standard-json", "--no-cache"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "CN", result.Region)
	require.True(t, gock.IsDone())
	return result
}

func TestPMIStandardOutput(t *testing.T) {
	defer gock.Off()
	mockNBSQuery(t, pmiFixture)
	result := runNBSStandard(t, "pmi", "--begin", "202607", "--end", "202609")
	require.Len(t, result.Series, 5)
	require.Len(t, result.Observations, 15)
	require.Equal(t, "cn.pmi.manufacturing-pmi.diffusion_index", result.Series[0].ID)
	require.Equal(t, "202607", result.Observations[0].Period)
	require.Equal(t, 49.2, *result.Observations[0].Value)

	mockNBSQuery(t, nonManufacturingLookup)
	result = runNBSStandard(t, "npmi", "--begin", "202607", "--end", "202609")
	require.Len(t, result.Series, 12)
	require.Len(t, result.Observations, 36)
	require.Equal(t, "cn.nonmanufacturing-pmi.business-activity.diffusion_index", result.Series[0].ID)
}

func TestIndustrialStandardOutput(t *testing.T) {
	defer gock.Off()
	mockNBSQuery(t, industrialFixture)
	mockIndustrialRelease(t)
	result := runNBSStandard(t, "ip", "--begin", "202601", "--end", "202609")
	require.Len(t, result.Series, 3)
	require.Len(t, result.Observations, 27)
	february := result.Observations[3:6]
	require.Nil(t, february[0].Value)
	require.Contains(t, february[0].Note, "1—2月")
	require.Equal(t, 6.3, *february[1].Value)
	require.Equal(t, "202601", february[1].PeriodBegin)
	require.Equal(t, 0.76, *february[2].Value)
	require.NotNil(t, february[2].PublishedAt)
	require.Nil(t, february[1].PublishedAt)
}

func TestUnemploymentStandardOutput(t *testing.T) {
	defer gock.Off()
	mockNBSQuery(t, unemploymentFixture)
	result := runNBSStandard(t, "unemployment", "--begin", "202301", "--end", "202409", "--age-groups")
	ids := map[string]bool{}
	for _, s := range result.Series {
		ids[s.ID] = true
	}
	require.True(t, ids["cn.unemployment.urban-surveyed.national-surveyed.rate"])
	require.True(t, ids["cn.unemployment.age-16-24.legacy-including-students.rate"])
	require.True(t, ids["cn.unemployment.age-16-24.excluding-students-since-202312.rate"])
	paused := 0
	for _, o := range result.Observations {
		if o.SeriesID == "cn.unemployment.age-16-24.publication-paused-202307-202311.rate" {
			require.Nil(t, o.Value)
			require.Equal(t, "publication_paused", *o.MissingReason)
			paused++
		}
	}
	require.Equal(t, 2, paused)
}

func TestNBSActivityStandardErrors(t *testing.T) {
	for _, test := range []struct {
		args    []string
		fixture nbsFixture
		release bool
	}{
		{[]string{"pmi", "--begin", "202607", "--end", "202609"}, pmiFixture, false},
		{[]string{"nonmanufacturing-pmi", "--begin", "202607", "--end", "202609"}, nonManufacturingLookup, false},
		{[]string{"industrial", "--begin", "202601", "--end", "202609"}, industrialFixture, true},
		{[]string{"unemployment", "--begin", "202301", "--end", "202409"}, unemploymentFixture, false},
	} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			t.Run(test.args[0]+"/"+mode, func(t *testing.T) {
				defer gock.Off()
				args := append(append([]string{"cn"}, test.args...), "--standard-json", "--no-cache")
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "http":
					gock.New(nbsHost).Get(nbsAPIPath + "/new/queryIndexTreeAsync").Reply(503)
				case "writer":
					mockNBSQuery(t, test.fixture)
					if test.release {
						mockIndustrialRelease(t)
					}
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
