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

// BondYieldItem 单日国债收益率数据
type BondYieldItem struct {
	Date     string
	DateTime time.Time

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
	XMLName         xml.Name `xml:"properties"`
	NewDate         string   `xml:"NEW_DATE"`
	BC1Month        float64  `xml:"BC_1MONTH"`
	BC1_5Month      float64  `xml:"BC_1_5MONTH"`
	BC2Month        float64  `xml:"BC_2MONTH"`
	BC3Month        float64  `xml:"BC_3MONTH"`
	BC4Month        float64  `xml:"BC_4MONTH"`
	BC6Month        float64  `xml:"BC_6MONTH"`
	BC1Year         float64  `xml:"BC_1YEAR"`
	BC2Year         float64  `xml:"BC_2YEAR"`
	BC3Year         float64  `xml:"BC_3YEAR"`
	BC5Year         float64  `xml:"BC_5YEAR"`
	BC7Year         float64  `xml:"BC_7YEAR"`
	BC10Year        float64  `xml:"BC_10YEAR"`
	BC20Year        float64  `xml:"BC_20YEAR"`
	BC30Year        float64  `xml:"BC_30YEAR"`
	BC30YearDisplay float64  `xml:"BC_30YEARDISPLAY"`
}

// QueryBondReq 请求结构体
type QueryBondReq struct {
	Start, End string
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
		items, err := fetchYearData(ctx, y)
		if err != nil {
			slog.WarnContext(ctx, "failed fetch treasury data", "year", y, "error", err)
			continue
		}
		allItems = append(allItems, items...)
	}

	if len(allItems) == 0 {
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
			allItems[i].ChangeRate = allItems[i].Change / allItems[i].YClose
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

// fetchYearData 获取指定年份的国债收益率数据
func fetchYearData(ctx context.Context, year int) ([]*BondYieldItem, error) {
	url := fmt.Sprintf(treasuryYieldCurveURL, year)
	slog.DebugContext(ctx, "fetchYearData", "reqURL", url)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseAtomFeed(body)
}

// parseAtomFeed 解析 Atom feed XML 返回 BondYieldItem 列表
func parseAtomFeed(data []byte) ([]*BondYieldItem, error) {
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, err
	}

	items := make([]*BondYieldItem, 0, len(feed.Entry))
	for _, entry := range feed.Entry {
		innerXML := strings.TrimSpace(entry.Content.InnerXML)
		if innerXML == "" {
			continue
		}
		var props treasuryProperties
		if err := xml.Unmarshal([]byte(innerXML), &props); err != nil {
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
			slog.Warn("failed parse date", "date", props.NewDate, "error", err)
			continue
		}

		items = append(items, &BondYieldItem{
			Date:     dateStr,
			DateTime: dateTime,

			BC1Month: props.BC1Month,
			BC3Month: props.BC3Month,
			BC6Month: props.BC6Month,
			BC1Year:  props.BC1Year,
			BC2Year:  props.BC2Year,
			BC3Year:  props.BC3Year,
			BC5Year:  props.BC5Year,
			BC7Year:  props.BC7Year,
			BC10Year: props.BC10Year,
			BC20Year: props.BC20Year,
			BC30Year: props.BC30Year,
		})
	}

	// 去重（按日期）
	sort.Slice(items, func(i, j int) bool {
		return items[i].DateTime.Before(items[j].DateTime)
	})
	deduped := make([]*BondYieldItem, 0, len(items))
	for i, item := range items {
		if i > 0 && items[i-1].Date == item.Date {
			continue
		}
		deduped = append(deduped, item)
	}

	return deduped, nil
}

// formatBasisPoint 将收益率变化转换为基点 (basis points) 显示
func formatBasisPoint(change float64) string {
	bp := change * 100 // 1% = 100 basis points
	return fmt.Sprintf("%+.1fbp", bp)
}

// FormatYield 格式化收益率显示
func FormatYield(item *BondYieldItem) string {
	changeStr := ""
	if item.YClose == -1 {
		changeStr = strconv.FormatFloat(0, 'g', -1, 64)
	} else {
		changeStr = formatBasisPoint(item.Change)
	}
	return fmt.Sprintf("%.2f%% %s", item.BC10Year, changeStr)
}
