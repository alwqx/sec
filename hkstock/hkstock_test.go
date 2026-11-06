package hkstock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gopkg.in/h2non/gock.v1"
)

const (
	tencentInfo = `var hq_str_hk00700="TENCENT,腾讯控股,421.000,430.400,428.600,416.400,416.600,-13.800,-3.206,416.60001,416.79999,10996220570,26009723,0.000,0.000,654.376,375.200,2026/11/06,15:35";
var hq_str_hk00700_i="EQTY,MAIN,654.376,375.200,3.5211,0,0,9092644829,0,9092644829,0,461968796828.17,131385642241.85,14.55,1,腾讯控股,3.700,0.2,100,腾讯控股,85112.696,248933814574.520,271150768522.250,1407533245006.300,,,1.2348,143.832377,,592.101|383.600|383.600|428.200|636.583,10.162965,5.539410,51,HKD,A,1";
`
	trackerInfo = `var hq_str_hk00700="TRACKER FUND,盈富基金,24.500,24.940,24.700,24.400,24.600,-0.340,-1.363,24.600,24.620,1000000,40000,0.000,0.000,26.600,23.420,2026/11/06,16:08";
var hq_str_hk00700_i="TRST,MAIN,28.320,22.920,0,0,0,0,0,0,0,0.00,0.00,0.00,1,盈富基金,0.000,0.02,500,盈富基金,0.000,0.000,0.000,0.000,,,,,,25.820|23.420|23.420|24.920|26.600,,,62,HKD,C,1";
`
	orgBody     = `{"result":{"data":[{"SECURITY_NAME_ABBR":"腾讯控股","ORG_NAME":"腾讯控股有限公司","BELONG_INDUSTRY":"软件服务","LISTING_DATE":"2004-06-16 00:00:00","ORG_WEB":"www.tencent.com","ORG_PROFILE":"腾讯是一家互联网科技公司。","MAIN_BUSINESS":"增值服务"}]},"success":true,"code":0}`
	fundOrgBody = `{"result":{"data":[{"SECURITY_NAME_ABBR":"盈富基金","ORG_NAME":"恒生投资管理有限公司","ORG_WEB":"www.hangseng.com"}]},"success":true,"code":0}`
	emptyBody   = `{"result":null,"success":false,"message":"返回数据为空","code":9201}`
	warrantInfo = `var hq_str_hk00700="TENCENT UBS@EC2701A,腾讯瑞银七乙沽A,0.100,0.101,0.105,0.095,0.100,-0.001,-0.990,0.099,0.100,100000,1000000,0.000,0.000,0.320,0.050,2026/11/06,16:08";
var hq_str_hk00700_i="WRNT,MAIN,0.320,0.050,0,0,0,0,0,0,0,0.00,0.00,0.00,1,腾讯瑞银七乙沽A,0.000,0.001,10000,腾讯瑞银七乙沽A,0.000,0.000,0.000,0.000,,,,,,0.1|0.1|0.1|0.1|0.1,,,,,,0";
`
	secBody   = `{"result":{"data":[{"ISSUE_PRICE":3.7}]},"success":true,"code":0}`
	quoteBody = `{"rc":0,"data":{"f164":1432,"f167":292}}`
)

func mockSina(t *testing.T, body string) {
	gb, err := simplifiedchinese.GBK.NewEncoder().String(body)
	require.NoError(t, err)
	gock.New("https://hq.sinajs.cn").Get("/list=hk00700,hk00700_i").Reply(200).SetHeader("Content-Type", "application/javascript; charset=GBK").BodyString(gb)
}

func mockF10(report string) *gock.Request {
	return gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", report)
}

func mockValuation() *gock.Request {
	return gock.New("https://push2.eastmoney.com").Get("/api/qt/stock/get")
}

func TestProfile(t *testing.T) {
	defer gock.Off()
	mockSina(t, tencentInfo)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(orgBody)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(secBody)
	mockValuation().Reply(200).BodyString(quoteBody)

	p, err := Profile(context.Background(), "HK00700")
	require.NoError(t, err)
	require.Equal(t, "HK00700", p.ExCode)
	require.Equal(t, "腾讯控股有限公司", p.Name)
	require.Equal(t, "腾讯控股", p.HistoryName)
	require.Equal(t, "2004-06-16", p.ListingDate)
	require.Equal(t, 3.7, p.ListingPrice)
	require.Equal(t, "软件服务", p.Category)
	require.Equal(t, "腾讯是一家互联网科技公司。", p.Description)
	require.InDelta(t, 14.32, p.PeTTM, 1e-9)
	require.InDelta(t, 2.92, p.PB, 1e-9)
	require.InDelta(t, 416.6*9092644829, p.MarketCap, 1)
	require.True(t, gock.IsDone())
}

// 公司资料失败时估值照常输出，名称用行情里的简称。
func TestProfileWithoutF10(t *testing.T) {
	defer gock.Off()
	mockSina(t, tencentInfo)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(503)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(503)
	mockValuation().Reply(200).BodyString(quoteBody)

	p, err := Profile(context.Background(), "HK00700")
	require.NoError(t, err)
	require.Equal(t, "腾讯控股", p.Name)
	require.Empty(t, p.Category)
	require.InDelta(t, 14.32, p.PeTTM, 1e-9, "估值不依赖公司资料")
	require.InDelta(t, 2.92, p.PB, 1e-9)
}

// 估值失败时公司资料照常输出。
func TestProfileWithoutValuation(t *testing.T) {
	defer gock.Off()
	mockSina(t, tencentInfo)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(orgBody)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(secBody)
	mockValuation().Reply(503)

	p, err := Profile(context.Background(), "HK00700")
	require.NoError(t, err)
	require.Equal(t, "腾讯控股有限公司", p.Name)
	require.Zero(t, p.PeTTM)
	require.Zero(t, p.PB)
}

// 交易所买卖基金在 F10 里的公司名称是基金管理人，不当作公司资料。
func TestProfileTrust(t *testing.T) {
	defer gock.Off()
	mockSina(t, trackerInfo)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(fundOrgBody)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(`{"result":null,"success":false,"message":"返回数据为空","code":9201}`)
	mockValuation().Reply(200).BodyString(`{"rc":0,"data":{"f164":"-","f167":"-"}}`)

	p, err := Profile(context.Background(), "HK00700")
	require.NoError(t, err)
	require.Equal(t, "盈富基金", p.Name)
	require.Empty(t, p.WebSite)
	require.False(t, Equity(p))
}

func TestProfileQuoteFails(t *testing.T) {
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=hk00700,hk00700_i").Reply(503)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(orgBody)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(secBody)
	mockValuation().Reply(200).BodyString(quoteBody)

	_, err := Profile(context.Background(), "HK00700")
	require.ErrorContains(t, err, "503", "行情失败要报错")
}

// Load 分别报告东方财富各部分的错误，没有数据时是 ErrNoData；权证等不是股本证券，不报错。
func TestLoadErrors(t *testing.T) {
	defer gock.Off()
	mockSina(t, tencentInfo)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(orgBody)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(503)
	mockValuation().Reply(200).BodyString(`{"rc":0,"data":null}`)
	res, err := Load(context.Background(), "HK00700")
	require.NoError(t, err)
	require.NoError(t, res.ProfileErr)
	require.ErrorContains(t, res.IssuePriceErr, "503")
	require.ErrorIs(t, res.ValuationErr, ErrNoData)

	mockSina(t, tencentInfo)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(emptyBody)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(emptyBody)
	mockValuation().Reply(200).BodyString(quoteBody)
	res, err = Load(context.Background(), "HK00700")
	require.NoError(t, err)
	require.ErrorIs(t, res.ProfileErr, ErrNoData)
	require.NoError(t, res.IssuePriceErr)
	require.NoError(t, res.ValuationErr)

	mockSina(t, warrantInfo)
	mockF10("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(emptyBody)
	mockF10("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(emptyBody)
	mockValuation().Reply(200).BodyString(`{"rc":0,"data":null}`)
	res, err = Load(context.Background(), "HK00700")
	require.NoError(t, err)
	require.False(t, Equity(res.Profile))
	require.NoError(t, res.ProfileErr, "权证没有公司资料不算错误")
	require.NoError(t, res.ValuationErr)
}
