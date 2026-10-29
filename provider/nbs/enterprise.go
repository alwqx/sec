package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// EnterpriseEarliestMonth 是本轮已核验且支持的最早查询月份。
const EnterpriseEarliestMonth = "199901"

// EnterpriseRequest 查询全国工业企业经营指标，月份为 YYYYMM；空 Metric 与 all 相同。
type EnterpriseRequest struct {
	Begin  string
	End    string
	Metric string
}

func (r *EnterpriseRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("工业企业查询参数不能为空")
	}
	if err := validateMonths(r.Begin, r.End); err != nil {
		return err
	}
	if r.Begin < EnterpriseEarliestMonth {
		return fmt.Errorf("工业企业查询已核验范围从 199901 开始；更早口径及数据尚未核验")
	}
	if r.Metric != "" && r.Metric != "all" {
		if _, ok := enterpriseSpecs[r.Metric]; !ok {
			return fmt.Errorf("工业企业指标必须是 all、profit、inventory、finished-goods 或 receivables")
		}
	}
	return nil
}

// EnterpriseSeries 保留官方元数据，不把空的覆盖时间或历史版本推断为已知信息。
type EnterpriseSeries struct {
	ID            string  `json:"_id"`
	Name          string  `json:"i_showname"`
	CatalogID     string  `json:"catalogid"`
	Unit          string  `json:"du_name"`
	PeriodCode    string  `json:"dp"`
	OriginalName  string  `json:"ek_name"`
	Definition    string  `json:"i_mark"`
	Annotation    string  `json:"i_annotation"`
	CoverageStart *string `json:"daterange_sdate"`
	CoverageEnd   *string `json:"daterange_edate"`
}

// EnterpriseMetric 将官方累计流量和月末存量分别描述，产成品不替代全部存货。
type EnterpriseMetric struct {
	Metric                string           `json:"metric"`
	Name                  string           `json:"name"`
	Measurement           string           `json:"measurement"`
	VerifiedFirstNonempty string           `json:"verified_first_nonempty"`
	ValueSeries           EnterpriseSeries `json:"value_series"`
	GrowthSeries          EnterpriseSeries `json:"growth_series"`
}

// EnterpriseObservation 的 Value 为亿元；利润是累计流量，其余为期末账面余额。
// YoY 始终使用官方同比；不由累计增速相减或与上年已发布值相除推算。
type EnterpriseObservation struct {
	Metric              string     `json:"metric"`
	Period              string     `json:"period"`
	Measurement         string     `json:"measurement"`
	YTDPeriod           *string    `json:"ytd_period"`
	AsOfDate            *string    `json:"as_of_date"`
	Value               *float64   `json:"value"`
	YoY                 *float64   `json:"yoy_percent"`
	YoYBasis            string     `json:"yoy_basis"`
	ScopeStandard       string     `json:"scope_standard"`
	SurveyScope         string     `json:"survey_scope"`
	ComparisonScopeYear string     `json:"comparison_scope_year"`
	SurveyFrequency     string     `json:"survey_frequency"`
	AccountingScope     string     `json:"accounting_scope,omitempty"`
	Status              string     `json:"status"`
	PublishedAt         *time.Time `json:"published_at"`
	Note                string     `json:"note"`
}

type EnterpriseResult struct {
	Region             string                  `json:"region"`
	CatalogID          string                  `json:"catalog_id"`
	CatalogName        string                  `json:"catalog_name"`
	AmountUnit         string                  `json:"amount_unit"`
	GrowthUnit         string                  `json:"growth_unit"`
	PriceBasis         string                  `json:"price_basis"`
	SeasonallyAdjusted bool                    `json:"seasonally_adjusted"`
	Metrics            []EnterpriseMetric      `json:"metrics"`
	Observations       []EnterpriseObservation `json:"observations"`
	Source             string                  `json:"source"`
	FetchedAt          time.Time               `json:"fetched_at"`
	HistoricalVersion  *string                 `json:"historical_version"`
	Notes              []string                `json:"notes"`
}

type enterpriseSpec struct {
	name, valueName, growthName, measurement, valueCode, growthCode, first string
}

var enterpriseOrder = []string{"profit", "inventory", "finished-goods", "receivables"}
var enterpriseSpecs = map[string]enterpriseSpec{
	"profit":         {"利润总额", "利润总额累计值(亿元)", "利润总额累计增长(%)", "year_to_date_flow", "21", "12", "199902"},
	"inventory":      {"存货", "存货本月末(亿元)", "存货增减(%)", "month_end_stock", "1", "11", "201002"},
	"finished-goods": {"产成品存货", "产成品存货本月末(亿元)", "产成品存货增减(%)", "month_end_stock", "1", "11", "200102"},
	"receivables":    {"应收账款", "应收账款本月末(亿元)", "应收账款增减(%)", "month_end_stock", "1", "11", "200102"},
}

type enterpriseMetadataResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Total int                `json:"total"`
		List  []EnterpriseSeries `json:"list"`
	} `json:"data"`
}

type enterpriseValuesResponse struct {
	Success bool       `json:"success"`
	Data    []valueRow `json:"data"`
}

// Enterprise 动态发现工业总计目录，单次查询全部所选金额及同比，校验后才更新缓存。
func (c *Client) Enterprise(ctx context.Context, req *EnterpriseRequest) (*EnterpriseResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	roots, err := c.tree(ctx, "")
	if err != nil {
		return nil, err
	}
	monthly, err := enterpriseNode(roots, "月度数据")
	if err != nil {
		return nil, err
	}
	groups, err := c.tree(ctx, monthly.ID)
	if err != nil {
		return nil, err
	}
	group, err := enterpriseNode(groups, "工业")
	if err != nil {
		return nil, err
	}
	catalogs, err := c.tree(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	catalog, err := enterpriseNode(catalogs, "工业企业主要经济指标")
	if err != nil {
		return nil, err
	}
	var metadata enterpriseMetadataResponse
	_, err = c.requestValidated(ctx, "/new/queryIndicatorsByCid", url.Values{"cid": {catalog.ID}, "pageSize": {"1000"}}, nil, &metadata, func(raw []byte) error {
		var candidate enterpriseMetadataResponse
		if err := json.Unmarshal(raw, &candidate); err != nil {
			return fmt.Errorf("解析工业企业元数据失败: %w", err)
		}
		_, err := selectEnterpriseSeries(candidate, catalog.ID, req.Metric)
		return err
	})
	if err != nil {
		return nil, err
	}
	selected, err := selectEnterpriseSeries(metadata, catalog.ID, req.Metric)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(selected)*2)
	for _, metric := range selected {
		ids = append(ids, metric.ValueSeries.ID, metric.GrowthSeries.ID)
	}
	body := map[string]any{"cid": catalog.ID, "rootId": group.ID, "indicatorIds": ids, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{req.Begin + "MM-" + req.End + "MM"}, "showType": 1}
	var response enterpriseValuesResponse
	fetched, err := c.requestValidated(ctx, "/stream/esData", nil, body, &response, func(raw []byte) error {
		var candidate enterpriseValuesResponse
		if err := json.Unmarshal(raw, &candidate); err != nil {
			return fmt.Errorf("解析工业企业观测失败: %w", err)
		}
		_, err := parseEnterprise(candidate, req, selected)
		return err
	})
	if err != nil {
		return nil, err
	}
	items, err := parseEnterprise(response, req, selected)
	if err != nil {
		return nil, err
	}
	return &EnterpriseResult{Region: "CN", CatalogID: catalog.ID, CatalogName: catalog.Name, AmountUnit: "亿元", GrowthUnit: "%", PriceBasis: "nominal", Metrics: selected, Observations: items, Source: baseURL + "/stream/esData", FetchedAt: fetched, Notes: []string{
		"利润总额为年初累计流量；存货、产成品存货和应收账款为本月末余额，2月余额不是1—2月相加；所有金额未作价格或季节调整。",
		"官方同比按当年可比范围计算。每年企业进退规、执法清理、基数修正及重复统计剔除等影响调查范围；不能用上年已发布金额重算同比，也不相减累计同比推算单月。",
		"1998—2006年调查全部国有及主营业务收入500万元及以上非国有工业法人单位；2007—2010年为500万元及以上工业法人单位；2011年起为2000万元及以上。各年度名录并非固定样本。",
		"2007—2010年只在2、5、8、11月调查；2011年起月度全面调查，1月免报。缺失值保持null，不补零或插值。",
		"2019年改发应收票据及应收账款，2020年起恢复单列应收账款；不把两种系列拼接填缺口。",
		"发布库未提供逐条公布时间和历史修订版本，published_at/historical_version为null；fetched_at仅为抓取时间。库中空值不等于官方尚未发布，可能存在入库滞后。",
	}}, nil
}

func enterpriseNode(nodes []node, name string) (node, error) {
	var selected node
	for _, candidate := range nodes {
		if compact(candidate.Name) != name {
			continue
		}
		if selected.ID != "" {
			return node{}, fmt.Errorf("工业企业目录重复: %s", name)
		}
		if strings.TrimSpace(candidate.ID) == "" {
			return node{}, fmt.Errorf("工业企业目录 ID 为空: %s", name)
		}
		selected = candidate
	}
	if selected.ID == "" {
		return node{}, fmt.Errorf("未发现工业企业目录: %s", name)
	}
	return selected, nil
}

func selectEnterpriseSeries(metadata enterpriseMetadataResponse, catalogID, filter string) ([]EnterpriseMetric, error) {
	if !metadata.Success {
		return nil, fmt.Errorf("工业企业元数据缺少成功标记或查询失败")
	}
	if metadata.Data.Total != len(metadata.Data.List) {
		return nil, fmt.Errorf("工业企业指标列表不完整")
	}
	var selected []EnterpriseMetric
	seen := map[string]bool{}
	for _, key := range enterpriseOrder {
		if filter != "" && filter != "all" && filter != key {
			continue
		}
		spec := enterpriseSpecs[key]
		metric := EnterpriseMetric{Metric: key, Name: spec.name, Measurement: spec.measurement, VerifiedFirstNonempty: spec.first}
		for i, name := range []string{spec.valueName, spec.growthName} {
			var series EnterpriseSeries
			for _, candidate := range metadata.Data.List {
				if compact(candidate.Name) != name {
					continue
				}
				if series.ID != "" {
					return nil, fmt.Errorf("工业企业指标重复: %s", name)
				}
				if strings.TrimSpace(candidate.ID) == "" || candidate.CatalogID != catalogID {
					return nil, fmt.Errorf("工业企业指标 ID 或目录不匹配: %s", name)
				}
				series = candidate
			}
			unit, code := "亿元", spec.valueCode
			if i == 1 {
				unit, code = "%", spec.growthCode
			}
			if series.ID == "" || series.Unit != unit || series.PeriodCode != code || strings.TrimSpace(series.Definition) == "" {
				return nil, fmt.Errorf("工业企业缺少指标或单位、时点口径已变更: %s", name)
			}
			if seen[series.ID] {
				return nil, fmt.Errorf("工业企业指标 ID 重复: %s", name)
			}
			seen[series.ID] = true
			if i == 0 {
				metric.ValueSeries = series
			} else {
				metric.GrowthSeries = series
			}
		}
		selected = append(selected, metric)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("工业企业未选择有效指标")
	}
	return selected, nil
}

func parseEnterprise(response enterpriseValuesResponse, req *EnterpriseRequest, selected []EnterpriseMetric) ([]EnterpriseObservation, error) {
	if !response.Success {
		return nil, fmt.Errorf("工业企业观测缺少成功标记或查询失败")
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("工业企业指标元数据为空")
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	rows := map[string]valueRow{}
	for _, row := range response.Data {
		month := strings.TrimSuffix(row.Code, "MM")
		if len(month) != 6 || row.Code != month+"MM" {
			return nil, fmt.Errorf("无效的工业企业月份: %s", row.Code)
		}
		if _, err := time.Parse("200601", month); err != nil {
			return nil, fmt.Errorf("无效的工业企业月份: %s", row.Code)
		}
		if month < req.Begin || month > req.End {
			return nil, fmt.Errorf("工业企业返回范围外月份: %s", month)
		}
		if _, exists := rows[month]; exists {
			return nil, fmt.Errorf("工业企业返回重复月份: %s", month)
		}
		rows[month] = row
	}
	items := make([]EnterpriseObservation, 0, len(rows)*len(selected))
	for date := begin; !date.After(end); date = date.AddDate(0, 1, 0) {
		month := date.Format("200601")
		row, ok := rows[month]
		if !ok {
			return nil, fmt.Errorf("工业企业响应缺少月份行: %s；空值应由官方空单元格明确表示", month)
		}
		for _, metric := range selected {
			item := enterpriseObservation(month, metric)
			for i, s := range []EnterpriseSeries{metric.ValueSeries, metric.GrowthSeries} {
				found := false
				for _, v := range row.Values {
					if v.ID != s.ID || v.Area != "000000000000" {
						continue
					}
					if found {
						return nil, fmt.Errorf("工业企业返回重复全国观测: %s %s", month, s.Name)
					}
					found = true
					n, marked, err := enterpriseNumber(v.Value, metric.Metric == "profit" && i == 1)
					if err != nil {
						return nil, fmt.Errorf("工业企业 %s %s: %w", month, s.Name, err)
					}
					if i == 0 && metric.Metric != "profit" && n != nil && *n < 0 {
						return nil, fmt.Errorf("工业企业月末余额不能为负: %s %s", month, s.Name)
					}
					if i == 0 {
						item.Value = n
					} else {
						item.YoY = n
					}
					if marked {
						item.Note = "官方利润同比标注为不可计算，不按零处理"
					}
				}
				if !found {
					return nil, fmt.Errorf("工业企业响应缺少全国指标单元格: %s %s", month, s.Name)
				}
			}
			finishEnterpriseObservation(&item, metric)
			items = append(items, item)
		}
	}
	return items, nil
}
