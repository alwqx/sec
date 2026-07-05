// Package cninfo provides access to the CNINFO (巨潮资讯网) official disclosure platform.
// CNINFO is the CSRC-designated platform for listed company announcements in China,
// covering all A-share stocks across Shanghai, Shenzhen, and Beijing exchanges.
package cninfo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const (
	stockListURL = "https://www.cninfo.com.cn/new/data/szse_stock.json"
	queryURL     = "https://www.cninfo.com.cn/new/hisAnnouncement/query"
	detailURL    = "https://www.cninfo.com.cn/new/disclosure/detail"
	pdfBaseURL   = "http://static.cninfo.com.cn"

	// Announcement category codes
	CategoryAnnual   = "category_ndbg_szsh"   // 年报
	CategoryHalfYear = "category_bndbg_szsh"  // 半年报
	CategoryQ1       = "category_yjdbg_szsh"  // 一季报
	CategoryQ3       = "category_sjdbg_szsh"  // 三季报
	CategoryIPO      = "category_sf_szsh"     // 首次公开发行及上市（招股书）
	CategoryProspect = "category_scgkfx_szsh" // 招股说明书公开发行
)

// StockInfo holds a stock entry from the CNINFO stock list.
type StockInfo struct {
	Code     string `json:"code"`
	OrgID    string `json:"orgId"`
	Name     string `json:"zwjc"`
	Category string `json:"category"`
}

// Announcement represents a single disclosure announcement.
type Announcement struct {
	ID               string `json:"announcementId"`
	Title            string `json:"announcementTitle"`
	Time             int64  `json:"announcementTime"`
	AdjunctURL       string `json:"adjunctUrl"`
	AdjunctSize      int64  `json:"adjunctSize"`
	AdjunctType      string `json:"adjunctType"`
	SecCode          string `json:"secCode"`
	SecName          string `json:"secName"`
	OrgID            string `json:"orgId"`
	TypeName         string `json:"announcementTypeName"`
	ExistFlag        int    `json:"existFlag"`
	InvalidationFlag int    `json:"invalidationFlag"`
	// Derived fields, 非 JSON 字段，由本包内部计算填充
	Date   string // YYYYMMDD，由 Time(毫秒) 字段派生
	PDFURL string // 完整 PDF 下载直链，由 AdjunctURL 派生
}

// QueryRequest holds parameters for querying announcements.
type QueryRequest struct {
	StockCode string // "{code},{orgId}" format
	Category  string // announcement category code
	StartDate string // "YYYY-MM-DD"
	EndDate   string // "YYYY-MM-DD"
	SearchKey string // full-text search keyword
	PageNum   int
	PageSize  int
}

// QueryResponse wraps the CNINFO API response.
type QueryResponse struct {
	Total      int             `json:"totalAnnouncement"`
	TotalPages int             `json:"totalpages"`
	HasMore    bool            `json:"hasMore"`
	Data       []*Announcement `json:"announcements"`
}

// columnForCode determines the exchange column parameter from a stock code.
// QueryIPOByDateRange 按公告日期范围查询 IPO / 发行相关公告。
// CNINFO 按 seDate 参数做过滤：seDate = "{start} ~ {end}"，其中日期格式为 YYYY-MM-DD。
// 返回范围为 (start, end) 区间内所有 category_sf_szsh 公告（亦含少量 scgkfx）。
func QueryIPOByDateRange(ctx context.Context, startDate, endDate string, max int) ([]*Announcement, error) {
	const pageSize = 30 // cninfo pageSize 固定 30（见 QueryAnnouncements 内部约束）
	items := make([]*Announcement, 0)
	pageNum := 1

	for {
		req := &QueryRequest{
			Category:  CategoryIPO,
			PageNum:   pageNum,
			PageSize:  pageSize,
			StartDate: startDate,
			EndDate:   endDate,
		}

		resp, err := QueryAnnouncements(ctx, req)
		if err != nil {
			return items, fmt.Errorf("QueryIPOByDateRange: %w", err)
		}
		if resp == nil || len(resp.Data) == 0 {
			break
		}

		for _, a := range resp.Data {
			if a == nil {
				continue
			}
			if a.Time > 0 {
				a.Date = time.Unix(a.Time/1000, 0).Format("20060102")
			}
			// if a.AdjunctURL != "" && !strings.HasPrefix(a.AdjunctURL, "http") {
			// 	a.PDFURL = "http://static.cninfo.com.cn/" + strings.TrimLeft(a.AdjunctURL, "/")
			// } else {
			// 	a.PDFURL = a.AdjunctURL
			// }
			a.PDFURL = resolvePDFURL(a)
			items = append(items, a)
			if max > 0 && len(items) >= max {
				return items, nil
			}
		}
		if !resp.HasMore {
			break
		}
		pageNum++
	}
	return items, nil
}

func columnForCode(code string) string {
	if len(code) < 2 {
		return "szse"
	}
	switch code[:2] {
	case "60", "68":
		return "sse"
	case "83", "87", "43", "40":
		return "bj"
	default:
		return "szse"
	}
}

// plateForCode determines the plate parameter (market segment).
func plateForCode(code string) string {
	if len(code) < 2 {
		return "sz;sh"
	}
	switch code[:2] {
	case "60", "68":
		return "sh"
	case "83", "87", "43", "40":
		return "bj"
	default:
		return "sz"
	}
}

// stockListCachePath returns the path for caching the stock list JSON.
// Uses ~/.sec/cache/ directory, falling back to os.TempDir if unavailable.
func stockListCachePath() (path string) {
	dir, err := utils.SecDir("cache")
	if err != nil {
		path = filepath.Join(os.TempDir(), "sec_cninfo_stocks.json")
		slog.Warn("stockListCachePath err and use tmp dir", "error", err, "tmp_path", path)
	} else {
		path = filepath.Join(dir, "cninfo_stocks.json")
	}

	return
}

// loadStockListCache tries to load cached stock list data.
func loadStockListCache() ([]byte, bool) {
	path := stockListCachePath()
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	// Cache valid for 24 hours
	if time.Since(info.ModTime()) > 24*time.Hour {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// saveStockListCache saves stock list data to cache.
func saveStockListCache(data []byte) {
	path := stockListCachePath()
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Warn("failed to cache stock list", "error", err)
	}
}

// GetStockList fetches the full stock-to-orgId mapping from CNINFO.
func GetStockList(ctx context.Context) ([]*StockInfo, error) {
	// Try cache first
	if cached, ok := loadStockListCache(); ok {
		var payload struct {
			StockList []*StockInfo `json:"stockList"`
		}
		err := json.Unmarshal(cached, &payload)
		if err == nil {
			slog.DebugContext(ctx, "GetStockList get cache")
			return payload.StockList, nil
		} else {
			slog.ErrorContext(ctx, "GetStockList failed json.Unmarshal cache", "error", err)
		}
	}

	resp, err := utils.MakeRequest(ctx, http.MethodGet, stockListURL, nil, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("fetch stock list: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	saveStockListCache(body)

	var stockList struct {
		StockList []*StockInfo `json:"stockList"`
	}
	if err := json.Unmarshal(body, &stockList); err != nil {
		return nil, fmt.Errorf("parse stock list: %w", err)
	}

	return stockList.StockList, nil
}

// LookupOrgID finds the orgId for a given stock code.
func LookupOrgID(ctx context.Context, code string) (string, string, error) {
	stocks, err := GetStockList(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "LookupOrgID", "error", err)
		return "", "", err
	}
	for _, s := range stocks {
		if s.Code == code {
			return s.OrgID, s.Name, nil
		}
	}
	return "", "", fmt.Errorf("stock code %s not found in CNINFO stock list", code)
}

// QueryIPOs 查询指定股票的 IPO 相关公告（招股书、发行公告等）。
// 覆盖 category_sf_szsh（首次公开发行及上市），并在返回时解析 PDF 直链。
func QueryIPOs(ctx context.Context, stockCode string, size int) ([]*Announcement, error) {
	if size <= 0 || size > 5000 {
		size = 30
	}
	req := &QueryRequest{
		StockCode: stockCode,
		Category:  CategoryIPO,
		PageSize:  size,
	}

	resp, err := QueryAnnouncements(ctx, req)
	if err != nil {
		return nil, err
	}

	// 对每个公告解析 PDF 直链 & 日期（由 Time(ms) 派生）
	out := make([]*Announcement, 0)
	for _, a := range resp.Data {
		if a == nil {
			continue
		}
		// 从毫秒时间戳派生日期（YYYYMMDD）
		if a.Time > 0 {
			a.Date = time.Unix(a.Time/1000, 0).Format("20060102")
		}

		// 解析 PDF 直链：优先用 AdjunctURL 直接拼接，
		// 否则构造 detail 链接
		// if a.AdjunctURL != "" && !strings.HasPrefix(a.AdjunctURL, "http") {
		// 	a.PDFURL = "http://static.cninfo.com.cn/" + strings.TrimLeft(a.AdjunctURL, "/")
		// } else {
		// 	a.PDFURL = a.AdjunctURL
		// }
		a.PDFURL = resolvePDFURL(a)

		if a.PDFURL == "" {
			a.PDFURL = fmt.Sprintf("https://www.cninfo.com.cn/new/disclosure/detail?stockCode=%s&announcementId=%s&orgId=%s",
				a.SecCode, a.ID, a.OrgID)
		}
		out = append(out, a)
	}
	return out, nil
}

// QueryAnnouncements queries CNINFO for announcements matching the request.
func QueryAnnouncements(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("req is nil")
	}

	pageNum := req.PageNum
	if pageNum <= 0 {
		pageNum = 1
	}
	// 经过在页面 https://www.cninfo.com.cn/new/disclosure/stock?stockCode=600036&orgId=gssh0600036&sjstsBond=false#latestAnnouncement
	// 测试，page-size=30 是固定的，如果调整成其它值，会导致查询参数失效，默认返回第一页的结果
	pageSize := req.PageSize
	if req.PageSize != 30 {
		slog.DebugContext(ctx, "QueryAnnouncements page is fixed to 30", "PageSize", req.PageSize)
		pageSize = 30
	}

	// Determine column and plate from stock code
	codeOnly := req.StockCode
	if idx := strings.Index(codeOnly, ","); idx > 0 {
		codeOnly = codeOnly[:idx]
	}
	column := columnForCode(codeOnly)

	form := url.Values{}
	form.Set("pageNum", strconv.Itoa(pageNum))
	form.Set("pageSize", strconv.Itoa(pageSize))
	form.Set("column", column)
	form.Set("tabName", "fulltext")
	// form.Set("sortName", "announcementTime")
	// form.Set("sortType", "desc")
	form.Set("isHLtitle", "true")

	if req.StockCode != "" {
		form.Set("stock", req.StockCode)
	}
	if req.Category != "" {
		form.Set("category", req.Category)
	}
	if req.StartDate != "" && req.EndDate != "" {
		form.Set("seDate", req.StartDate+"~"+req.EndDate)
	}
	if req.SearchKey != "" {
		form.Set("searchkey", req.SearchKey)
	}

	// CNINFO requires specific headers
	headers := http.Header{}
	headers.Set("X-Requested-With", "XMLHttpRequest")
	headers.Set("Referer", "https://www.cninfo.com.cn/new/commonUrl/pageOfSearch?url=disclosure/list/search")
	headers.Set("Accept", "application/json, text/javascript, */*; q=0.01")

	// Send parameters as query string in a POST request
	reqURL := queryURL + "?" + form.Encode()

	slog.DebugContext(ctx, "QueryAnnouncements", "params", form.Encode(), "reqURL", reqURL)

	resp, err := utils.MakeRequest(ctx, http.MethodPost, reqURL, headers, nil, 0)
	if err != nil {
		slog.ErrorContext(ctx, "QueryAnnouncements failed request", "url", reqURL)
		return nil, fmt.Errorf("query announcements: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "QueryAnnouncements http status code", "code", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("invalid status code %d", resp.StatusCode)
	}

	var qr QueryResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &qr, nil
}

// resolvePDFURL
func resolvePDFURL(a *Announcement) string {
	if a.AdjunctURL != "" && !strings.HasPrefix(a.AdjunctURL, "http") {
		return pdfBaseURL + "/" + strings.TrimLeft(a.AdjunctURL, "/")
	}
	if a.AdjunctURL != "" {
		return a.AdjunctURL
	}
	return fmt.Sprintf("%s?stockCode=%s&announcementId=%s&orgId=%s", detailURL, a.SecCode, a.ID, a.OrgID)
}

// PDFDownloadResult holds the result of downloading a PDF.
type PDFDownloadResult struct {
	Title    string
	Year     string
	FilePath string
	FileSize int64
}

// DownloadPDF downloads an announcement PDF from the CNINFO CDN.
func DownloadPDF(ctx context.Context, adjunctURL, destPath string) error {
	fullURL, err := url.JoinPath(pdfBaseURL, adjunctURL)
	if err != nil {
		slog.ErrorContext(ctx, "failed to JoinPath", "error", err)
		return err
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, fullURL, nil, nil, 10*time.Minute)
	if err != nil {
		slog.ErrorContext(ctx, "failed to download PDF", "fullURL", fullURL, "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download PDF returned status %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}

	slog.Info("downloaded PDF", "path", destPath, "size", written)
	return nil
}
