//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/usstock"
)

// edgarClient 用共享配置里的联系邮箱访问 SEC。
func (s *desktopServer) edgarClient(ctx context.Context) (*edgar.Client, error) {
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &edgar.Client{Email: cfg.SECEmail}, nil
}

// handleUSCompany 返回美股公司资料：SEC EDGAR 公司信息和新浪行情，任一失败时照常返回另一部分并说明原因。
func (s *desktopServer) handleUSCompany(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if !usTicker.MatchString(code) {
		writeError(w, http.StatusBadRequest, "只支持美股代码，比如 $AAPL")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client, err := s.edgarClient(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 SEC 联系邮箱失败，请在设置中检查")
		return
	}
	info, profileErr, quoteErr := usstock.Load(ctx, client, code)
	if profileErr != nil && quoteErr != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询美股公司资料失败", "code", code, "profile", profileErr, "quote", quoteErr)
		writeError(w, http.StatusBadGateway, "美股公司资料和行情都获取失败，请稍后重试")
		return
	}
	notes := []string{}
	switch {
	case errors.Is(profileErr, edgar.ErrNotFound):
		notes = append(notes, "SEC EDGAR 没有这个代码，基金、场外交易股票等可能不向 SEC 申报公司资料。")
	case profileErr != nil:
		notes = append(notes, "公司资料获取失败："+profileErr.Error())
	}
	if quoteErr != nil {
		notes = append(notes, "行情获取失败："+quoteErr.Error())
	}
	writeJSON(w, map[string]any{"info": info, "notes": notes})
}

// usFactsCache 按 CIK 缓存 XBRL 数据 10 分钟，切换报表和年度、季度时不用重新下载（大公司有几 MB）。
var usFactsCache = struct {
	sync.Mutex
	entries map[int]usFactsEntry
}{entries: map[int]usFactsEntry{}}

type usFactsEntry struct {
	at    time.Time
	facts *edgar.Facts
}

const (
	usFactsTTL        = 10 * time.Minute
	usFactsMaxEntries = 20
)

// cacheUSFacts 写入时清理过期条目，达到上限时移除最早写入的公司。调用方不持有锁。
func cacheUSFacts(cik int, facts *edgar.Facts, now time.Time) {
	usFactsCache.Lock()
	defer usFactsCache.Unlock()
	for key, entry := range usFactsCache.entries {
		if now.Sub(entry.at) >= usFactsTTL {
			delete(usFactsCache.entries, key)
		}
	}
	usFactsCache.entries[cik] = usFactsEntry{now, facts}
	for len(usFactsCache.entries) > usFactsMaxEntries {
		oldest := cik
		for key, entry := range usFactsCache.entries {
			if entry.at.Before(usFactsCache.entries[oldest].at) {
				oldest = key
			}
		}
		delete(usFactsCache.entries, oldest)
	}
}

// handleUSFinancials 返回美股财报的一张报表：年度最多 10 期，季度最多 12 期，从新到旧。
func (s *desktopServer) handleUSFinancials(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	code := strings.TrimSpace(query.Get("code"))
	if !usTicker.MatchString(code) {
		writeError(w, http.StatusBadRequest, "只支持美股代码，比如 $AAPL")
		return
	}
	statement := edgar.Statement(query.Get("statement"))
	if _, ok := edgar.Lines[statement]; !ok {
		writeError(w, http.StatusBadRequest, "报表须为 income、balance 或 cashflow")
		return
	}
	quarterly := false
	switch query.Get("period") {
	case "", "annual":
	case "quarter":
		quarterly = true
	default:
		writeError(w, http.StatusBadRequest, "period 须为 annual 或 quarter")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	client, err := s.edgarClient(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 SEC 联系邮箱失败，请在设置中检查")
		return
	}
	company, err := client.Lookup(ctx, code)
	if errors.Is(err, edgar.ErrNotFound) {
		writeError(w, http.StatusNotFound, "SEC EDGAR 没有这家公司的财报，可能不向 SEC 申报")
		return
	}
	var facts *edgar.Facts
	if err == nil {
		usFactsCache.Lock()
		entry, ok := usFactsCache.entries[company.CIK]
		usFactsCache.Unlock()
		if ok && time.Since(entry.at) < usFactsTTL {
			facts = entry.facts
		} else if facts, err = client.CompanyFacts(ctx, company.CIK); err == nil {
			cacheUSFacts(company.CIK, facts, time.Now())
		}
	}
	if errors.Is(err, edgar.ErrIFRS) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询美股财报失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "美股财报获取失败："+err.Error())
		return
	}
	limit := 10
	if quarterly {
		limit = 12
	}
	writeJSON(w, map[string]any{"ticker": company.Ticker, "name": facts.Name, "cik": company.CIK, "report": facts.Build(statement, quarterly, limit)})
}
