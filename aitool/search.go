package aitool

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
)

// maxSearchResults 是 search_security 最多返回的条数，新浪搜索一次可能返回 50 条。
const maxSearchResults = 10

const searchSchema = `{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "代码、名称或拼音，如 600519、贵州茅台、gzmt、腾讯、apple、00700",
      "minLength": 1
    }
  },
  "required": ["query"],
  "additionalProperties": false
}`

// SearchTool 返回 search_security：按代码、名称或拼音搜索证券。
func SearchTool() Tool {
	return Tool{
		Name: "search_security",
		Description: "按代码、名称或拼音搜索 A 股、港股、美股、指数和基金，返回 ex_code、名称、市场和类型，最多 10 条，按相关度排序，只含在市证券。" +
			"不确定代码时先用它，再把 ex_code 传给其他工具。结果有多条且看不出用户指的是哪一只时，向用户确认，不要自己挑。" +
			"只返回代码和名称，不含行情。",
		InputSchema: json.RawMessage(searchSchema),
		Call:        callSearch,
	}
}

type searchInput struct {
	Query string `json:"query"`
}

// searchItem 是一条搜索结果。
type searchItem struct {
	ExCode string `json:"ex_code"`
	Name   string `json:"name"`
	// Market 是 sse、szse、bse、hk、us 或 fund，A 股和港股与 types 的交易所常量一致。
	// 新浪不区分美股交易所，美股一律是 us；基金不区分场内场外，ETF 也是 fund。
	Market string `json:"market"`
	// Type 是 stock、index 或 fund。指数由代码判断，新浪把它和股票归为一类。
	Type string `json:"type"`
}

type searchData struct {
	Query   string       `json:"query"`
	Results []searchItem `json:"results"`
	Total   int          `json:"total"` // 新浪返回的在市条数，接口最多约 50 条，不是真实匹配总数；大于 len(results) 时结果被截短
}

func callSearch(ctx context.Context, input json.RawMessage) (*Result, error) {
	var in searchInput
	if err := Decode(input, &in); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, Inputf("query 不能为空，请给出证券代码、名称或拼音")
	}

	// 用 Search 而不是 Resolve：搜索失败时不按代码猜一个结果。
	secs, err := sina.Search(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("搜索证券失败：%w", err)
	}

	data := searchData{Query: query, Results: []searchItem{}, Total: len(secs)}
	result := NewResult("search_security", "新浪财经搜索", &data)
	hkIndex := false
	for _, sec := range secs {
		if len(data.Results) == maxSearchResults {
			break
		}
		item := newSearchItem(sec)
		hkIndex = hkIndex || item.Market == types.ExChangeHKex && item.Type == "index"
		data.Results = append(data.Results, item)
	}
	if hkIndex {
		result.Notes = append(result.Notes, "get_quote 目前查不到港股指数（如 HKhsi）的行情")
	}
	switch {
	case len(secs) == 0:
		result.Notes = append(result.Notes, "没有找到匹配 "+query+" 的在市证券，可以换成名称、拼音或带市场前缀的代码（如 SH600519、HK00700、$AAPL）重试")
	case len(secs) > maxSearchResults:
		result.Notes = append(result.Notes, fmt.Sprintf("新浪返回 %d 条结果（接口最多约 50 条，不是匹配总数），只返回最相关的前 %d 条，可以换更具体的关键字", len(secs), maxSearchResults))
	}
	return result, nil
}

// indexCode 匹配指数代码：沪市 000 开头、深市 399 开头、北交所 899 开头，港股指数是字母代码如 HKhsi（新浪原样返回小写）。
var indexCode = regexp.MustCompile(`^(SH000\d{3}|SZ399\d{3}|BJ899\d{3}|HK(?i:[a-z][a-z0-9\-]*))$`)

func newSearchItem(sec *sina.BasicSecurity) searchItem {
	item := searchItem{ExCode: sec.ExCode, Name: sec.Name, Type: string(sec.SecurityType)}
	switch {
	case sec.SecurityType == types.SecurityTypeFund:
		item.Market = "fund"
	case strings.HasPrefix(sec.ExCode, "$"):
		item.Market = "us"
	case strings.HasPrefix(sec.ExCode, "HK"):
		item.Market = types.ExChangeHKex
	case strings.HasPrefix(sec.ExCode, "SH"):
		item.Market = types.ExChangeSse
	case strings.HasPrefix(sec.ExCode, "SZ"):
		item.Market = types.ExChangeSzse
	case strings.HasPrefix(sec.ExCode, "BJ"):
		item.Market = types.ExChangeBse
	}
	if item.Type == string(types.SecurityTypeStock) && indexCode.MatchString(sec.ExCode) {
		item.Type = "index"
	}
	return item
}
