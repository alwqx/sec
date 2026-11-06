package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// hkQuoteURL 是东方财富个股行情接口，这里只取港股的市盈率和市净率。
var hkQuoteURL = "https://push2.eastmoney.com/api/qt/stock/get"

// 港股 F10 只取用到的列。
const (
	hkOrgColumns = "SECURITY_NAME_ABBR,ORG_NAME,BELONG_INDUSTRY,LISTING_DATE,ORG_WEB,ADDRESS,REG_ADDRESS,ORG_PROFILE,MAIN_BUSINESS"
	hkSecColumns = "ISSUE_PRICE"
)

// HKProfile 港股公司资料，来自东方财富港股 F10。上游没有的值是空字符串或 nil。
type HKProfile struct {
	ShortName       string   // 证券简称，如 腾讯控股
	Name            string   // 公司名称，如 腾讯控股有限公司
	Industry        string   // 所属行业，如 软件服务
	ListingDate     string   // 上市日期 2004-06-16
	IssuePrice      *float64 // 发行价，港元
	Website         string   // 公司网址，可能有多个，逗号分隔
	Address         string   // 办公地址
	RegisterAddress string   // 注册地址
	Description     string   // 公司简介
	MainBusiness    string   // 主营业务
	IssuePriceErr   error    // 发行价获取失败的原因，IssuePrice 为 nil
}

// GetHKProfile 查询港股公司资料，code 是 5 位港股代码，如 00700。没有这只证券时返回 nil 和 nil。
// 发行价是补充数据，请求失败时 IssuePrice 为 nil、原因在 IssuePriceErr，不影响公司资料。
// 交易所买卖基金也有一行，但公司名称是基金管理人，调用方应先按证券类型排除。
func GetHKProfile(ctx context.Context, code string) (*HKProfile, error) {
	filter := fmt.Sprintf(`(SECUCODE="%s.HK")`, code)
	var (
		wg               sync.WaitGroup
		orgRows, secRows []map[string]any
		orgErr, secErr   error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		orgRows, orgErr = datacenterRows(ctx, "RPT_HKF10_INFO_ORGPROFILE", hkOrgColumns, filter, "SECUCODE", 1)
	}()
	go func() {
		defer wg.Done()
		secRows, secErr = datacenterRows(ctx, "RPT_HKF10_INFO_SECURITYINFO", hkSecColumns, filter, "SECUCODE", 1)
	}()
	wg.Wait()

	if orgErr != nil {
		return nil, orgErr
	}
	if len(orgRows) == 0 {
		return nil, nil
	}
	row := orgRows[0]
	p := &HKProfile{
		ShortName:       rowText(row, "SECURITY_NAME_ABBR"),
		Name:            rowText(row, "ORG_NAME"),
		Industry:        rowText(row, "BELONG_INDUSTRY"),
		ListingDate:     rowDate(row, "LISTING_DATE"),
		Website:         rowText(row, "ORG_WEB"),
		Address:         rowText(row, "ADDRESS"),
		RegisterAddress: rowText(row, "REG_ADDRESS"),
		Description:     rowText(row, "ORG_PROFILE"),
		MainBusiness:    rowText(row, "MAIN_BUSINESS"),
	}
	p.IssuePriceErr = secErr
	if secErr == nil && len(secRows) > 0 {
		p.IssuePrice = RowNumber(secRows[0], "ISSUE_PRICE")
	}
	return p, nil
}

// GetHKValuation 查询港股的市盈率 TTM（f164）和市净率（f167），接口返回的是乘以 100 的整数。
// 东方财富已把人民币等报表货币的利润和净资产换算成港元。亏损时市盈率不大于 0，返回 nil；
// 0 表示没有数据，负的市净率保留。
func GetHKValuation(ctx context.Context, code string) (pe, pb *float64, err error) {
	params := url.Values{}
	params.Set("secid", fmt.Sprintf("%d.%s", MarketTypeHK, code))
	params.Set("fields", "f164,f167")
	body, err := getBody(ctx, hkQuoteURL+"?"+params.Encode())
	if err != nil {
		return nil, nil, err
	}
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("unmarshal quote: %w", err)
	}
	if resp.Data == nil {
		return nil, nil, nil
	}
	ratio := func(key string, positive bool) *float64 {
		value := RowNumber(resp.Data, key)
		if value == nil || *value == 0 || positive && *value < 0 {
			return nil
		}
		v := *value / 100
		return &v
	}
	return ratio("f164", true), ratio("f167", false), nil
}

// rowText 读取一行中的文本字段，缺失时返回空字符串。
// 简介等长文本里有换行和全角空格，连续空白合并成一个空格，方便按一行输出。
func rowText(row map[string]any, key string) string {
	text, _ := row[key].(string)
	return strings.Join(strings.Fields(text), " ")
}
