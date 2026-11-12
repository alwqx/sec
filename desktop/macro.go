package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/alwqx/sec/cmd/macro"
)

// handleMacroCatalog 返回宏观菜单：主题分类和各指标的日期格式、可选参数。
func (s *desktopServer) handleMacroCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"categories": macro.Categories(), "indicators": macro.Indicators()})
}

// handleMacroData 复用 CLI 的 --standard-json，所有指标共用同一份统一格式。
func (s *desktopServer) handleMacroData(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := macro.ExportQuery{Region: q.Get("region"), Command: q.Get("command"), Begin: q.Get("begin"), End: q.Get("end"), Refresh: q.Get("refresh") == "true", Options: map[string]string{}}
	// Export 只使用清单内的选项，其余参数不会传给命令。
	for key := range q {
		query.Options[key] = q.Get(key)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	raw, err := macro.Export(ctx, query)
	if err != nil {
		if errors.Is(err, macro.ErrInvalidQuery) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// 切换指标会取消上一个请求，这不是错误。
		if r.Context().Err() != nil {
			return
		}
		// 命令的错误信息已说明日期范围、上游失败等原因，直接展示给用户。
		slog.ErrorContext(ctx, "查询宏观数据失败", "region", query.Region, "command", query.Command, "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var dataset map[string]json.RawMessage
	if err := json.Unmarshal(raw, &dataset); err != nil {
		slog.ErrorContext(ctx, "解析宏观数据失败", "region", query.Region, "command", query.Command, "error", err)
		writeError(w, http.StatusInternalServerError, "宏观数据格式无效")
		return
	}
	// original 是 provider 原始结果，页面不展示，长区间下体积很大。
	delete(dataset, "original")
	writeJSON(w, dataset)
}
