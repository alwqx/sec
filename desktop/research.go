//go:build windows || darwin

package main

import (
	"context"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/eastmoney"
)

func requestPage(w http.ResponseWriter, r *http.Request) int {
	value := r.URL.Query().Get("page")
	if value == "" {
		return 1
	}
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 || page > 10000 {
		writeError(w, http.StatusBadRequest, "页码必须在 1 到 10000 之间")
		return 0
	}
	return page
}

func (s *desktopServer) handleIPO(w http.ResponseWriter, r *http.Request) {
	page := requestPage(w, r)
	if page == 0 {
		return
	}
	since, until := r.URL.Query().Get("since"), r.URL.Query().Get("until")
	if err := eastmoney.ValidateIPODates(since, until); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var items []*eastmoney.IListing
	var total int
	var err error
	if since == "" && until == "" {
		items, total, err = eastmoney.ListIPO(ctx, &eastmoney.IPOListReq{PageNum: page, PageSize: 30})
	} else {
		items, total, err = eastmoney.ListIPOFiltered(ctx, since, until, page, 30)
	}
	if err != nil {
		slog.ErrorContext(ctx, "查询 IPO 失败", "error", err)
		writeError(w, http.StatusBadGateway, "新股列表获取失败，请稍后重试")
		return
	}
	if items == nil {
		items = []*eastmoney.IListing{}
	}
	writeJSON(w, map[string]any{"results": items, "total": total, "has_more": page*30 < total})
}

var titleTags = regexp.MustCompile(`<[^>]*>`)

func announcementURL(a *cninfo.Announcement) string {
	if a.AdjunctURL != "" {
		u, err := url.Parse(a.AdjunctURL)
		if err == nil && u.Host == "" && !strings.HasPrefix(u.Path, "//") && strings.HasPrefix(u.Path, "finalpage/") {
			return "https://static.cninfo.com.cn/" + u.String()
		}
		if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host == "static.cninfo.com.cn" && u.User == nil {
			u.Scheme = "https"
			return u.String()
		}
	}
	values := url.Values{"stockCode": {a.SecCode}, "announcementId": {a.ID}, "orgId": {a.OrgID}}
	return "https://www.cninfo.com.cn/new/disclosure/detail?" + values.Encode()
}

func (s *desktopServer) handleAnnouncements(w http.ResponseWriter, r *http.Request) {
	page := requestPage(w, r)
	if page == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	categories := map[string]string{"": "", "annual": cninfo.CategoryAnnual, "halfyear": cninfo.CategoryHalfYear, "q1": cninfo.CategoryQ1, "q3": cninfo.CategoryQ3}
	category, ok := categories[r.URL.Query().Get("type")]
	if !ok {
		writeError(w, http.StatusBadRequest, "公告类别必须为 annual、halfyear、q1 或 q3，留空查询全部")
		return
	}
	req := &cninfo.QueryRequest{PageNum: page, PageSize: 30, Category: category}
	if strings.TrimSpace(r.URL.Query().Get("code")) != "" {
		code := companyCode(w, r)
		if code == "" {
			return
		}
		org, _, err := cninfo.LookupOrgID(ctx, code)
		if err != nil {
			writeError(w, http.StatusBadGateway, "证券公告信息获取失败，请稍后重试")
			return
		}
		req.StockCode = code + "," + org
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if len(key) > 200 {
		writeError(w, http.StatusBadRequest, "公告关键字过长")
		return
	}
	req.SearchKey = key
	response, err := cninfo.QueryAnnouncements(ctx, req)
	if err != nil {
		slog.ErrorContext(ctx, "查询公告失败", "error", err)
		writeError(w, http.StatusBadGateway, "公告获取失败，请稍后重试")
		return
	}
	results := make([]map[string]any, 0)
	if response == nil {
		writeError(w, http.StatusBadGateway, "公告数据暂时不可用")
		return
	}
	for _, a := range response.Data {
		if a == nil || a.ExistFlag != 0 || a.InvalidationFlag != 0 {
			continue
		}
		date := ""
		if a.Time > 0 {
			date = time.UnixMilli(a.Time).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
		}
		results = append(results, map[string]any{
			"id": a.ID, "title": html.UnescapeString(titleTags.ReplaceAllString(a.Title, "")),
			"date": date, "code": a.SecCode, "name": html.UnescapeString(titleTags.ReplaceAllString(a.SecName, "")), "url": announcementURL(a),
		})
	}
	writeJSON(w, map[string]any{"results": results, "total": response.Total, "has_more": response.HasMore || page < response.TotalPages})
}

func (s *desktopServer) handleFinancials(w http.ResponseWriter, r *http.Request) {
	code := companyCode(w, r)
	if code == "" {
		return
	}
	report := r.URL.Query().Get("report")
	if report == "" {
		report = "balance"
	}
	reportType, ok := eastmoney.ReportFromString(report)
	if !ok {
		writeError(w, http.StatusBadRequest, "不支持的财报类型")
		return
	}
	period := r.URL.Query().Get("period")
	periodCode := eastmoney.PeriodFromString(period)
	if period != "" && periodCode == "" {
		writeError(w, http.StatusBadRequest, "不支持的报告期")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, err := eastmoney.GetFinancialReport(ctx, &eastmoney.GetFinancialReportReq{Code: code, ReportType: reportType, Period: periodCode})
	if err != nil {
		slog.ErrorContext(ctx, "查询财报失败", "error", err)
		writeError(w, http.StatusBadGateway, "财报获取失败，请稍后重试")
		return
	}
	columns := make([]map[string]string, 0)
	for _, f := range eastmoney.KeyFields(reportType) {
		if f.API != "REPORT_DATE" {
			columns = append(columns, map[string]string{"key": f.API, "label": f.CN})
		}
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		results = append(results, map[string]any{"date": strings.Split(item.ReportDate, " ")[0], "fields": item.Fields})
	}
	writeJSON(w, map[string]any{"columns": columns, "results": results})
}

// handleHistory 按日期范围和复权方式返回日行情，默认最近三个月、不复权。
func (s *desktopServer) handleHistory(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	market := -1
	var symbol string
	if regexp.MustCompile(`^(SH|SZ)[0-9]{6}$`).MatchString(code) {
		symbol = code[2:]
		market = 0
		if strings.HasPrefix(code, "SH") {
			market = 1
		}
	} else if regexp.MustCompile(`^HK[0-9]{5}$`).MatchString(code) {
		symbol = code[2:]
		market = 116
	}
	if market < 0 {
		writeError(w, http.StatusBadRequest, "走势图暂支持沪深证券与港股")
		return
	}
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	query := r.URL.Query()
	end := query.Get("end")
	if end == "" {
		end = now.Format("2006-01-02")
	}
	endTime, err := time.Parse("2006-01-02", end)
	if err != nil {
		writeError(w, 400, "结束日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	begin := query.Get("begin")
	if begin == "" {
		begin = endTime.AddDate(0, -3, 0).Format("2006-01-02")
	}
	beginTime, err := time.Parse("2006-01-02", begin)
	if err != nil {
		writeError(w, 400, "开始日期格式无效，请使用 YYYY-MM-DD")
		return
	}
	if beginTime.After(endTime) || beginTime.Year() < 1990 || end > now.Format("2006-01-02") || endTime.After(beginTime.AddDate(5, 0, 0)) {
		writeError(w, 400, "日期范围须在 1990 年至今日之间，开始不晚于结束，单次最多 5 年")
		return
	}
	fq := query.Get("fq")
	fqt := eastmoney.QuoteFQTDefault
	switch fq {
	case "", "bfq":
	case "qfq":
		fqt = eastmoney.QuoteFQTFront
	case "hfq":
		fqt = eastmoney.QuoteFQTPost
	default:
		writeError(w, 400, "复权方式必须为 bfq、qfq 或 hfq")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	items, err := eastmoney.GetQuoteHistory(ctx, &eastmoney.GetQuoteHistoryReq{
		Code: symbol, MarketCode: market, Begin: beginTime.Format("20060102"), End: endTime.Format("20060102"), FQT: fqt,
	})
	if err != nil {
		slog.ErrorContext(ctx, "查询历史行情失败", "error", err)
		writeError(w, http.StatusBadGateway, "历史行情获取失败，请稍后重试")
		return
	}
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item != nil {
			results = append(results, map[string]any{
				"date": item.Date.Format("2006-01-02"), "open": item.Open, "close": item.Close,
				"high": item.High, "low": item.Low,
				"volume": item.Volume, "turnover": item.TurnOver,
			})
		}
	}
	writeJSON(w, map[string]any{"results": results})
}
