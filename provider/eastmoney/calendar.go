package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/alwqx/sec/utils"
)

// IPOCalendar provider: 东方财富新股日历（近期 IPO 排期，含申购、中签日等）。
// 接口暴露给 AKShare / efinance 社区，非官方。

const (
	ipoCalendarURL = "http://push2ex.eastmoney.com/api/qt/newcalendar/get"
)

// IPOCalendarItem 新股日历一条记录
type IPOCalendarItem struct {
	StatusCode    string `json:"status_code"` // 0: 已打新, 1: 待申购, 2: 待上市
	StatusLabel   string `json:"status_label"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	PurchaseCode  string `json:"purchase_code"`
	PurchaseDate  string `json:"purchase_date"`  // 申购日期 YYYY-MM-DD
	PublishDate   string `json:"publish_date"`   // 中签/配售结果公布
	ListingDate   string `json:"listing_date"`   // 预计上市日 YYYY-MM-DD
	PurchaseLimit string `json:"purchase_limit"` // 申购上限（万元）
	PE            string `json:"pe"`             // 市盈率（发行价口径）
	IssuePrice    string `json:"issue_price"`    // 发行价
	TotalAmount   string `json:"total_amount"`   // 募资总额
	TotalShares   string `json:"total_shares"`   // 发行股数
	Date          string `json:"date"`           // 数据日期
	Exchange      string `json:"exchange"`       // sh/sz/bj
}

// IPOListCalendarReq 新股日历查询请求
type IPOListCalendarReq struct {
	StartDate string // 查询开始日期 YYYY-MM-DD
	EndDate   string // 查询结束日期 YYYY-MM-DD；0 值时默认 StartDate+1 月
}

// IPOCalendarResponse 东财新股日历返回结构
type IPOCalendarResponse struct {
	Data []*IPOCalendarItem `json:"data"`
}

// ListIPOCalendar 获取新股日历（近期 IPO 排期）
func ListIPOCalendar(ctx context.Context, req *IPOListCalendarReq) ([]*IPOCalendarItem, error) {
	if req == nil {
		return nil, fmt.Errorf("req is nil")
	}

	startDate := req.StartDate
	if startDate == "" {
		startDate = time.Now().Format(utils.LayoutYYMMDD)
	}
	endDate := req.EndDate
	if endDate == "" {
		// 默认为 start 后一个月
		t, err := time.Parse(utils.LayoutYYMMDD, startDate)
		if err != nil {
			return nil, fmt.Errorf("invalid start_date: %w", err)
		}
		endDate = t.AddDate(0, 1, 0).Format(utils.LayoutYYMMDD)
	}

	v := url.Values{}
	v.Set("source", "newstock")
	v.Set("client", "app")
	v.Set("start_date", startDate)
	v.Set("end_date", endDate)

	reqURL := ipoCalendarURL + "?" + v.Encode()
	// 东方财富 push2ex 要求浏览器 UA
	headers := http.Header{}
	headers.Set("User-Agent", browserUA)
	headers.Set("Accept", "*/*")
	client := newHTTPClient(defaultTimeout)
	resp, err := doRequest(ctx, client, http.MethodGet, reqURL, headers, nil)
	if err != nil {
		return nil, fmt.Errorf("eastMoney IPO calendar request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("eastMoney IPO calendar read: %w", err)
	}

	var raw IPOCalendarResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("eastMoney IPO calendar parse: %w", err)
	}
	return raw.Data, nil
}
