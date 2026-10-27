package nbs

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GDPRequest 使用YYYYQ1至YYYYQ4的季度范围，包含端点。
type GDPRequest struct{ Begin, End string }

func (r *GDPRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("GDP查询请求不能为空")
	}
	for _, period := range []string{r.Begin, r.End} {
		if _, err := quarterOrdinal(period); err != nil {
			return err
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始季度不能晚于截至季度")
	}
	if r.Begin < "1992Q1" {
		return fmt.Errorf("GDP季度查询支持自1992Q1起，早期缺失值不补值")
	}
	return nil
}

func quarterOrdinal(period string) (int, error) {
	if len(period) != 6 || period[4] != 'Q' || period[5] < '1' || period[5] > '4' {
		return 0, fmt.Errorf("无效季度 %q，请使用YYYYQ1至YYYYQ4", period)
	}
	year, err := strconv.Atoi(period[:4])
	if err != nil || year < 1 || year > 9999 || fmt.Sprintf("%04d", year) != period[:4] {
		return 0, fmt.Errorf("无效季度: %s", period)
	}
	return year*4 + int(period[5]-'1'), nil
}

func quarterPeriod(ordinal int) string { return fmt.Sprintf("%04dQ%d", ordinal/4, ordinal%4+1) }

// GDPSeries 保存各源指标的口径、原始注释与抓取时间，不将抓取时间当作公布时间。
type GDPSeries struct {
	Key                string    `json:"key"`
	CatalogID          string    `json:"catalog_id"`
	SeriesID           string    `json:"series_id"`
	SeriesName         string    `json:"series_name"`
	Unit               string    `json:"unit"`
	Annotation         string    `json:"annotation"`
	SeasonallyAdjusted bool      `json:"seasonally_adjusted"`
	Source             string    `json:"source"`
	FetchedAt          time.Time `json:"fetched_at"`
}

type GDPObservation struct {
	Period       string   `json:"period"`
	RealValue    *float64 `json:"real_value"`
	RealYTDValue *float64 `json:"real_ytd_value"`
	YoYIndex     *float64 `json:"yoy_index"`
	YoY          *float64 `json:"yoy_percent"`
	YTDYoYIndex  *float64 `json:"ytd_yoy_index"`
	YTDYoY       *float64 `json:"ytd_yoy_percent"`
	QoQ          *float64 `json:"qoq_percent"`
	Status       string   `json:"status"`
}

type GDPResult struct {
	Region       string           `json:"region"`
	Frequency    string           `json:"frequency"`
	PriceBasis   string           `json:"price_basis"`
	Begin        string           `json:"begin"`
	End          string           `json:"end"`
	ValueUnit    string           `json:"value_unit"`
	GrowthUnit   string           `json:"growth_unit"`
	RealBaseYear *int             `json:"real_base_year"`
	DataVersion  string           `json:"data_version"`
	RevisionID   *string          `json:"revision_id"`
	PublishedAt  *time.Time       `json:"published_at"`
	Series       []GDPSeries      `json:"series"`
	Observations []GDPObservation `json:"observations"`
	Note         string           `json:"note"`
}

type gdpIndicator struct{ key, name, unit string }
type gdpCatalog struct {
	name       string
	indicators []gdpIndicator
}

var gdpCatalogs = []gdpCatalog{
	{"国内生产总值(不变价)", []gdpIndicator{{"real-value", "国内生产总值(不变价)当季值(亿元)", "亿元"}, {"real-ytd-value", "国内生产总值(不变价)累计值(亿元)", "亿元"}}},
	{"国内生产总值指数", []gdpIndicator{{"yoy-index", "国内生产总值指数(上年同期=100)当季值", "上年同期=100"}, {"ytd-yoy-index", "国内生产总值指数(上年同期=100)累计值", "上年同期=100"}}},
	{"国内生产总值环比增长速度", []gdpIndicator{{"qoq", "国内生产总值环比增长速度(%)", "%"}}},
}

// GDP 从官方季度发布库批量查询实际量、同比指数及季调环比。
func (c *Client) GDP(ctx context.Context, req *GDPRequest) (*GDPResult, error) {
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
	group, err := findNode(groups, "国民经济核算")
	if err != nil {
		return nil, err
	}
	catalogs, err := tree(group.ID)
	if err != nil {
		return nil, err
	}
	result := &GDPResult{Region: "CN", Frequency: "quarterly", PriceBasis: "constant-prices", Begin: req.Begin, End: req.End, ValueUnit: "亿元", GrowthUnit: "%", DataVersion: "latest-available", Series: []GDPSeries{}, Observations: []GDPObservation{}, Note: "同比与累计同比=官方上年同期指数−100；环比为官方季调实际增速，未年化。不变价金额按不同基期分段计算，不能跨基期直接比较或据此计算增速；逐期基期、历史修订版本和实际公布时间未由接口提供，相关字段为null。历史查询为抓取时可见版本，不能视为当时已知数据。缺失为null，不以现价金额代替实际量。"}
	begin, _ := quarterOrdinal(req.Begin)
	end, _ := quarterOrdinal(req.End)
	byPeriod := map[string]*GDPObservation{}
	for q := begin; q <= end; q++ {
		result.Observations = append(result.Observations, GDPObservation{Period: quarterPeriod(q)})
	}
	for i := range result.Observations {
		item := &result.Observations[i]
		byPeriod[item.Period] = item
	}
	for _, spec := range gdpCatalogs {
		catalog, err := findNode(catalogs, spec.name)
		if err != nil {
			return nil, err
		}
		var metadata struct {
			Data struct {
				Total int `json:"total"`
				List  []struct {
					series
					Annotation string `json:"i_annotation"`
				} `json:"list"`
			} `json:"data"`
		}
		_, err = c.request(ctx, "/new/queryIndicatorsByCid", url.Values{"cid": {catalog.ID}, "pageSize": {"1000"}}, nil, &metadata)
		if err != nil {
			return nil, err
		}
		if metadata.Data.Total > len(metadata.Data.List) {
			return nil, fmt.Errorf("GDP指标列表不完整: %s", spec.name)
		}
		selected := []GDPSeries{}
		ids := []string{}
		for _, indicator := range spec.indicators {
			var found *GDPSeries
			for _, s := range metadata.Data.List {
				if compact(s.Name) != indicator.name {
					continue
				}
				if found != nil || s.ID == "" {
					return nil, fmt.Errorf("GDP指标重复或标识为空: %s", indicator.name)
				}
				found = &GDPSeries{Key: indicator.key, CatalogID: catalog.ID, SeriesID: s.ID, SeriesName: s.Name, Unit: indicator.unit, Annotation: s.Annotation, SeasonallyAdjusted: indicator.key == "qoq", Source: baseURL + "/stream/esData"}
			}
			if found == nil {
				return nil, fmt.Errorf("GDP缺少指标: %s", indicator.name)
			}
			selected = append(selected, *found)
			ids = append(ids, found.SeriesID)
		}
		code := func(q string) string { return q[:4] + "0" + q[5:] + "SS" }
		body := map[string]any{"cid": catalog.ID, "rootId": group.ID, "indicatorIds": ids, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{code(req.Begin) + "-" + code(req.End)}, "showType": 1}
		var response struct {
			Data []valueRow `json:"data"`
		}
		fetched, err := c.request(ctx, "/stream/esData", nil, body, &response)
		if err != nil {
			return nil, err
		}
		if err := mergeGDP(response.Data, selected, byPeriod); err != nil {
			return nil, err
		}
		for _, s := range selected {
			s.FetchedAt = fetched
			result.Series = append(result.Series, s)
		}
	}
	for i := range result.Observations {
		item := &result.Observations[i]
		if item.YoYIndex != nil {
			n := math.Round((*item.YoYIndex-100)*1e8) / 1e8
			item.YoY = &n
		}
		if item.YTDYoYIndex != nil {
			n := math.Round((*item.YTDYoYIndex-100)*1e8) / 1e8
			item.YTDYoY = &n
		}
		item.Status = "complete"
		if item.RealValue == nil || item.RealYTDValue == nil || item.YoY == nil || item.YTDYoY == nil || item.QoQ == nil {
			item.Status = "missing-values"
		}
	}
	return result, nil
}

func mergeGDP(rows []valueRow, selected []GDPSeries, items map[string]*GDPObservation) error {
	seen := map[string]bool{}
	for _, row := range rows {
		if len(row.Code) != 8 || !strings.HasSuffix(row.Code, "SS") || row.Code[4] != '0' {
			return fmt.Errorf("无效的GDP季度编码: %s", row.Code)
		}
		period := row.Code[:4] + "Q" + row.Code[5:6]
		if _, err := quarterOrdinal(period); err != nil {
			return err
		}
		if seen[period] {
			return fmt.Errorf("GDP返回重复季度: %s", period)
		}
		seen[period] = true
		item := items[period]
		if item == nil {
			continue
		}
		for _, s := range selected {
			found := false
			for _, v := range row.Values {
				if v.ID != s.SeriesID || v.Area != "000000000000" {
					continue
				}
				if found {
					return fmt.Errorf("GDP返回重复观测值: %s %s", period, s.SeriesName)
				}
				found = true
				n, err := parseNumber(v.Value)
				if err != nil {
					return fmt.Errorf("%s %s: %w", period, s.SeriesName, err)
				}
				if n != nil && s.Key != "qoq" && *n < 0 {
					return fmt.Errorf("GDP实际量或指数为负: %s", period)
				}
				switch s.Key {
				case "real-value":
					item.RealValue = n
				case "real-ytd-value":
					item.RealYTDValue = n
				case "yoy-index":
					item.YoYIndex = n
				case "ytd-yoy-index":
					item.YTDYoYIndex = n
				case "qoq":
					item.QoQ = n
				}
			}
		}
	}
	return nil
}
