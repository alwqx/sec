//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/valuation"
)

func parseValuationParams(r *http.Request) (valuation.DCFParams, error) {
	params := valuation.DefaultDCFParams()
	// 增长率留空时自动取值，0 表示零增长。
	if text := strings.TrimSpace(r.URL.Query().Get("growth")); text != "" {
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return params, fmt.Errorf("增长率须为数字")
		}
		params.Growth = &value
	}
	for _, item := range []struct {
		name, label string
		target      *float64
	}{{"terminal", "永续增长率", &params.TerminalGrowth}, {"wacc", "折现率", &params.WACC}, {"margin", "安全边际", &params.MarginOfSafety}} {
		text := strings.TrimSpace(r.URL.Query().Get(item.name))
		if text == "" {
			continue
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return params, fmt.Errorf("%s须为数字", item.label)
		}
		*item.target = value
	}
	return params, params.Validate()
}

// handleValuation 返回完整估值；只有 DCF 参数变化时也重新查询上游，保证数据和日期一致。
func (s *desktopServer) handleValuation(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("code"))
	us := usTicker.MatchString(raw)
	if !us && companyCode(w, r) == "" {
		return
	}
	params, err := parseValuationParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 美股要下载 XBRL 和全部日线，比 A 股慢。
	timeout := 20 * time.Second
	if us {
		timeout = 40 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	code := strings.ToUpper(raw)
	var result *valuation.Result
	if us {
		var client *edgar.Client
		if client, err = s.edgarClient(ctx); err != nil {
			writeError(w, http.StatusInternalServerError, "读取 SEC 联系邮箱失败，请在设置中检查")
			return
		}
		result, err = valuation.AnalyzeUS(ctx, client, code, params, usFactsCache.Loader(client))
	} else {
		result, err = valuation.Analyze(ctx, code, params)
	}
	if errors.Is(err, valuation.ErrNoData) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, valuation.ErrForeign) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询估值失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "估值数据获取失败，请稍后重试")
		return
	}
	writeJSON(w, result)
}
