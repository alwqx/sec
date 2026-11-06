package eastmoney

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const (
	hkOrgBody = `{"result":{"data":[{"SECUCODE":"00700.HK","SECURITY_NAME_ABBR":"腾讯控股","ORG_NAME":"腾讯控股有限公司","ORG_EN_ABBR":"Tencent Holdings Limited","BELONG_INDUSTRY":"软件服务","LISTING_DATE":"2004-06-16 00:00:00","CHAIRMAN":"马化腾","ORG_WEB":"www.tencent.com","ADDRESS":"深圳市南山区","REG_ADDRESS":"Grand Cayman","ORG_PROFILE":"    腾讯是一家互联网科技公司。\r\n\u3000\u30002004 年在香港上市。","MAIN_BUSINESS":"增值服务"}]},"success":true,"code":0}`
	hkSecBody = `{"result":{"data":[{"SECUCODE":"00700.HK","ISSUE_PRICE":3.7}]},"success":true,"code":0}`
	emptyBody = `{"result":null,"success":false,"message":"返回数据为空","code":9201}`
)

func mockHKReport(report string) *gock.Request {
	return gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").
		MatchParam("reportName", report).MatchParam("filter", `\(SECUCODE="00700\.HK"\)`)
}

func mockHKQuote() *gock.Request {
	return gock.New("https://push2.eastmoney.com").Get("/api/qt/stock/get").MatchParam("secid", `116\.00700`)
}

func TestGetHKProfile(t *testing.T) {
	defer gock.Off()
	mockHKReport("RPT_HKF10_INFO_ORGPROFILE").MatchParam("columns", "^"+hkOrgColumns+"$").Reply(200).BodyString(hkOrgBody)
	mockHKReport("RPT_HKF10_INFO_SECURITYINFO").MatchParam("columns", "^ISSUE_PRICE$").Reply(200).BodyString(hkSecBody)

	p, err := GetHKProfile(context.Background(), "00700")
	require.NoError(t, err)
	require.Equal(t, "腾讯控股", p.ShortName)
	require.Equal(t, "腾讯控股有限公司", p.Name)
	require.Equal(t, "软件服务", p.Industry)
	require.Equal(t, "2004-06-16", p.ListingDate)
	require.Equal(t, "腾讯是一家互联网科技公司。 2004 年在香港上市。", p.Description, "换行和全角空格合并成一个空格")
	require.Equal(t, 3.7, *p.IssuePrice)
	require.True(t, gock.IsDone())
}

func TestGetHKProfileOptionalAndMissing(t *testing.T) {
	defer gock.Off()
	// 发行价是补充数据，取不到时留空，公司资料照常返回。
	mockHKReport("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(hkOrgBody)
	mockHKReport("RPT_HKF10_INFO_SECURITYINFO").Reply(503)
	p, err := GetHKProfile(context.Background(), "00700")
	require.NoError(t, err)
	require.Equal(t, "腾讯控股有限公司", p.Name)
	require.Nil(t, p.IssuePrice)
	require.ErrorContains(t, p.IssuePriceErr, "503", "发行价失败的原因交给调用方")

	mockHKReport("RPT_HKF10_INFO_ORGPROFILE").Reply(200).BodyString(emptyBody)
	mockHKReport("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(emptyBody)
	p, err = GetHKProfile(context.Background(), "00700")
	require.NoError(t, err)
	require.Nil(t, p, "没有这只证券")

	mockHKReport("RPT_HKF10_INFO_ORGPROFILE").Reply(503)
	mockHKReport("RPT_HKF10_INFO_SECURITYINFO").Reply(200).BodyString(hkSecBody)
	_, err = GetHKProfile(context.Background(), "00700")
	require.ErrorContains(t, err, "503", "公司资料失败要报错")
}

func TestGetHKValuation(t *testing.T) {
	defer gock.Off()
	mockHKQuote().Reply(200).BodyString(`{"rc":0,"data":{"f164":1432,"f167":292}}`)
	pe, pb, err := GetHKValuation(context.Background(), "00700")
	require.NoError(t, err)
	require.InDelta(t, 14.32, *pe, 1e-9)
	require.InDelta(t, 2.92, *pb, 1e-9)

	mockHKQuote().Reply(200).BodyString(`{"rc":0,"data":{"f164":-523,"f167":-35}}`)
	pe, pb, err = GetHKValuation(context.Background(), "00700")
	require.NoError(t, err)
	require.Nil(t, pe, "亏损时没有市盈率")
	require.InDelta(t, -0.35, *pb, 1e-9, "负的市净率保留")

	mockHKQuote().Reply(200).BodyString(`{"rc":0,"data":{"f164":0,"f167":"-"}}`)
	pe, pb, err = GetHKValuation(context.Background(), "00700")
	require.NoError(t, err)
	require.Nil(t, pe, "0 表示没有数据")
	require.Nil(t, pb)

	mockHKQuote().Reply(503)
	_, _, err = GetHKValuation(context.Background(), "00700")
	require.ErrorContains(t, err, "503")
}
