//go:build windows || darwin

package main

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/sina"
)

func validMetric(value float64, allowNegative bool) *float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) || value == 0 || (!allowNegative && value < 0) {
		return nil
	}
	return &value
}

func (s *desktopServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if companyCode(w, r) == "" {
		return
	}
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	metrics, err := sina.QueryCoreMetrics(ctx, code)
	if err != nil || metrics == nil {
		slog.ErrorContext(ctx, "查询公司核心指标失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "公司核心指标获取失败，请稍后重试")
		return
	}
	writeJSON(w, map[string]any{
		"code": code, "industry": strings.TrimSpace(metrics.Category), "pe_ttm": validMetric(metrics.PeTTM, false),
		"pb": validMetric(metrics.PB, true), "market_cap": validMetric(metrics.MarketCap, false),
		"float_market_cap": validMetric(metrics.TradedMarketCap, false), "currency": "CNY",
		"trade_date": metrics.TradeDate, "time": metrics.Time,
	})
}
