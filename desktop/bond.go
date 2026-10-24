//go:build windows || darwin

package main

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/bond"
)

type bondDay struct {
	Date        string              `json:"date"`
	Yields      map[string]*float64 `json:"yields"` // 百分比，例如 4.25 表示 4.25%
	Previous10Y *float64            `json:"previous_10y"`
	ChangeBP    *float64            `json:"change_bp"`
}

type bondTenor struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Months int    `json:"months"`
}

var bondTenors = []bondTenor{
	{"1m", "1个月", 1}, {"3m", "3个月", 3}, {"6m", "6个月", 6}, {"1y", "1年", 12},
	{"2y", "2年", 24}, {"3y", "3年", 36}, {"5y", "5年", 60}, {"7y", "7年", 84},
	{"10y", "10年", 120}, {"20y", "20年", 240}, {"30y", "30年", 360},
}

// handleBond 按所需年份的并集抓取数据，最新行情与历史范围独立；每个年份只请求一次。
func (s *desktopServer) handleBond(w http.ResponseWriter, r *http.Request) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		writeError(w, 500, "无法读取美东时区")
		return
	}
	today, _ := time.Parse("2006-01-02", time.Now().In(location).Format("2006-01-02"))
	endText := strings.TrimSpace(r.URL.Query().Get("end"))
	if endText == "" {
		endText = today.Format("2006-01-02")
	}
	end, err := time.Parse("2006-01-02", endText)
	if err != nil {
		writeError(w, 400, "结束日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	beginText := strings.TrimSpace(r.URL.Query().Get("begin"))
	if beginText == "" {
		beginText = end.AddDate(0, 0, -30).Format("2006-01-02")
	}
	begin, err := time.Parse("2006-01-02", beginText)
	if err != nil {
		writeError(w, 400, "开始日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	if begin.After(end) {
		writeError(w, 400, "开始日期不能晚于结束日期")
		return
	}
	if begin.Year() < 1990 || end.After(today) {
		writeError(w, 400, "请选择 1990 年至美东今日之间的日期")
		return
	}
	if end.After(begin.AddDate(5, 0, 0)) {
		writeError(w, 400, "单次历史查询范围不能超过 5 年")
		return
	}
	years := map[int]bool{}
	addYears := func(start, stop time.Time) {
		for year := start.Year(); year <= stop.Year(); year++ {
			if year >= 1990 {
				years[year] = true
			}
		}
	}
	// 额外读取区间前 35 天所在年份，确保跨年首日的日变动仍有前一交易日基准。
	addYears(begin.AddDate(0, 0, -35), end)
	addYears(today.AddDate(0, 0, -35), today)
	ordered := make([]int, 0, len(years))
	for year := range years {
		ordered = append(ordered, year)
	}
	sort.Ints(ordered)
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	items := []*bond.BondYieldItem{}
	for _, year := range ordered {
		// 每次只查询一个年份，使 QueryBond 的任何抓取失败都能返回错误，避免悄悄展示跨年部分数据。
		response, err := bond.QueryBond(ctx, &bond.QueryBondReq{Start: time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), End: time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02")})
		if err != nil || response == nil {
			slog.ErrorContext(ctx, "查询美债收益率失败", "year", year, "error", err)
			writeError(w, http.StatusBadGateway, "美债收益率获取失败，请稍后重试")
			return
		}
		items = append(items, response.Data...)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	results := []bondDay{}
	var latest *bondDay
	var previous *bondDay
	for _, item := range items {
		if item == nil || item.Date > today.Format("2006-01-02") {
			continue
		}
		values := []float64{item.BC1Month, item.BC3Month, item.BC6Month, item.BC1Year, item.BC2Year, item.BC3Year, item.BC5Year, item.BC7Year, item.BC10Year, item.BC20Year, item.BC30Year}
		day := bondDay{Date: item.Date, Yields: map[string]*float64{}}
		for index, tenor := range bondTenors {
			value := values[index]
			if !item.MissingTenors[tenor.Key] && !math.IsNaN(value) && !math.IsInf(value, 0) {
				day.Yields[tenor.Key] = &value
			} else {
				day.Yields[tenor.Key] = nil
			}
		}
		// 年份不连续时不能将多年前的值当作前一交易日收益率。
		if previous != nil && previous.Yields["10y"] != nil && day.Yields["10y"] != nil {
			previousDate, _ := time.Parse("2006-01-02", previous.Date)
			if item.DateTime.Sub(previousDate) <= 35*24*time.Hour {
				day.Previous10Y = previous.Yields["10y"]
				change := (*day.Yields["10y"] - *day.Previous10Y) * 100
				day.ChangeBP = &change
			}
		}
		previous = &day
		if item.DateTime.Year() == today.Year() || item.Date >= today.AddDate(0, 0, -35).Format("2006-01-02") {
			latest = &day
		}
		if item.Date >= beginText && item.Date <= endText {
			results = append(results, day)
		}
	}
	writeJSON(w, map[string]any{"begin": beginText, "end": endText, "latest": latest, "results": results, "tenors": bondTenors})
}
