package filings

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewFilingsCLI()
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

const submissions = `{"cik":"320193","name":"Apple Inc.","tickers":["AAPL"],"exchanges":["Nasdaq"],"sicDescription":"Electronic Computers","fiscalYearEnd":"0926",
"filings":{"recent":{"accessionNumber":["0000320193-26-000026","0000320193-26-000024","0001140361-26-041201"],"filingDate":["2026-11-03","2026-11-02","2026-10-29"],
"reportDate":["2026-09-26","2026-11-02",""],"form":["10-K","8-K","4"],"size":[1,1,1],"primaryDocument":["aapl-20260926.htm","aapl-20261102.htm","form4.xml"],
"primaryDocDescription":["10-K","8-K","FORM 4"],"items":["","2.02,9.01",""]},"files":[]}}`

func mockSEC() {
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").MatchHeader("User-Agent", `kenan3015@gmail\.com$`).
		Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000320193.json").Reply(200).BodyString(submissions)
}

// 默认不列出 Form 4；邮箱来自共享配置，没有配置文件时用默认邮箱。
func TestFilings(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSEC()
	output, err := run(t, "$aapl")
	require.NoError(t, err)
	require.Contains(t, output, "Apple Inc.（AAPL，CIK 320193）Nasdaq · Electronic Computers，财年截至 09-26")
	require.Contains(t, output, "重大事项：经营业绩")
	require.NotContains(t, output, "内部人交易")
	require.True(t, gock.IsDone())
}

func TestFilingsAllFormsJSONAndDownload(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockSEC()
	gock.New("https://www.sec.gov").Get("/Archives/edgar/data/320193/000032019326000026/aapl-20260926.htm").Reply(200).BodyString("<html>10-K</html>")
	dir := t.TempDir()
	output, err := run(t, "AAPL", "--form", "all", "-n", "1", "--json", "-o", dir)
	require.NoError(t, err)
	var data struct {
		Ticker  string           `json:"ticker"`
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &data))
	require.Equal(t, "AAPL", data.Ticker)
	require.Len(t, data.Results, 1)
	body, err := os.ReadFile(filepath.Join(dir, "AAPL-2026-11-03-10-K-0000320193-26-000026.htm"))
	require.NoError(t, err)
	require.Equal(t, "<html>10-K</html>", string(body))
	require.True(t, gock.IsDone())
}

func TestFilingsArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := run(t, "AAPL", "-n", "0")
	require.ErrorContains(t, err, "--limit")
	_, err = run(t, "AAPL", "-b", "20261103", "-e", "20261101")
	require.ErrorContains(t, err, "晚于")
}
