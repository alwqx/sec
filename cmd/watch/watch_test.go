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
