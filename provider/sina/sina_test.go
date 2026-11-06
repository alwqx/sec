package sina

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestParseBasicSecuritys(t *testing.T) {
	body := `var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,,";`
	res := parseBasicSecurity(body)
	require.Equal(t, 2, len(res))
	require.Equal(t, "龙芯中科", res[0].Name)
	require.Equal(t, "SH688047", res[0].ExCode)
	require.Equal(t, "sh", res[0].ExChange)
	require.Equal(t, types.SecurityTypeStock, res[0].SecurityType)

	require.Equal(t, "绿叶制药", res[1].Name)
	require.Equal(t, "HK02186", res[1].ExCode)
	require.Equal(t, "hk", res[1].ExChange)
	require.Equal(t, types.SecurityTypeStock, res[1].SecurityType)

	// 证券
	body2 := `var suggestvalue="汇泉兴至未来一年持有混合C,21,014826,of014826,汇泉兴至未来一年持有混合C,,汇泉兴至未来一年持有混合C,99,1,,,";`
	res = parseBasicSecurity(body2)
	fmt.Println(res)
}

// 搜索只依赖前 9 列；新浪会增减后面的可选字段，不能按固定总列数丢掉有效证券。
func TestParseBasicSecurityOptionalFields(t *testing.T) {
	base := "龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1"
	for _, tail := range []string{"", ",ESG,,", ",ESG,,,", ",ESG,,,new-field,another-field"} {
		t.Run(fmt.Sprintf("%d_columns", len(strings.Split(base+tail, ","))), func(t *testing.T) {
			res := parseBasicSecurity(`var suggestvalue="` + base + tail + `";`)
			require.Len(t, res, 1)
			require.Equal(t, "龙芯中科", res[0].Name)
			require.Equal(t, "688047", res[0].Code)
			require.Equal(t, "SH688047", res[0].ExCode)
			require.Equal(t, types.SecurityTypeStock, res[0].SecurityType)
		})
	}

	res := parseBasicSecurity(`var suggestvalue="too,short;龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99;已退市,11,688047,sh688047,已退市,,已退市,99,0,ESG,,,;` + base + `,ESG,,,";`)
	require.Len(t, res, 1, "仍跳过缺少必需字段和非在市的记录")
	require.Equal(t, "SH688047", res[0].ExCode)
}

func TestSearchExpandedFields(t *testing.T) {
	defer gock.Off()
	searchURL().Reply(200).BodyString(`var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,ESG,,,;万讯自控,11,300112,sz300112,万讯自控,,万讯自控,99,1,,,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,,,;龙鑫智能,11,920117,bj920117,龙鑫智能,,龙鑫智能,99,1,,,,;AAPL,41,aapl,aapl,苹果,,苹果,99,1,ESG,,,";`)
	res, err := Search(context.Background(), "lxzk")
	require.NoError(t, err)
	require.Len(t, res, 5)
	for i, code := range []string{"SH688047", "SZ300112", "HK02186", "BJ920117", "$AAPL"} {
		require.Equal(t, code, res[i].ExCode)
	}
	require.True(t, gock.IsDone())
}

func TestProfile(t *testing.T) {
	t.Skip("just test for dev/debug")
	ctx := context.TODO()
	// Profile("688047")
	quote, part, err := Info(ctx, "SH688047")
	require.Nil(t, err)
	require.NotNil(t, quote)
	require.NotNil(t, part)
}

func TestParseSinaInfoQuote(t *testing.T) {
	// 0. A 股
	exCode := "SH688047"
	body := `"龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,"`
	quote, err := parseSecQuote(exCode, body)
	require.Nil(t, err)
	require.NotNil(t, quote)
	require.Equal(t, "2024-09-30", quote.TradeDate)
	require.EqualValues(t, "15:00:01", quote.Time)
	require.Equal(t, "", quote.Code)
	require.Equal(t, "龙芯中科", quote.Name)
	require.EqualValues(t, 119.62, quote.Current)
	require.EqualValues(t, 106.00, quote.Open)
	require.EqualValues(t, 99.68, quote.YClose)
	require.EqualValues(t, 119.62, quote.High)
	require.EqualValues(t, 104.5, quote.Low)
	require.EqualValues(t, 8256723, quote.TurnOver)
	require.EqualValues(t, 938310086.000, quote.Volume)

	// 1. H 股
	exCode1 := "HK00700"
	body1 := `"TENCENT,腾讯控股,508.500,510.000,514.500,507.000,512.000,2.000,0.392,512.00000,512.50000,7662280393,14986877,0.000,0.000,542.266,345.980,2025/05/27,16:08"`
	quote1, err := parseSecQuote(exCode1, body1)
	require.Nil(t, err)
	require.NotNil(t, quote1)
	require.Equal(t, "2025-05-27", quote1.TradeDate)
	require.EqualValues(t, "16:08", quote1.Time)
	require.Equal(t, "", quote1.Code)
	require.Equal(t, "腾讯控股", quote1.Name)
	require.EqualValues(t, 512.000, quote1.Current)
	require.EqualValues(t, 508.500, quote1.Open)
	require.EqualValues(t, 510.000, quote1.YClose)
	require.EqualValues(t, 514.500, quote1.High)
	require.EqualValues(t, 507.000, quote1.Low)
	require.EqualValues(t, 14986877, quote1.TurnOver)
	require.EqualValues(t, 7662280393, quote1.Volume)

	// 2. 非 A/H 股
	exCode2 := "xxx"
	body2 := `"龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,"`
	quote2, err := parseSecQuote(exCode2, body2)
	require.Equal(t, fmt.Errorf("unsupported code %s", exCode2), err)
	require.Nil(t, quote2)
}

func TestDefaultHttpHeaders(t *testing.T) {
	hs := defaultHTTPHeaders()
	require.Equal(t, 1, len(hs))
	require.Equal(t, SinaReferer, hs.Get("Referer"))
	require.Equal(t, "", hs.Get("Others"))
}

func TestParseDividend(t *testing.T) {
	// 读取 body 信息
	data, err := os.ReadFile("./testdata/corp_ShareBonus_stockid_600036.html")
	require.Nil(t, err)
	res, err := parseDividend(data)
	require.Nil(t, err)
	require.NotNil(t, res)
	require.Equal(t, 24, len(res))

	// 2024年
	require.EqualValues(t, 19.72, res[0].Bonus)
	require.EqualValues(t, "2024-07-04", res[0].PublicDate)
	require.EqualValues(t, "2024-07-11", res[0].DividendedDate)
	require.EqualValues(t, "2024-07-10", res[0].RecordDate)
	require.EqualValues(t, 0, res[0].Shares)
	require.EqualValues(t, 0, res[0].AddShares)

	// 2009年
	require.EqualValues(t, 3, res[15].Shares)

	// 2004年
	require.EqualValues(t, 0.92, res[22].Bonus)

	// 2003年
	require.EqualValues(t, 1.2, res[23].Bonus)
	require.EqualValues(t, "2003-07-08", res[23].PublicDate)
	require.EqualValues(t, "2003-07-16", res[23].DividendedDate)
	require.EqualValues(t, "2003-07-15", res[23].RecordDate)
	require.EqualValues(t, 0, res[23].Shares)
	require.EqualValues(t, 0, res[23].AddShares)
}

func searchURL() *gock.Request {
	return gock.New("https://suggest3.sinajs.cn").Get("/suggest/")
}

func TestSearch(t *testing.T) {
	defer gock.Off()
	body := `var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`
	searchURL().Reply(200).BodyString(body)
	secs, err := Search(context.Background(), "招商银行")
	require.NoError(t, err)
	require.Equal(t, "SH600036", secs[0].ExCode)

	searchURL().Reply(200).BodyString(`var suggestvalue="";`)
	secs, err = Search(context.Background(), "没有这只证券")
	require.NoError(t, err, "没有匹配不是错误")
	require.Empty(t, secs)

	searchURL().Reply(503)
	_, err = Search(context.Background(), "招商银行")
	require.ErrorContains(t, err, "HTTP 503")

	searchURL().Reply(200).BodyString("<html>验证页面</html>")
	_, err = Search(context.Background(), "招商银行")
	require.ErrorContains(t, err, "无法识别", "200 的异常页面不是没有匹配")

	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").ReplyError(errors.New("network down"))
	_, err = Search(context.Background(), "招商银行")
	require.Error(t, err, "网络失败不能当成没有结果")
}

// 带 HK 前缀的港股代码改用数字搜索，只留下代码相同的港股，不要同数字的 A 股和基金。
func TestSearchHKPrefix(t *testing.T) {
	defer gock.Off()
	body := `var suggestvalue="00700,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG,,,;sz000700,11,000700,sz000700,模塑科技,,模塑科技,99,1,,,,;of007002,21,007002,of007002,某基金,,某基金,99,1,,,,";`
	for _, key := range []string{"hk00700", "HK00700", " Hk700 ", "hk0700"} {
		var got string
		searchURL().AddMatcher(func(req *http.Request, _ *gock.Request) (bool, error) {
			got = req.URL.EscapedPath()
			return true, nil
		}).Reply(200).BodyString(body)
		secs, err := Search(context.Background(), key)
		require.NoError(t, err, key)
		require.True(t, strings.HasSuffix(got, "key=00700"), "%s -> %s", key, got)
		require.Len(t, secs, 1, key)
		require.Equal(t, "HK00700", secs[0].ExCode, key)
		require.Equal(t, "腾讯控股", secs[0].Name, key)
	}

	searchURL().Reply(200).BodyString(`var suggestvalue="sz000700,11,000700,sz000700,模塑科技,,模塑科技,99,1,,,,";`)
	secs, err := Search(context.Background(), "hk00700")
	require.NoError(t, err)
	require.Empty(t, secs, "没有这只港股时不能返回同数字的 A 股")

	searchURL().Reply(503)
	_, err = Search(context.Background(), "hk00700")
	require.ErrorContains(t, err, "HTTP 503", "搜索失败仍然报错")

	// 不是 hk 加数字的关键字照常搜索。
	for _, key := range []string{"hk", "hk123456", "hkd", "腾讯"} {
		var got string
		searchURL().AddMatcher(func(req *http.Request, _ *gock.Request) (bool, error) {
			got = req.URL.EscapedPath()
			return true, nil
		}).Reply(200).BodyString(`var suggestvalue="";`)
		_, err := Search(context.Background(), key)
		require.NoError(t, err, key)
		require.False(t, strings.HasSuffix(got, "key=00700"), key)
		require.Contains(t, got, "key=", key)
	}
}

// 关键字里的 & 和 = 不能变成新的参数。
func TestSearchEscapesKeyword(t *testing.T) {
	defer gock.Off()
	for key, want := range map[string]string{"AT&T": "key=AT%26T", "a=b": "key=a%3Db", "a b": "key=a%20b", "a+b": "key=a%2Bb", "招商": "key=%E6%8B%9B%E5%95%86"} {
		var got string
		searchURL().AddMatcher(func(req *http.Request, _ *gock.Request) (bool, error) {
			got = req.URL.EscapedPath()
			return true, nil
		}).Reply(200).BodyString(`var suggestvalue="";`)
		_, err := Search(context.Background(), key)
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(got, want), "%s -> %s", key, got)
	}
}

// 搜索接口失败时，完整代码按代码处理；名称、简称等不是完整代码的关键字仍然报错。
func TestResolveFallsBackToCode(t *testing.T) {
	defer gock.Off()
	ctx := context.Background()
	for _, tc := range []struct {
		key  string
		want BasicSecurity
	}{
		{"sh600519", BasicSecurity{Code: "600519", ExCode: "SH600519", ExChange: "sh", SecurityType: types.SecurityTypeStock}},
		{" SZ000001 ", BasicSecurity{Code: "000001", ExCode: "SZ000001", ExChange: "sz", SecurityType: types.SecurityTypeStock}},
		{"BJ920000", BasicSecurity{Code: "920000", ExCode: "BJ920000", ExChange: "bj", SecurityType: types.SecurityTypeStock}},
		{"hk00700", BasicSecurity{Code: "00700", ExCode: "HK00700", ExChange: types.ExChangeHKex, SecurityType: types.SecurityTypeStock}},
		{"$brk.b", BasicSecurity{Code: "brk.b", ExCode: "$BRK.B", ExChange: types.ExChangeNasdaq, SecurityType: types.SecurityTypeStock}},
	} {
		gock.New("https://suggest3.sinajs.cn").Reply(503)
		secs, err := Resolve(ctx, tc.key)
		require.NoError(t, err, tc.key)
		require.Len(t, secs, 1, tc.key)
		require.Equal(t, tc.want, *secs[0], tc.key)
	}

	for _, key := range []string{"茅台", "mt", "AAPL", "600519", "SH60051", "HK0070"} {
		gock.New("https://suggest3.sinajs.cn").Reply(503)
		_, err := Resolve(ctx, key)
		require.ErrorContains(t, err, "HTTP 503", key)
	}

	// 搜索正常时用搜索结果，带名称。
	gock.New("https://suggest3.sinajs.cn").Reply(200).
		BodyString(`var suggestvalue="贵州茅台,11,600519,sh600519,贵州茅台,,贵州茅台,99,1,ESG,,,";`)
	secs, err := Resolve(ctx, "sh600519")
	require.NoError(t, err)
	require.Equal(t, "贵州茅台", secs[0].Name)
	require.True(t, gock.IsDone())
}
