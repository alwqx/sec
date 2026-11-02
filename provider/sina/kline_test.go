package sina

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestQueryDailyBars(t *testing.T) {
	defer gock.Off()
	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service/api/json_v2.php/CN_MarketData.getKLineData").
		MatchParam("symbol", "sh600036").MatchParam("datalen", "1023").
		MatchHeader("Referer", "finance.sina.com.cn").Reply(200).
		BodyString(`[{"day":"2026-10-29","open":"42.500","high":"43.030","low":"42.090","close":"42.940","volume":"108749446"}]`)
	bars, err := QueryDailyBars(context.Background(), "sh600036", MaxDailyBars)
	require.NoError(t, err)
	require.Equal(t, []*DailyBar{{Date: "2026-10-29", Open: 42.5, High: 43.03, Low: 42.09, Close: 42.94, Volume: 108749446}}, bars)

	gock.New("https://money.finance.sina.com.cn").Get("/quotes_service").Reply(456)
	_, err = QueryDailyBars(context.Background(), "sh600036", MaxDailyBars)
	require.ErrorContains(t, err, "HTTP 456", "新浪限流时返回 456")
}

func TestParseDailyBars(t *testing.T) {
	for _, empty := range []string{"", "null", " null\n"} {
		bars, err := parseDailyBars([]byte(empty))
		require.NoError(t, err)
		require.Empty(t, bars, "代码不存在")
	}
	for _, bad := range []string{"<html>", `[{"day":"2026-10-29","open":"x","high":"1","low":"1","close":"1","volume":"1"}]`, `[{"day":"2026-10-29","open":"1","high":"1","low":"1","close":"1","volume":"x"}]`} {
		_, err := parseDailyBars([]byte(bad))
		require.Error(t, err, bad)
	}
}
