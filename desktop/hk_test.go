//go:build windows || darwin

package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gopkg.in/h2non/gock.v1"
)

const hkSinaInfo = `var hq_str_hk00700="TENCENT,腾讯控股,421.000,430.400,428.600,416.400,416.600,-13.800,-3.206,416.60001,416.79999,10996220570,26009723,0.000,0.000,654.376,375.200,2026/11/06,15:35";
var hq_str_hk00700_i="EQTY,MAIN,654.376,375.200,3.5211,0,0,9092644829,0,9092644829,0,461968796828.17,131385642241.85,14.55,1,腾讯控股,3.700,0.2,100,腾讯控股,85112.696,248933814574.520,271150768522.250,1407533245006.300,,,1.2348,143.832377,,592.101|383.600|383.600|428.200|636.583,10.162965,5.539410,51,HKD,A,1";
`

func mockHKSina(t *testing.T) {
	mockHKSinaBody(t, hkSinaInfo)
}

func mockHKSinaBody(t *testing.T, body string) {
	gb, err := simplifiedchinese.GBK.NewEncoder().String(body)
	require.NoError(t, err)
	gock.New("https://hq.sinajs.cn").Get("/list=hk00700,hk00700_i").Reply(200).SetHeader("Content-Type", "application/javascript; charset=GBK").BodyString(gb)
}

func mockHKF10(report string) *gock.Request {
	return gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", report)
}

type hkCompanyResp struct {
	Name        string   `json:"name"`
	ShortName   string   `json:"short_name"`
	Industry    string   `json:"industry"`
	ListingDate string   `json:"listing_date"`
	IssuePrice  *float64 `json:"issue_price"`
	Description string   `json:"description"`
	PE          *float64 `json:"pe_ttm"`
	PB          *float64 `json:"pb"`
	MarketCap   *float64 `json:"market_cap"`
	Currency    string   `json:"currency"`
	Equity      bool     `json:"equity"`
	Notes       []string `json:"notes"`
}

func TestHKCompany(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	mockHKSina(t)
	mockHKF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(`{"result":{"data":[{"SECURITY_NAME_ABBR":"腾讯控股","ORG_NAME":"腾讯控股有限公司","BELONG_INDUSTRY":"软件服务","LISTING_DATE":"2004-06-16 00:00:00","ORG_PROFILE":"腾讯是一家互联网科技公司。"}]},"success":true,"code":0}`)
	mockHKF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(`{"result":{"data":[{"ISSUE_PRICE":3.7}]},"success":true,"code":0}`)
	gock.New("https://push2.eastmoney.com").Get("/api/qt/stock/get").Reply(200).BodyString(`{"rc":0,"data":{"f164":1432,"f167":292}}`)

	rr := usRequest(t, srv, "/api/v1/hk/company?code=hk00700")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data hkCompanyResp
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "腾讯控股有限公司", data.Name)
	require.Equal(t, "腾讯控股", data.ShortName)
	require.Equal(t, "软件服务", data.Industry)
	require.Equal(t, "2004-06-16", data.ListingDate)
	require.Equal(t, 3.7, *data.IssuePrice)
	require.Equal(t, "腾讯是一家互联网科技公司。", data.Description)
	require.InDelta(t, 14.32, *data.PE, 1e-9)
	require.InDelta(t, 2.92, *data.PB, 1e-9)
	require.Equal(t, "HKD", data.Currency)
	require.True(t, data.Equity)
	require.Empty(t, data.Notes)
}

// 东方财富失败时照常返回行情部分，并说明原因；新浪行情失败时返回 502；不是港股代码时返回 400。
func TestHKCompanyPartial(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	mockHKSina(t)
	mockHKF10("RPT_HKF10_INFO_ORGPROFILE").Reply(503)
	mockHKF10("RPT_HKF10_INFO_SECURITYINFO").Reply(503)
	gock.New("https://push2.eastmoney.com").Get("/api/qt/stock/get").Reply(503)

	rr := usRequest(t, srv, "/api/v1/hk/company?code=HK00700")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data hkCompanyResp
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "腾讯控股", data.Name)
	require.Nil(t, data.PE)
	require.NotNil(t, data.MarketCap)
	require.Len(t, data.Notes, 2)
	require.Contains(t, data.Notes[0], "公司资料获取失败")
	require.Contains(t, data.Notes[1], "市盈率和市净率获取失败")

	gock.New("https://hq.sinajs.cn").Get("/list=hk00700,hk00700_i").Reply(503)
	mockHKF10("RPT_HKF10_INFO_ORGPROFILE").Reply(503)
	mockHKF10("RPT_HKF10_INFO_SECURITYINFO").Reply(503)
	gock.New("https://push2.eastmoney.com").Get("/api/qt/stock/get").Reply(503)
	require.Equal(t, http.StatusBadGateway, usRequest(t, srv, "/api/v1/hk/company?code=HK00700").Code)

	for _, code := range []string{"SH600036", "$AAPL", "HK700", ""} {
		require.Equal(t, http.StatusBadRequest, usRequest(t, srv, "/api/v1/hk/company?code="+code).Code, code)
	}
}

// 东方财富没有数据时提示没有，不让用户重试；人民币柜台按交易币种返回；代码不存在时 404。
func TestHKCompanyNoDataAndCurrency(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	mockHKSinaBody(t, strings.Replace(hkSinaInfo, ",51,HKD,A,1", ",51,CNY,A,0", 1))
	empty := `{"result":null,"success":false,"message":"返回数据为空","code":9201}`
	mockHKF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(empty)
	mockHKF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(empty)
	gock.New("https://push2.eastmoney.com").Get("/api/qt/stock/get").Reply(200).BodyString(`{"rc":0,"data":null}`)

	rr := usRequest(t, srv, "/api/v1/hk/company?code=HK00700")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data hkCompanyResp
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "CNY", data.Currency)
	require.Equal(t, []string{"东方财富没有这只证券的公司资料。", "东方财富没有这只证券的市盈率和市净率。"}, data.Notes)

	gock.New("https://hq.sinajs.cn").Get("/list=hk99999,hk99999_i").Reply(200).BodyString("var hq_str_hk99999=\"\";\nvar hq_str_hk99999_i=\"\";\n")
	mockHKF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(empty)
	mockHKF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(empty)
	gock.New("https://push2.eastmoney.com").Get("/api/qt/stock/get").Reply(200).BodyString(`{"rc":0,"data":null}`)
	rr = usRequest(t, srv, "/api/v1/hk/company?code=HK99999")
	require.Equal(t, http.StatusNotFound, rr.Code, rr.Body.String())
}
