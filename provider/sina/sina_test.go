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
	require.Equal(t, "2025/05/27", quote1.TradeDate)
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

func TestMultiSearch(t *testing.T) {
	defer gock.Off()
	res, err := MultiSearch(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, res)

	// 超过上限时只查前面的关键字，也不能一直等不存在的结果。
	keys := make([]string, MAX_KEY_NUM+3)
	for i := range keys {
		keys[i] = fmt.Sprint(i)
	}
	searchURL().Times(MAX_KEY_NUM).Reply(200).BodyString(`var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`)
	res, err = MultiSearch(context.Background(), keys)
	require.NoError(t, err)
	require.Len(t, res, MAX_KEY_NUM)

	// 部分失败时返回成功的部分。
	searchURL().Reply(200).BodyString(`var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`)
	searchURL().Reply(500)
	res, err = MultiSearch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, res, 1)

	// 全部失败时返回错误。
	searchURL().Times(2).Reply(500)
	_, err = MultiSearch(context.Background(), []string{"a", "b"})
	require.ErrorContains(t, err, "HTTP 500")

	// 一个失败、一个没有匹配时也没有结果，不能当成查无结果。
	searchURL().Reply(200).BodyString(`var suggestvalue="";`)
	searchURL().Reply(500)
	_, err = MultiSearch(context.Background(), []string{"a", "b"})
	require.ErrorContains(t, err, "HTTP 500")

	// 都没有匹配才是空结果。
	searchURL().Times(2).Reply(200).BodyString(`var suggestvalue="";`)
	res, err = MultiSearch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Empty(t, res)
}
