package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/cninfo"
)

// 只接收 CDN 链接和临时模型配置，不接受浏览器提交的公告正文、日期或提示词。
func (s *desktopServer) handleAIAnnouncement(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL     string `json:"url"`
		Profile string `json:"profile"`
	}
	if !s.readAIRequest(w, r, &in) {
		return
	}
	doc, err := cninfo.DocumentURL(in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client, model, _, ok := s.openAIModel(w, r, in.Profile)
	if !ok {
		return
	}
	cacheDir, err := llm.DefaultCacheDir("announcements")
	if err != nil {
		slog.WarnContext(r.Context(), "无法创建公告摘要缓存目录，本次不缓存", "error", err)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	summarizer := llm.AnnouncementSummarizer{Client: client, Model: model, CacheDir: cacheDir}
	result, err := summarizer.Summarize(ctx, doc.URL)
	if err != nil {
		writeSummaryError(w, r, ctx, err, "公告摘要", func(err error) (int, string, bool) {
			switch {
			case errors.Is(err, cninfo.ErrDocument):
				// 下载错误可能含远端响应，给前端稳定、可操作的错误。
				return http.StatusBadGateway, "公告 PDF 无法读取：可能下载失败、损坏、加密、为扫描件或超过限制（20 MB、200 页、40 万字）；请查看原文", true
			case errors.Is(err, llm.ErrSummary):
				return http.StatusBadGateway, err.Error(), true
			}
			return 0, "", false
		})
		return
	}
	writeJSON(w, result)
}
