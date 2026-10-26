package nbs

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// RetailRequest 查询全国社会消费品零售总额，月份为 YYYYMM。
type RetailRequest struct{ Begin, End string }

func (r RetailRequest) Validate() error { return validateMonths(r.Begin, r.End) }

// RetailObservation 保留官方名义金额和增速，不用累计值推算缺失的单月值。
type RetailObservation struct {
	Region             string    `json:"region"`
	Period             string    `json:"period"`
	YTDPeriod          string    `json:"ytd_period"`
	Amount             *float64  `json:"amount"`
	YTDAmount          *float64  `json:"ytd_amount"`
	YoY                *float64  `json:"yoy_percent"`
	YTDGrowth          *float64  `json:"ytd_growth_percent"`
	AmountUnit         string    `json:"amount_unit"`
	GrowthUnit         string    `json:"growth_unit"`
	PriceBasis         string    `json:"price_basis"`
	SeasonallyAdjusted bool      `json:"seasonally_adjusted"`
	CatalogID          string    `json:"catalog_id"`
	AmountSeriesID     string    `json:"amount_series_id"`
	YTDAmountSeriesID  string    `json:"ytd_amount_series_id"`
	YoYSeriesID        string    `json:"yoy_series_id"`
	YTDGrowthSeriesID  string    `json:"ytd_growth_series_id"`
	Source             string    `json:"source"`
	FetchedAt          time.Time `json:"fetched_at"`
	Note               string    `json:"note,omitempty"`
}

var retailSeriesNames = []string{
	"社会消费品零售总额当期值(亿元)",
	"社会消费品零售总额累计值(亿元)",
	"社会消费品零售总额同比增长(%)",
	"社会消费品零售总额累计增长(%)",
}

// RetailSales 动态发现全国总额系列，一次采集四项；不混入限额以上单位系列。
func (c *Client) RetailSales(ctx context.Context, req RetailRequest) ([]RetailObservation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	roots, err := c.tree(ctx, "")
	if err != nil {
		return nil, err
	}
	monthly, err := findNode(roots, "月度数据")
	if err != nil {
		return nil, err
	}
	groups, err := c.tree(ctx, monthly.ID)
	if err != nil {
		return nil, err
	}
	group, err := findNode(groups, "国内贸易")
	if err != nil {
		return nil, err
	}
	catalogs, err := c.tree(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	catalog, err := findNode(catalogs, "社会消费品零售总额")
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Data struct {
			Total int      `json:"total"`
			List  []series `json:"list"`
		} `json:"data"`
	}
	_, err = c.request(ctx, "/new/queryIndicatorsByCid", url.Values{"cid": {catalog.ID}, "pageSize": {"1000"}}, nil, &metadata)
	if err != nil {
		return nil, err
	}
	if metadata.Data.Total > len(metadata.Data.List) {
		return nil, fmt.Errorf("社会消费品零售总额指标列表不完整")
	}
	selected := make([]series, len(retailSeriesNames))
	ids := make([]string, len(retailSeriesNames))
	seenIDs := map[string]bool{}
	for i, name := range retailSeriesNames {
		for _, s := range metadata.Data.List {
			if compact(s.Name) != name {
				continue
			}
			if selected[i].ID != "" {
				return nil, fmt.Errorf("社会消费品零售总额指标重复: %s", name)
			}
			selected[i] = s
		}
		if selected[i].ID == "" {
			return nil, fmt.Errorf("社会消费品零售总额缺少指标: %s", name)
		}
		if seenIDs[selected[i].ID] {
			return nil, fmt.Errorf("社会消费品零售总额指标 ID 重复")
		}
		seenIDs[selected[i].ID] = true
		ids[i] = selected[i].ID
	}
	body := map[string]any{"cid": catalog.ID, "rootId": group.ID, "indicatorIds": ids, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{req.Begin + "MM-" + req.End + "MM"}, "showType": 1}
	var response struct {
		Data []valueRow `json:"data"`
	}
	fetched, err := c.request(ctx, "/stream/esData", nil, body, &response)
	if err != nil {
		return nil, err
	}
	return parseRetail(response.Data, req, catalog.ID, selected, fetched)
}

func parseRetail(rows []valueRow, req RetailRequest, catalog string, selected []series, fetched time.Time) ([]RetailObservation, error) {
	if len(selected) != 4 {
		return nil, fmt.Errorf("社会消费品零售总额指标元数据不完整")
	}
	var items []RetailObservation
	seen := map[string]bool{}
	for _, row := range rows {
		month := strings.TrimSuffix(row.Code, "MM")
		if _, err := time.Parse("200601", month); err != nil || len(month) != 6 {
			return nil, fmt.Errorf("无效的社零月份: %s", row.Code)
		}
		if month < req.Begin || month > req.End {
			continue
		}
		if seen[month] {
			return nil, fmt.Errorf("社会消费品零售总额返回重复月份: %s", month)
		}
		seen[month] = true
		item := RetailObservation{Region: "CN", Period: month, YTDPeriod: month[:4] + "01-" + month, AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", SeasonallyAdjusted: false, CatalogID: catalog, AmountSeriesID: selected[0].ID, YTDAmountSeriesID: selected[1].ID, YoYSeriesID: selected[2].ID, YTDGrowthSeriesID: selected[3].ID, Source: baseURL + "/stream/esData", FetchedAt: fetched}
		fields := []**float64{&item.Amount, &item.YTDAmount, &item.YoY, &item.YTDGrowth}
		for i, s := range selected {
			found := false
			for _, v := range row.Values {
				if v.ID != s.ID || v.Area != "000000000000" {
					continue
				}
				if found {
					return nil, fmt.Errorf("社会消费品零售总额返回重复观测值: %s %s", month, s.Name)
				}
				found = true
				n, err := parseNumber(v.Value)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", month, s.Name, err)
				}
				if i < 2 && n != nil && *n < 0 {
					return nil, fmt.Errorf("社会消费品零售总额金额不能为负: %s", month)
				}
				*fields[i] = n
			}
		}
		if month[4:] == "02" && item.Amount == nil && item.YoY == nil && (item.YTDAmount != nil || item.YTDGrowth != nil) {
			item.Note = "1—2月合并累计，不是2月单月值"
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Period < items[j].Period })
	return items, nil
}
