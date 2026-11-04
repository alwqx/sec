package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// 美股的公司资料来自 SEC EDGAR，行情和估值来自新浪。
func TestUSInfoJSON(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSearch(`var suggestvalue="AAPL,41,aapl,aapl,苹果,,苹果,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").MatchHeader("User-Agent", `kenan3015@gmail\.com$`).
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
