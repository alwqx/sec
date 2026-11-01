//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/alwqx/sec/cmd/fx"
	"github.com/alwqx/sec/provider/forex"
)

// handleFXOptions 返回交叉汇率各来源支持的货币对。
func (s *desktopServer) handleFXOptions(w http.ResponseWriter, r *http.Request) {
	pairs := map[string][]string{}
	for _, source := range []string{"fred", "cfets", "sina"} {
		pairs[source] = forex.CrossPairs(source)
	}
	writeJSON(w, map[string]any{"cross_pairs": pairs})
}

// handleFXData 复用 CLI 的 --json，保留来源、价格类型和时间口径。
func (s *desktopServer) handleFXData(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := fx.ExportQuery{Command: q.Get("command"), Source: q.Get("source"), Begin: q.Get("begin"), End: q.Get("end"), Pair: q.Get("pair"), At: q.Get("at"), MaxSkew: q.Get("max_skew"), MaxAge: q.Get("max_age"), Refresh: q.Get("refresh") == "true"}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	raw, err := fx.Export(ctx, query)
	if err != nil {
		if errors.Is(err, fx.ErrInvalidQuery) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// 切换栏目会取消上一个请求，这不是错误。
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询汇率失败", "command", query.Command, "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(raw); err != nil {
		slog.ErrorContext(ctx, "写入汇率响应失败", "error", err)
	}
}
