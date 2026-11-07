package edgar

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestFactsCache(t *testing.T) {
	defer gock.Off()
	var cache FactsCache
	// 只模拟一次下载：第二次从缓存读取。
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000000001.json").Reply(200).BodyString(testFacts)
	first, err := cache.Facts(context.Background(), client(t), 1)
	require.NoError(t, err)
	second, err := cache.Facts(context.Background(), client(t), 1)
	require.NoError(t, err)
	require.Same(t, first, second)
	require.True(t, gock.IsDone())

	// 下载失败不缓存，下次重新下载。
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000000002.json").Reply(404)
	_, err = cache.Facts(context.Background(), client(t), 2)
	require.Error(t, err)
	require.NotContains(t, cache.entries, 2)

	// 过期后重新下载。
	cache.entries[1] = factsEntry{time.Now().Add(-factsCacheTTL), first}
	gock.New("https://data.sec.gov").Get("/api/xbrl/companyfacts/CIK0000000001.json").Reply(200).BodyString(testFacts)
	refreshed, err := cache.Facts(context.Background(), client(t), 1)
	require.NoError(t, err)
	require.NotSame(t, first, refreshed)
	require.True(t, gock.IsDone())
}

func TestFactsCacheEviction(t *testing.T) {
	var cache FactsCache
	now := time.Now()
	cache.put(1, &Facts{Name: "expired"}, now.Add(-factsCacheTTL))
	cache.put(2, &Facts{Name: "fresh"}, now.Add(-time.Minute))
	cache.put(3, &Facts{Name: "new"}, now)
	require.NotContains(t, cache.entries, 1)
	require.Contains(t, cache.entries, 2)
	require.Contains(t, cache.entries, 3)

	cache = FactsCache{}
	for i := 0; i < factsCacheMaxEntries+1; i++ {
		cache.put(i, &Facts{CIK: i}, now.Add(time.Duration(i)*time.Second))
	}
	require.Len(t, cache.entries, factsCacheMaxEntries)
	require.NotContains(t, cache.entries, 0, "淘汰最早写入的公司")
	cache.put(1, &Facts{Name: "refreshed"}, now.Add(time.Minute))
	require.Len(t, cache.entries, factsCacheMaxEntries, "更新已有公司不淘汰其他公司")
	require.Equal(t, "refreshed", cache.entries[1].facts.Name)
}

func TestFactsCacheLoader(t *testing.T) {
	var cache FactsCache
	facts := &Facts{Name: "cached"}
	cache.put(1, facts, time.Now())
	got, err := cache.Loader(client(t))(context.Background(), 1)
	require.NoError(t, err)
	require.Same(t, facts, got, "通过缓存读取")
}
