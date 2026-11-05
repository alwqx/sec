package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// 美股的公司资料来自 SEC EDGAR，行情和估值来自新浪。
func TestUSInfoJSON(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSearch(`var suggestvalue="AAPL,41,aapl,aapl,苹果,,苹果,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").MatchHeader("User-Agent", `sec_dev@example\.com$`).
		Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").
		Reply(200).BodyString(`{"name":"Apple Inc.","exchanges":["Nasdaq"],"sicDescription":"Electronic Computers","sic":"3571","fiscalYearEnd":"0926","category":"Large accelerated filer","filings":{"recent":{}}}`)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_aapl").
		Reply(200).BodyString(`var hq_str_gb_aapl="苹果,331.9000,-1.90,2026-11-04 09:14:47,-6.4200,325.5800,334.5000,325.3300,351.7300,242.8900,63160026,45152763,4849655801006,8.30,39.990000,0.00,0.00,0.00,0.00,14611798135,63,329.800";`)
	data, _, err := runJSON(t, "info", "AAPL", "--json")
	require.NoError(t, err)
	require.Equal(t, "$AAPL", data["ex_code"])
	company := data["company"].(map[string]any)
	require.Equal(t, "Apple Inc.", company["name"])
	require.Equal(t, "Large accelerated filer", company["category"])
	quote := data["quote"].(map[string]any)
	require.Equal(t, 39.99, quote["pe"])
	require.True(t, gock.IsDone())
}

// 行情失败时仍输出公司资料，quote 为 null。
func TestUSInfoWithoutQuote(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSearch(`var suggestvalue="AAPL,41,aapl,aapl,苹果,,苹果,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(`{"name":"Apple Inc.","filings":{"recent":{}}}`)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_aapl").Reply(502)
	data, _, err := runJSON(t, "info", "AAPL", "--json")
	require.NoError(t, err)
	require.Nil(t, data["quote"])
	require.Equal(t, "Apple Inc.", data["company"].(map[string]any)["name"])
}

// SEC 没有这个代码时只显示行情；类别股按新浪的写法请求行情。
func TestUSInfoNotInSEC(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSearch(`var suggestvalue="BRK.B,41,brk.b,brk.b,伯克希尔B,,伯克希尔B,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_brk\\$b").
		Reply(200).BodyString(`var hq_str_gb_brk$b="伯克希尔B,557.0700,0.17,2026-11-04 09:39:06,0.9600,554.2200,559,553,560,450,1,1,1,-1.5,0,0,0,0,0,1,1";`)
	data, _, err := runJSON(t, "info", "BRK.B", "--json")
	require.NoError(t, err)
	require.Nil(t, data["company"])
	quote := data["quote"].(map[string]any)
	require.Equal(t, 557.07, quote["price"])
	require.Equal(t, -1.5, quote["eps"])
	require.True(t, gock.IsDone())
}

// -d 时输出 SEC XBRL 整理的分红表；--json 放在 dividends。
func TestUSInfoDividends(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mock := func() {
		mockSearch(`var suggestvalue="KO,41,ko,ko,可口可乐,,可口可乐,99,1,ESG,,";`)
		gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(`{"0":{"cik_str":21344,"ticker":"KO","title":"COCA COLA CO"}}`)
		gock.New("https://data.sec.gov").Get("/submissions/CIK0000021344.json").Reply(200).BodyString(`{"name":"COCA COLA CO","filings":{"recent":{}}}`)
		gock.New("https://hq.sinajs.cn").Get("/list=gb_ko").Reply(502)
		gock.New("https://stock.finance.sina.com.cn").Get("/usstock/api/jsonp.php/var.t=/US_MinKService.getDailyK").Reply(200).BodyString(`var t=(null);`)
		gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000021344.json").Reply(200).BodyString(`{"entityName":"COCA COLA CO","facts":{"us-gaap":{
"CommonStockDividendsPerShareDeclared":{"units":{"USD/shares":[
 {"start":"2024-01-01","end":"2024-12-31","val":1.94,"form":"10-K","filed":"2025-02-20"},
 {"start":"2025-01-01","end":"2025-12-31","val":2.04,"form":"10-K","filed":"2026-02-20"}]}},
"NetIncomeLoss":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":13107000000,"form":"10-K","filed":"2026-02-20"}]}},
"PaymentsOfDividends":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":8779000000,"form":"10-K","filed":"2026-02-20"}]}}}}}`)
	}
	mock()
	root := NewCLI()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"info", "KO", "-d"})
	require.NoError(t, root.Execute())
	out := buf.String()
	require.Contains(t, out, "每股股息（美元）")
	require.Contains(t, out, "2.04")
	require.Contains(t, out, "+5.2%")
	require.Contains(t, out, "67.0%")
	require.Contains(t, out, "SEC 数据没有除权除息日")

	mock()
	data, _, err := runJSON(t, "info", "KO", "-d", "--json")
	require.NoError(t, err)
	years := data["dividends"].(map[string]any)["years"].([]any)
	require.Len(t, years, 2)
	require.Equal(t, 2.04, years[0].(map[string]any)["per_share"])
}

// SEC 没有这个代码时不读分红，-d --json 仍输出行情。
func TestUSInfoDividendsNotInSEC(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSearch(`var suggestvalue="QQQ,41,qqq,qqq,纳指100ETF,,纳指100ETF,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	gock.New("https://hq.sinajs.cn").Get("/list=gb_qqq").
		Reply(200).BodyString(`var hq_str_gb_qqq="纳指100ETF,500.0000,0.17,2026-11-04 09:39:06,0.9600,554.2200,559,553,560,450,1,1,1,0,0,0,0,0,0,1,1";`)
	data, _, err := runJSON(t, "info", "QQQ", "-d", "--json")
	require.NoError(t, err)
	require.Nil(t, data["company"])
	require.Nil(t, data["dividends"])
	require.Equal(t, 500.0, data["quote"].(map[string]any)["price"])
}
