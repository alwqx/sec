package macro

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const (
	mofHost       = "https://www.mof.gov.cn"
	mofFiscalPath = "/zhengwuxinxi/redianzhuanti/quanguocaizhengshouzhiqingkuang/"
	mofcomHost    = "https://data.mofcom.gov.cn"
)

func mockCNFiscal(t *testing.T) {
	t.Helper()
	read := func(name string) string {
		raw, err := os.ReadFile("../../provider/mof/testdata/" + name + ".html")
		require.NoError(t, err)
		return string(raw)
	}
	gock.New(mofHost).Get(mofFiscalPath + "$").Reply(200).BodyString(read("index-0"))
	for i := 1; i < 10; i++ {
		gock.New(mofHost).Get(fmt.Sprintf("%sindex_%d.html", mofFiscalPath, i)).Reply(200).BodyString(read(fmt.Sprintf("index-%d", i)))
	}
	gock.New("https://gks.mof.gov.cn").Get("/tongjishuju/202610/t20261023_4003380.htm").Reply(200).BodyString(read("fiscal-202609"))
}

func mockCNServices(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../../provider/mofcom/testdata/services-monthly.json")
	require.NoError(t, err)
	gock.New(mofcomHost).Get("/datamofcom/front/fwmy/mouthly").MatchParam("start_date", "202607").MatchParam("end_date", "202608").MatchParam("pageNumber", "1").Reply(200).BodyString(string(raw))
}

func TestCNFiscalStandardOutput(t *testing.T) {
	defer gock.Off()
	mockCNFiscal(t)
	_, byID := runPBCStandard(t, "fiscal", "--begin", "202609", "--end", "202609", "--budget", "fund")
	amount := byID["cn.fiscal.fund-revenue.ytd_level/202609"]
	require.Equal(t, 25010.0, *amount.Value)
	require.Equal(t, "202601", amount.PeriodBegin)
	require.NotNil(t, amount.PublishedDate)
	require.NotContains(t, byID, "cn.fiscal.general-revenue.ytd_level/202609", "预算筛选生效")
}

func TestCNServicesStandardOutput(t *testing.T) {
	defer gock.Off()
	mockCNServices(t)
	result, byID := runPBCStandard(t, "services-trade", "--begin", "202607", "--end", "202608")
	require.Len(t, result.Series, 8)
	require.Len(t, result.Observations, 16)
	require.Equal(t, -218.5, *byID["cn.services-trade.usd.balance.level/202608"].Value)
	require.NotNil(t, byID["cn.services-trade.usd.balance.level/202608"].RawValue)
}

func TestFiscalTradeStandardErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		mock func(*testing.T)
		fail func()
	}{
		{[]string{"fiscal", "--begin", "202609", "--end", "202609", "--budget", "fund"}, mockCNFiscal, func() { gock.New(mofHost).Get(mofFiscalPath + "$").Reply(503) }},
		{[]string{"services-trade", "--begin", "202607", "--end", "202608"}, mockCNServices, func() { gock.New(mofcomHost).Get("/datamofcom/front/fwmy/mouthly").Reply(503) }},
		{[]string{"trade", "--begin", "202608", "--end", "202608"}, nil, func() { gock.New(mofcomHost).Post("/datamofcom/front/totalmonth/query").Reply(503) }},
	} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			// 货物贸易的人民币来源使用独立TLS客户端，命令层无法离线走完，写入失败由适配器测试和真实导出覆盖。
			if test.mock == nil && mode == "writer" {
				continue
			}
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
					test.fail()
				case "writer":
					test.mock(t)
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
