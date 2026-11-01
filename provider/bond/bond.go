package bond

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

var (
	treasuryYieldCurveURL = "https://home.treasury.gov/resource-center/data-chart-center/interest-rates/pages/xml?data=daily_treasury_yield_curve&field_tdr_date_value=%d"
)

// YieldCurveYearURL 返回财政部收益率曲线单个年度的 XML 地址。
func YieldCurveYearURL(year int) string {
	return fmt.Sprintf(treasuryYieldCurveURL, year)
}

// BondYieldItem 单日国债收益率数据
type BondYieldItem struct {
	Date          string
	DateTime      time.Time
	MissingTenors map[string]bool // 缺失的期限键，区分真实 0% 与上游空值

	BC1Month float64 // 1个月
	BC3Month float64 // 3个月
	BC6Month float64 // 6个月
	BC1Year  float64 // 1年
	BC2Year  float64 // 2年
	BC3Year  float64 // 3年
	BC5Year  float64 // 5年
	BC7Year  float64 // 7年
	BC10Year float64 // 10年
	BC20Year float64 // 20年
	BC30Year float64 // 30年

	// 前一日收益率（10年期），接口中默认没有，排序后人工计算填充
	YClose     float64
	Change     float64
	ChangeRate float64
}

// atomFeed Atom feed 外层结构
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entry   []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Content atomContent `xml:"content"`
}

type atomContent struct {
	InnerXML string `xml:",innerxml"`
}

// treasuryProperties 内层 m:properties 结构，包含全部期限收益率
type treasuryProperties struct {
	XMLName         xml.Name      `xml:"properties"`
	NewDate         string        `xml:"NEW_DATE"`
	BC1Month        nullableYield `xml:"BC_1MONTH"`
	BC1_5Month      float64       `xml:"BC_1_5MONTH"`
	BC2Month        float64       `xml:"BC_2MONTH"`
	BC3Month        nullableYield `xml:"BC_3MONTH"`
	BC4Month        float64       `xml:"BC_4MONTH"`
	BC6Month        nullableYield `xml:"BC_6MONTH"`
	BC1Year         nullableYield `xml:"BC_1YEAR"`
	BC2Year         nullableYield `xml:"BC_2YEAR"`
	BC3Year         nullableYield `xml:"BC_3YEAR"`
	BC5Year         nullableYield `xml:"BC_5YEAR"`
	BC7Year         nullableYield `xml:"BC_7YEAR"`
	BC10Year        nullableYield `xml:"BC_10YEAR"`
	BC20Year        nullableYield `xml:"BC_20YEAR"`
	BC30Year        nullableYield `xml:"BC_30YEAR"`
	BC30YearDisplay float64       `xml:"BC_30YEARDISPLAY"`
}

// nullableYield 保留空值状态，避免把未公布的期限收益率误报为 0%。
type nullableYield struct {
	Null string `xml:"null,attr"`
	Text string `xml:",chardata"`
}

func (v nullableYield) number() (float64, bool) {
	if v.Null == "true" || strings.TrimSpace(v.Text) == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(v.Text), 64)
	return value, err == nil
}

// invalidTenor 返回第一个既不是空值、也不是数值的期限，全部合法时返回空字符串。
func (p treasuryProperties) invalidTenor() string {
	for _, tenor := range []struct {
		id    string
		value nullableYield
	}{
		{"1m", p.BC1Month}, {"3m", p.BC3Month}, {"6m", p.BC6Month}, {"1y", p.BC1Year}, {"2y", p.BC2Year}, {"3y", p.BC3Year},
		{"5y", p.BC5Year}, {"7y", p.BC7Year}, {"10y", p.BC10Year}, {"20y", p.BC20Year}, {"30y", p.BC30Year},
	} {
		if tenor.value.Null == "true" || strings.TrimSpace(tenor.value.Text) == "" {
			continue
		}
		if _, ok := tenor.value.number(); !ok {
			return tenor.id
		}
	}
	return ""
}

// QueryBondReq 请求结构体
type QueryBondReq struct {
	Start, End string
	// Strict 为 true 时不输出部分结果：年度抓取失败、已过去的年度没有记录、记录或数值无法解析都返回错误。
	// 当前年度可以没有记录，此时返回空列表。
	Strict bool
}

// QueryBondResp 返回数据结构体
type QueryBondResp struct {
	Data []*BondYieldItem
}

// QueryBond 查询美国国债收益率曲线
func QueryBond(ctx context.Context, req *QueryBondReq) (*QueryBondResp, error) {
	if req == nil {
		return nil, errors.New("req is nil")
	}

	var (
		start, end time.Time
		err        error
	)
	start, err = time.Parse(utils.LayoutYYMMDD, req.Start)
	if err != nil {
		return nil, err
	}
	end, err = time.Parse(utils.LayoutYYMMDD, req.End)
	if err != nil {
		return nil, err
	}

	// 收集需要查询的年份
	years := make(map[int]struct{})
	for y := start.Year(); y <= end.Year(); y++ {
		years[y] = struct{}{}
	}

	var allItems []*BondYieldItem
	for y := range years {
		items, err := fetchYearData(ctx, y, req.Strict)
		if err == nil && req.Strict && len(items) == 0 && y < time.Now().UTC().Year() {
			err = errors.New("no records for a past year")
		}
		if err != nil {
			if req.Strict {
				return nil, fmt.Errorf("fetch treasury data %d: %w", y, err)
			}
			slog.WarnContext(ctx, "failed fetch treasury data", "year", y, "error", err)
			continue
		}
		allItems = append(allItems, items...)
	}

	if len(allItems) == 0 && !req.Strict {
		return nil, errors.New("no treasury yield data fetched")
	}

	// 按时间升序排列，填充 YClose
	sort.Slice(allItems, func(i, j int) bool {
		return allItems[i].DateTime.Before(allItems[j].DateTime)
	})
	for i := range allItems {
		if i == 0 {
			allItems[i].YClose = -1
			allItems[i].ChangeRate = 0
		} else {
			allItems[i].YClose = allItems[i-1].BC10Year
			allItems[i].Change = allItems[i].BC10Year - allItems[i].YClose
			if allItems[i].YClose == 0.0 {
				allItems[i].ChangeRate = -1
			} else {
				allItems[i].ChangeRate = allItems[i].Change / allItems[i].YClose
			}
		}
	}

	// 按时间范围过滤
	data := make([]*BondYieldItem, 0)
	for i := range allItems {
		item := allItems[i]
		if start.After(item.DateTime) || end.Before(item.DateTime) {
			continue
		}
		data = append(data, item)
	}

	return &QueryBondResp{Data: data}, nil
}

// CurveDay 是一天的收益率曲线，未公布的期限为 null。
type CurveDay struct {
	Date   string   `json:"date"`
	Month1 *float64 `json:"1m"`
	Month3 *float64 `json:"3m"`
	Month6 *float64 `json:"6m"`
	Year1  *float64 `json:"1y"`
	Year2  *float64 `json:"2y"`
	Year3  *float64 `json:"3y"`
	Year5  *float64 `json:"5y"`
	Year7  *float64 `json:"7y"`
	Year10 *float64 `json:"10y"`
	Year20 *float64 `json:"20y"`
	Year30 *float64 `json:"30y"`
}

// CurveDays 转为官方期限的输出结构，不含 YClose、Change 等依赖查询范围的计算字段。
func CurveDays(items []*BondYieldItem) []CurveDay {
	days := make([]CurveDay, 0, len(items))
	for _, item := range items {
		yield := func(id string, value float64) *float64 {
			if item.MissingTenors[id] {
				return nil
			}
			return &value
		}
		days = append(days, CurveDay{
			Date:   item.Date,
			Month1: yield("1m", item.BC1Month), Month3: yield("3m", item.BC3Month), Month6: yield("6m", item.BC6Month),
			Year1: yield("1y", item.BC1Year), Year2: yield("2y", item.BC2Year), Year3: yield("3y", item.BC3Year),
			Year5: yield("5y", item.BC5Year), Year7: yield("7y", item.BC7Year), Year10: yield("10y", item.BC10Year),
			Year20: yield("20y", item.BC20Year), Year30: yield("30y", item.BC30Year),
		})
	}
	return days
}

// fetchYearData 获取指定年份的国债收益率数据
func fetchYearData(ctx context.Context, year int, strict bool) ([]*BondYieldItem, error) {
	url := YieldCurveYearURL(year)
	slog.DebugContext(ctx, "fetchYearData", "reqURL", url)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, url, nil, nil, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("treasury yield curve returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseFeed(body, strict)
}

// parseAtomFeed 解析 Atom feed XML 返回 BondYieldItem 列表
func parseAtomFeed(data []byte) ([]*BondYieldItem, error) {
	return parseFeed(data, false)
}

// parseFeed 解析 Atom feed；strict 为 true 时无法解析的记录和重复日期返回错误，否则跳过。
func parseFeed(data []byte, strict bool) ([]*BondYieldItem, error) {
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}

	items := make([]*BondYieldItem, 0, len(feed.Entry))
	for _, entry := range feed.Entry {
		innerXML := strings.TrimSpace(entry.Content.InnerXML)
		if innerXML == "" {
			if strict {
				return nil, errors.New("treasury entry has no content")
			}
			continue
		}
		var props treasuryProperties
		if err := xml.Unmarshal([]byte(innerXML), &props); err != nil {
			if strict {
				return nil, fmt.Errorf("parse treasury entry: %w", err)
			}
			slog.Warn("failed parse inner properties", "error", err)
			continue
		}

		// NEW_DATE 格式: "2026-01-02T00:00:00"
		dateStr := strings.TrimSpace(props.NewDate)
		if len(dateStr) >= 10 {
			dateStr = dateStr[:10]
		}
		dateTime, err := time.Parse(utils.LayoutYYMMDD, dateStr)
		if err != nil {
			if strict {
				return nil, fmt.Errorf("parse treasury date %q: %w", props.NewDate, err)
			}
			slog.Warn("failed parse date", "date", props.NewDate, "error", err)
			continue
		}
		if strict {
			if tenor := props.invalidTenor(); tenor != "" {
				return nil, fmt.Errorf("treasury %s %s is not a number", dateStr, tenor)
			}
		}

		item := &BondYieldItem{Date: dateStr, DateTime: dateTime, MissingTenors: map[string]bool{}}
		if value, ok := props.BC1Month.number(); ok {
			item.BC1Month = value
		} else {
			item.MissingTenors["1m"] = true
		}
		if value, ok := props.BC3Month.number(); ok {
			item.BC3Month = value
		} else {
			item.MissingTenors["3m"] = true
		}
		if value, ok := props.BC6Month.number(); ok {
			item.BC6Month = value
		} else {
			item.MissingTenors["6m"] = true
		}
		if value, ok := props.BC1Year.number(); ok {
			item.BC1Year = value
		} else {
			item.MissingTenors["1y"] = true
		}
		if value, ok := props.BC2Year.number(); ok {
			item.BC2Year = value
		} else {
			item.MissingTenors["2y"] = true
		}
		if value, ok := props.BC3Year.number(); ok {
			item.BC3Year = value
		} else {
			item.MissingTenors["3y"] = true
		}
		if value, ok := props.BC5Year.number(); ok {
			item.BC5Year = value
		} else {
			item.MissingTenors["5y"] = true
		}
		if value, ok := props.BC7Year.number(); ok {
			item.BC7Year = value
		} else {
			item.MissingTenors["7y"] = true
		}
		if value, ok := props.BC10Year.number(); ok {
			item.BC10Year = value
		} else {
			item.MissingTenors["10y"] = true
		}
		if value, ok := props.BC20Year.number(); ok {
			item.BC20Year = value
		} else {
			item.MissingTenors["20y"] = true
		}
		if value, ok := props.BC30Year.number(); ok {
			item.BC30Year = value
		} else {
			item.MissingTenors["30y"] = true
		}
		items = append(items, item)
	}

	// 去重（按日期）
	sort.Slice(items, func(i, j int) bool {
		return items[i].DateTime.Before(items[j].DateTime)
	})
	deduped := make([]*BondYieldItem, 0, len(items))
	for i, item := range items {
		if i > 0 && items[i-1].Date == item.Date {
			if strict {
				return nil, fmt.Errorf("duplicate treasury date %s", item.Date)
			}
			continue
		}
		deduped = append(deduped, item)
	}

	return deduped, nil
}
