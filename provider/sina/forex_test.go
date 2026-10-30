package sina

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/forex"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/simplifiedchinese"
	"gopkg.in/h2non/gock.v1"
)

func fxSample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/forex-" + name + ".txt")
	require.NoError(t, err)
	return raw
}

func TestParseForexQuotePairsAndTimes(t *testing.T) {
	raw := fxSample(t, "quotes")
	items, err := parseForexQuotes(raw, forex.MarketPairs)
	require.NoError(t, err)
	require.Len(t, items, 4)
	byPair := map[string]forex.Observation{}
	for _, item := range items {
		byPair[item.Pair] = item
		require.Contains(t, item.ObservedAt, "+08:00")
		require.Equal(t, "2026-10-30", item.Date)
	}
	require.Equal(t, 6.655, *byPair["USD/CNH"].Value)
	require.Equal(t, "offshore", byPair["USD/CNH"].Market)
	require.Equal(t, 6.659, *byPair["USD/CNY"].Bid)
	require.Equal(t, 6.6591, *byPair["USD/CNY"].Ask)
	require.Equal(t, "onshore", byPair["USD/CNY"].Market)
	require.Contains(t, byPair["HKD/CNY"].Note, "计算得出")
	require.Equal(t, "HKD", byPair["HKD/CNY"].BaseCurrency)
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes(raw)
	require.NoError(t, err)
	decoded, err := parseForexQuotes(gb, forex.MarketPairs)
	require.NoError(t, err)
	require.Equal(t, items, decoded)
	for _, changed := range []string{
		"<html>403</html>",
		strings.Replace(string(raw), "10:45:01", "99:99:99", 1),
		strings.Replace(string(raw), "2026-10-30", "2026-02-30", 1),
		strings.Replace(string(raw), "6.6591000000", "6.0000000000", 1),
		strings.Replace(string(raw), "6.6590000000", "-1", 1),
		string(raw) + strings.Split(string(raw), "\n")[0],
	} {
		_, err := parseForexQuotes([]byte(changed), forex.MarketPairs)
		require.Error(t, err)
	}
	_, err = parseForexQuotes(raw, []string{"USD/CNH"})
	require.Error(t, err, "不能把其他币种报价错配给请求币种")
}

func TestForexQuotesGBKCacheAndCancellation(t *testing.T) {
	defer gock.Off()
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes(fxSample(t, "quotes"))
	require.NoError(t, err)
	gock.New("https://hq.sinajs.cn").Get("/").MatchHeader("Referer", SinaReferer).Reply(200).BodyString(string(gb))
	client := NewForexClient(t.TempDir())
	req := &forex.Request{Pairs: forex.MarketPairs}
	result, err := client.ForexQuotes(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 4)
	again, err := client.ForexQuotes(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, again)
	require.Equal(t, "indicative-quote", result.PriceType)
	require.NotEmpty(t, result.Observations[0].Source)
	require.False(t, result.Observations[0].FetchedAt.IsZero())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.ForexQuotes(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	_, err = client.ForexQuotes(context.Background(), nil)
	require.Error(t, err)
	require.True(t, gock.IsDone())
}

func TestParseForexHistoryOHLCAndIntegrity(t *testing.T) {
	items, err := parseForexHistory(fxSample(t, "history"), "USD/CNH")
	require.NoError(t, err)
	require.Len(t, items, 3)
	require.Equal(t, "2014-11-07", items[0].Date)
	require.Equal(t, 6.1375, *items[0].Open)
	require.Equal(t, 6.1303, *items[0].Low)
	require.Equal(t, 6.1377, *items[0].High)
	require.Equal(t, 6.1341, *items[0].Close)
	require.Equal(t, items[0].Close, items[0].Value)
	for _, body := range []string{
		`alert("bad")`, `var _data=("");`,
		`var _data=("2024-01-02,7,6,8");`,
		`var _data=("2024-01-02,7,6,8,NaN");`,
		`var _data=("2024-02-30,7,6,8,7");`,
		`var _data=("2024-01-02,7,6,8,7|2024-01-02,7,6,8,7");`,
	} {
		_, err := parseForexHistory([]byte(body), "USD/CNH")
		require.Error(t, err, body)
	}
	invalid, err := parseForexHistory([]byte(`var _data=("2011-10-03,6.3340,6.3745,6.4830,6.3755,|2011-10-04,6.4,6.3,6.5,6.4");`), "USD/CNY")
	require.NoError(t, err)
	require.Len(t, invalid, 2)
	require.Nil(t, invalid[0].Value)
	require.Nil(t, invalid[0].Open)
	require.Nil(t, invalid[0].Close)
	require.Nil(t, invalid[0].High)
	require.Nil(t, invalid[0].Low)
	require.Contains(t, invalid[0].Note, "异常")
	require.Equal(t, "6.3340", invalid[0].RawValues["open"])
	require.Equal(t, 6.4, *invalid[1].Value)
}

func TestForexHistoryBoundsAndCache(t *testing.T) {
	defer gock.Off()
	gock.New("https://vip.stock.finance.sina.com.cn").Get("/forex/api/jsonp.php/").MatchParam("symbol", "fx_susdcnh").Reply(200).BodyString(string(fxSample(t, "history")))
	client := NewForexClient(t.TempDir())
	req := &forex.Request{Pairs: []string{"USD/CNH"}, Begin: "20141107", End: "20141110"}
	result, err := client.ForexHistory(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
	require.Equal(t, [2]string{"2014-11-07", "2026-10-29"}, result.SnapshotCoverage["USD/CNH"])
	require.Equal(t, "unspecified", result.Timezone)
	require.Empty(t, result.Observations[0].ObservedAt)
	again, err := client.ForexHistory(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, again)
	req.Begin, req.End = "20100101", "20100131"
	result, err = client.ForexHistory(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, result.Observations)
	require.NotEmpty(t, result.SnapshotCoverage)
	require.True(t, gock.IsDone())
}
