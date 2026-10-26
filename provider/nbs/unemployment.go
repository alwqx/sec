package nbs

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// UnemploymentRequest 查询全国城镇调查失业率；AgeGroups 同时查询四个年龄组。
type UnemploymentRequest struct {
	Begin, End string
	AgeGroups  bool
}

func (r UnemploymentRequest) Validate() error { return validateMonths(r.Begin, r.End) }

// UnemploymentObservation 保留失业率水平和口径版本；SeriesKey 是可比序列标识。
type UnemploymentObservation struct {
	Region             string    `json:"region"`
	Period             string    `json:"period"`
	Indicator          string    `json:"indicator"`
	Rate               *float64  `json:"rate_percent"`
	Unit               string    `json:"unit"`
	SeasonallyAdjusted bool      `json:"seasonally_adjusted"`
	DefinitionVersion  string    `json:"definition_version"`
	SeriesKey          string    `json:"series_key"`
	StudentScope       string    `json:"student_scope"`
	CatalogID          string    `json:"catalog_id"`
	SeriesID           string    `json:"series_id"`
	SeriesName         string    `json:"series_name"`
	Source             string    `json:"source"`
	FetchedAt          time.Time `json:"fetched_at"`
	Note               string    `json:"note,omitempty"`
}

var unemploymentComponents = []struct{ key, name string }{
	{"urban-surveyed", "全国城镇调查失业率(%)"},
	{"age-16-24", "全国城镇16—24岁劳动力失业率(%)"},
	{"age-25-29", "全国城镇25—29岁劳动力失业率(%)"},
	{"age-30-59", "全国城镇30—59岁劳动力失业率(%)"},
	{"age-25-59", "全国城镇25—59岁劳动力失业率(%)"},
}

// Unemployment 动态发现失业率系列，不混入大城市、户籍分组或工作时间指标。
func (c *Client) Unemployment(ctx context.Context, req UnemploymentRequest) ([]UnemploymentObservation, error) {
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
	group, err := findNode(groups, "城镇调查失业率")
	if err != nil {
		return nil, err
	}
	catalogs, err := c.tree(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	catalog, err := findNode(catalogs, "城镇调查失业率")
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
		return nil, fmt.Errorf("失业率指标列表不完整")
	}
	count := 1
	if req.AgeGroups {
		count = len(unemploymentComponents)
	}
	selected := make([]series, count)
	ids := make([]string, count)
	seen := map[string]bool{}
	for i, component := range unemploymentComponents[:count] {
		for _, s := range metadata.Data.List {
			if compact(s.Name) != component.name {
				continue
			}
			if selected[i].ID != "" {
				return nil, fmt.Errorf("失业率指标重复: %s", component.name)
			}
			selected[i] = s
		}
		if selected[i].ID == "" {
			return nil, fmt.Errorf("缺少失业率指标: %s", component.name)
		}
		if seen[selected[i].ID] {
			return nil, fmt.Errorf("失业率指标 ID 重复")
		}
		seen[selected[i].ID] = true
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
	return parseUnemployment(response.Data, req, catalog.ID, selected, fetched)
}

func parseUnemployment(rows []valueRow, req UnemploymentRequest, catalog string, selected []series, fetched time.Time) ([]UnemploymentObservation, error) {
	count := 1
	if req.AgeGroups {
		count = len(unemploymentComponents)
	}
	if len(selected) != count {
		return nil, fmt.Errorf("失业率指标元数据不完整")
	}
	var items []UnemploymentObservation
	seen := map[string]bool{}
	for _, row := range rows {
		month := strings.TrimSuffix(row.Code, "MM")
		if _, err := time.Parse("200601", month); err != nil || len(month) != 6 {
			return nil, fmt.Errorf("无效的失业率月份: %s", row.Code)
		}
		if month < req.Begin || month > req.End {
			continue
		}
		if seen[month] {
			return nil, fmt.Errorf("失业率返回重复月份: %s", month)
		}
		seen[month] = true
		for i, s := range selected {
			item := UnemploymentObservation{Region: "CN", Period: month, Indicator: unemploymentComponents[i].key, Unit: "%", SeasonallyAdjusted: false, CatalogID: catalog, SeriesID: s.ID, SeriesName: s.Name, Source: baseURL + "/stream/esData", FetchedAt: fetched}
			found := false
			for _, v := range row.Values {
				if v.ID != s.ID || v.Area != "000000000000" {
					continue
				}
				if found {
					return nil, fmt.Errorf("失业率返回重复观测值: %s %s", month, s.Name)
				}
				found = true
				n, err := parseNumber(v.Value)
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", month, s.Name, err)
				}
				if n != nil && (*n < 0 || *n > 100) {
					return nil, fmt.Errorf("失业率超出 0—100 范围: %s", month)
				}
				item.Rate = n
			}
			setUnemploymentDefinition(&item)
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Period < items[j].Period })
	return items, nil
}

func setUnemploymentDefinition(item *UnemploymentObservation) {
	switch item.Indicator {
	case "urban-surveyed":
		item.DefinitionVersion = "national-surveyed"
		item.StudentScope = "included-if-in-labour-force"
	case "age-16-24":
		if item.Period <= "202306" {
			item.DefinitionVersion = "legacy-including-students"
			item.StudentScope = "included-if-in-labour-force"
			item.Note = "旧口径含在校生，不能与2023年12月起新口径直接比较"
		} else if item.Period < "202312" {
			item.DefinitionVersion = "publication-paused-202307-202311"
			item.StudentScope = "not-applicable"
			item.Note = "分年龄组暂停发布期，不插值或拼接"
		} else {
			item.DefinitionVersion = "excluding-students-since-202312"
			item.StudentScope = "excluded"
			item.Note = "不含在校生；与旧口径不直接可比"
		}
	case "age-25-29", "age-30-59":
		item.DefinitionVersion = "excluding-students-since-202312"
		item.StudentScope = "excluded"
		if item.Period < "202312" {
			item.Note = "该新分组口径从2023年12月起发布，历史缺口不补值"
		} else {
			item.Note = "不含在校生"
		}
	case "age-25-59":
		item.DefinitionVersion = "legacy-including-students"
		item.StudentScope = "included-if-in-labour-force"
		item.Note = "旧25—59岁分组，不能与新25—29岁或30—59岁分组拼接"
	}
	item.SeriesKey = item.Indicator + ":" + item.DefinitionVersion
}
