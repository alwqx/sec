package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/provider/sina"
)

// commodityCache 缓存各品种的全部日行情 10 分钟。WTI 从 1996 年起有上万条，切换品种和区间时不用重新下载。
var commodityCache = struct {
	sync.Mutex
	entries map[string]commodityEntry
}{entries: map[string]commodityEntry{}}

type commodityEntry struct {
	at   time.Time
	bars []sina.CommodityBar
}

const commodityCacheTTL = 10 * time.Minute

func commodityHistory(ctx context.Context, item sina.Commodity) ([]sina.CommodityBar, error) {
	commodityCache.Lock()
	entry, ok := commodityCache.entries[item.ID]
	commodityCache.Unlock()
	if ok && time.Since(entry.at) < commodityCacheTTL {
		return entry.bars, nil
	}
	bars, err := sina.QueryCommodityHistoryAll(ctx, item)
	if err != nil {
		return nil, err
	}
	commodityCache.Lock()
	commodityCache.entries[item.ID] = commodityEntry{time.Now(), bars}
	commodityCache.Unlock()
	return bars, nil
}

// handleCommodities 返回全部大宗商品的最新行情，一次请求新浪。个别品种失败时其余照常返回，failed 说明原因。
func (s *desktopServer) handleCommodities(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	quotes, failed, err := sina.QueryCommodities(ctx, sina.Commodities)
	if err == nil && len(quotes) == 0 {
		err = errors.New(strings.Join(failed, "；"))
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询大宗商品行情失败", "error", err)
		writeError(w, http.StatusBadGateway, "大宗商品行情获取失败，请稍后重试")
		return
	}
	writeJSON(w, map[string]any{"results": quotes, "failed": failed})
}

// handleCommodityHistory 返回一个品种的日行情，区间参数和贵金属相同。
func (s *desktopServer) handleCommodityHistory(w http.ResponseWriter, r *http.Request) {
	item, err := sina.FindCommodity(r.URL.Query().Get("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	begin, end, _, ok := dailyRange(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	all, err := commodityHistory(ctx, item)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询大宗商品历史行情失败", "id", item.ID, "error", err)
		writeError(w, http.StatusBadGateway, item.Name+"历史行情获取失败，请稍后重试")
		return
	}
	writeJSON(w, map[string]any{"commodity": item, "begin": begin, "end": end, "results": sina.FilterCommodityBars(all, begin, end)})
}
