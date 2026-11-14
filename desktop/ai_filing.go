package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
)

// handleAIFiling 摘要一份 8-K。只接收 SEC 申报链接和临时模型配置，不接受浏览器提交的申报正文、日期或提示词。
func (s *desktopServer) handleAIFiling(w http.ResponseWriter, r *http.Request) {
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
	cacheDir, err := llm.DefaultCacheDir("filings")
	if err != nil {
		slog.WarnContext(r.Context(), "无法创建申报摘要缓存目录，本次不缓存", "error", err)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	sec := &edgar.Client{Email: cfg.SECEmail}
	summarizer := llm.FilingSummarizer{Client: client, Model: model, CacheDir: cacheDir, Read: sec.Read8K}
	result, err := summarizer.Summarize(ctx, in.URL)
	if err != nil {
		writeSummaryError(w, r, ctx, err, "申报摘要", func(err error) (int, string, bool) {
			switch {
			// 下载错误可能带网络细节和 SEC 联系邮箱，只给稳定的提示。
			case errors.Is(err, edgar.ErrDownload):
				return http.StatusBadGateway, "从 SEC 读取申报失败，请稍后重试；如果一直失败，请在设置中检查 SEC 联系邮箱", true
			// 其他原文错误是申报内容的问题（不是 8-K、没有 Item、超过大小），重试也不会成功；文字由 sec 生成。
			case errors.Is(err, edgar.ErrDocument):
				return http.StatusUnprocessableEntity, err.Error(), true
			case errors.Is(err, llm.ErrFilingSummary):
				return http.StatusBadGateway, err.Error(), true
			}
			return 0, "", false
		})
		return
	}
	writeJSON(w, result)
}
