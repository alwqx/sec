package macro

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func mockGDPFixture(t *testing.T) {
	t.Helper()
	get := func(path, file string, params map[string]string) {
		gock.New(nbsHost).Get(nbsAPIPath + path).MatchParams(params).Reply(200).BodyString(nbsTestdata(t, "gdp-"+file+".json"))
	}
	get("/new/queryIndexTreeAsync", "root", map[string]string{"code": "2"})
	get("/new/queryIndexTreeAsync", "groups", map[string]string{"code": "2", "pid": "a94b8b7365a94874968cabbe392cf679"})
	get("/new/queryIndexTreeAsync", "catalogs", map[string]string{"code": "2", "pid": "1b1ce0cfd03646dc9e15103ea4c570f6"})
	for _, catalog := range [][2]string{{"real", "b676631776424600bdae363df047559f"}, {"index", "f9b694c9b79e4ce5958bc88c6410fa67"}, {"qoq", "2b0aa179980e41b8b636037ebae01d91"}} {
		get("/new/queryIndicatorsByCid", catalog[0]+"-series", map[string]string{"cid": catalog[1]})
		gock.New(nbsHost).Post(nbsAPIPath + "/stream/esData").Reply(200).BodyString(nbsTestdata(t, "gdp-"+catalog[0]+"-values.json"))
	}
}

func mockHouseholdFixture(t *testing.T) {
	t.Helper()
	data := func(name string) string { return nbsTestdata(t, "household/"+name+".json") }
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("code", "2").Reply(200).BodyString(data("roots"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("pid", "a94b8b7365a94874968cabbe392cf679").Reply(200).BodyString(data("groups"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("pid", "85ee7ccd4212489d9d3b774c8558e274").Reply(200).BodyString(data("catalogs"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndicatorsByCid").MatchParam("cid", "ec2d57ed282f456e8d025aff035b4fad").Reply(200).BodyString(data("income-metadata"))
	gock.New(nbsHost).Post(nbsAPIPath + "/stream/esData").Reply(200).BodyString(data("income-values"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndicatorsByCid").MatchParam("cid", "71e8c15775d242b8b212edb4cab6e808").Reply(200).BodyString(data("consumption-metadata"))
	gock.New(nbsHost).Post(nbsAPIPath + "/stream/esData").Reply(200).BodyString(data("consumption-values"))
}

func mockEnterpriseFixture(t *testing.T) {
	t.Helper()
	data := func(name string) string { return nbsTestdata(t, "enterprise-"+name+".json") }
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("code", "1").Reply(200).BodyString(data("root"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("pid", "fc982599aa684be7969d7b90b1bd0e84").Reply(200).BodyString(data("categories"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("pid", "f484811113a5470892cb52b00c2b35c2").Reply(200).BodyString(data("catalogs"))
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndicatorsByCid").MatchParam("cid", "d760d189206642ce8afad57640408e09").Reply(200).BodyString(data("series"))
	gock.New(nbsHost).Post(nbsAPIPath + "/stream/esData").Reply(200).BodyString(data("current-values"))
}

func TestCNGDPStandardOutput(t *testing.T) {
	defer gock.Off()
	mockGDPFixture(t)
	result := runNBSStandard(t, "gdp", "--begin", "2025Q1", "--end", "2026Q3")
	require.Len(t, result.Series, 7)
	require.Len(t, result.Observations, 7*7)
	require.Equal(t, "cn.gdp.level", result.Series[0].ID)
	require.Equal(t, "2025Q1", result.Observations[0].Period)
	for _, o := range result.Observations {
		if o.SeriesID == "cn.gdp.ytd_yoy_percent" && o.Value != nil {
			require.NotEmpty(t, o.Inputs)
			require.Equal(t, o.Period[:4]+"Q1", o.PeriodBegin)
		}
	}
}

func TestHouseholdStandardOutput(t *testing.T) {
	defer gock.Off()
	mockHouseholdFixture(t)
	result := runNBSStandard(t, "household", "--begin", "2013Q1", "--end", "2026Q4")
	require.Len(t, result.Series, 4)
	require.Equal(t, 56*4, len(result.Observations))
	require.Equal(t, "CNY_per_person", result.Series[0].Unit)
	notes := 0
	for _, o := range result.Observations {
		if o.Note != "" {
			require.Equal(t, "Q4", o.Period[4:])
			notes++
		}
	}
	require.Equal(t, 14*4, notes, "只有全年累计说明自产自用")
}

func TestEnterpriseStandardOutput(t *testing.T) {
	defer gock.Off()
	mockEnterpriseFixture(t)
	result := runNBSStandard(t, "enterprise", "--begin", "202601", "--end", "202609")
	require.Len(t, result.Observations, 36*2)
	byReason := map[string]int{}
	for _, o := range result.Observations {
		if o.MissingReason != nil {
			byReason[*o.MissingReason]++
		}
	}
	require.Equal(t, 8, byReason["january_exempt"], "1月每个指标两条观测")
}

func TestInvestmentStandardJanuary(t *testing.T) {
	result := runNBSStandard(t, "investment", "--begin", "202601", "--end", "202601", "--sector", "manufacturing")
	require.Len(t, result.Observations, 2)
	require.Equal(t, "january_exempt", *result.Observations[0].MissingReason)
	require.Contains(t, result.Observations[0].Note, "1月份免报")
	require.Nil(t, result.Observations[0].Source)
	require.Equal(t, "cn.investment.manufacturing.ytd_level", result.Series[0].ID)
}

func TestNBSAccountsStandardErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		mock func(*testing.T)
		path string
	}{
		{[]string{"gdp", "--begin", "2025Q1", "--end", "2026Q3"}, mockGDPFixture, "/new/queryIndexTreeAsync"},
		{[]string{"household", "--begin", "2013Q1", "--end", "2026Q4"}, mockHouseholdFixture, "/new/queryIndexTreeAsync"},
		{[]string{"enterprise", "--begin", "202601", "--end", "202609"}, mockEnterpriseFixture, "/new/queryIndexTreeAsync"},
		{[]string{"investment", "--begin", "202609", "--end", "202609"}, nil, "/new/queryCMSArticles"},
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
					gock.New(nbsHost).Get(nbsAPIPath + test.path).Reply(503)
				case "writer":
					if test.mock == nil {
						args = []string{"cn", "investment", "--begin", "202601", "--end", "202601", "--standard-json", "--no-cache"}
					} else {
						test.mock(t)
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
