package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const suggestBody = `var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`

// runJSON 运行命令并返回 stdout 和错误；stdout 必须是合法 JSON 或为空。
func runJSON(t *testing.T, args ...string) (map[string]any, string, error) {
	t.Helper()
	root := NewCLI()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	err := root.Execute()
	var data map[string]any
	if out.Len() > 0 {
		require.NoError(t, json.Unmarshal(out.Bytes(), &data), out.String())
	}
	return data, out.String(), err
}

func mockSearch(body string) {
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).BodyString(body)
}

func klineBody(rows ...string) string {
	body := `{"data":{"code":"600036","market":1,"name":"招商银行","klines":[`
	for i, row := range rows {
		if i > 0 {
			body += ","
		}
		body += `"` + row + `"`
	}
	return body + `]}}`
}

func TestSearchJSON(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	data, _, err := runJSON(t, "search", "招商银行", "--json")
	require.NoError(t, err)
	first := data["results"].([]any)[0].(map[string]any)
	require.Equal(t, "SH600036", first["ex_code"])

	mockSearch(`var suggestvalue="";`)
	data, _, err = runJSON(t, "search", "没有", "--json")
	require.NoError(t, err)
	require.Equal(t, []any{}, data["results"], "没有结果时是空数组，不是 null")
}

// 找不到证券或上游失败时 stdout 为空，退出码由错误决定。
func TestJSONErrorsLeaveStdoutEmpty(t *testing.T) {
	for _, args := range [][]string{
		{"info", "x", "--json"}, {"quote-history", "x", "--json"}, {"kline", "x", "--json"}, {"financials", "x", "--json"},
		{"valuation", "x", "--json"}, {"insider", "x", "--json"}, {"announcements", "x", "--json"},
	} {
		mockSearch(`var suggestvalue="";`)
		_, output, err := runJSON(t, args...)
		require.ErrorContains(t, err, "未找到证券", args[0])
		require.Empty(t, output, args[0])
		gock.Off()
	}
	for _, args := range [][]string{{"search", "x", "--json"}, {"quote", "x", "--json"}, {"strategy", "ma", "x", "--json"}} {
		gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(500)
		_, output, err := runJSON(t, args...)
		require.ErrorContains(t, err, "HTTP 500", args[0])
		require.Empty(t, output, args[0])
		gock.Off()
	}
}

func TestQuoteHistoryJSON(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").MatchParam("klt", "102").MatchParam("fqt", "1").Reply(200).
		BodyString(klineBody("2026-10-30,1,2,3,0.5,100,1000,5,6,7,8"))
	data, _, err := runJSON(t, "quote-history", "600036", "--json", "--freq", "week", "--fq", "qfq")
	require.NoError(t, err)
	require.Equal(t, "SH600036", data["ex_code"])
	require.Equal(t, "week", data["freq"])
	require.Equal(t, "qfq", data["fq"])
	require.Equal(t, "", data["source_note"])
	row := data["results"].([]any)[0].(map[string]any)
	require.Equal(t, "2026-10-30", row["date"])
	require.Equal(t, 2.0, row["close"])
	require.Equal(t, 10000.0, row["volume"], "沪深成交量由手换算成股")
	require.Equal(t, 1000.0, row["amount"])
}

// 备用数据没有的字段是 null，不是 0；第一根没有前一日收盘价。
func TestKlineJSONFallback(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(503)
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").MatchParam("scale", "7200").Reply(200).BodyString(`[
		{"day":"2026-09-30","open":"1","high":"2","low":"1","close":"2","volume":"100"},
		{"day":"2026-10-30","open":"2","high":"3","low":"2","close":"3","volume":"200"}]`)
	data, _, err := runJSON(t, "kline", "600036", "--json", "--freq", "m", "--begin", "20260101", "--end", "20261031")
	require.NoError(t, err)
	require.Contains(t, data["source_note"], "新浪备用数据")
	rows := data["results"].([]any)
	first, second := rows[0].(map[string]any), rows[1].(map[string]any)
	require.Nil(t, first["amount"])
	require.Nil(t, first["turnover_rate"])
	require.Nil(t, first["change"])
	require.Nil(t, first["amplitude"])
	require.Equal(t, 1.0, second["change"])
	require.Nil(t, second["amount"])
}

func TestStrategyJSONKeepsAllRowsAndPrecision(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	rows := []string{}
	for i, price := range []string{"10", "9", "8", "7", "6", "7", "8", "9", "10", "11"} {
		rows = append(rows, fmt.Sprintf("2026-01-%02d,%s,%s,%s,%s,100,1000,0,0,0,0", i+1, price, price, price, price))
	}
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(200).BodyString(klineBody(rows...))
	data, _, err := runJSON(t, "strategy", "ma", "600036", "--json", "-f", "2", "-s", "3", "-b", "20260101", "-e", "20260131")
	require.NoError(t, err)
	require.Equal(t, "ma", data["strategy"])
	require.Equal(t, "day", data["freq"])
	results := data["results"].([]any)
	require.Len(t, results, 10)
	require.Nil(t, results[1].(map[string]any)["ma_slow"], "窗口不足是 null")
	require.NotNil(t, results[1].(map[string]any)["ma_fast"])
	require.InDelta(t, 9, results[2].(map[string]any)["ma_slow"], 1e-9)
	signal := data["signals"].([]any)[0].(map[string]any)
	require.Equal(t, "buy", signal["type"])

	mockSearch(suggestBody)
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(200).BodyString(klineBody(rows[:2]...))
	_, output, err := runJSON(t, "strategy", "ma", "600036", "--json", "-f", "2", "-s", "3", "-b", "20260101", "-e", "20260131")
	require.ErrorContains(t, err, "数据不足")
	require.Empty(t, output)
}

func TestInvalidFreq(t *testing.T) {
	defer gock.Off()
	for _, args := range [][]string{{"kline", "600036", "--freq", "year"}, {"quote-history", "600036", "--freq", "year"}, {"strategy", "ma", "600036", "--freq", "year"}} {
		mockSearch(suggestBody)
		_, _, err := runJSON(t, args...)
		require.ErrorContains(t, err, "不支持的周期", args[0])
	}
}

func TestJSONFlagConflicts(t *testing.T) {
	for _, args := range [][]string{{"quote", "x", "--json", "-r"}, {"kline", "x", "--json", "--ma", "5"}, {"kline", "x", "--json", "--boll", "20,2"}, {"financials", "x", "--json", "-o", "a.json"}} {
		_, _, err := runJSON(t, args...)
		require.ErrorContains(t, err, "none of the others can be", args[0])
	}
}

// --json 按关键字顺序逐个搜索，mock 的顺序和关键字一致。
const (
	cmbA = `var hq_str_sh600036="招商银行,40.000,39.000,41.000,42.000,39.500,41.000,41.010,100,4100.000,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-10-30,15:00:00,00,";`
	cmbH = `var hq_str_rt_hk03968="CM BANK,招商银行,44.000,43.000,45.000,43.500,44.500,1.500,3.488,44.500,44.550,2225.000,50,0,0,50,30,2026/10/30,16:08:15,100|0,N|Y|Y,0|0|0,0|||0|0|0, |0,Y";`
)

// A 股和 H 股同名时不能互相覆盖代码，否则价格会对到错的证券上。
func TestQuoteJSONSameNameKeepsCodes(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	mockSearch(`var suggestvalue="招商银行,31,03968,03968,招商银行,,招商银行,99,1,,,";`)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036,hk03968").Reply(200).BodyString(cmbA + "\n" + cmbH)
	data, _, err := runJSON(t, "quote", "600036,03968", "--json")
	require.NoError(t, err)
	results := data["results"].([]any)
	require.Len(t, results, 2)
	first, second := results[0].(map[string]any), results[1].(map[string]any)
	require.Equal(t, "SH600036", first["ex_code"])
	require.Equal(t, 41.0, first["price"])
	require.Equal(t, "2026-10-30", first["trade_date"])
	require.Equal(t, "HK03968", second["ex_code"])
	require.Equal(t, 44.5, second["price"])
	require.Equal(t, "2026-10-30", second["trade_date"], "港股日期和其他市场格式一致")
}

func TestQuoteJSONFailsOnPartialResults(t *testing.T) {
	defer gock.Off()
	// 其中一个关键字没有匹配。
	mockSearch(suggestBody)
	mockSearch(`var suggestvalue="";`)
	_, output, err := runJSON(t, "quote", "600036,nosuch", "--json")
	require.ErrorContains(t, err, "未找到证券: nosuch")
	require.Empty(t, output)
	gock.Off()

	// 其中一个关键字搜索失败。
	mockSearch(suggestBody)
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(500)
	_, output, err = runJSON(t, "quote", "600036,03968", "--json")
	require.ErrorContains(t, err, "HTTP 500")
	require.Empty(t, output)
	gock.Off()

	// 行情里少了一只。
	mockSearch(suggestBody)
	mockSearch(`var suggestvalue="招商银行,31,03968,03968,招商银行,,招商银行,99,1,,,";`)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036,hk03968").Reply(200).BodyString(cmbA)
	_, output, err = runJSON(t, "quote", "600036,03968", "--json")
	require.ErrorContains(t, err, "HK03968 没有行情数据")
	require.Empty(t, output)
	gock.Off()

	// 超过上限时不能只查前 5 个。
	_, output, err = runJSON(t, "quote", "a,b,c,d,e,f", "--json")
	require.ErrorContains(t, err, "一次最多查询 5 只")
	require.Empty(t, output)
}

func TestFinancialsJSONFailsOnPartialResults(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").Reply(200).
		BodyString(`{"success":true,"result":{"data":[{"REPORT_DATE":"2025-12-31 00:00:00","SECURITY_CODE":"600036"}]}}`)
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").Reply(500)
	_, output, err := runJSON(t, "financials", "600036", "--json")
	require.ErrorContains(t, err, "失败")
	require.Empty(t, output)
}

func TestStrategyRejectsInvalidPeriodsBeforeFetching(t *testing.T) {
	defer gock.Off()
	for _, args := range [][]string{
		{"strategy", "rsi", "600036", "-p", "0", "--json"},
		{"strategy", "boll", "600036", "-p", "0"},
		{"strategy", "ma", "600036", "-f", "0"},
		{"strategy", "macd", "600036", "--signal", "0"},
	} {
		_, output, err := runJSON(t, args...)
		require.ErrorContains(t, err, "必须大于 0", args)
		require.Empty(t, output)
	}
}

func TestStrategyJSONFastLongerThanData(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").Reply(200).
		BodyString(klineBody("2026-01-02,10,10,10,10,100,1000,0,0,0,0", "2026-01-03,10,10,10,10,100,1000,0,0,0,0", "2026-01-04,10,10,10,10,100,1000,0,0,0,0"))
	_, output, err := runJSON(t, "strategy", "ma", "600036", "-f", "5", "-s", "2", "--json", "-b", "20260101", "-e", "20260131")
	require.ErrorContains(t, err, "需要至少 5 根")
	require.Empty(t, output)
}

func TestInfoRowMissingValues(t *testing.T) {
	marshal := func(row infoRow) map[string]any {
		body, err := json.Marshal(row)
		require.NoError(t, err)
		var data map[string]any
		require.NoError(t, json.Unmarshal(body, &data))
		return data
	}
	data := marshal(infoRow{})
	require.NotContains(t, data, "dividends", "没有 -d 时不输出")
	require.Contains(t, data, "listing_price")
	require.Nil(t, data["listing_price"])
	data = marshal(infoRow{Dividends: []sina.Dividend{}})
	require.Equal(t, []any{}, data["dividends"], "查了但没有分红是空数组")
}

// 只给 --end 时，默认区间从结束日期往前推，预热数据再往前多取一年。
func TestStrategyEndOnly(t *testing.T) {
	defer gock.Off()
	mockSearch(suggestBody)
	rows := []string{}
	for i, price := range []string{"10", "9", "8", "7", "6", "7", "8", "9", "10", "11"} {
		rows = append(rows, fmt.Sprintf("2020-01-%02d,%s,%s,%s,%s,100,1000,0,0,0,0", i+1, price, price, price, price))
	}
	gock.New("https://push2his.eastmoney.com").Get("/api/qt/stock/kline/get").MatchParam("beg", "20180526").MatchParam("end", "20200131").Reply(200).BodyString(klineBody(rows...))
	data, _, err := runJSON(t, "strategy", "ma", "600036", "--json", "-f", "2", "-s", "3", "-e", "20200131")
	require.NoError(t, err)
	require.Len(t, data["results"].([]any), 10)
}

// 回测参数和周期在联网前检查：搜索接口返回错误也不影响报错内容。
func TestStrategyBacktestFlagsCheckedFirst(t *testing.T) {
	defer gock.Off()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--cash", "10"}, "初始资金"},
		{[]string{"--freq", "week"}, "回测只支持日线"},
		{[]string{"-b", "20260201", "-e", "20260101"}, "晚于"},
	} {
		gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(500)
		_, _, err := runJSON(t, append([]string{"strategy", "ma", "600036", "--backtest"}, tc.args...)...)
		require.ErrorContains(t, err, tc.want, tc.args)
		gock.Off()
	}
}
