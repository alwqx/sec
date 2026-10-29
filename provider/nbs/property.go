package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// PropertyRequest 查询房地产全国累计数据，月份使用 YYYYMM。Component 为空时查询全部三项。
type PropertyRequest struct {
	Begin, End string
	Component  string
}

// Validate 在请求网络前检查参数。发布库当前三项系列的目录起点为 2000 年。
func (r *PropertyRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("房地产查询参数不能为空")
	}
	if err := validateMonths(r.Begin, r.End); err != nil {
		return err
	}
	if r.Begin < "200001" {
		return fmt.Errorf("房地产发布库查询范围不能早于 200001")
	}
	if r.Component != "" && r.Component != "all" {
		if _, ok := propertySpecs[r.Component]; !ok {
			return fmt.Errorf("不支持的房地产指标: %s，应为 all、sales-area、sales-value 或 starts-area", r.Component)
		}
	}
	return nil
}

// PropertyResult 保留各项实际可得的累计值和官方累计同比，不拼接旧接口或计算单月值。
type PropertyResult struct {
	Observations []PropertyObservation `json:"observations"`
	Notes        []string              `json:"notes"`
}

// PropertyObservation 的空数值为未提供，零值为真实的零；FetchedAt 不是发布时间。
type PropertyObservation struct {
	Region             string     `json:"region"`
	Component          string     `json:"component"`
	Period             string     `json:"period"`
	YTDPeriod          string     `json:"ytd_period"`
	YTDAmount          *float64   `json:"ytd_amount"`
	YTDGrowth          *float64   `json:"ytd_growth_percent"`
	AmountUnit         string     `json:"amount_unit"`
	GrowthUnit         string     `json:"growth_unit"`
	PriceBasis         string     `json:"price_basis"`
	SeasonallyAdjusted bool       `json:"seasonally_adjusted"`
	CatalogID          string     `json:"catalog_id"`
	CatalogName        string     `json:"catalog_name"`
	AmountSeriesID     string     `json:"amount_series_id"`
	GrowthSeriesID     string     `json:"growth_series_id"`
	AmountSeriesName   string     `json:"amount_series_name"`
	GrowthSeriesName   string     `json:"growth_series_name"`
	OriginalMetricName string     `json:"original_metric_name"`
	CoverageStart      string     `json:"catalog_coverage_start"`
	ScopeVersion       string     `json:"scope_version"`
	Definition         string     `json:"definition"`
	Annotation         string     `json:"annotation,omitempty"`
	Source             string     `json:"source"`
	FetchedAt          time.Time  `json:"fetched_at"`
	PublishedAt        *time.Time `json:"published_at"`
	Note               string     `json:"note,omitempty"`
}

type propertySpec struct {
	catalog, amountName, growthName, unit string
}

var propertySpecs = map[string]propertySpec{
	"sales-area":  {"新建商品房销售面积", "新建商品房销售面积累计值(万平方米)", "新建商品房销售面积累计增长(%)", "万平方米"},
	"sales-value": {"新建商品房销售额", "新建商品房销售额累计值(亿元)", "新建商品房销售额累计增长(%)", "亿元"},
	"starts-area": {"房地产施工、竣工面积", "房地产新开工施工面积累计值(万平方米)", "房地产新开工施工面积累计增长(%)", "万平方米"},
}

type propertySeries struct {
	ID            string `json:"_id"`
	Name          string `json:"i_showname"`
	OriginalName  string `json:"_name"`
	CatalogID     string `json:"catalogid"`
	Unit          string `json:"du_name"`
	Definition    string `json:"i_mark"`
	Annotation    string `json:"i_annotation"`
	CoverageStart string `json:"daterange_sdate"`
}

type propertyMetadataResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Total int              `json:"total"`
		List  []propertySeries `json:"list"`
	} `json:"data"`
}

type propertyValuesResponse struct {
	Success bool       `json:"success"`
	Data    []valueRow `json:"data"`
}

// Property 动态发现当前发布库的全国新建商品房与房屋新开工系列。
func (c *Client) Property(ctx context.Context, req *PropertyRequest) (*PropertyResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	roots, err := c.tree(ctx, "")
	if err != nil {
		return nil, err
	}
	monthly, err := propertyNode(roots, "月度数据")
	if err != nil {
		return nil, err
	}
	groups, err := c.tree(ctx, monthly.ID)
	if err != nil {
		return nil, err
	}
	group, err := propertyNode(groups, "房地产")
	if err != nil {
		return nil, err
	}
	catalogs, err := c.tree(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	result := &PropertyResult{Observations: []PropertyObservation{}, Notes: []string{
		"全国房地产开发企业口径；销售为新建商品房总量，包含住宅、办公楼、商业营业用房等，不是仅住宅或二手房。",
		"各项为年初至所选月份的累计值及官方累计同比，未季调；销售额为名义合同金额。1—2月合并发布，不拆算单月。",
		"累计同比按官方可比口径计算，不能用当前累计值与去年发布值直接相除替代；企业统计范围、执法检查和历史数据修订可能改变可比基数。",
		"当前发布库系列名称为新建商品房，内部原始名称仍含商品房；仅使用同一当前发布库系列，不与其他版本拼接。",
		"销售目录注明2005年8月以前为实际销售统计口径，2005年8月以后包含期房和现房；过渡月及跨口径比较应谨慎。",
		"空值表示发布库未提供，不补零；接口不提供逐条发布时间或修订批次，published_at 为 null，fetched_at 仅表示采集时间。",
	}}
	for _, component := range []string{"sales-area", "sales-value", "starts-area"} {
		if req.Component != "" && req.Component != "all" && req.Component != component {
			continue
		}
		spec := propertySpecs[component]
		catalog, err := propertyNode(catalogs, spec.catalog)
		if err != nil {
			return nil, err
		}
		selected, err := c.propertyMetadata(ctx, catalog, spec)
		if err != nil {
			return nil, err
		}
		body := map[string]any{"cid": catalog.ID, "rootId": group.ID, "indicatorIds": []string{selected[0].ID, selected[1].ID}, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{req.Begin + "MM-" + req.End + "MM"}, "showType": 1}
		var response propertyValuesResponse
		fetched, err := c.requestValidated(ctx, "/stream/esData", nil, body, &response, func(raw []byte) error {
			var candidate propertyValuesResponse
			if err := json.Unmarshal(raw, &candidate); err != nil {
				return fmt.Errorf("解析房地产数据响应失败: %w", err)
			}
			if !candidate.Success {
				return fmt.Errorf("房地产数据响应缺少成功标记或查询失败")
			}
			_, err := parseProperty(candidate.Data, req, component, catalog, selected, time.Time{})
			return err
		})
		if err != nil {
			return nil, err
		}
		items, err := parseProperty(response.Data, req, component, catalog, selected, fetched)
		if err != nil {
			return nil, err
		}
		result.Observations = append(result.Observations, items...)
	}
	sort.SliceStable(result.Observations, func(i, j int) bool {
		return result.Observations[i].Period < result.Observations[j].Period
	})
	return result, nil
}

func propertyNode(nodes []node, name string) (node, error) {
	var selected node
	for _, n := range nodes {
		if compact(n.Name) != name {
			continue
		}
		if selected.ID != "" || strings.TrimSpace(n.ID) == "" {
			return node{}, fmt.Errorf("房地产目录重复或缺少 ID: %s", name)
		}
		selected = n
	}
	if selected.ID == "" {
		return node{}, fmt.Errorf("国家统计局目录缺少 %s，可能已调整接口", name)
	}
	return selected, nil
}

func (c *Client) propertyMetadata(ctx context.Context, catalog node, spec propertySpec) ([2]propertySeries, error) {
	var metadata propertyMetadataResponse
	_, err := c.requestValidated(ctx, "/new/queryIndicatorsByCid", url.Values{"cid": {catalog.ID}, "pageSize": {"1000"}}, nil, &metadata, func(raw []byte) error {
		var candidate propertyMetadataResponse
		if err := json.Unmarshal(raw, &candidate); err != nil {
			return fmt.Errorf("解析房地产指标响应失败: %w", err)
		}
		_, err := selectPropertySeries(candidate, catalog, spec)
		return err
	})
	if err != nil {
		return [2]propertySeries{}, err
	}
	return selectPropertySeries(metadata, catalog, spec)
}

// selectPropertySeries 同时用于缓存及网络响应校验，不修改查询状态。
func selectPropertySeries(metadata propertyMetadataResponse, catalog node, spec propertySpec) ([2]propertySeries, error) {
	var selected [2]propertySeries
	if !metadata.Success {
		return selected, fmt.Errorf("房地产指标响应缺少成功标记或查询失败")
	}
	if metadata.Data.Total != len(metadata.Data.List) {
		return selected, fmt.Errorf("房地产指标列表不完整: %s", spec.catalog)
	}
	for i, name := range []string{spec.amountName, spec.growthName} {
		for _, s := range metadata.Data.List {
			if compact(s.Name) != name {
				continue
			}
			if selected[i].ID != "" {
				return selected, fmt.Errorf("房地产指标重复: %s", name)
			}
			if strings.TrimSpace(s.ID) == "" || s.CatalogID != catalog.ID {
				return selected, fmt.Errorf("房地产指标 ID 或目录不匹配: %s", name)
			}
			selected[i] = s
		}
		unit := spec.unit
		if i == 1 {
			unit = "%"
		}
		if selected[i].ID == "" || selected[i].Unit != unit {
			return selected, fmt.Errorf("房地产缺少指标或单位已变更: %s", name)
		}
	}
	if selected[0].ID == selected[1].ID {
		return selected, fmt.Errorf("房地产累计值和累计同比指标 ID 重复")
	}
	return selected, nil
}

func parseProperty(rows []valueRow, req *PropertyRequest, component string, catalog node, selected [2]propertySeries, fetched time.Time) ([]PropertyObservation, error) {
	items := make([]PropertyObservation, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		month := strings.TrimSuffix(row.Code, "MM")
		if len(month) != 6 {
			return nil, fmt.Errorf("无效的房地产月份: %s", row.Code)
		}
		if _, err := time.Parse("200601", month); err != nil {
			return nil, fmt.Errorf("无效的房地产月份: %s", row.Code)
		}
		if month < req.Begin || month > req.End {
			continue
		}
		if seen[month] {
			return nil, fmt.Errorf("房地产返回重复月份: %s", month)
		}
		seen[month] = true
		item := PropertyObservation{Region: "CN", Component: component, Period: month, YTDPeriod: month[:4] + "01-" + month, AmountUnit: propertySpecs[component].unit, GrowthUnit: "%", PriceBasis: "not_applicable", CatalogID: catalog.ID, CatalogName: catalog.Name, AmountSeriesID: selected[0].ID, GrowthSeriesID: selected[1].ID, AmountSeriesName: selected[0].Name, GrowthSeriesName: selected[1].Name, OriginalMetricName: selected[0].OriginalName, CoverageStart: selected[0].CoverageStart, ScopeVersion: "real_estate_developers", Definition: selected[0].Definition, Annotation: selected[0].Annotation, Source: baseURL + "/stream/esData", FetchedAt: fetched}
		if component == "sales-value" {
			item.PriceBasis = "nominal"
		}
		if component != "starts-area" {
			switch {
			case month < "200508":
				item.ScopeVersion = "before_200508_actual_sales"
				item.Note = "历史实际销售口径，与2005年8月以后的期房和现房合计口径不同"
			case month == "200508":
				item.ScopeVersion = "200508_transition_unspecified"
				item.Note = "口径过渡月，目录未明确8月本身归属，不跨口径比较"
			default:
				item.ScopeVersion = "after_200508_presales_and_completed_sales"
			}
		}
		fields := []**float64{&item.YTDAmount, &item.YTDGrowth}
		for i, s := range selected {
			found := false
			for _, v := range row.Values {
				if v.ID != s.ID || v.Area != "000000000000" {
					continue
				}
				if found {
					return nil, fmt.Errorf("房地产返回重复全国观测: %s %s", month, s.Name)
				}
				found = true
				n, err := parseNumber(v.Value)
				if err != nil {
					return nil, fmt.Errorf("房地产 %s %s: %w", month, s.Name, err)
				}
				if i == 0 && n != nil && *n < 0 {
					return nil, fmt.Errorf("房地产累计值不能为负: %s %s", month, s.Name)
				}
				*fields[i] = n
			}
		}
		if month[4:] == "02" && (item.YTDAmount != nil || item.YTDGrowth != nil) {
			item.Note = strings.TrimSpace(item.Note + " 1—2月合并累计，不是2月单月值")
		}
		if item.YTDAmount == nil && item.YTDGrowth == nil {
			item.Note = strings.TrimSpace(item.Note + " 发布库未提供数值")
			if month[4:] == "01" {
				item.Note += "；1月不拆出1—2月合并数据"
			}
		}
		items = append(items, item)
	}
	return items, nil
}
