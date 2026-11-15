package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
)

// riskChangesTimeout 是对比风险因素的时限：要读申报列表和两年的 10-K 主文件，摩根大通这样的主文件有 12 MB。
const riskChangesTimeout = 2 * time.Minute

// riskChangesCache 缓存风险因素对比：申报不可变，对比是确定的，点了「风险因素变化」再点「解读」时不再读一遍
// 申报列表和两年的 10-K（摩根大通的主文件有 12 MB）。只在内存里，按申报编号，最多 riskCacheSize 份，满了丢掉最早的。
var riskChangesCache = struct {
	sync.Mutex
	entries map[string]*edgar.RiskChanges
	order   []string
}{entries: map[string]*edgar.RiskChanges{}}

const riskCacheSize = 16

// cachedRiskChanges 返回 raw 所在 10-K 的风险因素对比，先查缓存。失败的结果不缓存。
func cachedRiskChanges(ctx context.Context, client *edgar.Client, raw string) (*edgar.RiskChanges, error) {
	ref, err := edgar.ParseFilingURL(raw)
	if err != nil {
		return nil, err
	}
	riskChangesCache.Lock()
	changes, ok := riskChangesCache.entries[ref.Accession]
	riskChangesCache.Unlock()
	if ok {
		return changes, nil
	}
	if changes, err = client.RiskChanges(ctx, raw); err != nil {
		return nil, err
	}
	riskChangesCache.Lock()
	defer riskChangesCache.Unlock()
	if _, ok := riskChangesCache.entries[ref.Accession]; !ok {
		if len(riskChangesCache.order) == riskCacheSize {
			delete(riskChangesCache.entries, riskChangesCache.order[0])
			riskChangesCache.order = riskChangesCache.order[1:]
		}
		riskChangesCache.order = append(riskChangesCache.order, ref.Accession)
	}
	riskChangesCache.entries[ref.Accession] = changes
	return changes, nil
}

// edgarDownloadError 是从 SEC 下载失败时的提示：错误可能带网络细节和 SEC 联系邮箱，只给稳定的文字。
const edgarDownloadError = "从 SEC 读取申报失败，请稍后重试；如果一直失败，请在设置中检查 SEC 联系邮箱"

// handleFilingRisks 对比一份 10-K 和上一年 10-K 的风险因素（GET ?url=），不调用模型，和 sec filings --risks 一致。
func (s *desktopServer) handleFilingRisks(w http.ResponseWriter, r *http.Request) {
	link := r.URL.Query().Get("url")
	if _, err := edgar.ParseFilingURL(link); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), riskChangesTimeout)
	defer cancel()
	client, err := s.edgarClient(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "读取共享配置失败", "error", err)
		writeError(w, http.StatusInternalServerError, "读取配置失败，请稍后重试")
		return
	}
	changes, err := cachedRiskChanges(ctx, client, link)
	switch {
	case err == nil:
		writeJSON(w, changes)
	case r.Context().Err() != nil:
	case ctx.Err() != nil:
		writeError(w, http.StatusGatewayTimeout, "读取两年的 10-K 超时，请稍后重试")
	case errors.Is(err, edgar.ErrDownload):
		writeError(w, http.StatusBadGateway, edgarDownloadError)
	// 申报内容的问题（不是 10-K、识别不出小标题、没有上一年的 10-K），文字由 sec 生成。
	case errors.Is(err, edgar.ErrDocument):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		slog.ErrorContext(ctx, "对比风险因素失败", "url", link, "error", err)
		writeError(w, http.StatusBadGateway, edgarDownloadError)
	}
}

// handleAIFilingRisks 用模型解读风险因素的逐年变化，和 sec filings --risks --explain 一致。只接收 SEC 申报链接和临时模型配置。
func (s *desktopServer) handleAIFilingRisks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL     string `json:"url"`
		Profile string `json:"profile"`
	}
	if !s.readAIRequest(w, r, &in) {
		return
	}
	if _, err := edgar.ParseFilingURL(in.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client, model, cfg, ok := s.openAIModel(w, r, in.Profile)
	if !ok {
		return
	}
	cacheDir, err := llm.DefaultCacheDir("risks")
	if err != nil {
		slog.WarnContext(r.Context(), "无法创建风险因素解读缓存目录，本次不缓存", "error", err)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	sec := &edgar.Client{Email: cfg.SECEmail}
	explainer := llm.RiskExplainer{Client: client, Model: model, CacheDir: cacheDir, Read: func(ctx context.Context, raw string) (*edgar.RiskChanges, error) {
		return cachedRiskChanges(ctx, sec, raw)
	}}
	result, err := explainer.Explain(ctx, in.URL)
	if err != nil {
		writeSummaryError(w, r, ctx, err, "风险因素变化解读", func(err error) (int, string, bool) {
			switch {
			case errors.Is(err, edgar.ErrDownload):
				return http.StatusBadGateway, edgarDownloadError, true
			case errors.Is(err, edgar.ErrDocument):
				return http.StatusUnprocessableEntity, err.Error(), true
			case errors.Is(err, llm.ErrRiskExplain):
				return http.StatusBadGateway, err.Error(), true
			}
			return 0, "", false
		})
		return
	}
	writeJSON(w, result)
}
