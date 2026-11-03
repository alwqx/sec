package metal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/alwqx/sec/utils"
)

// ErrBlocked 表示上海黄金交易所的防火墙拦截了请求。它的频率限制很低，短时间内请求十几次就会被拦截一段时间。
var ErrBlocked = errors.New("上海黄金交易所拒绝了请求，可能是访问过于频繁，请稍后再试")

// CacheTTL 是全量数据的缓存时间，0 表示不缓存。命令行每次运行只请求一两次，不需要缓存；
// Desktop 切换品种和区间都会重新查询，启动时设置缓存，减少请求次数。
var CacheTTL time.Duration

var cache = struct {
	sync.Mutex
	entries map[string]cacheEntry
}{entries: map[string]cacheEntry{}}

type cacheEntry struct {
	at   time.Time
	body []byte
}

// blockedMark 是防火墙拦截页的标题，拦截页的状态码不一定是 403。
const blockedMark = "请求已被阻断"

// resetCache 清空缓存，供测试使用。
func resetCache() {
	cache.Lock()
	cache.entries = map[string]cacheEntry{}
	cache.Unlock()
}

// fetch 请求上海黄金交易所的接口，返回响应正文。fresh 为 true 时不用缓存。
// valid 解析正文，命中缓存时也调用；解析通过后才写入缓存，避免把拦截页或截断的数据缓存下来。
func fetch(ctx context.Context, link string, fresh bool, valid func([]byte) error) ([]byte, error) {
	if CacheTTL > 0 && !fresh {
		cache.Lock()
		entry, ok := cache.entries[link]
		cache.Unlock()
		if ok && time.Since(entry.at) < CacheTTL {
			return entry.body, valid(entry.body)
		}
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, link, nil, nil, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusForbidden || bytes.Contains(body, []byte(blockedMark)) {
		return nil, ErrBlocked
	}
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if err := valid(body); err != nil {
		return nil, err
	}
	if CacheTTL > 0 {
		cache.Lock()
		cache.entries[link] = cacheEntry{time.Now(), body}
		cache.Unlock()
	}
	return body, nil
}
