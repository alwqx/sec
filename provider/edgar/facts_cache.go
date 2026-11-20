package edgar

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	factsCacheTTL        = 10 * time.Minute
	factsCacheMaxEntries = 20
)

// FactsCache 按 CIK 缓存 CompanyFacts 10 分钟，切换报表、年度和季度或者先查财报再查估值时不用重新下载（大公司有几 MB）。
// 最多 20 家公司，零值可用，可以并发使用。Desktop 和 sec mcp 各用一个。同一家公司同时只下载一次：问答同一轮同时
// 查财报和估值时，后来的调用等前一个下载完成。
// 返回的 *Facts 和它 Build 出的报表被所有调用方共用，只能读，不能修改。
type FactsCache struct {
	mu      sync.Mutex
	entries map[int]factsEntry
	loading map[int]*factsLoad
}

type factsEntry struct {
	at    time.Time
	facts *Facts
}

// factsLoad 是一家公司正在进行的下载，done 关闭后 facts、err 可读。
type factsLoad struct {
	done  chan struct{}
	facts *Facts
	err   error
	// cancelled 是下载它的调用方已经取消：等待的调用方自己还没取消时重新下载。
	cancelled bool
}

// errFactsInterrupted 是下载没有正常结束（panic）时等待的调用方得到的错误。
var errFactsInterrupted = errors.New("SEC XBRL 数据下载没有完成")

// Facts 先查缓存，没有或过期时用 client 下载并写入缓存；同一家公司正在下载时等它完成。下载失败不缓存。
func (c *FactsCache) Facts(ctx context.Context, client *Client, cik int) (*Facts, error) {
	for {
		c.mu.Lock()
		if entry, ok := c.entries[cik]; ok && time.Since(entry.at) < factsCacheTTL {
			c.mu.Unlock()
			return entry.facts, nil
		}
		load, waiting := c.loading[cik]
		if !waiting {
			if c.loading == nil {
				c.loading = map[int]*factsLoad{}
			}
			load = &factsLoad{done: make(chan struct{}), err: errFactsInterrupted}
			c.loading[cik] = load
		}
		c.mu.Unlock()
		if !waiting {
			return c.load(ctx, client, cik, load)
		}
		select {
		case <-load.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if !load.cancelled {
			return load.facts, load.err
		}
	}
}

// load 下载一家公司的数据，成功时写入缓存，最后唤醒等待的调用方。
func (c *FactsCache) load(ctx context.Context, client *Client, cik int, load *factsLoad) (*Facts, error) {
	defer func() {
		c.mu.Lock()
		delete(c.loading, cik)
		c.mu.Unlock()
		close(load.done)
	}()
	load.facts, load.err = client.CompanyFacts(ctx, cik)
	load.cancelled = load.err != nil && ctx.Err() != nil
	if load.err == nil {
		c.put(cik, load.facts, time.Now())
	}
	return load.facts, load.err
}

// Loader 返回用 client 下载、先查缓存的 XBRL 读取函数，供 valuation.AnalyzeUS、usstock.LoadDividends 等接受读取函数的调用方使用。
func (c *FactsCache) Loader(client *Client) func(context.Context, int) (*Facts, error) {
	return func(ctx context.Context, cik int) (*Facts, error) { return c.Facts(ctx, client, cik) }
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
