package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/valuation"
)

// 默认列出 10 条，最多 20 条：每条有标题和链接，约 200 字节。
const (
	defaultFilings = 10
	maxFilings     = 20
)

const filingsSchema = `{
  "type": "object",
  "properties": {
    "code": {
      "type": "string",
      "description": "一只 A 股或美股的代码或名称，最好用 search_security 返回的 ex_code，如 SH600519、$AAPL。不支持港股、基金和指数",
      "minLength": 1
    },
    "category": {
      "type": "string",
      "enum": ["annual", "halfyear", "q1", "q3"],
      "description": "只用于 A 股：annual 年报、halfyear 半年报、q1 一季报、q3 三季报。不传时列出全部公告"
    },
    "forms": {
      "type": "array",
      "items": {"type": "string", "minLength": 1},
      "description": "只用于美股：SEC 申报类型，如 [\"10-K\"]、[\"8-K\"]、[\"10-Q\",\"10-K\"]；10-K 也包含修订版 10-K/A。不传时列出 10-K、10-Q、8-K、20-F、6-K、40-F、DEF 14A、S-1、F-1"
    },
    "begin": {
      "type": "string",
      "description": "开始日期 YYYY-MM-DD，含当天，按公告日或申报日筛选"
    },
    "end": {
      "type": "string",
      "description": "结束日期 YYYY-MM-DD，含当天，默认今天"
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 20,
      "description": "最多返回几条，默认 10"
    }
  },
  "required": ["code"],
  "additionalProperties": false
}`

// FilingsTool 返回 list_filings：A 股的巨潮资讯公告和美股的 SEC 申报列表。
func FilingsTool() Tool {
	return Tool{
		Name: "list_filings",
		Description: "列出一只 A 股在巨潮资讯的公告，或一只美股在 SEC EDGAR 的申报，按日期从新到旧：日期、标题和原文链接，最多 20 条。" +
			"美股另带申报类型和报告期，A 股没有类型字段，可以看标题。只有标题和链接，不含正文。A 股可以用 category 只看定期报告，美股可以用 forms 只看某类申报（如 10-K、8-K），两者都可以用 begin、end 限定日期。" +
			"不支持港股、基金和指数。",
		InputSchema: json.RawMessage(filingsSchema),
		Call:        callFilings,
	}
}

type filingsInput struct {
	Code     string   `json:"code"`
	Category string   `json:"category"`
	Forms    []string `json:"forms"`
	Begin    string   `json:"begin"`
	End      string   `json:"end"`
	Limit    *int     `json:"limit"`
}

// filingItem 是一条公告或申报。
type filingItem struct {
	Date       string `json:"date"`                  // 公告日或申报日，北京时间（A 股）或美东时间（美股）
	Title      string `json:"title"`                 // A 股是公告标题，美股是申报类型的中文说明（8-K 带事项）
	Form       string `json:"form,omitempty"`        // 只有美股：SEC 申报类型，如 10-K
	ReportDate string `json:"report_date,omitempty"` // 只有美股：报告期末，8-K 等没有报告期时省略
	URL        string `json:"url"`
}

type filingsData struct {
	ExCode  string       `json:"ex_code"`
	Name    string       `json:"name"`
	Results []filingItem `json:"results"` // 从新到旧
	// Total 只有 A 股：巨潮资讯符合条件的公告总数，大于 len(results) 时只列了最近几条。
	Total *int `json:"total,omitempty"`
}

// queryAnnouncements 和 queryFilings 查询上游，测试时替换。
var (
	queryAnnouncements = func(ctx context.Context, code string, req *cninfo.QueryRequest) (*cninfo.QueryResponse, error) {
		// 不用 LookupOrgID：它分不清证券列表下载失败和列表里没有这个代码。
		stocks, err := cninfo.GetStockList(ctx)
		if err != nil {
			return nil, fmt.Errorf("巨潮资讯证券列表获取失败：%w", err)
		}
		for _, stock := range stocks {
			if stock.Code == code {
				req.StockCode = code + "," + stock.OrgID
				return cninfo.QueryAnnouncements(ctx, req)
			}
		}
		return nil, errCNINFONotFound
	}
	queryFilings = func(ctx context.Context, ticker string, query edgar.FilingQuery) (*edgar.FilingsResult, error) {
		client := secClient(ctx)
		company, err := client.Lookup(ctx, ticker)
		if err != nil {
			return nil, err
		}
		return client.Filings(ctx, company.CIK, query)
	}
)

// errCNINFONotFound 表示巨潮资讯的证券列表里没有这个代码。列表在本地缓存一天，没找到时不重新下载。
var errCNINFONotFound = errors.New("巨潮资讯没有这只证券")

var cninfoCategories = map[string]string{
	"annual": cninfo.CategoryAnnual, "halfyear": cninfo.CategoryHalfYear, "q1": cninfo.CategoryQ1, "q3": cninfo.CategoryQ3,
}

func callFilings(ctx context.Context, input json.RawMessage) (*Result, error) {
	var in filingsInput
	if err := Decode(input, &in); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, Inputf("code 不能为空，请给出一只 A 股或美股的代码或名称")
	}
	limit := defaultFilings
	if in.Limit != nil {
		if *in.Limit < 1 || *in.Limit > maxFilings {
			return nil, Inputf("limit 须在 1 到 %d 之间", maxFilings)
		}
		limit = *in.Limit
	}
	begin, end, err := filingsRange(in.Begin, in.End)
	if err != nil {
		return nil, err
	}
	category := ""
	if text := strings.ToLower(strings.TrimSpace(in.Category)); text != "" {
		var ok bool
		if category, ok = cninfoCategories[text]; !ok {
			return nil, Inputf("category 只能是 annual、halfyear、q1 或 q3")
		}
	}
	forms := []string{}
	for _, form := range in.Forms {
		if form = strings.TrimSpace(form); form != "" {
			forms = append(forms, form)
		}
	}

	secs, err := sina.Resolve(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("搜索证券失败：%w", err)
	}
	if len(secs) == 0 {
		return nil, Inputf("没有找到证券 %s，请检查代码，或先用 search_security 搜索", code)
	}
	// 000001 既是上证指数也是平安银行：同代码的几个 A 股里取第一个公司。
	candidates := valuation.Candidates(secs, code)
	sec := candidates[0]
	for _, c := range candidates {
		if filingsSupported(c) {
			sec = c
			break
		}
	}
	if !filingsSupported(sec) {
		return nil, Inputf("%s 暂不支持公告和申报，只支持 A 股和美股的公司", securityLabel(sec.ExCode, sec.Name))
	}
	us := strings.HasPrefix(sec.ExCode, "$")
	if us && category != "" {
		return nil, Inputf("category 只用于 A 股，美股请用 forms，如 [\"10-K\"]")
	}
	if !us && len(forms) > 0 {
		return nil, Inputf("forms 只用于美股，A 股请用 category（annual、halfyear、q1、q3）")
	}
	if us {
		return listSECFilings(ctx, sec, forms, begin, end, limit)
	}
	return listAnnouncements(ctx, sec, category, begin, end, limit)
}

// filingsSupported 判断是不是 A 股或美股的公司：基金、指数和港股没有公告或申报列表。
func filingsSupported(sec *sina.BasicSecurity) bool {
	if sec.SecurityType != types.SecurityTypeStock || indexCode.MatchString(sec.ExCode) {
		return false
	}
	return strings.HasPrefix(sec.ExCode, "$") || types.IsACode(sec.ExCode)
}

// filingsRange 解析日期，空字符串表示不限。只给开始日期时结束日期是今天。
func filingsRange(beginText, endText string) (string, string, error) {
	var begin, end time.Time
	var err error
	if beginText = strings.TrimSpace(beginText); beginText != "" {
		if begin, err = time.Parse(time.DateOnly, beginText); err != nil {
			return "", "", Inputf("begin 的格式应为 YYYY-MM-DD，如 2026-01-01")
		}
	}
	if endText = strings.TrimSpace(endText); endText != "" {
		if end, err = time.Parse(time.DateOnly, endText); err != nil {
			return "", "", Inputf("end 的格式应为 YYYY-MM-DD，如 2026-12-31")
		}
	}
	if beginText != "" && endText == "" {
		end = utils.ShanghaiToday()
	}
	if beginText != "" && begin.After(end) {
		return "", "", Inputf("begin（%s）不能晚于 end（%s）", beginText, end.Format(time.DateOnly))
	}
	format := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.DateOnly)
	}
	return format(begin), format(end), nil
}

func listAnnouncements(ctx context.Context, sec *sina.BasicSecurity, category, begin, end string, limit int) (*Result, error) {
	req := &cninfo.QueryRequest{PageNum: 1, Category: category}
	// 巨潮资讯的日期筛选要同时给开始和结束日期。
	if begin != "" || end != "" {
		req.StartDate, req.EndDate = begin, end
		if req.StartDate == "" {
			req.StartDate = "1990-01-01"
		}
	}
	resp, err := queryAnnouncements(ctx, sec.Code, req)
	if errors.Is(err, errCNINFONotFound) {
		return nil, Inputf("巨潮资讯的证券列表里没有 %s；列表在本地缓存一天，刚上市的股票可能要等缓存过期后才能查到", securityLabel(sec.ExCode, sec.Name))
	}
	if err != nil {
		return nil, fmt.Errorf("巨潮资讯公告查询失败：%w", err)
	}

	data := &filingsData{ExCode: sec.ExCode, Name: sec.Name, Results: []filingItem{}}
	total := 0
	if resp != nil {
		total = resp.Total
		for _, row := range cninfo.ToRows(resp.Data) {
			if len(data.Results) == limit {
				break
			}
			item := filingItem{Title: row.Title, URL: row.URL}
			if row.Date != nil {
				item.Date = *row.Date
			}
			data.Results = append(data.Results, item)
		}
	}
	data.Total = &total
	result := NewResult("list_filings", "巨潮资讯", data)
	result.Notes = append(result.Notes, "按公告日期从新到旧，只有标题和原文链接，不含正文")
	if total > len(data.Results) {
		result.Notes = append(result.Notes, fmt.Sprintf("符合条件的公告共 %d 条，只列最近 %d 条；可以用 category 或 begin、end 缩小范围", total, len(data.Results)))
	}
	if len(data.Results) == 0 {
		result.Notes = append(result.Notes, "没有符合条件的公告")
	}
	return result, nil
}

func listSECFilings(ctx context.Context, sec *sina.BasicSecurity, forms []string, begin, end string, limit int) (*Result, error) {
	query := edgar.FilingQuery{Forms: forms, Begin: begin, End: end, Want: limit}
	defaultForms := len(query.Forms) == 0
	if defaultForms {
		query.Forms = edgar.DefaultForms
	}
	got, err := queryFilings(ctx, strings.TrimPrefix(sec.ExCode, "$"), query)
	if errors.Is(err, edgar.ErrNotFound) {
		return nil, Inputf("SEC EDGAR 没有 %s 的申报，可能不向 SEC 申报", securityLabel(sec.ExCode, sec.Name))
	}
	if err != nil {
		return nil, fmt.Errorf("SEC 申报查询失败：%w", err)
	}

	data := &filingsData{ExCode: sec.ExCode, Name: sec.Name, Results: []filingItem{}}
	if data.Name == "" && got.Profile != nil {
		data.Name = got.Profile.Name
	}
	for _, f := range got.Filings {
		if len(data.Results) == limit {
			break
		}
		title := f.Description
		if title == "" {
			title = f.Form
		}
		data.Results = append(data.Results, filingItem{Date: f.FilingDate, Title: title, Form: f.Form, ReportDate: f.ReportDate, URL: f.URL})
	}
	result := NewResult("list_filings", "SEC EDGAR", data)
	result.Notes = append(result.Notes, "按申报日期从新到旧，只有类型说明和主文件链接，不含正文")
	if defaultForms {
		result.Notes = append(result.Notes, "默认只列 "+strings.Join(edgar.DefaultForms, "、")+"，不含 Form 4 等内部人交易；需要时用 forms 指定")
	}
	if got.Truncated && len(data.Results) < limit {
		result.Notes = append(result.Notes, "这家公司申报很多，只查了最近一段时间，结果可能不全；用 begin 指定更早的日期可以查全")
	}
	if len(data.Results) == 0 {
		result.Notes = append(result.Notes, "没有符合条件的申报")
	}
	return result, nil
}
