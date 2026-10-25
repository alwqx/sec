//go:build windows || darwin

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/utils"
)

type annualReport struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Date  string `json:"date"`
	Year  int    `json:"year"`
	URL   string `json:"url"`
}

var annualTitle = regexp.MustCompile(`((?:19|20)[0-9]{2})年?(?:年度报告|年报)(?:[（(][^）)]*[）)])?$`)
var annualPDFPath = regexp.MustCompile(`^/finalpage/[0-9]{4}-[0-9]{2}-[0-9]{2}/[A-Za-z0-9_-]+\.(?i:pdf)$`)

func reportYear(value string) (int, error) {
	year, err := strconv.Atoi(value)
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	if err != nil || year < 1990 || year >= now.Year() {
		return 0, errors.New("报告年度必须在 1990 年至上一年度之间")
	}
	return year, nil
}

func validatedPDFURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "static.cninfo.com.cn" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || !annualPDFPath.MatchString(parsed.Path) {
		return nil, errors.New("无效的巨潮资讯 PDF 地址")
	}
	return parsed, nil
}

func annualPDFURL(raw string) string {
	if strings.HasPrefix(raw, "finalpage/") || strings.HasPrefix(raw, "/finalpage/") {
		raw = "https://static.cninfo.com.cn/" + strings.TrimPrefix(raw, "/")
	}
	if strings.HasPrefix(raw, "http://static.cninfo.com.cn/") {
		raw = "https://" + strings.TrimPrefix(raw, "http://")
	}
	if _, err := validatedPDFURL(raw); err != nil {
		return ""
	}
	return raw
}

// queryAnnualReports 按报告年度过滤正文，披露日期从次年开始，避免将报告年度误作公告年度。
func queryAnnualReports(ctx context.Context, stock string, year, page int) ([]annualReport, bool, error) {
	return queryAnnualReportRange(ctx, stock, year, year, page)
}

func queryAnnualReportRange(ctx context.Context, stock string, startYear, endYear, page int) ([]annualReport, bool, error) {
	search := ""
	if startYear == endYear {
		search = strconv.Itoa(startYear)
	}
	today := time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
	response, err := cninfo.QueryAnnouncements(ctx, &cninfo.QueryRequest{
		StockCode: stock, Category: cninfo.CategoryAnnual, SearchKey: search,
		StartDate: fmt.Sprintf("%d-01-01", startYear+1), EndDate: today, PageNum: page, PageSize: 30,
	})
	if err != nil {
		return nil, false, err
	}
	if response == nil {
		return nil, false, errors.New("年报数据为空")
	}
	results := []annualReport{}
	for _, item := range response.Data {
		if item == nil || item.ExistFlag != 0 || item.InvalidationFlag != 0 {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(titleTags.ReplaceAllString(item.Title, "")))
		skip := false
		for _, word := range []string{"摘要", "英文", "已取消", "更正", "修订", "English", "英文版"} {
			if strings.Contains(title, word) {
				skip = true
				break
			}
		}
		compact := strings.Join(strings.Fields(title), "")
		match := annualTitle.FindStringSubmatch(compact)
		if skip || len(match) < 2 {
			continue
		}
		year, _ := strconv.Atoi(match[1])
		if year < startYear || year > endYear {
			continue
		}
		pdfURL := annualPDFURL(item.AdjunctURL)
		if pdfURL == "" {
			continue
		}
		date := ""
		if item.Time > 0 {
			date = time.UnixMilli(item.Time).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
		}
		results = append(results, annualReport{ID: item.ID, Title: title, Date: date, Year: year, URL: pdfURL})
	}
	return results, response.HasMore || page < response.TotalPages, nil
}

func (s *desktopServer) handleAnnualReports(w http.ResponseWriter, r *http.Request) {
	code := companyCode(w, r)
	if code == "" {
		return
	}
	startValue, endValue := r.URL.Query().Get("start_year"), r.URL.Query().Get("end_year")
	if startValue == "" && endValue == "" {
		startValue = r.URL.Query().Get("year")
		endValue = startValue
	}
	startYear, err := reportYear(startValue)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	endYear, err := reportYear(endValue)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if startYear > endYear {
		writeError(w, 400, "开始年度不能晚于结束年度")
		return
	}
	page := requestPage(w, r)
	if page == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	org, _, err := cninfo.LookupOrgID(ctx, code)
	if err != nil {
		slog.ErrorContext(ctx, "年报证券查询失败", "error", err)
		writeError(w, 502, "证券公告信息获取失败，请稍后重试")
		return
	}
	results, more, err := queryAnnualReportRange(ctx, code+","+org, startYear, endYear, page)
	if err != nil {
		slog.ErrorContext(ctx, "查询年度报告失败", "error", err)
		writeError(w, 502, "年度报告获取失败，请稍后重试")
		return
	}
	writeJSON(w, map[string]any{"results": results, "page": page, "has_more": more})
}

func copyAnnualPDF(ctx context.Context, raw string, output io.Writer) error {
	if _, err := validatedPDFURL(raw); err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("PDF 重定向次数过多")
		}
		_, err := validatedPDFURL(request.URL.String())
		return err
	}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", utils.UserAgent())
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("PDF 请求返回 %d", response.StatusCode)
	}
	const maxSize = 128 * 1024 * 1024
	if response.ContentLength > maxSize {
		return errors.New("PDF 文件超过 128 MB")
	}
	reader := bufio.NewReader(response.Body)
	signature, err := reader.Peek(5)
	if err != nil || string(signature) != "%PDF-" {
		return errors.New("返回内容不是 PDF")
	}
	written, err := io.Copy(output, io.LimitReader(reader, maxSize+1))
	if err != nil {
		return err
	}
	if written > maxSize {
		return errors.New("PDF 文件超过 128 MB")
	}
	return ctx.Err()
}

func (s *desktopServer) handleAnnualDownload(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code string `json:"code"`
		Year string `json:"year"`
		URL  string `json:"url"`
	}
	if !s.readDownloadRequest(w, r, &request) {
		return
	}
	request.Code = strings.ToUpper(strings.TrimSpace(request.Code))
	if !aShareCode.MatchString(request.Code) {
		writeError(w, 400, "年报下载暂仅支持沪深北 A 股")
		return
	}
	year, err := reportYear(request.Year)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	parsed, err := validatedPDFURL(request.URL)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	filename := fmt.Sprintf("%s_%d_%s", request.Code, year, parsed.Path[strings.LastIndex(parsed.Path, "/")+1:])
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	saved, err := s.saveDownload(filename, func(output io.Writer) error { return copyAnnualPDF(ctx, parsed.String(), output) })
	if err != nil {
		slog.ErrorContext(ctx, "下载年度报告失败", "error", err)
		writeError(w, 502, "年报 PDF 下载或保存失败，请稍后重试")
		return
	}
	writeJSON(w, saved)
}
