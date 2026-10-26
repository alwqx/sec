package nbs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PriceRequest 查询全国月度价格数据。月份使用 YYYYMM；指标为 cpi、core-cpi、ppi。
type PriceRequest struct{ Indicator, Begin, End string }

// Observation 保留官方原始指数与衍生涨跌幅。空值不等于零；不推断公布时间。
type Observation struct {
	Region     string    `json:"region"`
	Indicator  string    `json:"indicator"`
	Period     string    `json:"period"`
	Basis      string    `json:"basis"`
	Index      *float64  `json:"index"`
	Change     *float64  `json:"change_percent"`
	CatalogID  string    `json:"catalog_id"`
	SeriesID   string    `json:"series_id"`
	SeriesName string    `json:"series_name"`
	Source     string    `json:"source"`
	FetchedAt  time.Time `json:"fetched_at"`
	Note       string    `json:"note,omitempty"`
}

type node struct {
	ID   string `json:"_id"`
	Name string `json:"_name"`
	Leaf bool   `json:"isLeaf"`
}

type series struct {
	ID   string `json:"_id"`
	Name string `json:"i_showname"`
}

type valueRow struct {
	Code   string `json:"code"`
	Values []struct {
		ID    string          `json:"_id"`
		Area  string          `json:"da"`
		Value json.RawMessage `json:"value"`
	} `json:"values"`
}

func (c *Client) tree(ctx context.Context, parent string) ([]node, error) {
	var result struct {
		Data []node `json:"data"`
	}
	params := url.Values{"code": {"1"}}
	if parent != "" {
		params.Set("pid", parent)
	}
	_, err := c.request(ctx, "/new/queryIndexTreeAsync", params, nil, &result)
	return result.Data, err
}

func compact(s string) string { return strings.Join(strings.Fields(s), "") }

func findNode(nodes []node, name string) (node, error) {
	for _, n := range nodes {
		if compact(n.Name) == name {
			return n, nil
		}
	}
	return node{}, fmt.Errorf("国家统计局目录缺少 %s，可能已调整接口", name)
}

// Validate 检查指标和月份范围，避免错误参数触发网络请求。
func (r PriceRequest) Validate() error {
	if r.Indicator != "cpi" && r.Indicator != "core-cpi" && r.Indicator != "ppi" {
		return fmt.Errorf("不支持的价格指标: %s", r.Indicator)
	}
	for _, month := range []string{r.Begin, r.End} {
		if len(month) != 6 {
			return fmt.Errorf("月份必须使用 YYYYMM 格式: %s", month)
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

var partitionPattern = regexp.MustCompile(`\((\d{4})?-(\d{4})?\)$`)

func overlaps(name, begin, end string) bool {
	m := partitionPattern.FindStringSubmatch(compact(name))
	if m == nil {
		return true
	}
	return (m[1] == "" || m[1]+"01" <= end) && (m[2] == "" || m[2]+"12" >= begin)
}

// Prices 从新版发布库发现目录和指标，分别采集同比、环比指数，不使用旧版固定编码。
func (c *Client) Prices(ctx context.Context, req PriceRequest) ([]Observation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	root, err := c.tree(ctx, "")
	if err != nil {
		return nil, err
	}
	monthly, err := findNode(root, "月度数据")
	if err != nil {
		return nil, err
	}
	groups, err := c.tree(ctx, monthly.ID)
	if err != nil {
		return nil, err
	}
	prices, err := findNode(groups, "价格指数")
	if err != nil {
		return nil, err
	}
	groups, err = c.tree(ctx, prices.ID)
	if err != nil {
		return nil, err
	}
	var result []Observation
	for _, basis := range []string{"上年同月=100", "上月=100"} {
		var catalogs []node
		if req.Indicator == "ppi" {
			group, e := findNode(groups, "工业生产者出厂价格分类指数")
			if e != nil {
				return nil, e
			}
			children, e := c.tree(ctx, group.ID)
			if e != nil {
				return nil, e
			}
			catalog, e := findNode(children, "工业生产者出厂价格指数("+basis+")")
			if e != nil {
				return nil, e
			}
			catalogs = []node{catalog}
		} else {
			group, e := findNode(groups, "居民消费价格分类指数("+basis+")")
			if e != nil {
				return nil, e
			}
			children, e := c.tree(ctx, group.ID)
			if e != nil {
				return nil, e
			}
			for _, n := range children {
				if strings.HasPrefix(compact(n.Name), "全国居民消费价格分类指数("+basis+")") && n.Leaf && overlaps(n.Name, req.Begin, req.End) {
					catalogs = append(catalogs, n)
				}
			}
			if len(catalogs) == 0 {
				return nil, fmt.Errorf("未找到全国 CPI %s 历史目录", basis)
			}
		}
		for _, catalog := range catalogs {
			// 总指数的底层序列可能跨目录共享，按目录区间裁剪，避免重复或混用分项口径。
			partitionReq := req
			if bounds := partitionPattern.FindStringSubmatch(compact(catalog.Name)); bounds != nil {
				if bounds[1] != "" && bounds[1]+"01" > partitionReq.Begin {
					partitionReq.Begin = bounds[1] + "01"
				}
				if bounds[2] != "" && bounds[2]+"12" < partitionReq.End {
					partitionReq.End = bounds[2] + "12"
				}
			}
			observations, e := c.queryCatalog(ctx, partitionReq, basis, prices.ID, catalog)
			if e != nil {
				return nil, e
			}
			result = append(result, observations...)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Period == result[j].Period {
			return result[i].Basis < result[j].Basis
		}
		return result[i].Period < result[j].Period
	})
	seen := map[string]bool{}
	for _, item := range result {
		key := item.Period + item.Basis
		if seen[key] {
			return nil, fmt.Errorf("国家统计局历史分段存在重复月份: %s %s", item.Period, item.Basis)
		}
		seen[key] = true
	}
	return result, nil
}

func (c *Client) queryCatalog(ctx context.Context, req PriceRequest, basis, root string, catalog node) ([]Observation, error) {
	var indicators struct {
		Data struct {
			Total int      `json:"total"`
			List  []series `json:"list"`
		} `json:"data"`
	}
	metadataFetched, err := c.request(ctx, "/new/queryIndicatorsByCid", url.Values{"cid": {catalog.ID}, "pageSize": {"1000"}}, nil, &indicators)
	if err != nil {
		return nil, err
	}
	if indicators.Data.Total > len(indicators.Data.List) {
		return nil, fmt.Errorf("国家统计局指标列表不完整: %s", catalog.Name)
	}
	name := "居民消费价格指数"
	if req.Indicator == "core-cpi" {
		name = "不包括食品和能源居民消费价格指数"
	}
	if req.Indicator == "ppi" {
		name = "工业生产者出厂价格指数"
	}
	var selected *series
	for _, s := range indicators.Data.List {
		if compact(s.Name) == name+"("+basis+")" {
			copy := s
			selected = &copy
			break
		}
	}
	if selected == nil {
		if req.Indicator == "core-cpi" {
			// 较早的分类目录并未提供核心 CPI；保留缺口，不用非食品 CPI 替代。
			var missing []Observation
			begin, _ := time.Parse("200601", req.Begin)
			end, _ := time.Parse("200601", req.End)
			for date := begin; !date.After(end); date = date.AddDate(0, 1, 0) {
				missing = append(missing, Observation{Region: "CN", Indicator: req.Indicator, Period: date.Format("200601"), Basis: basis, CatalogID: catalog.ID, SeriesName: name + "(" + basis + ")", Source: baseURL + "/new/queryIndicatorsByCid", FetchedAt: metadataFetched, Note: "该历史目录未提供核心 CPI 指标"})
			}
			return missing, nil
		}
		return nil, fmt.Errorf("目录 %s 缺少 %s（%s）", catalog.Name, name, basis)
	}
	body := map[string]any{
		"cid": catalog.ID, "rootId": root, "indicatorIds": []string{selected.ID}, "daCatalogId": "",
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
	var result []Observation
	for _, row := range response.Data {
		month := strings.TrimSuffix(row.Code, "MM")
		if _, err := time.Parse("200601", month); err != nil || len(month) != 6 {
			return nil, fmt.Errorf("无效的国家统计局月份: %s", row.Code)
		}
		if month < req.Begin || month > req.End {
			continue
		}
		item := Observation{Region: "CN", Indicator: req.Indicator, Period: month, Basis: basis, CatalogID: catalog.ID, SeriesID: selected.ID, SeriesName: selected.Name, Source: baseURL + "/stream/esData", FetchedAt: fetched}
		found := false
		for _, value := range row.Values {
			if value.ID != selected.ID || value.Area != "000000000000" {
				continue
			}
			if found {
				return nil, fmt.Errorf("国家统计局返回重复观测值: %s", month)
			}
			found = true
			number, err := parseNumber(value.Value)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", month, selected.Name, err)
			}
			item.Index = number
			if number != nil {
				change := math.Round((*number-100)*1e8) / 1e8
				item.Change = &change
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func parseNumber(raw json.RawMessage) (*float64, error) {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "\"") {
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		text = strings.TrimSpace(text)
	}
	switch text {
	case "", "null", "--", "—", "…", "...":
		return nil, nil
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return nil, fmt.Errorf("无效数值: %q", text)
	}
	return &number, nil
}
