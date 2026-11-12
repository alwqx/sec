//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/alwqx/sec/config"
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
	path, err := s.configPath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	cfg, err := config.LoadFile(r.Context(), path)
	switch {
	case errors.Is(err, config.ErrInvalidAI), errors.Is(err, config.ErrInvalidJSON):
		writeError(w, http.StatusConflict, "读取模型配置有问题，请先手动修正配置文件："+err.Error())
		return
	case errors.Is(err, config.ErrInvalid):
	case err != nil:
		writeError(w, http.StatusInternalServerError, "读取模型配置失败")
		return
	}
	secret, err := s.aiSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模型 Key 失败")
		return
	}
	client, model, err := llm.Open(&cfg.AI, in.Profile, secret, os.Getenv)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error()+"；请在设置页「AI 模型」中检查配置")
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
		if r.Context().Err() != nil {
			return
		}
		status, message := http.StatusBadGateway, "公告摘要失败，请重试或选择其他模型"
		var apiErr *llm.APIError
		switch {
		case errors.Is(err, context.DeadlineExceeded), ctx.Err() != nil:
			status, message = http.StatusGatewayTimeout, "公告摘要超时，请稍后重试或选择其他模型"
		case errors.As(err, &apiErr):
			message = fmt.Sprintf("模型接口返回 HTTP %d，请检查模型配置或稍后重试", apiErr.Status)
		case errors.Is(err, cninfo.ErrDocument):
			// 下载错误可能含远端响应，给前端稳定、可操作的错误。
			message = "公告 PDF 无法读取：可能下载失败、损坏、加密、为扫描件或超过限制（20 MB、200 页、40 万字）；请查看原文"
		case errors.Is(err, llm.ErrSummary):
			message = err.Error()
		}
		writeError(w, status, message)
		return
	}
	writeJSON(w, result)
}
