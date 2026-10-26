package pbc

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FinancingRequest 查询全国社融，月份使用 YYYYMM；全年表始终完整读取，保证累计求和。
type FinancingRequest struct{ Begin, End string }

func (r FinancingRequest) Validate() error {
	for _, month := range []string{r.Begin, r.End} {
		if len(month) != 6 {
			return fmt.Errorf("月份必须为 YYYYMM: %s", month)
		}
		if _, err := time.Parse("200601", month); err != nil {
			return fmt.Errorf("无效月份: %s", month)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于结束月份")
	}
	return nil
}

// FinancingComponent 保留结构金额，不假设已展示分项之和等于社融总额。
type FinancingComponent struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Stock    *float64 `json:"stock"`
	StockYoY *float64 `json:"stock_yoy_percent"`
	Flow     *float64 `json:"flow"`
	YTDFlow  *float64 `json:"ytd_flow"`
}

// FinancingObservation 金额统一为亿元，原单位和原始响应另行保留。
type FinancingObservation struct {
	Region          string               `json:"region"`
	Period          string               `json:"period"`
	YTDPeriod       string               `json:"ytd_period"`
	Stock           *float64             `json:"stock"`
	StockYoY        *float64             `json:"stock_yoy_percent"`
	Flow            *float64             `json:"flow"`
	YTDFlow         *float64             `json:"ytd_flow"`
	Unit            string               `json:"unit"`
	YTDMethod       string               `json:"ytd_method"`
	Components      []FinancingComponent `json:"components"`
	FlowSource      string               `json:"flow_source"`
	StockSource     string               `json:"stock_source"`
	FlowSourceUnit  string               `json:"flow_source_unit"`
	StockSourceUnit string               `json:"stock_source_unit"`
	FlowFetchedAt   time.Time            `json:"flow_fetched_at"`
	StockFetchedAt  time.Time            `json:"stock_fetched_at"`
	FlowNotes       []string             `json:"flow_notes"`
	StockNotes      []string             `json:"stock_notes"`
	Note            string               `json:"note,omitempty"`
}

func (c *Client) financingLinks(ctx context.Context, year int, root *goquery.Document) (string, string, error) {
	yearURL, err := c.link(root, c.origin+statisticsPath, strconv.Itoa(year)+"年统计数据")
	if err != nil {
		return "", "", err
	}
	doc, _, err := c.get(ctx, yearURL)
	if err != nil {
		return "", "", err
	}
	socialURL, err := c.link(doc, yearURL, "社会融资规模")
	if err != nil {
		return "", "", err
	}
	doc, _, err = c.get(ctx, socialURL)
	if err != nil {
		return "", "", err
	}
	find := func(label string) (string, error) {
		result := ""
		var linkErr error
		doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
			// 只匹配本行直接单元格，避免外层布局表包含所有子表而串列。
			matched := false
			row.ChildrenFiltered("td,th").Each(func(_ int, cell *goquery.Selection) {
				if cell.Find("table").Length() == 0 && strings.HasPrefix(compact(cell.Text()), label) {
					matched = true
				}
			})
			if !matched {
				return
			}
			row.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if !strings.HasSuffix(strings.ToLower(strings.Split(href, "?")[0]), ".htm") && !strings.HasSuffix(strings.ToLower(href), ".html") {
					return
				}
				address, err := c.resolve(socialURL, href)
				if err != nil {
					linkErr = err
					return
				}
				if result != "" && result != address {
					linkErr = fmt.Errorf("社融表链接重复: %s", label)
					return
				}
				result = address
			})
		})
		if linkErr != nil {
			return "", linkErr
		}
		if result == "" {
			return "", fmt.Errorf("%d 年社融目录缺少 %s HTML 表", year, label)
		}
		return result, nil
	}
	flow, err := find("社会融资规模增量统计表")
	if err != nil {
		return "", "", err
	}
	stock, err := find("社会融资规模存量统计表")
	return flow, stock, err
}

// SocialFinancing 按年份采集官方存量、增量表，累计值从年初完整月度增量求和。
func (c *Client) SocialFinancing(ctx context.Context, req FinancingRequest) ([]FinancingObservation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	root, _, err := c.get(ctx, c.origin+statisticsPath)
	if err != nil {
		return nil, err
	}
	first, _ := strconv.Atoi(req.Begin[:4])
	last, _ := strconv.Atoi(req.End[:4])
	var result []FinancingObservation
	for year := first; year <= last; year++ {
		flowURL, stockURL, err := c.financingLinks(ctx, year, root)
		if err != nil {
			return nil, err
		}
		flowDoc, flowFetched, err := c.get(ctx, flowURL)
		if err != nil {
			return nil, err
		}
		stockDoc, stockFetched, err := c.get(ctx, stockURL)
		if err != nil {
			return nil, err
		}
		flow, err := parseFinancing(flowDoc, year, "flow")
		if err != nil {
			return nil, fmt.Errorf("%d 年增量表: %w", year, err)
		}
		stock, err := parseFinancing(stockDoc, year, "stock")
		if err != nil {
			return nil, fmt.Errorf("%d 年存量表: %w", year, err)
		}
		for month := 1; month <= 12; month++ {
			period := fmt.Sprintf("%04d%02d", year, month)
			if period < req.Begin || period > req.End {
				continue
			}
			f := flow.Values[period]["total"].Amount
			s := stock.Values[period]["total"]
			if f == nil && s.Amount == nil && s.YoY == nil {
				continue
			} // 年度模板预留的未发布月份不输出为观测。
			item := FinancingObservation{Region: "CN", Period: period, YTDPeriod: fmt.Sprintf("%04d01-%s", year, period), Stock: normalise(s.Amount, stock.Unit), StockYoY: s.YoY, Flow: normalise(f, flow.Unit), YTDFlow: yearToDate(flow, year, month, "total"), Unit: "亿元", YTDMethod: "sum-of-published-monthly-flows", FlowSource: flowURL, StockSource: stockURL, FlowSourceUnit: flow.Unit, StockSourceUnit: stock.Unit, FlowFetchedAt: flowFetched, StockFetchedAt: stockFetched, FlowNotes: flow.Notes, StockNotes: stock.Notes}
			keys := map[string]bool{}
			for key := range flow.Names {
				keys[key] = true
			}
			for key := range stock.Names {
				keys[key] = true
			}
			var ordered []string
			for key := range keys {
				if key != "total" {
					ordered = append(ordered, key)
				}
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				name := flow.Names[key]
				if name == "" {
					name = stock.Names[key]
				}
				s := stock.Values[period][key]
				item.Components = append(item.Components, FinancingComponent{Key: key, Name: name, Stock: normalise(s.Amount, stock.Unit), StockYoY: s.YoY, Flow: normalise(flow.Values[period][key].Amount, flow.Unit), YTDFlow: yearToDate(flow, year, month, key)})
			}
			if item.YTDFlow == nil {
				item.Note = "年初至当月存在增量缺失，累计值不计算"
			}
			if item.Stock == nil || item.Flow == nil {
				if item.Note != "" {
					item.Note += "；"
				}
				item.Note += "存量或当月增量缺失，未用差额推算"
			}
			result = append(result, item)
		}
	}
	return result, nil
}

func normalise(value *float64, unit string) *float64 {
	if value == nil {
		return nil
	}
	n := *value
	if unit == "万亿元" {
		n *= 10000
	}
	n = math.Round(n*1e6) / 1e6
	return &n
}

func yearToDate(flow financingTable, year, month int, key string) *float64 {
	total := 0.0
	for m := 1; m <= month; m++ {
		n := normalise(flow.Values[fmt.Sprintf("%04d%02d", year, m)][key].Amount, flow.Unit)
		if n == nil {
			return nil
		}
		total += *n
	}
	total = math.Round(total*1e6) / 1e6
	return &total
}
