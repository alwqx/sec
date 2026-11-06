//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/alwqx/sec/hkstock"
	"github.com/alwqx/sec/provider/sina"
)

// hkCode 是带 HK 前缀的 5 位港股代码。
var hkCode = regexp.MustCompile(`^HK\d{5}$`)

// handleHKCompany 返回港股的公司概况：东方财富的公司资料和估值，新浪的价格和市值。
// 东方财富一部分失败或没有数据时照常返回其他内容，原因写在 notes；代码不存在时 404，新浪行情失败时 502。
func (s *desktopServer) handleHKCompany(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	if !hkCode.MatchString(code) {
		writeError(w, http.StatusBadRequest, "只支持港股代码，比如 HK00700")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	res, err := hkstock.Load(ctx, code)
	if errors.Is(err, sina.ErrNotFound) {
		writeError(w, http.StatusNotFound, "没有找到港股 "+code)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "查询港股公司概况失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "港股行情获取失败，请稍后重试")
		return
	}
	p := res.Profile
	equity := hkstock.Equity(p)
	notes := []string{}
	if !equity {
		notes = append(notes, "交易所买卖基金、房地产投资信托、权证等不是公司，没有公司资料，只显示行情。")
	}
	notes = appendHKNote(ctx, notes, code, res.ProfileErr, "公司资料")
	notes = appendHKNote(ctx, notes, code, res.IssuePriceErr, "发行价")
	notes = appendHKNote(ctx, notes, code, res.ValuationErr, "市盈率和市净率")
	currency := p.Currency
	if currency == "" {
		currency = "HKD"
	}
	writeJSON(w, map[string]any{
		"ex_code": p.ExCode, "short_name": p.HistoryName, "name": p.Name, "industry": p.Category,
		"listing_date": p.ListingDate, "issue_price": validMetric(p.ListingPrice, false), "website": p.WebSite,
		"business_address": p.BusinessAddress, "register_address": p.RegisterAddress,
		"main_business": p.MainBusiness, "description": p.Description,
		"price": validMetric(p.Current, false), "price_is_yclose": p.PriceIsYClose,
		"pe_ttm": validMetric(p.PeTTM, false), "pb": validMetric(p.PB, true),
		"market_cap": validMetric(p.MarketCap, false), "float_market_cap": validMetric(p.TradedMarketCap, false),
		"currency": currency, "equity": equity, "notes": notes,
	})
}

// appendHKNote 把东方财富一部分的错误写成页面提示：没有数据时说明东方财富没有，请求失败时请用户稍后重试。
func appendHKNote(ctx context.Context, notes []string, code string, err error, what string) []string {
	switch {
	case err == nil:
		return notes
	case errors.Is(err, hkstock.ErrNoData):
		return append(notes, "东方财富没有这只证券的"+what+"。")
	default:
		slog.WarnContext(ctx, "港股"+what+"获取失败", "code", code, "error", err)
		return append(notes, what+"获取失败，请稍后重试。")
	}
}
