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

	"github.com/alwqx/sec/financials"
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
// cachedUSFacts 返回先查缓存、过期或没有时再下载的 XBRL 读取函数，财报和估值栏目共用。
func cachedUSFacts(client *edgar.Client) func(context.Context, int) (*edgar.Facts, error) {
	return func(ctx context.Context, cik int) (*edgar.Facts, error) {
		usFactsCache.Lock()
		entry, ok := usFactsCache.entries[cik]
		usFactsCache.Unlock()
		if ok && time.Since(entry.at) < usFactsTTL {
			return entry.facts, nil
		}
		facts, err := client.CompanyFacts(ctx, cik)
		if err == nil {
			cacheUSFacts(cik, facts, time.Now())
		}
		return facts, err
	}
}

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
		facts, err = cachedUSFacts(client)(ctx, company.CIK)
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

// handleUSDividends 返回美股按财年的分红表（usstock.Dividends），和 CLI 的 sec info -d 相同；XBRL 数据和财报、估值栏目共用缓存。
func (s *desktopServer) handleUSDividends(w http.ResponseWriter, r *http.Request) {
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
	dividends, err := usstock.LoadDividends(ctx, client, code, nil, cachedUSFacts(client))
	if errors.Is(err, edgar.ErrNotFound) || errors.Is(err, edgar.ErrNoXBRL) {
		writeError(w, http.StatusNotFound, "SEC EDGAR 没有这家公司的分红数据，可能不向 SEC 申报")
		return
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询美股分红失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "美股分红获取失败："+err.Error())
		return
	}
	writeJSON(w, dividends)
}

// handleUSFinancialCharts 返回美股财报的图表数据（financials.FromSEC），格式和 A 股的 /api/v1/financials/charts 相同；
// XBRL 数据和财报、估值、分红栏目共用缓存。
func (s *desktopServer) handleUSFinancialCharts(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	code := strings.TrimSpace(query.Get("code"))
	if !usTicker.MatchString(code) {
		writeError(w, http.StatusBadRequest, "只支持美股代码，比如 $AAPL")
		return
	}
	freq := query.Get("freq")
	if freq != "" && freq != "annual" && freq != "quarter" {
		writeError(w, http.StatusBadRequest, "freq 只能是 annual 或 quarter")
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
	var facts *edgar.Facts
	if err == nil {
		facts, err = cachedUSFacts(client)(ctx, company.CIK)
	}
	var result *financials.Analysis
	if err == nil {
		result, err = financials.FromSEC(company.Ticker, facts, freq == "quarter")
	}
	switch {
	case errors.Is(err, edgar.ErrNotFound) || errors.Is(err, edgar.ErrNoXBRL):
		writeError(w, http.StatusNotFound, "SEC EDGAR 没有这家公司的财报数据，可能不向 SEC 申报")
		return
	case errors.Is(err, financials.ErrNoSECData) || errors.Is(err, financials.ErrIFRSQuarterly):
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询美股财报图表失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "美股财报获取失败："+err.Error())
		return
	}
	writeJSON(w, result)
}
