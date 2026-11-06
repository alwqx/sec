package sina

import (
	"context"
	"testing"

	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gopkg.in/h2non/gock.v1"
)

const hkInfoBody = `var hq_str_hk00700="TENCENT,腾讯控股,421.000,430.400,428.600,416.400,416.600,-13.800,-3.206,416.60001,416.79999,10996220570,26009723,0.000,0.000,654.376,375.200,2026/11/06,15:35";
var hq_str_hk00700_i="EQTY,MAIN,654.376,375.200,3.5211,0,0,9092644829,0,9092644829,0,461968796828.17,131385642241.85,14.55,1,腾讯控股,3.700,0.2,100,腾讯控股,85112.696,248933814574.520,271150768522.250,1407533245006.300,,,1.2348,143.832377,,592.101|383.600|383.600|428.200|636.583,10.162965,5.539410,51,HKD,A,1";
`

func mockHKInfo(t *testing.T) {
	gb, err := simplifiedchinese.GBK.NewEncoder().String(hkInfoBody)
	require.NoError(t, err)
	gock.New("https://hq.sinajs.cn").Get("/list=hk00700,hk00700_i").Reply(200).SetHeader("Content-Type", "application/javascript; charset=GBK").BodyString(gb)
}

// 港股只有新浪的行情部分，不请求 A 股的公司资料页。
func TestProfileHK(t *testing.T) {
	defer gock.Off()
	mockHKInfo(t)

	p, err := Profile(context.Background(), &types.InfoOptions{Code: "00700", ExCode: "HK00700"})
	require.NoError(t, err)
	require.Equal(t, "HK00700", p.ExCode)
	require.Equal(t, "腾讯控股", p.Name)
	require.Equal(t, "腾讯控股", p.HistoryName)
	require.Empty(t, p.Category, "新浪的行业编号不当作行业")
	require.Zero(t, p.PeTTM)
	require.EqualValues(t, 416.6, p.Current)
	require.InDelta(t, 416.6*9092644829, p.MarketCap, 1)
	require.False(t, p.HKTrust)
	require.True(t, gock.IsDone())
}

// 盈富基金等交易所买卖基金没有每股净资产，这一列为空。
func TestParseInfoPartProfileOfHstockEmptyFields(t *testing.T) {
	line := `TRST,MAIN,28.320,22.920,0,0,0,0,0,0,0,0.00,0.00,0.00,1,盈富基金,0.000,0.02,500,盈富基金,0.000,0.000,0.000,0.000,,,,,,25.820|23.420|23.420|24.920|26.600,,,62,HKD,C,1`
	p, err := parseInfoPartProfileOfHstock(line)
	require.NoError(t, err)
	require.Zero(t, p.VPS)
	require.Equal(t, "62", p.Category)
	require.True(t, p.HKTrust)
}
