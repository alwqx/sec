package edgar

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const tickersBody = `{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."},"1":{"cik_str":1067983,"ticker":"BRK-B","title":"BERKSHIRE HATHAWAY INC"}}`

const submissionsBody = `{"cik":"320193","name":"Apple Inc.","tickers":["AAPL"],"exchanges":["Nasdaq"],"sicDescription":"Electronic Computers","fiscalYearEnd":"0926",
"sic":"3571","category":"Large accelerated filer","stateOfIncorporationDescription":"CA","phone":"(408) 996-1010",
"addresses":{"business":{"street1":"ONE APPLE PARK WAY","street2":null,"city":"CUPERTINO","stateOrCountry":"CA","zipCode":"95014"}},
"formerNames":[{"name":"APPLE INC","from":"2007-01-10T05:00:00.000Z","to":"2019-08-05T04:00:00.000Z"}],
"filings":{"recent":{"accessionNumber":["0000320193-26-000026","0000320193-26-000024","0001140361-26-041201","0000320193-25-000079"],
"filingDate":["2026-11-03","2026-11-02","2026-10-29","2025-10-31"],"reportDate":["2026-09-26","2026-11-02","2026-10-27","2025-09-27"],
"form":["10-K","8-K","4","10-K/A"],"size":[100,20,5,90],"primaryDocument":["aapl-20260926.htm","aapl-20261102.htm","xslF345X06/form4.xml",""],
"primaryDocDescription":["10-K","8-K","FORM 4","10-K/A"],"items":["","2.02,9.01","",""]},
"files":[{"name":"CIK0000320193-submissions-001.json","filingFrom":"1994-01-26","filingTo":"2025-10-30"}]}}`

const olderBody = `{"accessionNumber":["0001104659-05-058421"],"filingDate":["2005-12-01"],"reportDate":["2005-09-24"],"form":["10-K"],"size":[1],"primaryDocument":["a05-20674_110k.htm"],"primaryDocDescription":["10-K"]}`

func client(t *testing.T) *Client { return &Client{Email: "me@example.org", CacheDir: t.TempDir()} }

func TestLookup(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").MatchHeader("User-Agent", `^alwqx-sec/.* me@example\.org$`).Reply(200).BodyString(tickersBody)
	c := client(t)
	company, err := c.Lookup(context.Background(), "$aapl")
	require.NoError(t, err)
	require.Equal(t, Company{CIK: 320193, Ticker: "AAPL", Name: "Apple Inc."}, company)
	company, err = c.Lookup(context.Background(), "brk.b")
	require.NoError(t, err, "第二次查询用缓存，不再请求")
	require.Equal(t, 1067983, company.CIK)
	_, err = c.Lookup(context.Background(), "ZZZZ")
	require.ErrorIs(t, err, ErrNotFound)
	require.True(t, gock.IsDone())
}

func TestFilings(t *testing.T) {
	defer gock.Off()
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(submissionsBody)
	c := client(t)
	result, err := c.Filings(context.Background(), 320193, FilingQuery{Forms: []string{"10-k"}})
	require.NoError(t, err)
	filings := result.Filings
	require.Equal(t, "Apple Inc.", result.Profile.Name)
	require.Equal(t, "0926", result.Profile.FiscalYearEnd)
	require.Len(t, filings, 2, "10-K 也包含修订版 10-K/A")
	require.Equal(t, "https://www.sec.gov/Archives/edgar/data/320193/000032019326000026/aapl-20260926.htm", filings[0].URL)
	require.Equal(t, "2026-09-26", filings[0].ReportDate)
	require.Equal(t, "https://www.sec.gov/Archives/edgar/data/320193/000032019325000079/0000320193-25-000079-index.htm", filings[1].URL, "没有主文件时用申报目录")

	result, err = c.Filings(context.Background(), 320193, FilingQuery{Begin: "2026-11-01", End: "2026-11-02"})
	require.NoError(t, err)
	filings = result.Filings
	require.Len(t, filings, 1)
	require.Equal(t, "8-K", filings[0].Form)
	require.Equal(t, "重大事项：经营业绩", filings[0].Description)
	require.Equal(t, "2.02,9.01", filings[0].Items)
	require.True(t, gock.IsDone())
}

// 开始日期早于最近列表时，读取历史分页。
func TestFilingsOlderPages(t *testing.T) {
	defer gock.Off()
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(submissionsBody)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193-submissions-001.json").Reply(200).BodyString(olderBody)
	result, err := client(t).Filings(context.Background(), 320193, FilingQuery{Forms: []string{"10-K"}, Begin: "2005-01-01", End: "2005-12-31"})
	require.NoError(t, err)
	require.Len(t, result.Filings, 1)
	require.Equal(t, "2005-12-01", result.Filings[0].FilingDate)
	require.True(t, gock.IsDone())
}

func TestErrors(t *testing.T) {
	defer gock.Off()
	_, err := (&Client{CacheDir: t.TempDir()}).Lookup(context.Background(), "AAPL")
	require.ErrorContains(t, err, "没有配置 SEC 联系邮箱")

	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(403)
	_, err = client(t).Filings(context.Background(), 320193, FilingQuery{})
	require.ErrorContains(t, err, "me@example.org")

	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(404)
	_, err = client(t).Filings(context.Background(), 320193, FilingQuery{})
	require.ErrorContains(t, err, "找不到 https://data.sec.gov/submissions/CIK0000320193.json")
	require.NotErrorIs(t, err, ErrNotFound, "只有代码查不到才是 ErrNotFound")

	_, err = client(t).Download(context.Background(), "https://example.com/a.htm")
	require.ErrorContains(t, err, "不是 SEC 申报文件地址")
}

func TestNormalizeTicker(t *testing.T) {
	require.Equal(t, "BRK-B", NormalizeTicker(" $brk.b "))
	require.Equal(t, []string{"NYSE", "OTC"}, unique([]string{"NYSE", "OTC", "NYSE", "", "NYSE"}))
	require.Equal(t, "AAPL", NormalizeTicker("aapl"))
}

func TestDescribe(t *testing.T) {
	require.Equal(t, "年报", Describe("10-K", ""))
	require.Equal(t, "年报（修订）", Describe("10-K/A", ""))
	require.Equal(t, "重大事项：经营业绩", Describe("8-K", "2.02,9.01"))
	require.Equal(t, "重大事项：董事或高管变动、股东大会表决结果", Describe("8-K", "5.02,5.07"))
	require.Equal(t, "重大事项", Describe("8-K", "9.01"))
	require.Equal(t, "", Describe("N-CSR", ""))
}

// 按类型筛选后不够 Want 条时继续读历史分页；没有开始日期时最多读 maxOlderPages 页，读到上限标记 Truncated。
func TestFilingsWantReadsOlderPages(t *testing.T) {
	defer gock.Off()
	files := `"files":[`
	for i := 1; i <= maxOlderPages+1; i++ {
		if i > 1 {
			files += ","
		}
		files += fmt.Sprintf(`{"name":"CIK0000886982-submissions-%03d.json","filingFrom":"20%02d-01-01"}`, i, 25-i)
	}
	files += "]"
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000886982.json").Reply(200).BodyString(`{"name":"GOLDMAN SACHS","filings":{"recent":{"accessionNumber":["a-1","a-2"],"filingDate":["2026-02-20","2026-01-02"],"form":["10-K","424B2"]},` + files + `}}`)
	mockPages := func() {
		for i := 1; i <= maxOlderPages; i++ {
			gock.New("https://data.sec.gov").Get(fmt.Sprintf("/submissions/CIK0000886982-submissions-%03d.json", i)).
				Reply(200).BodyString(fmt.Sprintf(`{"accessionNumber":["b-%d","c-%d"],"filingDate":["20%02d-02-20","20%02d-01-02"],"form":["10-K","424B2"]}`, i, i, 25-i, 25-i))
		}
	}
	mockPages()
	result, err := client(t).Filings(context.Background(), 886982, FilingQuery{Forms: []string{"10-K"}, Want: 3})
	require.NoError(t, err)
	require.Len(t, result.Filings, 3, "凑够 3 份就停")
	require.False(t, result.Truncated)

	gock.Off()
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000886982.json").Reply(200).BodyString(`{"name":"GOLDMAN SACHS","filings":{"recent":{"accessionNumber":["a-1"],"filingDate":["2026-02-20"],"form":["10-K"]},` + files + `}}`)
	mockPages()
	result, err = client(t).Filings(context.Background(), 886982, FilingQuery{Forms: []string{"10-K"}, Want: 100})
	require.NoError(t, err)
	require.Len(t, result.Filings, 1+maxOlderPages)
	require.True(t, result.Truncated, "读到上限时还有更早的分页")
}

// 同一个代码对应多家公司时，按对照表的顺序保留排在前面的，每次结果一样。
func TestParseTickersOrder(t *testing.T) {
	body := `{"0":{"cik_str":1,"ticker":"DUP","title":"First"}`
	for i := 1; i < 50; i++ {
		body += fmt.Sprintf(`,"%d":{"cik_str":%d,"ticker":"DUP","title":"Other"}`, i, i+1)
	}
	body += "}"
	for range 20 {
		companies, err := parseTickers([]byte(body))
		require.NoError(t, err)
		require.Equal(t, 1, companies["DUP"].CIK)
	}
}

// 磁盘缓存写坏时重新下载。
func TestTickersCorruptCache(t *testing.T) {
	defer gock.Off()
	c := client(t)
	require.NoError(t, os.WriteFile(filepath.Join(c.CacheDir, "company_tickers.json"), []byte(`{"0":{"cik_str":1`), 0600))
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(tickersBody)
	company, err := c.Lookup(context.Background(), "AAPL")
	require.NoError(t, err)
	require.Equal(t, 320193, company.CIK)
	data, err := os.ReadFile(filepath.Join(c.CacheDir, "company_tickers.json"))
	require.NoError(t, err)
	require.JSONEq(t, tickersBody, string(data))
}

func TestIsDocumentURL(t *testing.T) {
	require.True(t, IsDocumentURL("https://www.sec.gov/Archives/edgar/data/320193/000032019326000026/aapl-20260926.htm"))
	for _, raw := range []string{
		"https://www.sec.gov/Archives/edgar/data/../../cgi-bin/browse-edgar",
		"https://www.sec.gov/Archives/edgar/data/1/%2e%2e/x",
		"https://www.sec.gov/Archives/edgar/data//1/a.htm",
		"https://www.sec.gov/cgi-bin/browse-edgar",
		"http://www.sec.gov/Archives/edgar/data/1/a.htm",
		"https://sec.gov.evil.com/Archives/edgar/data/1/a.htm",
		"https://www.sec.gov/Archives/edgar/data/1/a.htm?x=1",
	} {
		require.False(t, IsDocumentURL(raw), raw)
	}
}

func TestProfile(t *testing.T) {
	defer gock.Off()
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(submissionsBody)
	p, err := client(t).Profile(context.Background(), 320193)
	require.NoError(t, err)
	require.Equal(t, "Large accelerated filer", p.Category)
	require.Equal(t, "ONE APPLE PARK WAY, CUPERTINO, CA 95014", p.Address)
	require.Equal(t, "3571", p.SIC)
	require.Equal(t, []FormerName{{"APPLE INC", "2007-01-10", "2019-08-05"}}, p.FormerNames)
}
