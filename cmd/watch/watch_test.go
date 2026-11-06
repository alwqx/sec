package watch

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/watchlist"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestCLIUsesSharedStore(t *testing.T) {
	defer gock.Off()
	store := watchlist.Store{Path: filepath.Join(t.TempDir(), "watchlist.json")}
	previous := newWatchStore
	newWatchStore = func() (watchlist.Store, error) { return store, nil }
	defer func() { newWatchStore = previous }()
	_, _, err := store.Add(context.Background(), sina.BasicSecurity{Code: "600036", ExCode: "SH600036", Name: "招商银行", SecurityType: types.SecurityTypeStock})
	require.NoError(t, err)
	command := NewWatchCLI()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetArgs([]string{"remove", "sh600036"})
	require.NoError(t, command.Execute())
	items, err := store.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, items)
	// CLI 搜索证券后调用公共增添逻辑，元数据可供 Desktop 直接使用。
	gock.New("https://suggest3.sinajs.cn").Get("/suggest").Reply(200).BodyString(`var suggestvalue="ETF,22,510300,sh510300,ETF,,ETF,99,1,,,";`)
	command = NewWatchCLI()
	command.SetOut(output)
	command.SetArgs([]string{"add", "510300"})
	require.NoError(t, command.Execute())
	items, err = store.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "SH510300", items[0].ExCode)
	require.Equal(t, types.SecurityTypeFund, items[0].SecurityType)
	require.Equal(t, types.ExChangeSse, items[0].Exchange)
	require.True(t, gock.IsDone())
}

func TestPrintWatchQuotesMissingValues(t *testing.T) {
	var out bytes.Buffer
	printWatchQuotes(&out, []quoteRow{
		{item: WatchItem{ExCode: "SH600036", Name: "招商银行"}, price: 43.55, priceIsYClose: true},
		{item: WatchItem{ExCode: "SZ000001", Name: "平安银行"}},
		{item: WatchItem{ExCode: "SH600519", Name: "贵州茅台"}, price: 11.5, hasChange: true, chg: 1.25, chgPct: 9.5, high: 12.5, low: 9.5},
	})
	text := out.String()
	require.Contains(t, text, "43.55（昨收）")
	require.NotContains(t, text, "-100")
	require.NotContains(t, text, "0.00", "缺失不能显示成 0")
	require.Contains(t, text, "+9.50%")

	out.Reset()
	printWatchQuotes(&out, []quoteRow{
		{item: WatchItem{ExCode: "OF005827", Name: "易方达蓝筹精选混合"}, digits: 4, price: 1.4164, hasChange: true, chg: 0.0155, chgPct: 1.106},
		{item: WatchItem{ExCode: "OF510300", Name: "沪深300ETF"}, digits: 3, price: 4.125, priceIsYClose: true, high: 4.169, low: 4.095},
	})
	text = out.String()
	require.Contains(t, text, "1.4164", "基金净值保留 4 位小数")
	require.Contains(t, text, "+0.0155")
	require.Contains(t, text, "4.125（昨收）", "场内基金的昨收也保留 3 位小数")
	require.Contains(t, text, "4.169")
	require.Equal(t, 4, priceDigits(&sina.SecurityQuote{ExCode: "OF005827", NAV: true}))
	require.Equal(t, 3, priceDigits(&sina.SecurityQuote{ExCode: "OF510300"}))
	require.Equal(t, 2, priceDigits(&sina.SecurityQuote{ExCode: "SH600519"}))
}

func TestWatchAddContinuesAfterSearchFailure(t *testing.T) {
	defer gock.Off()
	store := watchlist.Store{Path: filepath.Join(t.TempDir(), "watchlist.json")}
	previous := newWatchStore
	newWatchStore = func() (watchlist.Store, error) { return store, nil }
	defer func() { newWatchStore = previous }()
	body := `var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(500)
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).BodyString(body)

	cmd := NewWatchCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"add", "bad", "招商银行"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.ExecuteContext(context.Background())
	require.ErrorContains(t, err, "搜索 bad 失败")
	require.Contains(t, out.String(), "bad 搜索失败")
	require.Contains(t, out.String(), "✓ SH600036")
	require.Contains(t, out.String(), "已添加 1 只")
}

func TestWatchJSONRowUsesNullForMissingValues(t *testing.T) {
	row := newWatchJSONRow(quoteRow{item: WatchItem{ExCode: "SH600036", Name: "招商银行"}, price: 43.55, priceIsYClose: true})
	require.Equal(t, 43.55, *row.Price)
	require.True(t, row.PriceIsYClose)
	require.Nil(t, row.Change)
	require.Nil(t, row.High)
	require.Nil(t, newWatchJSONRow(quoteRow{}).Price)
}
