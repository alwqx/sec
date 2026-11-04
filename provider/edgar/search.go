package edgar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// searchURL 是 EDGAR 全文检索（https://efts.sec.gov/LATEST/search-index）。SEC 网站的全文检索页面用的就是它，
// 但没有写进 API 文档；不带关键词、按类型和公司检索时返回每份申报的主文件，覆盖 2001 年以来的申报。
const searchURL = "https://efts.sec.gov/LATEST/search-index"

// searchPageSize 是全文检索每页的条数，maxSearchPages 防止异常数据导致一直翻页。
const (
	searchPageSize = 100
	maxSearchPages = 20
)

type searchResponse struct {
	Hits struct {
		Total struct {
			Value    int    `json:"value"`
			Relation string `json:"relation"`
		} `json:"total"`
		Hits []struct {
			ID     string `json:"_id"` // 申报编号:文件名
			Source struct {
				Accession    string `json:"adsh"`
				Form         string `json:"form"`
				FileDate     string `json:"file_date"`
				PeriodEnding string `json:"period_ending"`
				Description  string `json:"file_description"`
				Items        []string
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

// FilingSearchResult 保留已取得的结果，Truncated 表示未满足查询需求就遇到分页上限或错误。
type FilingSearchResult struct {
	Filings   []Filing
	Truncated bool
}

// SearchFilings 用全文检索按类型列出公司的申报，按申报日期从新到旧。用于申报很多的公司：
// 申报列表的历史分页按时间排列，按类型筛选要读几十页，全文检索一次就能按类型查到。
func (c *Client) SearchFilings(ctx context.Context, cik int, q FilingQuery) (*FilingSearchResult, error) {
	if len(q.Forms) == 0 {
		return nil, fmt.Errorf("全文检索需要指定申报类型")
	}
	// 全文检索按基础类型匹配，结果已包含修订版（10-K 也返回 10-K/A）；同时传 10-K 和 10-K/A 反而只返回修订版。
	requested := map[string]bool{}
	for _, form := range q.Forms {
		requested[strings.ToUpper(form)] = true
	}
	all := []string{}
	for _, form := range q.Forms {
		form = strings.ToUpper(form)
		if base, amended := strings.CutSuffix(form, "/A"); !amended || !requested[base] {
			all = append(all, form)
		}
	}
	seen := map[string]bool{}
	result := &FilingSearchResult{Filings: []Filing{}, Truncated: true}
	defer func() {
		sort.SliceStable(result.Filings, func(i, j int) bool { return result.Filings[i].FilingDate > result.Filings[j].FilingDate })
	}()
	for page := 0; page < maxSearchPages; page++ {
		query := url.Values{}
		query.Set("forms", strings.Join(all, ","))
		query.Set("ciks", fmt.Sprintf("%010d", cik))
		query.Set("from", strconv.Itoa(page*searchPageSize))
		query.Set("sort", "desc")
		if q.Begin != "" || q.End != "" {
			query.Set("dateRange", "custom")
			if q.Begin != "" {
				query.Set("startdt", q.Begin)
			}
			if q.End != "" {
				query.Set("enddt", q.End)
			}
		}
		body, err := c.get(ctx, searchURL+"?"+query.Encode())
		if err != nil {
			return result, err
		}
		var data searchResponse
		if err := json.Unmarshal(body, &data); err != nil {
			return result, fmt.Errorf("SEC 全文检索结果格式无效：%w", err)
		}
		for _, hit := range data.Hits.Hits {
			src := hit.Source
			if src.Accession == "" || seen[src.Accession] || !q.match(src.Form, src.FileDate) {
				continue
			}
			seen[src.Accession] = true
			folder := archivesURL + strconv.Itoa(cik) + "/" + strings.ReplaceAll(src.Accession, "-", "") + "/"
			f := Filing{Form: src.Form, FilingDate: src.FileDate, ReportDate: src.PeriodEnding, Accession: src.Accession, Items: strings.Join(src.Items, ",")}
			f.IndexURL = folder + src.Accession + "-index.htm"
			f.URL = f.IndexURL
			if _, name, ok := strings.Cut(hit.ID, ":"); ok && name != "" {
				f.URL = folder + name
			}
			f.Description = Describe(f.Form, f.Items)
			if f.Description == "" {
				f.Description = src.Description
			}
			result.Filings = append(result.Filings, f)
		}
		if len(data.Hits.Hits) == 0 ||
			(data.Hits.Total.Relation != "gte" && (page*searchPageSize+len(data.Hits.Hits)) >= data.Hits.Total.Value) ||
			(q.Want > 0 && len(result.Filings) >= q.Want) {
			result.Truncated = false
			break
		}
	}
	return result, nil
}
