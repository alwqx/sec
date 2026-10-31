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

const chinabondHost = "https://yield.chinabond.com.cn"

func mockTreasury(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../../provider/chinabond/testdata/treasury-20060301-20060307.json")
	require.NoError(t, err)
	gock.New(chinabondHost).Get("/cbweb-czb-web/czb/historyQuery").MatchParam("startDate", "2006-03-01").MatchParam("endDate", "2006-03-07").Reply(200).BodyString(string(raw))
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
	}
}

func TestCNRatesStandardErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		fail func()
		mock func(*testing.T)
	}{
		{[]string{"lpr", "--begin", "202501", "--end", "202502"}, func() { gock.New(pbcHost).Get("/zhengcehuobisi/125207/125213/125440/3876551/index.html").Reply(503) }, nil},
		{[]string{"reverse-repo", "--begin", "20250101", "--end", "20250201"}, func() { gock.New(pbcHost).Get("/zhengcehuobisi/125207/125213/125431/125469/index.html").Reply(503) }, nil},
		{[]string{"treasury", "--begin", "20060301", "--end", "20060307"}, func() { gock.New(chinabondHost).Get("/cbweb-czb-web/czb/historyQuery").Reply(503) }, mockTreasury},
		{[]string{"rrr", "--begin", "20200101", "--end", "20200201"}, nil, nil},
		{[]string{"reverse-repo-history", "--begin", "20160101", "--end", "20160201"}, nil, nil},
	} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			if (mode == "http" && test.fail == nil) || (mode == "writer" && test.mock == nil && test.args[0] != "reverse-repo-history") {
				continue
			}
			// 内置历史的逆回购和准备金率窗口不一定请求网络，取消由有网络请求的命令覆盖。
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
