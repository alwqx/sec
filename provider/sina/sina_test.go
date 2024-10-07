package sina

import (
	"fmt"
	"os"
	"testing"

	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
)

func TestParseBasicSecuritys(t *testing.T) {
	body := `var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,";`
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
	body2 := `var suggestvalue="汇泉兴至未来一年持有混合C,21,014826,of014826,汇泉兴至未来一年持有混合C,,汇泉兴至未来一年持有混合C,99,1,,";`
	res = parseBasicSecurity(body2)
	fmt.Println(res)
}

func TestProfile(t *testing.T) {
	// Profile("688047")
	quote, part, err := Info("SH688047")
	require.Nil(t, err)
	require.NotNil(t, quote)
	require.NotNil(t, part)
}

func TestParseSinaInfoQuote(t *testing.T) {
	body := `"龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,"`
	quote := parseSecQuote(body)
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
}

func TestDefaultHttpHeaders(t *testing.T) {
	hs := defaultHttpHeaders()
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
