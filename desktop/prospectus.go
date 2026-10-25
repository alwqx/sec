//go:build windows || darwin

package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
)

var prospectusCode = regexp.MustCompile(`^[0-9]{6}$`)

func normalizeProspectusCode(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if aShareCode.MatchString(code) {
		code = code[2:]
	}
	if !prospectusCode.MatchString(code) {
		return ""
	}
	return code
}

type prospectusDocument struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Date  string `json:"date"`
	URL   string `json:"url"`
}

// queryProspectuses 复用 CLI 的首次公开发行及上市分类，保留招股意向书、招股说明书和发行公告。
func queryProspectuses(ctx context.Context, stock, begin, end string, page int) ([]prospectusDocument, bool, error) {
	response, err := cninfo.QueryAnnouncements(ctx, &cninfo.QueryRequest{StockCode: stock, Category: cninfo.CategoryIPO, StartDate: begin, EndDate: end, PageNum: page, PageSize: 30})
	if err != nil {
		return nil, false, err
	}
	if response == nil {
		return nil, false, fmt.Errorf("IPO 公告数据为空")
	}
	results := []prospectusDocument{}
	seen := map[string]bool{}
	for _, item := range response.Data {
		if item == nil || item.ExistFlag != 0 || item.InvalidationFlag != 0 {
			continue
		}
		pdf := annualPDFURL(item.AdjunctURL)
		if pdf == "" || seen[pdf] {
			continue
		}
		seen[pdf] = true
		date := ""
		if item.Time > 0 {
			date = time.UnixMilli(item.Time).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
		}
		results = append(results, prospectusDocument{ID: item.ID, Title: strings.TrimSpace(html.UnescapeString(titleTags.ReplaceAllString(item.Title, ""))), Date: date, URL: pdf})
	}
	return results, response.HasMore || page < response.TotalPages, nil
}

func (s *desktopServer) handleProspectuses(w http.ResponseWriter, r *http.Request) {
	code := normalizeProspectusCode(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, 400, "请输入沪深北 A 股六位证券代码，可带 SH/SZ/BJ 前缀")
		return
	}
	page := requestPage(w, r)
	if page == 0 {
		return
	}
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	begin, end := r.URL.Query().Get("begin"), r.URL.Query().Get("end")
	if begin == "" {
		begin = "1990-01-01"
	}
	if end == "" {
		end = today
	}
	start, err1 := time.Parse("2006-01-02", begin)
	stop, err2 := time.Parse("2006-01-02", end)
	if err1 != nil || err2 != nil || start.After(stop) || begin < "1990-01-01" || end > today {
		writeError(w, 400, "公告日期须为 YYYY-MM-DD，范围在 1990 年至今日之间，开始不晚于结束")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	org, name, err := cninfo.LookupOrgID(ctx, code)
	if err != nil || org == "" {
		writeError(w, 502, "证券公告信息获取失败，请确认代码或稍后重试")
		return
	}
	results, more, err := queryProspectuses(ctx, code+","+org, begin, end, page)
	if err != nil {
		slog.ErrorContext(ctx, "查询招股说明书失败", "error", err)
		writeError(w, 502, "招股说明书获取失败，请稍后重试")
		return
	}
	writeJSON(w, map[string]any{"code": code, "name": name, "results": results, "page": page, "has_more": more})
}

func (s *desktopServer) handleProspectusDownload(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	if !s.readDownloadRequest(w, r, &request) {
		return
	}
	code := normalizeProspectusCode(request.Code)
	if code == "" {
		writeError(w, 400, "招股说明书下载暂仅支持沪深北 A 股证券代码")
		return
	}
	parsed, err := validatedPDFURL(request.URL)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	saved, err := s.saveDownload(code+"_ipo_"+path.Base(parsed.Path), func(output io.Writer) error { return copyAnnualPDF(ctx, parsed.String(), output) })
	if err != nil {
		slog.ErrorContext(ctx, "下载招股说明书失败", "error", err)
		writeError(w, 502, "PDF 下载或保存失败，请稍后重试")
		return
	}
	writeJSON(w, saved)
}
