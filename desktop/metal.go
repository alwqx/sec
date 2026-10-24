//go:build windows || darwin

package main

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/metal"
	"github.com/alwqx/sec/utils"
)

// metalQuote 将未知昨收、涨跌额与涨跌幅表示为 null，避免误报为零或输出非有限数值。
type metalQuote struct {
	Date       string   `json:"date"`
	Open       float64  `json:"open"`
	Close      float64  `json:"close"`
	High       float64  `json:"high"`
	Low        float64  `json:"low"`
	YClose     *float64 `json:"yclose"`
	Change     *float64 `json:"change"`
	ChangeRate *float64 `json:"change_rate"` // 百分比，例如 1.5 表示 1.5%
}

type metalResponse struct {
	Name    string       `json:"name"`
	Unit    string       `json:"unit"`
	Begin   string       `json:"begin"`
	End     string       `json:"end"`
	Latest  *metalQuote  `json:"latest"`
	Results []metalQuote `json:"results"`
}

func mapMetalQuote(item *metal.DailyHQItem) metalQuote {
	quote := metalQuote{Date: item.Date, Open: item.Open, Close: item.Close, High: item.High, Low: item.Low}
	if item.YClose > 0 && !math.IsNaN(item.YClose) && !math.IsInf(item.YClose, 0) {
		change := item.Close - item.YClose
		rate := change / item.YClose * 100
		quote.YClose, quote.Change, quote.ChangeRate = &item.YClose, &change, &rate
	}
	return quote
}

// handleMetal 一次获取数据，同时返回最新日行情与指定区间历史，避免重复抓取同一上游全量接口。
func (s *desktopServer) handleMetal(w http.ResponseWriter, r *http.Request) {
	const layout = utils.LayoutYYMMDD
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format(layout)
	end := strings.TrimSpace(r.URL.Query().Get("end"))
	if end == "" {
		end = today
	}
	endTime, err := time.Parse(layout, end)
	if err != nil {
		writeError(w, http.StatusBadRequest, "结束日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	begin := strings.TrimSpace(r.URL.Query().Get("begin"))
	if begin == "" {
		begin = endTime.AddDate(0, 0, -30).Format(layout)
	}
	beginTime, err := time.Parse(layout, begin)
	if err != nil {
		writeError(w, http.StatusBadRequest, "开始日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	if beginTime.After(endTime) {
		writeError(w, http.StatusBadRequest, "开始日期不能晚于结束日期")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// 查询截止今日的全部日行情，最新记录独立于用户选择的历史区间。
	// provider 在区间筛选前计算昨收，确保首个历史交易日仍有正确的涨跌基准。
	response, err := metal.QueryAu999(ctx, &metal.QueryAu999Req{Start: "1900-01-01", End: today})
	if err != nil || response == nil {
		slog.ErrorContext(ctx, "查询黄金日行情失败", "error", err)
		writeError(w, http.StatusBadGateway, "黄金行情获取失败，请稍后重试")
		return
	}
	result := metalResponse{Name: "Au99.99", Unit: "元/克", Begin: begin, End: end, Results: []metalQuote{}}
	for _, item := range response.Data {
		if item == nil {
			continue
		}
		quote := mapMetalQuote(item)
		result.Latest = &quote
		if item.Date >= begin && item.Date <= end {
			result.Results = append(result.Results, quote)
		}
	}
	writeJSON(w, result)
}
