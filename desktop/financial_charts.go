package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/alwqx/sec/financials"
)

// 报表一季度才更新一次，上游又限流，短时间内切换年度和单季不必重新请求。
const financialChartsTTL = 10 * time.Minute

type cachedCharts struct {
	result *financials.Analysis
	at     time.Time
}

var (
	financialChartsMu    sync.Mutex
	financialChartsCache = map[string]cachedCharts{}
)

// handleFinancialCharts 返回多期报表整理出的图表数据。freq 为 quarter 时是最近 12 个单季，否则是最近 10 个年度。
func (s *desktopServer) handleFinancialCharts(w http.ResponseWriter, r *http.Request) {
	code := companyExCode(w, r)
	if code == "" {
		return
	}
	freq := r.URL.Query().Get("freq")
	if freq != "" && freq != "annual" && freq != "quarter" {
		writeError(w, http.StatusBadRequest, "freq 只能是 annual 或 quarter")
		return
	}
	quarterly := freq == "quarter"
	key := code + "|" + freq
	financialChartsMu.Lock()
	cached, ok := financialChartsCache[key]
	financialChartsMu.Unlock()
	if ok && time.Since(cached.at) < financialChartsTTL {
		writeJSON(w, cached.result)
		return
	}
	// 一次要请求十几次上游，接口限流时还会重试。
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	result, err := financials.Analyze(ctx, code, quarterly)
	if errors.Is(err, financials.ErrNoData) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询财报图表数据失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "财报数据获取失败，请稍后重试")
		return
	}
	financialChartsMu.Lock()
	financialChartsCache[key] = cachedCharts{result, time.Now()}
	financialChartsMu.Unlock()
	writeJSON(w, result)
}
