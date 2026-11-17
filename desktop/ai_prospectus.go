package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/cninfo"
)

var prospectusReadError = fmt.Sprintf("招股书 PDF 无法读取：可能下载失败、损坏、加密、为扫描件或超过限制（%d MB、%d 页、%d 万字）；请查看原文",
	cninfo.MaxProspectusBytes>>20, cninfo.MaxProspectusPages, cninfo.MaxProspectusRunes/10000)

// handleAIProspectus 回答关于一份招股书的问题，和 sec ipo ask 一样按问题挑页、共用缓存。只接收巨潮 PDF 链接、问题和临时模型配置，
// 不接受浏览器提交的招股书正文、页码或提示词。问题在读取模型配置和下载 PDF 之前校验。
func (s *desktopServer) handleAIProspectus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL      string `json:"url"`
		Question string `json:"question"`
		Profile  string `json:"profile"`
	}
	if !s.readAIRequest(w, r, &in) {
		return
	}
	doc, err := cninfo.DocumentURL(in.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	question, err := llm.NormalizeQuestion(in.Question)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	client, model, _, ok := s.openAIModel(w, r, in.Profile)
	if !ok {
		return
	}
	cacheDir, err := llm.DefaultCacheDir("prospectus")
	if err != nil {
		slog.WarnContext(r.Context(), "无法创建招股书问答缓存目录，本次不缓存", "error", err)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	answerer := llm.ProspectusAnswerer{Client: client, Model: model, CacheDir: cacheDir}
	result, err := answerer.Ask(ctx, doc.URL, question)
	if err != nil {
		writeSummaryError(w, r, ctx, err, "招股书问答", func(err error) (int, string, bool) {
			switch {
			case errors.Is(err, cninfo.ErrDocument):
				// 下载错误可能含远端响应，给前端稳定、可操作的错误。
				return http.StatusBadGateway, prospectusReadError, true
			// 问题的词在招股书里找不到，要换个问法，不是模型或网络的问题。
			case errors.Is(err, llm.ErrNoRelatedPages):
				return http.StatusUnprocessableEntity, err.Error(), true
			case errors.Is(err, llm.ErrProspectusAsk):
				return http.StatusBadGateway, err.Error(), true
			}
			return 0, "", false
		})
		return
	}
	writeJSON(w, result)
}
