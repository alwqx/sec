package nbs

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// PMIRequest 查询全国采购经理调查指数；月份为 YYYYMM。
type PMIRequest struct{ Begin, End string }

// Validate 检查月份范围。
func (r PMIRequest) Validate() error { return validateMonths(r.Begin, r.End) }

// PMIObservation 是季调后的扩散指数水平，不是同比/环比增速或价格相对指数。
type PMIObservation struct {
	Region             string    `json:"region"`
	Survey             string    `json:"survey"`
	Indicator          string    `json:"indicator"`
	Period             string    `json:"period"`
	Index              *float64  `json:"index"`
	Unit               string    `json:"unit"`
	SeasonallyAdjusted bool      `json:"seasonally_adjusted"`
	CatalogID          string    `json:"catalog_id"`
	SeriesID           string    `json:"series_id"`
	SeriesName         string    `json:"series_name"`
	Source             string    `json:"source"`
	FetchedAt          time.Time `json:"fetched_at"`
}

type pmiComponent struct{ key, name string }

type pmiSurvey struct {
	key, name  string
	components []pmiComponent
}

var pmiComponents = []pmiComponent{
	{"manufacturing-pmi", "制造业采购经理指数(%)"},
	{"production", "生产指数(%)"},
	{"new-orders", "新订单指数(%)"},
	{"new-export-orders", "新出口订单指数(%)"},
	{"employment", "从业人员指数(%)"},
}

var manufacturingSurvey = pmiSurvey{"manufacturing", "制造业采购经理指数", pmiComponents}

var nonManufacturingSurvey = pmiSurvey{"nonmanufacturing", "非制造业采购经理指数", []pmiComponent{
	{"business-activity", "非制造业商务活动指数(%)"},
	{"services-business-activity", "服务业商务活动指数(%)"},
	{"construction-business-activity", "建筑业商务活动指数(%)"},
	{"new-orders", "新订单指数(%)"},
	{"new-export-orders", "新出口订单指数(%)"},
	{"outstanding-orders", "在手订单指数(%)"},
	{"inventory", "存货指数(%)"},
	{"input-prices", "投入品价格指数(%)"},
	{"selling-prices", "销售价格指数(%)"},
	{"employment", "从业人员指数(%)"},
	{"supplier-delivery-times", "供应商配送时间指数(%)"},
	{"business-expectations", "业务活动预期指数(%)"},
}}

// ManufacturingPMI 动态发现官方目录，一次请求采集总指数及四个分项。
func (c *Client) ManufacturingPMI(ctx context.Context, req PMIRequest) ([]PMIObservation, error) {
	return c.queryPMI(ctx, req, manufacturingSurvey)
}

// NonManufacturingPMI 采集非制造业总指数、服务业、建筑业及九个全国分项。
func (c *Client) NonManufacturingPMI(ctx context.Context, req PMIRequest) ([]PMIObservation, error) {
	return c.queryPMI(ctx, req, nonManufacturingSurvey)
}

func (c *Client) queryPMI(ctx context.Context, req PMIRequest, survey pmiSurvey) ([]PMIObservation, error) {
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
	group, err := findNode(groups, "采购经理指数")
	if err != nil {
		return nil, err
	}
	catalogs, err := c.tree(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	catalog, err := findNode(catalogs, survey.name)
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
		return nil, fmt.Errorf("国家统计局%s指标列表不完整", survey.name)
	}
	selected := make([]series, len(survey.components))
	ids := make([]string, len(survey.components))
	for i, component := range survey.components {
		for _, s := range metadata.Data.List {
			if compact(s.Name) == component.name {
				if selected[i].ID != "" {
					return nil, fmt.Errorf("国家统计局%s指标重复: %s", survey.name, component.name)
				}
				selected[i] = s
			}
		}
		if selected[i].ID == "" {
			return nil, fmt.Errorf("国家统计局%s缺少指标: %s", survey.name, component.name)
		}
		ids[i] = selected[i].ID
	}
	body := map[string]any{
		"cid": catalog.ID, "rootId": group.ID, "indicatorIds": ids, "daCatalogId": "",
		"das": []map[string]string{{"text": "全国", "value": "000000000000"}},
		"dts": []string{req.Begin + "MM-" + req.End + "MM"}, "showType": 1,
	}
	var response struct {
		Data []valueRow `json:"data"`
	}
	fetched, err := c.request(ctx, "/stream/esData", nil, body, &response)
	if err != nil {
		return nil, err
	}
	return parseSurvey(response.Data, req, catalog.ID, selected, fetched, survey)
}

func parsePMI(rows []valueRow, req PMIRequest, catalogID string, selected []series, fetched time.Time) ([]PMIObservation, error) {
	return parseSurvey(rows, req, catalogID, selected, fetched, manufacturingSurvey)
}

func parseSurvey(rows []valueRow, req PMIRequest, catalogID string, selected []series, fetched time.Time, survey pmiSurvey) ([]PMIObservation, error) {
	if len(selected) != len(survey.components) {
		return nil, fmt.Errorf("%s指标元数据不完整", survey.name)
	}
	var result []PMIObservation
	seen := map[string]bool{}
	for _, row := range rows {
		month := strings.TrimSuffix(row.Code, "MM")
		if _, err := time.Parse("200601", month); err != nil || len(month) != 6 {
			return nil, fmt.Errorf("无效的国家统计局月份: %s", row.Code)
		}
		if month < req.Begin || month > req.End {
			continue
		}
		if seen[month] {
			return nil, fmt.Errorf("国家统计局%s返回重复月份: %s", survey.name, month)
		}
		seen[month] = true
		for i, s := range selected {
			item := PMIObservation{Region: "CN", Survey: survey.key, Indicator: survey.components[i].key, Period: month, Unit: "%", SeasonallyAdjusted: true, CatalogID: catalogID, SeriesID: s.ID, SeriesName: s.Name, Source: baseURL + "/stream/esData", FetchedAt: fetched}
			found := false
			for _, v := range row.Values {
				if v.ID != s.ID || v.Area != "000000000000" {
					continue
				}
				if found {
					return nil, fmt.Errorf("国家统计局%s返回重复观测值: %s %s", survey.name, month, s.Name)
				}
				found = true
				number, err := parseNumber(v.Value)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", month, s.Name, err)
				}
				if number != nil && (*number < 0 || *number > 100) {
					return nil, fmt.Errorf("%s超出 0—100 范围: %s %s", survey.name, month, s.Name)
				}
				item.Index = number
			}
			result = append(result, item)
		}
	}
	// 稳定排序保留每个月各调查分项的定义顺序。
	sort.SliceStable(result, func(i, j int) bool { return result[i].Period < result[j].Period })
	return result, nil
}
