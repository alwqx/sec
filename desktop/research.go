//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/eastmoney"
	quotehistory "github.com/alwqx/sec/provider/history"
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

// announcementItem 转为页面使用的公告字段，标题和名称去掉高亮标签。
func announcementItem(a *cninfo.Announcement) map[string]any {
	date := ""
	if a.Time > 0 {
		date = time.UnixMilli(a.Time).In(cninfo.Beijing).Format("2006-01-02")
	}
	return map[string]any{
		"id": a.ID, "title": cninfo.CleanTitle(a.Title),
		"date": date, "code": a.SecCode, "name": cninfo.CleanTitle(a.SecName), "url": announcementURL(a),
	}
}

const announcementPageSize = 30

// handleInsiderAnnouncements 返回当前证券的增减持公告。两个关键字的结果合并后排序，再在这里分页。
func (s *desktopServer) handleInsiderAnnouncements(w http.ResponseWriter, r *http.Request, page int) {
	// 两个关键字可能各翻 10 页，比普通公告查询需要更长时间。
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	code := companyCode(w, r)
	if code == "" {
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if len(key) > 200 {
		writeError(w, http.StatusBadRequest, "公告关键字过长")
		return
	}
	org, _, err := cninfo.LookupOrgID(ctx, code)
	if err != nil {
		writeError(w, http.StatusBadGateway, "证券公告信息获取失败，请稍后重试")
		return
	}
	items, truncated, err := cninfo.QueryInsiderAnnouncements(ctx, code+","+org)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询增减持公告失败", "code", code, "error", err)
		writeError(w, http.StatusBadGateway, "公告获取失败，请稍后重试")
		return
	}
	// 不区分大小写，空格分隔的词都要命中。
	terms := strings.Fields(strings.ToLower(key))
	matched := make([]*cninfo.Announcement, 0, len(items))
	for _, item := range items {
		title := strings.ToLower(cninfo.CleanTitle(item.Title))
		if !slices.ContainsFunc(terms, func(term string) bool { return !strings.Contains(title, term) }) {
			matched = append(matched, item)
		}
	}
	begin := min((page-1)*announcementPageSize, len(matched))
	end := min(begin+announcementPageSize, len(matched))
	results := make([]map[string]any, 0, end-begin)
	for _, item := range matched[begin:end] {
		results = append(results, announcementItem(item))
	}
	writeJSON(w, map[string]any{"results": results, "total": len(matched), "has_more": end < len(matched), "truncated": truncated})
}

// handleIPOAnnouncements 按公告日期查询 IPO 相关公告。公告日期不是申购日或上市日。
func (s *desktopServer) handleIPOAnnouncements(w http.ResponseWriter, r *http.Request) {
	page := requestPage(w, r)
	if page == 0 {
		return
	}
	since, until := r.URL.Query().Get("since"), r.URL.Query().Get("until")
	if err := eastmoney.ValidateIPODates(since, until); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// CNINFO 只有起止日期都提供时才按日期筛选，这里补齐另一端。
	if since != "" && until == "" {
		until = time.Now().In(cninfo.Beijing).Format("2006-01-02")
	}
	if until != "" && since == "" {
		since = "1990-01-01"
	}
	// 补齐后范围可能颠倒，例如开始日期晚于今天。
	if err := eastmoney.ValidateIPODates(since, until); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	response, err := cninfo.QueryAnnouncements(ctx, &cninfo.QueryRequest{Category: cninfo.CategoryIPO, PageNum: page, PageSize: announcementPageSize, StartDate: since, EndDate: until})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询 IPO 公告失败", "error", err)
		writeError(w, http.StatusBadGateway, "公告获取失败，请稍后重试")
		return
	}
	if response == nil {
		writeError(w, http.StatusBadGateway, "公告数据暂时不可用")
		return
	}
	results := make([]map[string]any, 0, len(response.Data))
	for _, a := range response.Data {
		if a != nil && a.ExistFlag == 0 && a.InvalidationFlag == 0 {
			results = append(results, announcementItem(a))
		}
	}
	writeJSON(w, map[string]any{"results": results, "total": response.Total, "has_more": response.HasMore || page < response.TotalPages})
}

func (s *desktopServer) handleAnnouncements(w http.ResponseWriter, r *http.Request) {
	page := requestPage(w, r)
	if page == 0 {
		return
	}
	if r.URL.Query().Get("type") == "insider" {
		s.handleInsiderAnnouncements(w, r, page)
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
		results = append(results, announcementItem(a))
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

// historyQuery 是走势和技术分析共用的证券、日期和复权参数。
type historyQuery struct {
	symbol     string
	market     int
	begin, end time.Time
	fqt        eastmoney.FuQuanType
}

func parseHistoryQuery(w http.ResponseWriter, r *http.Request) (historyQuery, bool) {
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
		return historyQuery{}, false
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
		return historyQuery{}, false
	}
	begin := query.Get("begin")
	if begin == "" {
		begin = endTime.AddDate(0, -3, 0).Format("2006-01-02")
	}
	beginTime, err := time.Parse("2006-01-02", begin)
	if err != nil {
		writeError(w, 400, "开始日期格式无效，请使用 YYYY-MM-DD")
		return historyQuery{}, false
	}
	if beginTime.After(endTime) || beginTime.Year() < 1990 || end > now.Format("2006-01-02") || endTime.After(beginTime.AddDate(5, 0, 0)) {
		writeError(w, 400, "日期范围须在 1990 年至今日之间，开始不晚于结束，单次最多 5 年")
		return historyQuery{}, false
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
		return historyQuery{}, false
	}
	return historyQuery{symbol, market, beginTime, endTime, fqt}, true
}

// historyErrorMessage 在只有复权行情失败时提示可以切换，备用数据源只有不复权行情。
func historyErrorMessage(err error) string {
	if errors.Is(err, quotehistory.ErrAdjustedUnavailable) {
		return "复权行情暂时获取失败，可切换为不复权"
	}
	if errors.Is(err, quotehistory.ErrRangeNotCovered) {
		return "东方财富行情暂不可用，备用数据只有最近约 4 年，请缩短查询区间"
	}
	return "历史行情获取失败，请稍后重试"
}

// turnoverValue 在数据源没有成交额时返回 null，不返回 0。
func turnoverValue(item *eastmoney.Quote) any {
	if item.NoTurnover {
		return nil
	}
	return item.TurnOver
}

// handleHistory 按日期范围和复权方式返回日行情，默认最近三个月、不复权。
func (s *desktopServer) handleHistory(w http.ResponseWriter, r *http.Request) {
	query, ok := parseHistoryQuery(w, r)
	if !ok {
		return
	}
	beginTime, endTime := query.begin, query.end
	specs, err := parseOverlays(r.URL.Query().Get("ma"), r.URL.Query().Get("boll"))
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	fetched, err := quotehistory.GetFrom(ctx, &eastmoney.GetQuoteHistoryReq{
		Code: query.symbol, MarketCode: query.market, Begin: warmupBegin(beginTime, specs).Format("20060102"), End: endTime.Format("20060102"), FQT: query.fqt,
	}, beginTime.Format("20060102"))
	if err != nil {
		slog.ErrorContext(ctx, "查询历史行情失败", "error", err)
		writeError(w, http.StatusBadGateway, historyErrorMessage(err))
		return
	}
	quotes := make([]*eastmoney.Quote, 0, len(fetched.Quotes))
	for _, item := range fetched.Quotes {
		if item != nil {
			quotes = append(quotes, item)
		}
	}
	// 预热数据只用于计算指标，不进入结果。
	beginDay := beginTime.Format("2006-01-02")
	first := sort.Search(len(quotes), func(i int) bool { return quotes[i].Date.Format("2006-01-02") >= beginDay })
	results := make([]map[string]any, 0, len(quotes)-first)
	for _, item := range quotes[first:] {
		results = append(results, map[string]any{
			"date": item.Date.Format("2006-01-02"), "open": item.Open, "close": item.Close,
			"high": item.High, "low": item.Low,
			"volume": item.Volume, "turnover": turnoverValue(item),
		})
	}
	response := map[string]any{"results": results}
	if fetched.Note != "" {
		response["source_note"] = fetched.Note
	}
	if len(specs) > 0 {
		response["overlays"] = computeOverlays(quotes, first, specs)
	}
	writeJSON(w, response)
}
