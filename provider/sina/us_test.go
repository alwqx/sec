package sina

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseUSDailyBars(t *testing.T) {
	bars, err := parseUSDailyBars([]byte(`/*<script>location.href='//sina.com';</script>*/
var t=([{"d":"2020-08-28","o":"504.05","h":"505.77","l":"498.31","c":"499.23","v":"46907479","a":"0"},{"d":"2020-08-31","o":"127.58","h":"131.00","l":"126.00","c":"129.04","v":"225702688"},{"d":"bad","o":"1","h":"1","l":"1","c":"1","v":"1"}]);`))
	require.NoError(t, err)
	require.Len(t, bars, 2, "无效行跳过")
	require.Equal(t, "2020-08-31", bars[1].Date)
	require.Equal(t, 129.04, bars[1].Close, "不复权，拆股当天价格跳变")
	require.Equal(t, int64(225702688), bars[1].Volume)

	bars, err = parseUSDailyBars([]byte(`var t=(null);`))
	require.NoError(t, err)
	require.Empty(t, bars)
}

func TestParseUSQuote(t *testing.T) {
	quote, err := parseUSQuote(`var hq_str_gb_aapl="苹果,331.9000,-1.90,2026-11-04 09:14:47,-6.4200,325.5800,334.5000,325.3300,351.7300,242.8900,63160026,45152763,4849655801006,8.30,39.990000,0.00,0.00,0.00,0.00,14611798135,63,329.800";`, "AAPL")
	require.NoError(t, err)
	require.Equal(t, "苹果", quote.Name)
	require.Equal(t, 331.9, quote.Price)
	require.Equal(t, -1.9, quote.ChangeRate)
	require.Equal(t, 4849655801006.0, *quote.MarketCap)
	require.Equal(t, 8.3, *quote.EPS)
	require.Equal(t, 39.99, *quote.PE)
	require.Equal(t, 14611798135.0, *quote.Shares)
	require.Equal(t, 242.89, *quote.Low52)

	// 亏损公司没有市盈率。
	quote, err = parseUSQuote(`var hq_str_gb_x="X,10,1,2026-11-04 09:00:00,0.1,1,1,1,12,8,1,1,1000,-2.5,0.000000,0,0,0,0,100,1";`, "X")
	require.NoError(t, err)
	require.Nil(t, quote.PE)
	require.Equal(t, -2.5, *quote.EPS, "亏损公司的每股收益是负数")

	_, err = parseUSQuote(`var hq_str_gb_zzzz="";`, "ZZZZ")
	require.ErrorContains(t, err, "没有 ZZZZ 的美股行情")
}

func TestUSQuoteKey(t *testing.T) {
	require.Equal(t, "gb_aapl", USQuoteKey("$AAPL"))
	require.Equal(t, "gb_brk$b", USQuoteKey("BRK.B"))
	require.Equal(t, "gb_brk$b", USQuoteKey("brk-b"))
	quote, err := parseUSQuote(`var hq_str_gb_brk$b="伯克希尔B,557.0700,0.17,2026-11-04 09:39:06,0.9600,554.2200,559,553,560,450,1,1,1,1,1,0,0,0,0,1,1";`, "BRK.B")
	require.NoError(t, err)
	require.Equal(t, "伯克希尔B", quote.Name)
}
