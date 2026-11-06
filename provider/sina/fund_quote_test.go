package sina

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gopkg.in/h2non/gock.v1"
)

const (
	navLine     = `易方达蓝筹精选混合,1.4164,1.4164,1.4009,2026-11-05,129.913`
	etfLine     = `沪深300ETF华泰柏瑞,4.113,4.195,4.124,4.169,4.095,4.124,4.125,1262937040,5211672667.000,860175,4.124,1132600,4.123,909700,4.122,653200,4.121,3680100,4.120,2393800,4.125,277500,4.126,211200,4.127,330500,4.128,272400,4.129,2026-11-06,15:34:59,00,D|947900|3909139.60`
	etfNAVLine  = `华泰柏瑞沪深300ETF,4.1909,1.881,4.1785,2026-11-05,250.1`
	stockLine   = `贵州茅台,1450.000,1452.000,1453.000,1457.720,1436.000,1453.000,1453.010,4284772,6199528289.000,100,1453.000,200,1452.990,100,1452.980,100,1452.970,100,1452.960,100,1453.010,100,1453.020,100,1453.030,100,1453.040,100,1453.050,2026-11-06,15:00:00,00,`
	sinaGBKType = "application/javascript; charset=GBK"
)

func mockSinaList(t *testing.T, path, body string) {
	gb, err := simplifiedchinese.GBK.NewEncoder().String(body)
	require.NoError(t, err)
	gock.New("https://hq.sinajs.cn").Get(path).Reply(200).SetHeader("Content-Type", sinaGBKType).BodyString(gb)
}

func TestListedFundMarket(t *testing.T) {
	for code, market := range map[string]string{"510300": "sh", "588000": "sh", "159919": "sz", "161725": "sz", "184801": "sz", "005827": "", "110011": "", "000001": ""} {
		require.Equal(t, market, listedFundMarket(code), code)
	}
	require.True(t, IsFundCode("OF005827"))
	require.True(t, IsFundCode("of510300"))
	require.False(t, IsFundCode("SH510300"))
	require.False(t, IsFundCode("OF5103"))
}

func TestParseFundNAV(t *testing.T) {
	q, err := parseFundNAV(navLine)
	require.NoError(t, err)
	require.Equal(t, "易方达蓝筹精选混合", q.Name)
	require.Equal(t, 1.4164, q.Current)
	require.Equal(t, 1.4009, q.YClose)
	require.Equal(t, "2026-11-05", q.TradeDate)
	require.Empty(t, q.Time)
	require.True(t, q.NAV)

	_, err = parseFundNAV("易方达蓝筹精选混合,1.4164")
	require.Error(t, err)
}

// 基金和其他证券在同一次请求里：场内基金有价格时用交易所行情，没有价格（空行或全是 0）时用净值；
// 场外基金用净值；货币基金、解析不了的行和新浪没有的基金只跳过这一只，结果按请求顺序。
func TestQueryQuoteListWithFunds(t *testing.T) {
	defer gock.Off()
	zeroLine := `银河创新,0.000,0.000,0.000,0.000,0.000,0.000,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2026-11-06,09:00:00,00,`
	mockSinaList(t, "/list=sh600519,sh510300,f_510300,sz161725,f_161725,f_005827,sh519674,f_519674,f_000198,f_000001,f_999999",
		`var hq_str_sh600519="`+stockLine+`";
var hq_str_sh510300="`+etfLine+`";
var hq_str_f_510300="`+etfNAVLine+`";
var hq_str_sz161725="";
var hq_str_f_161725="招商中证白酒指数A,0.5926,2.3087,0.5843,2026-11-05,394.159";
var hq_str_f_005827="`+navLine+`";
var hq_str_sh519674="`+zeroLine+`";
var hq_str_f_519674="银河创新混合A,9.9996,9.9996,9.8614,2026-11-05,8.60379";
var hq_str_f_000198="天弘余额宝货币,0.2211,0.798,,2026-11-05,6451.36";
var hq_str_f_000001="华夏成长混合,--,1.0,--,2026-11-05,1.0";
var hq_str_f_999999="";
`)
	codes := []string{"OF510300", "SH600519", "OF161725", "of005827", "OF519674", "OF000198", "OF000001", "OF999999"}
	quotes, err := QueryQuoteList(context.Background(), codes)
	require.NoError(t, err)
	var got []string
	for _, q := range quotes {
		got = append(got, q.ExCode)
	}
	require.Equal(t, []string{"OF510300", "SH600519", "OF161725", "OF005827", "OF519674"}, got, "按请求顺序，跳过货币基金、坏行和没有的基金")

	require.False(t, quotes[0].NAV, "场内基金用交易所行情")
	require.Equal(t, 4.124, quotes[0].Current)
	require.Equal(t, "15:34:59", quotes[0].Time)
	require.True(t, quotes[2].NAV, "交易所空行时用净值")
	require.Equal(t, 0.5926, quotes[2].Current)
	require.True(t, quotes[3].NAV)
	require.True(t, quotes[4].NAV, "交易所全是 0 时用净值")
	require.Equal(t, 9.9996, quotes[4].Current)
	require.True(t, gock.IsDone())
}

// 只有基金时响应不是行情也要报错。
func TestQueryQuoteListFundsInvalidBody(t *testing.T) {
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=f_005827").Reply(200).BodyString("<html>busy</html>")
	_, err := QueryQuoteList(context.Background(), []string{"OF005827"})
	require.ErrorContains(t, err, "invalid quote list body")
}

func TestQuerySecQuoteFund(t *testing.T) {
	defer gock.Off()
	mockSinaList(t, "/list=f_005827", `var hq_str_f_005827="`+navLine+`";`+"\n")
	q, err := QuerySecQuote(context.Background(), "OF005827")
	require.NoError(t, err)
	require.Equal(t, "OF005827", q.ExCode)
	require.True(t, q.NAV)

	mockSinaList(t, "/list=f_999999", `var hq_str_f_999999="";`+"\n")
	_, err = QuerySecQuote(context.Background(), "OF999999")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestParseFundNAVMoneyFund(t *testing.T) {
	q, err := parseFundNAV("天弘余额宝货币,0.2211,0.798,,2026-11-05,6451.36")
	require.NoError(t, err)
	require.Nil(t, q, "货币基金没有单位净值")
	require.Equal(t, 2, QuoteKeyCount("OF510300"))
	require.Equal(t, 1, QuoteKeyCount("OF005827"))
	require.Equal(t, 1, QuoteKeyCount("SH600519"))
}
