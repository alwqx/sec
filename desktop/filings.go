//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/provider/edgar"
)

// configPath 返回共享配置文件；测试时放在工作台文件旁边，和共享自选一样。
func (s *desktopServer) configPath() (string, error) {
	if s.workspace.path != "" {
		return filepath.Join(filepath.Dir(s.workspace.path), "config.json"), nil
	}
	return config.Path()
}

// loadConfig 读取共享配置；文件内容无效时记录警告并用默认值，不影响工作台和查询。
func (s *desktopServer) loadConfig(ctx context.Context) (config.Config, error) {
	path, err := s.configPath()
	if err != nil {
		return config.Default(), err
	}
	cfg, err := config.LoadFile(ctx, path)
	if errors.Is(err, config.ErrInvalid) {
		slog.WarnContext(ctx, "共享配置无效，使用默认值", "error", err)
		return cfg, nil
	}
	return cfg, err
}

// updateConfig 在同一把锁里修改共享配置，不覆盖 CLI 同时做的修改。
func (s *desktopServer) updateConfig(ctx context.Context, change func(*config.Config) error) error {
	path, err := s.configPath()
	if err != nil {
		return err
	}
	return config.UpdateFile(ctx, path, change)
}

func (s *desktopServer) saveConfig(ctx context.Context, cfg config.Config) error {
	return s.updateConfig(ctx, func(c *config.Config) error { *c = cfg; return nil })
}

// filingGroups 是页面上的申报分类，和 sec filings --form 一致。
var filingGroups = map[string][]string{
	"":         edgar.DefaultForms,
	"periodic": {"10-K", "10-Q", "20-F", "40-F"},
	"current":  {"8-K", "6-K"},
	"insider":  {"3", "4", "5", "144"},
	"all":      nil,
}

var usTicker = regexp.MustCompile(`^\$[A-Za-z][A-Za-z0-9.-]{0,9}$`)

const filingsPageSize = 20

// filingsWant 是按类型筛选时希望凑够的条数（5 页），不够时再读 SEC 的历史分页。
const filingsWant = 5 * filingsPageSize

// filingsCache 按公司和类别缓存 5 分钟，翻页时不用重新下载几 MB 的申报列表。
var filingsCache = struct {
	sync.Mutex
	entries map[string]filingsEntry
}{entries: map[string]filingsEntry{}}

type filingsEntry struct {
	at      time.Time
	company edgar.Company
	result  *edgar.FilingsResult
}

const filingsCacheTTL = 5 * time.Minute

// handleFilings 返回美股公司的 SEC 申报，按申报日期从新到旧分页。
func (s *desktopServer) handleFilings(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if !usTicker.MatchString(code) {
		writeError(w, http.StatusBadRequest, "SEC 申报只支持美股代码，比如 $AAPL")
		return
	}
	forms, ok := filingGroups[r.URL.Query().Get("type")]
	if !ok {
		writeError(w, http.StatusBadRequest, "申报类别必须为 periodic、current、insider、all，留空查询主要申报")
		return
	}
	page := requestPage(w, r)
	if page == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "读取共享配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取 SEC 联系邮箱失败，请在设置中检查")
		return
	}
	key := strings.ToUpper(code) + "|" + r.URL.Query().Get("type")
	filingsCache.Lock()
	entry, cached := filingsCache.entries[key]
	filingsCache.Unlock()
	if !cached || time.Since(entry.at) > filingsCacheTTL {
		client := &edgar.Client{Email: cfg.SECEmail}
		entry = filingsEntry{at: time.Now()}
		entry.company, err = client.Lookup(ctx, code)
		if errors.Is(err, edgar.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SEC EDGAR 没有这家公司的申报，可能不向 SEC 申报")
			return
		}
		if err == nil {
			want := filingsWant
			if forms == nil {
				want = 0
			}
			entry.result, err = client.Filings(ctx, entry.company.CIK, edgar.FilingQuery{Forms: forms, Want: want})
		}
		if err == nil {
			filingsCache.Lock()
			filingsCache.entries[key] = entry
			filingsCache.Unlock()
		}
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询 SEC 申报失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "SEC 申报获取失败："+err.Error())
		return
	}
	filings := entry.result.Filings
	start := min((page-1)*filingsPageSize, len(filings))
	end := min(start+filingsPageSize, len(filings))
	writeJSON(w, map[string]any{"company": entry.result.Profile, "ticker": entry.company.Ticker, "total": len(filings), "has_more": end < len(filings), "truncated": entry.result.Truncated, "results": filings[start:end]})
}
