package edgar

import (
	"context"
	"sync"
	"time"
)

const (
	factsCacheTTL        = 10 * time.Minute
	factsCacheMaxEntries = 20
)

// FactsCache 按 CIK 缓存 CompanyFacts 10 分钟，切换报表、年度和季度或者先查财报再查估值时不用重新下载（大公司有几 MB）。
// 最多 20 家公司，零值可用，可以并发使用。Desktop 和 sec mcp 各用一个。
type FactsCache struct {
	mu      sync.Mutex
	entries map[int]factsEntry
}

type factsEntry struct {
	at    time.Time
	facts *Facts
}

// Facts 先查缓存，没有或过期时用 client 下载并写入缓存。下载失败不缓存。
func (c *FactsCache) Facts(ctx context.Context, client *Client, cik int) (*Facts, error) {
	c.mu.Lock()
	entry, ok := c.entries[cik]
	c.mu.Unlock()
	if ok && time.Since(entry.at) < factsCacheTTL {
		return entry.facts, nil
	}
	facts, err := client.CompanyFacts(ctx, cik)
	if err == nil {
		c.put(cik, facts, time.Now())
	}
	return facts, err
}

// put 写入时清理过期条目，达到上限时移除最早写入的公司。
func (c *FactsCache) put(cik int, facts *Facts, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[int]factsEntry{}
	}
	for key, entry := range c.entries {
		if now.Sub(entry.at) >= factsCacheTTL {
			delete(c.entries, key)
		}
	}
	c.entries[cik] = factsEntry{now, facts}
	for len(c.entries) > factsCacheMaxEntries {
		oldest := cik
		for key, entry := range c.entries {
			if entry.at.Before(c.entries[oldest].at) {
				oldest = key
			}
		}
		delete(c.entries, oldest)
	}
}
