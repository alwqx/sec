package balancesheet

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func runFinancials(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewBalanceSheetCLI()
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func mockUS() {
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).BodyString(`var suggestvalue="AAPL,41,aapl,aapl,苹果,,苹果,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).BodyString(`{"0":{"cik_str":320193,"ticker":"AAPL","title":"Apple Inc."}}`)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000320193.json").Reply(200).BodyString(`{"entityName":"Apple Inc.","facts":{"us-gaap":{
"NetIncomeLoss":{"units":{"USD":[{"start":"2024-09-29","end":"2025-09-27","val":11200000000,"fp":"FY","form":"10-K","filed":"2025-10-31"}]}},
"NetCashProvidedByUsedInOperatingActivities":{"units":{"USD":[{"start":"2024-09-29","end":"2025-09-27","val":11000000000,"fp":"FY","form":"10-K","filed":"2025-10-31"}]}},
"PaymentsToAcquirePropertyPlantAndEquipment":{"units":{"USD":[{"start":"2024-09-29","end":"2025-09-27","val":1000000000,"fp":"FY","form":"10-K","filed":"2025-10-31"}]}}}}}`)
}

// 美股财报来自 SEC XBRL，金额单位亿美元，推算值标 *。
func TestUSFinancialsText(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockUS()
	output, err := runFinancials(t, "AAPL", "-t", "cashflow")
	require.NoError(t, err)
	require.Contains(t, output, "Apple Inc.（AAPL，CIK 320193）")
	require.Contains(t, output, "【现金流量表】")
	require.Contains(t, output, "自由现金流（亿美元）")
	require.Contains(t, output, "100.00*")
	require.Contains(t, output, "* 推算值")
	require.True(t, gock.IsDone())
}

func TestUSFinancialsJSON(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	mockUS()
	output, err := runFinancials(t, "AAPL", "--json")
	require.NoError(t, err)
	var data struct {
		Ticker     string `json:"ticker"`
		Standard   string `json:"standard"`
		Statements []struct {
			Statement string `json:"statement"`
		} `json:"statements"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &data))
	require.Equal(t, "AAPL", data.Ticker)
	require.Equal(t, "US GAAP", data.Standard)
	require.Len(t, data.Statements, 3)
}

func TestUSFinancialsArgs(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{{"-p", "q1"}, {"-t", "x"}, {"-o", "a.csv"}} {
		gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).BodyString(`var suggestvalue="AAPL,41,aapl,aapl,苹果,,苹果,99,1,ESG,,";`)
		_, err := runFinancials(t, append([]string{"AAPL"}, args...)...)
		require.Error(t, err, args)
	}
}

func TestUSFinancialsQuarterlyBeforeAnnual(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).
		BodyString(`var suggestvalue="IPO,41,ipo,ipo,New IPO,,New IPO,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).
		BodyString(`{"0":{"cik_str":1,"ticker":"IPO","title":"New IPO"}}`)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000000001.json").Reply(200).
		BodyString(`{"entityName":"New IPO","facts":{"us-gaap":{"NetIncomeLoss":{"units":{"USD":[
{"start":"2025-04-01","end":"2025-06-30","val":100,"fp":"Q1","form":"10-Q","filed":"2025-08-01"},
{"start":"2025-04-01","end":"2025-09-30","val":250,"fp":"Q2","form":"10-Q","filed":"2025-11-01"}
]}}}}}`)
	output, err := runFinancials(t, "IPO", "-p", "quarter", "-t", "income", "--json")
	require.NoError(t, err)
	var data usFinancials
	require.NoError(t, json.Unmarshal([]byte(output), &data))
	require.Len(t, data.Statements, 1)
	periods := data.Statements[0].Periods
	require.Len(t, periods, 2)
	require.Equal(t, 2026, periods[0].FiscalYear)
	require.Equal(t, "Q2", periods[0].FiscalPeriod)
	require.Equal(t, 150.0, *periods[0].Values["net_income"])
	require.Contains(t, periods[0].Derived, "net_income")
	require.True(t, gock.IsDone())
}

// 会计准则是公司层面的属性：JSON 顶层给出，页脚用它（没有报表时也一样）。
func TestUSFinancialsIFRSStandard(t *testing.T) {
	defer gock.Off()
	t.Setenv("HOME", t.TempDir())
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).
		BodyString(`var suggestvalue="TSM,41,tsm,tsm,台积电,,台积电,99,1,ESG,,";`)
	gock.New("https://www.sec.gov").Get("/files/company_tickers.json").Reply(200).
		BodyString(`{"0":{"cik_str":1,"ticker":"TSM","title":"TSMC"}}`)
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000000001.json").Reply(200).
		BodyString(`{"entityName":"TSMC","facts":{"ifrs-full":{"Revenue":{"units":{"TWD":[
{"start":"2024-01-01","end":"2024-12-31","val":2894,"form":"20-F","filed":"2025-04-17","accn":"t1"}]}}}}}`)
	output, err := runFinancials(t, "TSM", "-t", "income", "--json")
	require.NoError(t, err)
	var data usFinancials
	require.NoError(t, json.Unmarshal([]byte(output), &data))
	require.Equal(t, edgar.IFRS, data.Standard)
	require.True(t, gock.IsDone())

	var out bytes.Buffer
	data.Statements = nil
	printUSFinancials(&out, data)
	require.Contains(t, out.String(), "XBRL 财务数据（IFRS）")
}
