package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/alwqx/sec/fund"
	"github.com/alwqx/sec/provider/sina"
)

// handleFundOverview 返回基金概况（fund.Info）：天天基金 App 接口的净值和收益率，网页档案的全称、托管人和业绩比较基准等。
// 一个数据源失败时照常返回另一个，原因在 notes；没有这只基金时 404，都失败时 502。
func (s *desktopServer) handleFundOverview(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	if !sina.IsFundCode(code) {
		writeError(w, http.StatusBadRequest, "只支持基金代码，比如 OF005827")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	info, err := fund.Load(ctx, code)
	if errors.Is(err, fund.ErrNotFound) {
		writeError(w, http.StatusNotFound, "没有找到基金 "+code)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "查询基金概况失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "基金信息获取失败，请稍后重试")
		return
	}
	writeJSON(w, info)
}
