package valuation

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	shared "github.com/alwqx/sec/valuation"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func num(value float64) *float64 { return &value }

func sampleResult(pe float64) *shared.Result {
	points := make([]*eastmoney.ValuationPoint, 0, 300)
	for i := 0; i < 300; i++ {
		points = append(points, &eastmoney.ValuationPoint{
			Date: "2026-01-01", Name: "招商银行", Board: "银行Ⅱ", Close: 50, MarketCap: 5e9, Shares: 1e8,
			PE: num(pe), PB: num(2), PS: num(3),
		})
	}
	reports := []*eastmoney.PerformanceReport{
		{Date: "2025-12-31", EPS: num(5), BPS: num(25), ROE: num(20), Revenue: num(1e9), Profit: num(144)},
		{Date: "2023-12-31", EPS: num(4), Profit: num(100)},
	}
	result := shared.Compute(points, reports, shared.DefaultDCFParams())
	result.Code = "SH600036"
	return result
}

func TestRenderMethods(t *testing.T) {
	for method, want := range map[string]string{
		"overview": "【规则汇总】", "pe": "【估值情景】", "pb": "每股净资产", "ps": "营业收入",
		"peg": "【历年归母净利润】", "graham": "格雷厄姆数", "dcf": "金融行业",
	} {
		var out bytes.Buffer
		render(&out, method, sampleResult(10))
		require.Contains(t, out.String(), want, method)
		require.Contains(t, out.String(), "不构成投资建议", method)
	}
}

func TestCandidates(t *testing.T) {
	index := &sina.BasicSecurity{Code: "000001", ExCode: "SH000001"}
	stock := &sina.BasicSecurity{Code: "000001", ExCode: "SZ000001"}
	fund := &sina.BasicSecurity{Code: "000001", ExCode: "OF000001"}
	require.Equal(t, []*sina.BasicSecurity{index, stock}, candidates([]*sina.BasicSecurity{index, stock, fund}, "000001"), "指数没有数据时继续试股票")
	require.Equal(t, []*sina.BasicSecurity{index}, candidates([]*sina.BasicSecurity{index, stock}, "sh000001"), "带交易所时只查它")

	tencent := &sina.BasicSecurity{Code: "00700", ExCode: "HK00700"}
	other := &sina.BasicSecurity{Code: "000700", ExCode: "SZ000700"}
	require.Equal(t, []*sina.BasicSecurity{tencent}, candidates([]*sina.BasicSecurity{tencent, other}, "00700"), "不能换成别的公司")

	bank := &sina.BasicSecurity{Code: "600036", ExCode: "SH600036"}
	require.Equal(t, []*sina.BasicSecurity{bank}, candidates([]*sina.BasicSecurity{bank, {Code: "601398", ExCode: "SH601398"}}, "招商银行"))
}

func TestUnknownMethodFailsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.CleanUnmatchedRequest()
	gock.Intercept()
	cmd := NewValuationCLI()
	cmd.SetArgs([]string{"600036", "-m", "gramham"})
	require.ErrorContains(t, cmd.Execute(), "不支持的估值方法 gramham")
	require.False(t, gock.HasUnmatchedRequest(), "校验失败时不发请求")
}

func TestRenderShortHistory(t *testing.T) {
	points := []*eastmoney.ValuationPoint{{Date: "2026-01-01", Close: 50, PE: num(10), PB: num(2), PS: num(3)}}
	var out bytes.Buffer
	render(&out, "overview", shared.Compute(points, nil, shared.DefaultDCFParams()))
	require.Contains(t, out.String(), "样本不足")
	require.NotContains(t, out.String(), "100%")
}

func TestRenderLoss(t *testing.T) {
	var out bytes.Buffer
	render(&out, "dcf", sampleResult(-10))
	require.Contains(t, out.String(), "DCF 不适用")
	out.Reset()
	render(&out, "graham", sampleResult(-10))
	require.Contains(t, out.String(), "不适用")
	out.Reset()
	render(&out, "overview", sampleResult(-10))
	require.Contains(t, out.String(), "亏损或净资产为负")
}

func TestRenderPeersNote(t *testing.T) {
	r := sampleResult(10)
	r.PeersNote = "同行业数据获取失败，规则汇总未计入行业规则"
	for _, method := range []string{"overview", "peers"} {
		var out bytes.Buffer
		render(&out, method, r)
		require.Contains(t, out.String(), r.PeersNote, method)
	}
}
