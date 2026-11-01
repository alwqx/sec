//go:build windows || darwin

package main

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/provider/sina"
)

const (
	// 自选上限是 500，旧列表可能超过上限，这里留出余量。
	maxQuoteCodes  = 1000
	quoteBatchSize = 100
)

// 与自选列表的代码规则一致：美股是 $AMD，基金等也可以保存。
var quoteCodePattern = regexp.MustCompile(`^[A-Z0-9$._-]{1,40}$`)

// handleQuotes 批量查询行情。不可用的证券列入 missing，不影响其他证券。
func (s *desktopServer) handleQuotes(w http.ResponseWriter, r *http.Request) {
	codes := []string{}
	for _, code := range strings.Split(r.URL.Query().Get("codes"), ",") {
		code = strings.ToUpper(strings.TrimSpace(code))
		if code == "" {
			continue
		}
		if !quoteCodePattern.MatchString(code) {
			writeError(w, http.StatusBadRequest, "证券代码格式无效")
			return
		}
		codes = append(codes, code)
	}
	if len(codes) == 0 || len(codes) > maxQuoteCodes {
		writeError(w, http.StatusBadRequest, "codes 须包含 1 至 1000 个证券代码")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	found, failed := queryQuoteBatches(ctx, codes)
	if r.Context().Err() != nil {
		return
	}
	results := make([]*sina.SecurityQuote, 0, len(codes))
	missing := []string{}
	for _, code := range codes {
		if item := found[code]; item != nil {
			results = append(results, item)
		} else {
			missing = append(missing, code)
		}
	}
	if len(results) == 0 && failed {
		writeError(w, http.StatusBadGateway, "行情获取失败，请稍后重试")
		return
	}
	writeJSON(w, map[string]any{"results": results, "missing": missing})
}

// queryQuoteBatches 分批查询，failed 表示有批次请求失败。
func queryQuoteBatches(ctx context.Context, codes []string) (map[string]*sina.SecurityQuote, bool) {
	found := map[string]*sina.SecurityQuote{}
	failed := false
	var (
		mu    sync.Mutex
		group sync.WaitGroup
	)
	limit := make(chan struct{}, 4)
	for begin := 0; begin < len(codes); begin += quoteBatchSize {
		batch := codes[begin:min(begin+quoteBatchSize, len(codes))]
		group.Add(1)
		go func() {
			defer group.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			items, err := sina.QueryQuoteList(ctx, batch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				slog.WarnContext(ctx, "批量查询行情失败", "error", err)
				failed = true
				return
			}
			for _, item := range items {
				found[strings.ToUpper(item.ExCode)] = item
			}
		}()
	}
	group.Wait()
	return found, failed
}
