package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// HouseholdRequest 查询全国居民年初至本季末的累计收支，不拆分单季。
type HouseholdRequest struct{ Begin, End string }

func (r *HouseholdRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("居民收支查询请求不能为空")
	}
	for _, p := range []string{r.Begin, r.End} {
		if _, err := quarterOrdinal(p); err != nil {
			return err
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始季度不能晚于截至季度")
	}
	if r.Begin < "2013Q1" {
		return fmt.Errorf("全国居民收支支持自2013Q1起的城乡一体化住户调查口径")
	}
	now := time.Now().In(time.FixedZone("CST", 8*3600))
	if r.End > fmt.Sprintf("%04dQ%d", now.Year(), (int(now.Month())-1)/3+1) {
		return fmt.Errorf("截至季度不能晚于当前北京时间季度")
	}
	return nil
}

type HouseholdSeries struct {
	Key        string    `json:"key"`
	CatalogID  string    `json:"catalog_id"`
	SeriesID   string    `json:"series_id"`
	SeriesName string    `json:"series_name"`
	Unit       string    `json:"unit"`
	PriceBasis string    `json:"price_basis"`
	Annotation string    `json:"annotation"`
	Source     string    `json:"source"`
	FetchedAt  time.Time `json:"fetched_at"`
}

type HouseholdObservation struct {
	Period                string                     `json:"period"`
	IncomeYTD             *float64                   `json:"income_ytd"`
	IncomeRealYTDYoY      *float64                   `json:"income_real_ytd_yoy_percent"`
	ConsumptionYTD        *float64                   `json:"consumption_ytd"`
	ConsumptionRealYTDYoY *float64                   `json:"consumption_real_ytd_yoy_percent"`
	RawValues             map[string]json.RawMessage `json:"raw_values"`
	IncludesOwnProduction bool                       `json:"includes_own_production"`
	Status                string                     `json:"status"`
}

type HouseholdResult struct {
	Region             string                 `json:"region"`
	Frequency          string                 `json:"frequency"`
	Measurement        string                 `json:"measurement"`
	Population         string                 `json:"population"`
	SurveyMethod       string                 `json:"survey_method"`
	ValueUnit          string                 `json:"value_unit"`
	GrowthUnit         string                 `json:"growth_unit"`
	SeasonallyAdjusted bool                   `json:"seasonally_adjusted"`
	Annualized         bool                   `json:"annualized"`
	Begin              string                 `json:"begin"`
	End                string                 `json:"end"`
	DataVersion        string                 `json:"data_version"`
	PublishedAt        *time.Time             `json:"published_at"`
	RevisionID         *string                `json:"revision_id"`
	Series             []HouseholdSeries      `json:"series"`
	Observations       []HouseholdObservation `json:"observations"`
	Note               string                 `json:"note"`
}

type householdIndicator struct{ key, name, unit, priceBasis string }

var householdCatalogs = []struct {
	name       string
	indicators []householdIndicator
}{
	{"全国居民人均收入情况", []householdIndicator{
		{"income-ytd", "居民人均可支配收入累计值(元)", "元/人", "current-prices"},
		{"income-real-yoy", "居民人均可支配收入累计增长(%)", "%", "real-growth"},
	}},
	{"全国居民人均支出情况", []householdIndicator{
		{"consumption-ytd", "居民人均消费支出累计值(元)", "元/人", "current-prices"},
		{"consumption-real-yoy", "居民人均消费支出累计增长(%)", "%", "real-growth"},
	}},
}

// Household 通过官方目录解析指标标识；累计增长按目录注释解释为实际同比。
func (c *Client) Household(ctx context.Context, req *HouseholdRequest) (*HouseholdResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	tree := func(parent string) ([]node, error) {
		var response struct {
			Data []node `json:"data"`
		}
		params := url.Values{"code": {"2"}}
		if parent != "" {
			params.Set("pid", parent)
		}
		_, err := c.request(ctx, "/new/queryIndexTreeAsync", params, nil, &response)
		return response.Data, err
	}
	roots, err := tree("")
	if err != nil {
		return nil, err
	}
	quarterly, err := findNode(roots, "季度数据")
	if err != nil {
		return nil, err
	}
	groups, err := tree(quarterly.ID)
	if err != nil {
		return nil, err
	}
	group, err := findNode(groups, "人民生活")
	if err != nil {
		return nil, err
	}
	catalogs, err := tree(group.ID)
	if err != nil {
		return nil, err
	}
	result := &HouseholdResult{Region: "CN", Frequency: "quarterly", Measurement: "year-to-date-per-capita-flow", Population: "全国居民", SurveyMethod: "城乡一体化住户收支与生活状况调查", ValueUnit: "元/人", GrowthUnit: "%", Begin: req.Begin, End: req.End, DataVersion: "latest-available", Series: []HouseholdSeries{}, Observations: []HouseholdObservation{}, Note: "金额为现价人均累计值，增长为官方扣除价格因素的累计实际同比；未季调、未年化，不由金额推算实际增速。Q1/Q2/Q3分别为一季度/上半年/前三季度，Q4为全年；季度收支不含自产自用部分，年度收支包含，不以全年减前三季度构造可比单季，不计算累计环比或储蓄率。全国城乡一体化调查口径自2013年起，2013年实际增速缺失保留空值。当前可见修订版本，逐期公布时间和修订编号不可得时为null，抓取时间不能代替公布时间。"}
	begin, _ := quarterOrdinal(req.Begin)
	end, _ := quarterOrdinal(req.End)
	items := map[string]*HouseholdObservation{}
	for q := begin; q <= end; q++ {
		result.Observations = append(result.Observations, HouseholdObservation{Period: quarterPeriod(q), RawValues: map[string]json.RawMessage{}, IncludesOwnProduction: q%4 == 3})
	}
	for i := range result.Observations {
		items[result.Observations[i].Period] = &result.Observations[i]
	}
	for _, spec := range householdCatalogs {
		catalog, err := findNode(catalogs, spec.name)
		if err != nil {
			return nil, err
		}
		type metadataRow struct {
			series
			Annotation string `json:"i_annotation"`
		}
		var metadata struct {
			Data struct {
				Total int           `json:"total"`
				List  []metadataRow `json:"list"`
			} `json:"data"`
		}
		selected := []HouseholdSeries{}
		validateMetadata := func(raw []byte) error {
			metadata.Data.List = nil
			metadata.Data.Total = 0
			if err := json.Unmarshal(raw, &metadata); err != nil {
				return err
			}
			if metadata.Data.Total > len(metadata.Data.List) {
				return fmt.Errorf("居民收支指标列表不完整: %s", spec.name)
			}
			selected = nil
			used := map[string]bool{}
			for _, indicator := range spec.indicators {
				var found *HouseholdSeries
				for _, s := range metadata.Data.List {
					if compact(s.Name) != indicator.name {
						continue
					}
					if found != nil || s.ID == "" || used[s.ID] {
						return fmt.Errorf("居民收支指标重复或标识为空: %s", indicator.name)
					}
					if indicator.priceBasis == "real-growth" && !strings.Contains(s.Annotation, "扣除价格因素的实际增速") {
						return fmt.Errorf("居民收支增速口径注释不符: %s", s.Name)
					}
					found = &HouseholdSeries{Key: indicator.key, CatalogID: catalog.ID, SeriesID: s.ID, SeriesName: s.Name, Unit: indicator.unit, PriceBasis: indicator.priceBasis, Annotation: s.Annotation, Source: baseURL + "/stream/esData"}
				}
				if found == nil {
					return fmt.Errorf("居民收支缺少指标: %s", indicator.name)
				}
				used[found.SeriesID] = true
				selected = append(selected, *found)
			}
			return nil
		}
		_, err = c.requestValidated(ctx, "/new/queryIndicatorsByCid", url.Values{"cid": {catalog.ID}, "pageSize": {"1000"}}, nil, &metadata, validateMetadata)
		if err != nil {
			return nil, err
		}
		ids := []string{}
		for _, s := range selected {
			ids = append(ids, s.SeriesID)
		}
		code := func(q string) string { return q[:4] + "0" + q[5:] + "SS" }
		body := map[string]any{"cid": catalog.ID, "rootId": group.ID, "indicatorIds": ids, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{code(req.Begin) + "-" + code(req.End)}, "showType": 1}
		var response struct {
			Data []valueRow `json:"data"`
		}
		validateValues := func(raw []byte) error {
			response.Data = nil
			if err := json.Unmarshal(raw, &response); err != nil {
				return err
			}
			_, err := parseHousehold(response.Data, selected, req)
			return err
		}
		fetched, err := c.requestValidated(ctx, "/stream/esData", nil, body, &response, validateValues)
		if err != nil {
			return nil, err
		}
		values, err := parseHousehold(response.Data, selected, req)
		if err != nil {
			return nil, err
		}
		for period, fields := range values {
			item := items[period]
			for key, raw := range fields {
				n, _ := parseNumber(raw)
				item.RawValues[key] = raw
				switch key {
				case "income-ytd":
					item.IncomeYTD = n
				case "income-real-yoy":
					item.IncomeRealYTDYoY = n
				case "consumption-ytd":
					item.ConsumptionYTD = n
				case "consumption-real-yoy":
					item.ConsumptionRealYTDYoY = n
				}
			}
		}
		for _, s := range selected {
			s.FetchedAt = fetched
			result.Series = append(result.Series, s)
		}
	}
	for i := range result.Observations {
		item := &result.Observations[i]
		item.Status = "complete"
		if item.IncomeYTD == nil || item.IncomeRealYTDYoY == nil || item.ConsumptionYTD == nil || item.ConsumptionRealYTDYoY == nil {
			item.Status = "missing-values"
		}
	}
	return result, nil
}

func parseHousehold(rows []valueRow, selected []HouseholdSeries, req *HouseholdRequest) (map[string]map[string]json.RawMessage, error) {
	result := map[string]map[string]json.RawMessage{}
	for _, row := range rows {
		if len(row.Code) != 8 || row.Code[4] != '0' || !strings.HasSuffix(row.Code, "SS") {
			return nil, fmt.Errorf("无效居民收支季度编码: %s", row.Code)
		}
		period := row.Code[:4] + "Q" + row.Code[5:6]
		if _, err := quarterOrdinal(period); err != nil {
			return nil, err
		}
		if period < req.Begin || period > req.End {
			return nil, fmt.Errorf("居民收支返回范围外季度: %s", period)
		}
		if _, ok := result[period]; ok {
			return nil, fmt.Errorf("居民收支返回重复季度: %s", period)
		}
		fields := map[string]json.RawMessage{}
		for _, s := range selected {
			for _, v := range row.Values {
				if v.ID != s.SeriesID || v.Area != "000000000000" {
					continue
				}
				if _, ok := fields[s.Key]; ok {
					return nil, fmt.Errorf("居民收支返回重复值: %s %s", period, s.SeriesName)
				}
				n, err := parseNumber(v.Value)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", period, s.SeriesName, err)
				}
				if n != nil && s.PriceBasis == "current-prices" && *n < 0 {
					return nil, fmt.Errorf("居民收支金额为负: %s", period)
				}
				fields[s.Key] = v.Value
			}
		}
		result[period] = fields
	}
	return result, nil
}
