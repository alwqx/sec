//go:build windows || darwin

package main

import (
	"context"
	"errors"
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
	Inst        string             `json:"inst"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Unit        string             `json:"unit"`
	Instruments []metal.Instrument `json:"instruments"`
	Begin       string             `json:"begin"`
	End         string             `json:"end"`
	Latest      *metalQuote        `json:"latest"`
	Results     []metalQuote       `json:"results"`
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

// dailyRange 读取 begin、end（YYYY-MM-DD），默认结束于今天（上海时区）、往前 30 天。
func dailyRange(w http.ResponseWriter, r *http.Request) (begin, end, today string, ok bool) {
	const layout = utils.LayoutYYMMDD
	today = time.Now().In(time.FixedZone("CST", 8*3600)).Format(layout)
	end = strings.TrimSpace(r.URL.Query().Get("end"))
	if end == "" {
		end = today
	}
	endTime, err := time.Parse(layout, end)
	if err != nil {
		writeError(w, http.StatusBadRequest, "结束日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	begin = strings.TrimSpace(r.URL.Query().Get("begin"))
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
	return begin, end, today, true
}

// handleMetal 一次获取数据，同时返回最新日行情与指定区间历史，避免重复抓取同一上游全量接口。inst 为空时查询 Au99.99。
func (s *desktopServer) handleMetal(w http.ResponseWriter, r *http.Request) {
	inst, err := metal.FindInstrument(r.URL.Query().Get("inst"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	begin, end, today, ok := dailyRange(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// 查询截止今日的全部日行情，最新记录独立于用户选择的历史区间。
	// provider 在区间筛选前计算昨收，确保首个历史交易日仍有正确的涨跌基准。
	response, err := metal.Query(ctx, &metal.QueryReq{Inst: inst.ID, Start: "1900-01-01", End: today, Fresh: r.URL.Query().Get("refresh") == "1"})
	if err != nil || response == nil {
		slog.ErrorContext(ctx, "查询贵金属日行情失败", "inst", inst.ID, "error", err)
		writeError(w, http.StatusBadGateway, metalErrorMessage(err, inst.ID+" "+inst.Name+"行情获取失败，请稍后重试"))
		return
	}
	result := metalResponse{Inst: inst.ID, Name: inst.ID, Description: inst.Name, Unit: inst.Unit, Instruments: metal.Instruments, Begin: begin, End: end, Results: []metalQuote{}}
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

// metalErrorMessage 被上海黄金交易所拦截时说明原因，其他错误用 fallback。
func metalErrorMessage(err error, fallback string) string {
	if errors.Is(err, metal.ErrBlocked) {
		return err.Error()
	}
	return fallback
}
