package nbs

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// IndustrialRequest 查询全国规模以上工业增加值增速，月份为 YYYYMM。
type IndustrialRequest struct{ Begin, End string }

func (r IndustrialRequest) Validate() error { return validateMonths(r.Begin, r.End) }

// IndustrialObservation 区分单月同比、年初累计同比和季调环比；数值均为实际增速（%）。
type IndustrialObservation struct {
	Region                string     `json:"region"`
	Period                string     `json:"period"`
	YTDPeriod             string     `json:"ytd_period"`
	YoY                   *float64   `json:"yoy_percent"`
	YTD                   *float64   `json:"ytd_percent"`
	MoM                   *float64   `json:"mom_percent"`
	Unit                  string     `json:"unit"`
	PriceBasis            string     `json:"price_basis"`
	MoMSeasonallyAdjusted bool       `json:"mom_seasonally_adjusted"`
	CatalogID             string     `json:"catalog_id"`
	YoYSeriesID           string     `json:"yoy_series_id"`
	YTDSeriesID           string     `json:"ytd_series_id"`
	Source                string     `json:"source"`
	FetchedAt             time.Time  `json:"fetched_at"`
	MoMSource             string     `json:"mom_source,omitempty"`
	MoMPublishedAt        *time.Time `json:"mom_published_at"`
	MoMFetchedAt          *time.Time `json:"mom_fetched_at"`
	Note                  string     `json:"note,omitempty"`
}

// IndustrialProduction 采集发布库同比/累计增速，并用官方发布稿修订表补充季调环比。
func (c *Client) IndustrialProduction(ctx context.Context, req IndustrialRequest) ([]IndustrialObservation, error) {
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
	group, err := findNode(groups, "工业")
	if err != nil {
		return nil, err
	}
	catalogs, err := c.tree(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	catalog, err := findNode(catalogs, "规上工业增加值增长速度")
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
		return nil, fmt.Errorf("工业增加值指标列表不完整")
	}
	var selected []series
	for _, name := range []string{"规上工业增加值同比增长(%)", "规上工业增加值累计增长(%)"} {
		var found *series
		for _, s := range metadata.Data.List {
			if compact(s.Name) == name {
				if found != nil {
					return nil, fmt.Errorf("工业增加值指标重复: %s", name)
				}
				v := s
				found = &v
			}
		}
		if found == nil {
			return nil, fmt.Errorf("工业增加值缺少指标: %s", name)
		}
		selected = append(selected, *found)
	}
	body := map[string]any{"cid": catalog.ID, "rootId": group.ID, "indicatorIds": []string{selected[0].ID, selected[1].ID}, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{req.Begin + "MM-" + req.End + "MM"}, "showType": 1}
	var response struct {
		Data []valueRow `json:"data"`
	}
	fetched, err := c.request(ctx, "/stream/esData", nil, body, &response)
	if err != nil {
		return nil, err
	}
	items, err := parseIndustrial(response.Data, req, catalog.ID, selected, fetched)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return items, nil
	}
	if err := c.supplementIndustrialMoM(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func parseIndustrial(rows []valueRow, req IndustrialRequest, catalog string, selected []series, fetched time.Time) ([]IndustrialObservation, error) {
	if len(selected) != 2 {
		return nil, fmt.Errorf("工业增加值指标元数据不完整")
	}
	var items []IndustrialObservation
	seen := map[string]bool{}
	for _, row := range rows {
		month := strings.TrimSuffix(row.Code, "MM")
		if _, err := time.Parse("200601", month); err != nil || len(month) != 6 {
			return nil, fmt.Errorf("无效的工业增加值月份: %s", row.Code)
		}
		if month < req.Begin || month > req.End {
			continue
		}
		if seen[month] {
			return nil, fmt.Errorf("工业增加值返回重复月份: %s", month)
		}
		seen[month] = true
		item := IndustrialObservation{Region: "CN", Period: month, YTDPeriod: month[:4] + "01-" + month, Unit: "%", PriceBasis: "real", MoMSeasonallyAdjusted: true, CatalogID: catalog, YoYSeriesID: selected[0].ID, YTDSeriesID: selected[1].ID, Source: baseURL + "/stream/esData", FetchedAt: fetched}
		for i, s := range selected {
			found := false
			for _, v := range row.Values {
				if v.ID != s.ID || v.Area != "000000000000" {
					continue
				}
				if found {
					return nil, fmt.Errorf("工业增加值返回重复观测值: %s %s", month, s.Name)
				}
				found = true
				n, err := parseNumber(v.Value)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", month, s.Name, err)
				}
				if i == 0 {
					item.YoY = n
				} else {
					item.YTD = n
				}
			}
		}
		if month[4:] == "02" && item.YoY == nil && item.YTD != nil {
			item.Note = "1—2月合并累计，非2月单月同比"
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Period < items[j].Period })
	return items, nil
}
