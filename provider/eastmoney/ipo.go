package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

// newHTTPClient 返回仅走 IPv4 的 HTTP Client（东方财富 push2 接口 IPv6 不可达，且 keep-alive 导致 EOF）。
func newHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout: timeout,
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, "tcp4", addr)
			},
			// 东方财富 push2 对 Go 的 keep-alive 连接返回 EOF；强制禁用
			DisableKeepAlives:   true,
			MaxIdleConns:        0,
			MaxIdleConnsPerHost: 0,
		},
		Timeout: timeout,
	}
}

// doRequest 在指定 http.Client 上发送请求
func doRequest(ctx context.Context, client *http.Client, method, reqURL string, headers http.Header, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}
	if headers != nil {
		req.Header = headers
	} else {
		req.Header.Set("User-Agent", utilsUserAgent)
	}
	return client.Do(req)
}

const utilsUserAgent = "sec/1.0 (+https://github.com/alwqx/sec)"

// IPO provider: 基于东方财富 PUSH2 的新股列表（已上市/待上市）。
//
// 接口文档（非官方）: http://push2.eastmoney.com/api/qt/clist/get
// 字段语义参考 AKShare stock_new_ipo_em / efinance get_ipo_info 源码。

// IPO 列表 field constants（东方财富 push2 接口字段 ID）
var (
	// ipoURL 用 var 而非 const 以方便测试用 httptest server 覆盖
	ipoURL = "http://push2.eastmoney.com/api/qt/clist/get"

	// 请求字段
	ipoFields = "f12,f14,f26,f33,f43,f44,f45,f57,f58,f162,f163,f167,f2,f3,f4"

	// f12:  证券代码
	// f14:  证券简称
	// f26:  上市日期（20260702 / -）
	// f33:  发行价（元）
	// f43:  发行市盈率 PE（东财用 100 倍整数存储：/100 即实际值）
	// f44:  行业 PE（100 倍整数）
	// f45:  首日涨跌幅（%，100 倍整数）
	// f57:  首日收盘价
	// f58:  首日开盘价
	// f162: 网上中签率（%，100 倍整数）
	// f163: 募资总额（万元）
	// f167: 换手率（%，100 倍整数）
	// f2:   最新价
	// f3:   涨跌幅（%，100 倍整数）
	// f4:   涨跌额

	// 已上市新股列表的 fs 参数
	// m:0 沪深 + s:2048 北交所 + t:80 上交所主板 + t:81 深交所主板
	ipoFS = "m:0+t:80+t:81+s:2048"

	// 排序字段：f26 = 按上市日期
	ipoSortField = "f26"

	browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)" +
		" AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// IPO 单条记录
type IListing struct {
	Code         string `json:"code"`           // 证券代码
	Name         string `json:"name"`           // 证券简称
	ListingDate  string `json:"listing_date"`   // 上市日期 (YYYYMMDD / -)
	PurchaseCode string `json:"purchase_code"`  // 申购代码（东方财富列表中不存在此字段，暂留）
	PurchaseName string `json:"purchase_name"`  // 申购简称（同上）
	Date         string `json:"date,omitempty"` // YYYY-MM-DD 格式，来自请求参数或接口返回
}

// IPOListReq 新股列表查询参数
type IPOListReq struct {
	SortDesc bool // 排序方向：true=倒序（近到远），false=正序（远到近）
	PageNum  int  // 页码，从 1 开始
	PageSize int  // 每页条数（东财上限 5000）
	MaxTotal int  // 最大获取条数；0 表示按 PageSize 取一页
}

// ListIPOResponse 东财新股列表返回原始结构
type ListIPOResponse struct {
	Rc   int             `json:"rc"`
	Data *IPOListRawData `json:"data"`
}

// IPOListRawData 新股列表 data 字段
type IPOListRawData struct {
	Total int                      `json:"total"`
	Diff  []map[string]interface{} `json:"diff"`
}

// ListIPO 获取东方财富新股列表（已上市新股 + 待上市新股）
func ListIPO(ctx context.Context, req *IPOListReq) ([]*IListing, int, error) {
	if req == nil {
		req = &IPOListReq{}
	}
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 5000 {
		pageSize = 30
	}
	pageNum := req.PageNum
	if pageNum <= 0 {
		pageNum = 1
	}

	items := make([]*IListing, 0)
	maxItems := req.MaxTotal
	if maxItems <= 0 {
		maxItems = pageSize
	}

	var firstTotal int
	for {
		batch, total, err := fetchIPOListBatch(ctx, pageNum, pageSize)
		if err != nil {
			return nil, 0, err
		}

		if len(items) == 0 {
			firstTotal = total
		}
		items = append(items, batch...)
		if len(items) >= maxItems || len(batch) < pageSize {
			break
		}
		pageNum++
	}
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return items, firstTotal, nil
}

func fetchIPOListBatch(ctx context.Context, pageNum, pageSize int) ([]*IListing, int, error) {
	po := "1"
	if pageNum > 0 {
		// "po" 没有正式文档，观测经验：1 表示倒序（最近上市在前）
		po = "1"
	}

	v := url.Values{}
	v.Set("pn", strconv.Itoa(pageNum))
	v.Set("pz", strconv.Itoa(pageSize))
	v.Set("np", "1")
	v.Set("fltt", "2")
	v.Set("invt", "2")
	v.Set("fid", ipoSortField)
	v.Set("po", po)
	v.Set("fs", ipoFS)
	v.Set("fields", ipoFields)

	reqURL := ipoURL + "?" + v.Encode()
	// 东方财富 push2 要求浏览器 UA + Referer 才能正常返回；偶发 EOF 时自动重试
	headers := http.Header{}
	headers.Set("User-Agent", browserUA)
	headers.Set("Referer", "http://data.eastmoney.com/xg/xg/default.html")
	headers.Set("Accept", "*/*")
	client := newHTTPClient(defaultTimeout)
	var resp *http.Response
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		resp, err = doRequest(ctx, client, http.MethodGet, reqURL, headers, nil)
		if err == nil {
			break
		}
		slog.ErrorContext(ctx, "failed fetchIPOListBatch", "attempt", attempt, "error", err)
		if attempt < 5 {
			// 指数退避：1s, 2s, 4s, 8s
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				slog.WarnContext(ctx, "fetchIPOListBatch ctx.Done")
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if err != nil {
		return nil, 0, fmt.Errorf("eastMoney IPO list request (5 attempts): %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.ErrorContext(ctx, "failed read body", "error", err)
		return nil, 0, err
	}

	var raw ListIPOResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, 0, fmt.Errorf("eastMoney IPO list parse: %w", err)
	}
	if raw.Data == nil {
		return nil, 0, fmt.Errorf("eastMoney IPO list: nil data")
	}

	listings := make([]*IListing, 0, len(raw.Data.Diff))
	for _, item := range raw.Data.Diff {
		listing, err := parseIPOListItem(item)
		if err != nil {
			continue
		}
		listings = append(listings, listing)
	}
	return listings, raw.Data.Total, nil
}

func parseIPOListItem(m map[string]interface{}) (*IListing, error) {
	code := getStrField(m, "f12")
	name := getStrField(m, "f14")
	if code == "" || code == "-" {
		return nil, fmt.Errorf("invalid IPO item: empty code")
	}

	listingDateString := getStrField(m, "f26")
	var listingDate time.Time
	if listingDateString != "-" && listingDateString != "" {
		// 东财 f26 字段可能是整数 20260702 或 "20260702" 字符串
		parsedDate, err := time.Parse("20060102", listingDateString)
		if err != nil {
			// 尝试从 float 解析
			if f, ok := listingDateString2Float(listingDateString); ok {
				listingDateString = strconv.FormatInt(int64(math.Round(f)), 10)
			}
			parsedDate, err = time.Parse("20060102", listingDateString)
			if err != nil {
				listingDate = time.Time{}
			} else {
				listingDate = parsedDate
			}
		} else {
			listingDate = parsedDate
		}
	}
	ListingDateString := listingDateString
	if !listingDate.IsZero() {
		ListingDateString = listingDate.Format("20060102")
	}

	return &IListing{
		Code:         code,
		Name:         name,
		ListingDate:  ListingDateString,
		PurchaseCode: getStrField(m, "f57"),
		PurchaseName: getStrField(m, "f58"),
	}, nil
}

// listingDateString2Float 将形如 "20260702" 的字符串转 float（float 模式）
func listingDateString2Float(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// StrField 从 map 提取字符串字段
func getStrField(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "-" || strings.TrimSpace(x) == "" {
			return "-"
		}
		return strings.TrimSpace(x)
	case float64:
		if x == 0 {
			return "-"
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

// Float64Field 从 map 提取 float64 字段
func getFloat64Field(m map[string]interface{}, key string) float64 {
	v, ok := m[key]
	if !ok {
		return -1
	}
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case int:
		return float64(x)
	case string:
		if x == "-" || x == "" {
			return -1
		}
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return -1
		}
		return f
	default:
		return -1
	}
}
