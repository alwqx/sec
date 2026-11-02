package cninfo

import (
	"net/url"
	"strings"
	"time"
)

// AnnouncementURL 返回公告原文链接；上游地址不是巨潮资讯的文件时，返回公告详情页。
func AnnouncementURL(a *Announcement) string {
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

// SizeBytes 返回原文大小的字节数。接口的单位是 KB，所以是近似值。
func (a *Announcement) SizeBytes() float64 {
	return float64(a.AdjunctSize) * 1024
}

// DateText 返回北京时间的公告日期 YYYY-MM-DD，上游缺失时为空字符串。巨潮资讯的时间是北京时间当日零点，按本地时区格式化可能差一天。
func (a *Announcement) DateText() string {
	if a.Time <= 0 {
		return ""
	}
	return time.UnixMilli(a.Time).In(Beijing).Format("2006-01-02")
}

// Row 是公告的通用输出，用于 --json，上游缺失的值是 null。
type Row struct {
	Date   *string `json:"date"` // 北京时间 YYYY-MM-DD
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	Title  string  `json:"title"`
	Type   string  `json:"type"`
	SizeKB *int64  `json:"size_kb"`
	URL    string  `json:"url"`
}

// ToRows 转为通用输出，跳过已失效的公告，结果不为 nil。
func ToRows(items []*Announcement) []Row {
	rows := make([]Row, 0, len(items))
	for _, a := range items {
		if a == nil || a.ExistFlag != 0 || a.InvalidationFlag != 0 {
			continue
		}
		row := Row{Code: a.SecCode, Name: CleanTitle(a.SecName), Title: CleanTitle(a.Title), Type: a.TypeName, URL: AnnouncementURL(a)}
		if date := a.DateText(); date != "" {
			row.Date = &date
		}
		if a.AdjunctSize > 0 {
			size := a.AdjunctSize
			row.SizeKB = &size
		}
		rows = append(rows, row)
	}
	return rows
}
