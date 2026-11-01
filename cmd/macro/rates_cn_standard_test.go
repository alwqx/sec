package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const chinabondHost = "https://yield.chinabond.com.cn"

func mockTreasury(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../../provider/chinabond/testdata/treasury-20060301-20060307.json")
	require.NoError(t, err)
	gock.New(chinabondHost).Get("/cbweb-czb-web/czb/historyQuery").MatchParam("startDate", "2006-03-01").MatchParam("endDate", "2006-03-07").Reply(200).BodyString(string(raw))
}

func pbcSample(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("../../provider/pbc/testdata/" + name + ".html")
	require.NoError(t, err)
	return string(raw)
}

func mockLPR(t *testing.T) {
	t.Helper()
	link := func(date, href string) string {
		return fmt.Sprintf(`<a href="%s" title="%s全国银行间同业拆借中心受权公布贷款市场报价利率（LPR）公告">公告</a>`, href, date)
	}
	gock.New(pbcHost).Get("/zhengcehuobisi/125207/125213/125440/3876551/index.html").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(link("2025年5月20日", "/may") + link("2025年4月21日", "/apr") + link("2025年3月20日", "/old"))
	gock.New(pbcHost).Get("/may").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(pbcSample(t, "lpr-202505"))
	gock.New(pbcHost).Get("/apr").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(pbcSample(t, "lpr-202504"))
}

func mockReverseRepo(t *testing.T) {
	t.Helper()
	gock.New(pbcHost).Get("/zhengcehuobisi/125207/125213/125431/125469/index.html").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(`<table><tr><td><a href="/may">中国人民银行公开市场业务公告［2025］第1号</a><span>2025-05-07</span></td></tr><tr><td><a href="/sep">公开市场业务公告［2024］第5号</a><span>2024-09-27</span></td></tr><tr><td><a href="/jul">公开市场业务公告［2024］第4号</a><span>2024-07-22</span></td></tr><tr><td><a href="/old">公开市场业务公告［2024］第3号</a><span>2024-07-08</span></td></tr></table>`)
	gock.New(pbcHost).Get("/may").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(pbcSample(t, "reverse-repo-20250508"))
	gock.New(pbcHost).Get("/sep").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(pbcSample(t, "reverse-repo-20240927"))
	gock.New(pbcHost).Get("/jul").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(pbcSample(t, "reverse-repo-20240722"))
}

func TestLPRStandardOutput(t *testing.T) {
	defer gock.Off()
	mockLPR(t)
	result, byID := runPBCStandard(t, "lpr", "--begin", "202505", "--end", "202505")
	require.Len(t, result.Series, 4)
	require.Equal(t, 3.0, *byID["cn.lpr.1y.rate/202505"].Value)
	require.Equal(t, "2025-05-20", *byID["cn.lpr.1y.rate/202505"].AsOfDate)
	change := byID["cn.lpr.5y-plus.change/202505"]
	require.Equal(t, -10.0, *change.Value)
	require.Equal(t, "202504", change.Base.Period)
	require.Equal(t, 3.6, *change.Base.Value, "上月在查询窗口外时基期也有值")
}

func TestReverseRepoStandardOutput(t *testing.T) {
	defer gock.Off()
	mockReverseRepo(t)
	result, byID := runPBCStandard(t, "reverse-repo", "--begin", "20250101", "--end", "20251231")
	require.Len(t, result.Observations, 2)
	require.Equal(t, 1.4, *byID["cn.reverse-repo.7d.rate/20250508"].Value)
	change := byID["cn.reverse-repo.7d.change/20250508"]
	require.Equal(t, -10.0, *change.Value)
	require.Equal(t, "20240927", change.Base.Period)
	require.Equal(t, 1.5, *change.Base.Value)
}

func TestRRRStandardOutput(t *testing.T) {
	result, _ := runPBCStandard(t, "rrr", "--begin", "20200301", "--end", "20200331")
	require.Len(t, result.PolicyEvents, 2, "同日不同机构范围分开")
	require.Len(t, result.Observations, 4)
	lower, upper := result.Observations[0], result.Observations[1]
	require.Equal(t, result.PolicyEvents[0].ID, lower.EventID)
	require.Equal(t, lower.EventID, upper.EventID)
	require.Equal(t, "cn.rrr.change_lower", lower.SeriesID)
	require.Equal(t, -1.0, *lower.Value)
	require.Equal(t, -0.5, *upper.Value, "区间不取均值")
}

func TestCNTreasuryStandardOutput(t *testing.T) {
	defer gock.Off()
	mockTreasury(t)
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"cn", "treasury", "--begin", "20060301", "--end", "20060307", "--standard-json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Series, 11)
	byID := map[string]macrodata.Observation{}
	for _, o := range result.Observations {
		byID[o.SeriesID+"/"+o.Period] = o
	}
	require.Equal(t, 1.68, *byID["cn.treasury.1y.yield/20060301"].Value)
	require.Equal(t, "1.51", *byID["cn.treasury.3m.yield/20060301"].RawValue)
	require.NotNil(t, byID["cn.treasury.1y.yield/20060301"].Source)
	require.NotNil(t, byID["cn.treasury.1y.yield/20060301"].FetchedAt)
	require.True(t, gock.IsDone())
}

func TestReverseRepoHistoryStandardIgnoresDisplayLimit(t *testing.T) {
	result, _ := runPBCStandard(t, "reverse-repo-history", "--begin", "20150122", "--end", "20240719")
	require.Len(t, result.Observations, 1606, "统一导出不受终端默认条数限制")
	seen := map[string]bool{}
	for _, o := range result.Observations {
		require.NotEmpty(t, o.EventID)
		require.False(t, seen[o.EventID])
		seen[o.EventID] = true
		require.NotNil(t, o.PublishedAt)
	}
}

func TestReverseRepoHistoryStandardRejectsNegativeLimit(t *testing.T) {
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"cn", "reverse-repo-history", "--begin", "20160101", "--end", "20160201", "--limit", "-1", "--standard-json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SilenceUsage = true
	require.ErrorContains(t, cmd.Execute(), "--limit 不能小于0")
	require.Empty(t, out.String())
}

func TestCNRatesStandardErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		fail func()
		mock func(*testing.T)
	}{
		{[]string{"lpr", "--begin", "202505", "--end", "202505"}, func() { gock.New(pbcHost).Get("/zhengcehuobisi/125207/125213/125440/3876551/index.html").Reply(503) }, mockLPR},
		{[]string{"reverse-repo", "--begin", "20250101", "--end", "20251231"}, func() { gock.New(pbcHost).Get("/zhengcehuobisi/125207/125213/125431/125469/index.html").Reply(503) }, mockReverseRepo},
		{[]string{"treasury", "--begin", "20060301", "--end", "20060307"}, func() { gock.New(chinabondHost).Get("/cbweb-czb-web/czb/historyQuery").Reply(503) }, mockTreasury},
		{[]string{"rrr", "--begin", "20200101", "--end", "20200201"}, nil, nil},
		{[]string{"reverse-repo-history", "--begin", "20160101", "--end", "20160201"}, nil, nil},
	} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			if mode == "http" && test.fail == nil {
				continue
			}
			// 内置历史的逆回购窗口不请求网络，取消由有网络请求的命令覆盖。
			if mode == "cancel" && test.args[0] == "reverse-repo-history" {
				continue
			}
			t.Run(test.args[0]+"/"+mode, func(t *testing.T) {
				defer gock.Off()
				args := append(append([]string{"cn"}, test.args...), "--standard-json")
				if test.args[0] != "treasury" {
					args = append(args, "--no-cache")
				}
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "http":
					test.fail()
				case "writer":
					if test.mock != nil {
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
			})
		}
	}
}
