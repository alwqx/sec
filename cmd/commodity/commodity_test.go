package commodity

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewCommodityCLI()
	cmd.SilenceUsage = true // 和根命令一致，出错时不打印用法
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

const quoteBody = `var hq_str_nf_CU0="铜连续,150000,107810.000,108470.000,106090.000,106090.000,106090.000,106100.000,106090.000,107230.000,107580.000,15,1,190787.000,126807,沪,铜,2026-11-03,1";
var hq_str_hf_CL="98.456,,98.420,98.440,98.690,96.120,19:28:50,96.460,96.300,0,7,4,2026-11-03,纽约原油,0";
`

func TestCommodityQuotes(t *testing.T) {
	defer gock.Off()
	// 指定一个品种时只请求这个品种。
	gock.New("https://hq.sinajs.cn").Get("/list=hf_CL").Reply(200).BodyString(quoteBody)
	output, err := run(t, "--inst", "CL", "--json")
	require.NoError(t, err)
	var data struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &data))
	require.Len(t, data.Results, 1)
	require.Equal(t, "cl", data.Results[0]["id"])
	require.Equal(t, "美元/桶", data.Results[0]["unit"])
	require.InDelta(t, 98.456-96.46, data.Results[0]["change"], 1e-9)
	require.True(t, gock.IsDone())
}

// 个别品种没有行情时，文本模式先输出其余品种再报错；--json 直接报错。
func TestCommodityPartialQuotes(t *testing.T) {
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=").Reply(200).BodyString(quoteBody)
	output, err := run(t)
	require.ErrorContains(t, err, "原油：新浪没有返回行情")
	require.Contains(t, output, "106090")
	require.Contains(t, output, "WTI 原油")

	gock.New("https://hq.sinajs.cn").Get("/list=").Reply(200).BodyString(quoteBody)
	output, err = run(t, "--json")
	require.ErrorContains(t, err, "原油：新浪没有返回行情")
	require.Empty(t, output)
}

// 国内品种前一天没有结算价时，涨跌标 * 并说明。
func TestCommodityHistoryCloseBasis(t *testing.T) {
	defer gock.Off()
	gock.New("https://stock2.finance.sina.com.cn").Get("/futures/api/jsonp.php/var.t=/InnerFuturesNewService.getDailyKLine").MatchParam("symbol", "CU0").
		Reply(200).BodyString(`var t=([{"d":"2005-01-04","o":"1","h":"1","l":"1","c":"100","v":"1","s":"0"},{"d":"2005-01-05","o":"1","h":"1","l":"1","c":"103","v":"1","s":"101"}]);`)
	output, err := run(t, "-i", "cu", "-b", "20050105", "-e", "20050105")
	require.NoError(t, err)
	require.Contains(t, output, "+3.00*")
	require.Contains(t, output, "* 前一日没有结算价")
}

func TestCommodityHistory(t *testing.T) {
	defer gock.Off()
	gock.New("https://stock2.finance.sina.com.cn").Get("/futures/api/jsonp.php/var.t=/GlobalFuturesService.getGlobalFuturesDailyKLine").MatchParam("symbol", "CL").
		Reply(200).BodyString(`var t=([{"date":"2026-11-02","open":"102.770","high":"102.750","low":"95.150","close":"96.250","volume":"0"},{"date":"2026-11-03","open":"96.250","high":"98.690","low":"96.120","close":"98.430","volume":"0"}]);`)
	output, err := run(t, "-i", "cl", "-b", "20261103", "-e", "20261103")
	require.NoError(t, err)
	require.Contains(t, output, "WTI 原油（cl，纽约商品交易所），单位 美元/桶")
	require.Contains(t, output, "2026-11-03")
	require.NotContains(t, output, "2026-11-02")
	require.Contains(t, output, "+2.18", "涨跌相对前一日收盘价")
	require.Contains(t, output, "前一日收盘价")
	require.True(t, gock.IsDone())
}

func TestCommodityArgs(t *testing.T) {
	_, err := run(t, "-i", "au")
	require.ErrorContains(t, err, "不支持的品种 au")
	_, err = run(t, "-b", "20261101")
	require.ErrorContains(t, err, "需要用 --inst 指定品种")
	_, err = run(t, "-i", "cu", "-b", "20261103", "-e", "20261101")
	require.ErrorContains(t, err, "晚于")
}
