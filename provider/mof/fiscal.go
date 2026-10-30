package mof

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// FiscalRequest 查询所属月份 YYYYMM；年度报告记入12月，不拆分1—2月合并值。
type FiscalRequest struct{ Begin, End, Budget string }

func (r *FiscalRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("财政查询请求不能为空")
	}
	for _, p := range []string{r.Begin, r.End} {
		d, err := time.Parse("200601", p)
		if err != nil || d.Format("200601") != p {
			return fmt.Errorf("无效财政月份: %s", p)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "201501" {
		return fmt.Errorf("财政收支支持自201501起的一般公共预算口径，目录缺口保留空值")
	}
	if r.End > time.Now().In(time.FixedZone("CST", 8*3600)).Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前北京时间月份")
	}
	if r.Budget != "all" && r.Budget != "general" && r.Budget != "fund" {
		return fmt.Errorf("预算类型须为all、general或fund")
	}
	return nil
}

type FiscalMetric struct {
	Key                     string   `json:"key"`
	Name                    string   `json:"name"`
	Budget                  string   `json:"budget"`
	YTDAmount               *float64 `json:"ytd_amount"`
	YTDYoY                  *float64 `json:"ytd_yoy_percent"`
	ComparableYTDYoY        *float64 `json:"comparable_ytd_yoy_percent"`
	TaxRefundAdjustedYTDYoY *float64 `json:"tax_refund_adjusted_ytd_yoy_percent"`
	RawParagraph            string   `json:"raw_paragraph"`
	Note                    string   `json:"note"`
}
type FiscalObservation struct {
	Period        string         `json:"period"`
	YTDPeriod     string         `json:"ytd_period"`
	Metrics       []FiscalMetric `json:"metrics"`
	Source        string         `json:"source"`
	Title         string         `json:"title"`
	PublishedAt   *time.Time     `json:"published_at"`
	PublishedDate string         `json:"published_date"`
	FetchedAt     *time.Time     `json:"fetched_at"`
	RevisionID    *string        `json:"revision_id"`
	Status        string         `json:"status"`
	Note          string         `json:"note"`
}
type FiscalResult struct {
	Region             string              `json:"region"`
	Frequency          string              `json:"frequency"`
	Measurement        string              `json:"measurement"`
	Unit               string              `json:"unit"`
	GrowthUnit         string              `json:"growth_unit"`
	PriceBasis         string              `json:"price_basis"`
	SeasonallyAdjusted bool                `json:"seasonally_adjusted"`
	Annualized         bool                `json:"annualized"`
	DataVersion        string              `json:"data_version"`
	Begin              string              `json:"begin"`
	End                string              `json:"end"`
	Budget             string              `json:"budget"`
	Observations       []FiscalObservation `json:"observations"`
	Note               string              `json:"note"`
}

var fiscalSpecs = []struct{ key, name, budget, pattern string }{
	{"general-revenue", "一般公共预算收入", "general", `全国一般公共预算收入`},
	{"general-expenditure", "一般公共预算支出", "general", `全国一般公共预算支出`},
	{"fund-revenue", "政府性基金收入", "fund", `全国政府性基金(?:预算)?收入`},
	{"fund-expenditure", "政府性基金支出", "fund", `全国政府性基金(?:预算)?支出`},
}

type release struct{ Period, Title, URL, PublishedDate string }

var titleRE = regexp.MustCompile(`^(\d{4})年(?:(1[-—－至](\d{1,2})月)|(\d{1,2})月份?|前三季度|前3季度|上半年|一季度|第一季度|全年)?财政收支情况$`)
var pageRE = regexp.MustCompile(`var\s+countPage\s*=\s*(\d+)`)

func compact(s string) string { return strings.Join(strings.Fields(s), "") }
func titlePeriod(title string) (string, bool) {
	m := titleRE.FindStringSubmatch(compact(title))
	if m == nil {
		return "", false
	}
	month := 12
	switch {
	case m[3] != "":
		month, _ = strconv.Atoi(m[3])
	case m[4] != "":
		month, _ = strconv.Atoi(m[4])
	case strings.Contains(title, "前三季度") || strings.Contains(title, "前3季度"):
		month = 9
	case strings.Contains(title, "上半年"):
		month = 6
	case strings.Contains(title, "一季度"):
		month = 3
	}
	if month < 1 || month > 12 {
		return "", false
	}
	return fmt.Sprintf("%s%02d", m[1], month), true
}

func (c *Client) directory(doc *goquery.Document, address string) ([]release, int, error) {
	matches := pageRE.FindStringSubmatch(doc.Text())
	if matches == nil {
		return nil, 0, fmt.Errorf("财政目录缺少分页总数")
	}
	pages, _ := strconv.Atoi(matches[1])
	if pages < 1 || pages > 100 {
		return nil, 0, fmt.Errorf("财政目录分页数量异常")
	}
	links := []release{}
	count := 0
	var linkErr error
	doc.Find(".xwbd_lianbolistfrcon li").Each(func(_ int, li *goquery.Selection) {
		a := li.Find("a[href]").First()
		if a.Length() == 0 {
			return
		}
		count++
		title := compact(a.Text())
		period, ok := titlePeriod(title)
		if !ok {
			return
		}
		href, _ := a.Attr("href")
		resolved, err := c.resolve(address, href)
		if err != nil {
			linkErr = err
			return
		}
		date := strings.TrimSpace(li.Find("span").First().Text())
		if _, err := time.Parse("2006-01-02", date); err != nil {
			linkErr = fmt.Errorf("财政目录公布日期无效: %s", date)
			return
		}
		links = append(links, release{Period: period, Title: title, URL: resolved, PublishedDate: date})
	})
	if linkErr != nil {
		return nil, 0, linkErr
	}
	if count == 0 {
		return nil, 0, fmt.Errorf("财政目录未找到发布稿列表")
	}
	return links, pages, nil
}

func (c *Client) Fiscal(ctx context.Context, req *FiscalRequest) (*FiscalResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	result := &FiscalResult{Region: "CN", Frequency: "monthly", Measurement: "year-to-date-flow", Unit: "亿元", GrowthUnit: "%", PriceBasis: "current-prices", DataVersion: "current-visible-release", Begin: req.Begin, End: req.End, Budget: req.Budget, Observations: []FiscalObservation{}, Note: "全国一般公共预算和政府性基金预算分别列示，金额为当年累计名义流量、同比为官方累计增速，未季调未年化；同口径增速另列，不据累计增速相减计算当月。1—2月合并发布不拆分；12月对应全年初步执行数，不替换为后续预算或决算。目录未收录报告保留缺失，公布时间按官网元数据，修订编号不可得。收支差额不等同于官方财政赤字，不据这四项计算赤字。"}
	releases := map[string]release{}
	pages := 1
	for page := 0; page < pages; page++ {
		address := c.topicURL
		if page > 0 {
			address += fmt.Sprintf("index_%d.html", page)
		}
		validate := func(doc *goquery.Document) error { _, _, err := c.directory(doc, address); return err }
		doc, _, err := c.get(ctx, address, validate)
		if err != nil {
			return nil, err
		}
		list, total, err := c.directory(doc, address)
		if err != nil {
			return nil, err
		}
		pages = total
		for _, r := range list {
			if r.Period < req.Begin || r.Period > req.End {
				continue
			}
			if old, ok := releases[r.Period]; ok && old.URL != r.URL {
				return nil, fmt.Errorf("财政目录重复所属期: %s", r.Period)
			}
			releases[r.Period] = r
		}
	}
	start, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for d := start; !d.After(end); d = d.AddDate(0, 1, 0) {
		period := d.Format("200601")
		item := FiscalObservation{Period: period, YTDPeriod: period[:4] + "01-" + period, Metrics: []FiscalMetric{}, Status: "missing-release", Note: "当前官方专题目录未检索到该所属期；保留缺失"}
		for _, s := range fiscalSpecs {
			if req.Budget == "all" || req.Budget == s.budget {
				item.Metrics = append(item.Metrics, FiscalMetric{Key: s.key, Name: s.name, Budget: s.budget})
			}
		}
		if r, ok := releases[period]; ok {
			validate := func(doc *goquery.Document) error {
				_, err := parseRelease(doc, r)
				if err != nil {
					return fmt.Errorf("%s 财政发布稿: %w", r.Period, err)
				}
				return nil
			}
			doc, fetched, err := c.get(ctx, r.URL, validate)
			if err != nil {
				return nil, err
			}
			parsed, err := parseRelease(doc, r)
			if err != nil {
				return nil, err
			}
			parsed.FetchedAt = &fetched
			if req.Budget != "all" {
				filtered := []FiscalMetric{}
				for _, m := range parsed.Metrics {
					if m.Budget == req.Budget {
						filtered = append(filtered, m)
					}
				}
				parsed.Metrics = filtered
			}
			item = parsed
		} else if period[4:] == "01" {
			item.Note = "年初通常发布1—2月合并数，不以2月累计值拆分1月"
		}
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}

var amountRE = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)亿元`)
var growthRE = regexp.MustCompile(`(?:同比|比上年(?:同期|同月)?|比去年(?:同期|同月)?|[，；,;])(增长|下降|减少|增加)([0-9]+(?:\.[0-9]+)?)(%|％|倍)`)
var comparableRE = regexp.MustCompile(`同口径(?:为)?(增长|下降|减少|增加)([0-9]+(?:\.[0-9]+)?)(%|％|倍)`)
var naturalRE = regexp.MustCompile(`按自然口径(?:计算)?(?:同比)?(增长|下降|减少|增加)([0-9]+(?:\.[0-9]+)?)(%|％|倍)`)
var taxRefundRE = regexp.MustCompile(`扣除留抵退税因素(?:影响)?后(?:同比)?(增长|下降|减少|增加)([0-9]+(?:\.[0-9]+)?)(%|％|倍)`)

func rate(match []string) (*float64, error) {
	if match == nil {
		return nil, nil
	}
	n, err := strconv.ParseFloat(match[2], 64)
	if len(match) > 3 && match[3] == "倍" {
		n *= 100
	}
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("财政累计同比不是有限数值")
	}
	if match[1] == "下降" || match[1] == "减少" {
		n = -n
	}
	return &n, nil
}

func parseRelease(doc *goquery.Document, r release) (FiscalObservation, error) {
	item := FiscalObservation{Period: r.Period, YTDPeriod: r.Period[:4] + "01-" + r.Period, Source: r.URL, Title: r.Title, PublishedDate: r.PublishedDate, Metrics: []FiscalMetric{}, Status: "complete"}
	title, _ := doc.Find(`meta[name="ArticleTitle"]`).Attr("content")
	if title == "" {
		title = doc.Find("h2.title_con").First().Text()
	}
	if period, ok := titlePeriod(title); !ok || period != r.Period {
		return item, fmt.Errorf("财政发布稿标题与目录不符: %s", title)
	}
	if text, ok := doc.Find(`meta[name="PubDate"]`).Attr("content"); ok && text != "" {
		published, err := time.ParseInLocation("2006-01-02 15:04:05", text, time.FixedZone("CST", 8*3600))
		if err != nil {
			return item, fmt.Errorf("财政发布稿公布时间无效: %s", text)
		}
		if published.Format("2006-01-02") != r.PublishedDate {
			return item, fmt.Errorf("财政发布稿与目录公布日期不一致")
		}
		item.PublishedAt = &published
	}
	type passage struct{ Text, Raw string }
	paragraphs := []passage{}
	doc.Find(".TRS_Editor p").Each(func(_ int, p *goquery.Selection) {
		text := compact(p.Text())
		if text != "" {
			for _, sentence := range strings.Split(text, "。") {
				if sentence != "" {
					paragraphs = append(paragraphs, passage{Text: sentence, Raw: text})
				}
			}
		}
	})
	// 历史稿可能以div包裹正文，不把导航或关联链接混入财政段落。
	if len(paragraphs) == 0 {
		return item, fmt.Errorf("财政发布稿缺少正文段落")
	}
	month, _ := strconv.Atoi(r.Period[4:])
	year := r.Period[:4]
	prefixes := []string{fmt.Sprintf("1-%d月", month), fmt.Sprintf("1—%d月", month), fmt.Sprintf("1至%d月", month), fmt.Sprintf("1－%d月", month)}
	if month == 3 {
		prefixes = append(prefixes, "一季度", "第一季度")
	}
	if month == 6 {
		prefixes = append(prefixes, "上半年")
	}
	if month == 9 {
		prefixes = append(prefixes, "前三季度", "前3季度")
	}
	if month == 12 {
		prefixes = append(prefixes, "全年", year+"年")
	}
	if month == 1 {
		prefixes = append(prefixes, "1月")
	}
	for _, s := range fiscalSpecs {
		metric := FiscalMetric{Key: s.key, Name: s.name, Budget: s.budget}
		label := regexp.MustCompile(s.pattern)
		for _, entry := range paragraphs {
			paragraph := entry.Text
			loc := label.FindStringIndex(paragraph)
			if loc == nil {
				continue
			}
			prefix := paragraph[:loc[0]]
			if last := strings.LastIndex(prefix, "。"); last >= 0 {
				prefix = prefix[last+len("。"):]
			}
			prefix = strings.TrimRight(prefix, "，,")
			prefix = strings.TrimPrefix(prefix, year+"年")
			isYTD := false
			for _, p := range prefixes {
				if prefix == p || prefix == p+"累计" {
					isYTD = true
				}
			}
			// 年度前缀在去掉年份后为空。
			if month == 12 && (prefix == "" || prefix == "全年") {
				isYTD = true
			}
			if !isYTD {
				continue
			}
			tail := paragraph[loc[1]:]
			amount := amountRE.FindStringSubmatch(tail)
			// 年度回顾段可能再次提到同比而不列金额，只选择金额主段。
			if amount == nil {
				continue
			}
			if metric.YTDAmount != nil {
				return item, fmt.Errorf("财政累计指标重复: %s", s.name)
			}
			n, err := strconv.ParseFloat(amount[1], 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return item, fmt.Errorf("财政累计金额无效: %s", s.name)
			}
			metric.YTDAmount = &n
			metric.RawParagraph = entry.Raw
			// 仅在全国总量到首个分项之间提取增长，不误取中央、地方或税收增速。
			growthText := tail[len(amount[0]):]
			for _, stop := range []string{"其中", "分中央"} {
				if index := strings.Index(growthText, stop); index >= 0 {
					growthText = growthText[:index]
				}
			}
			reportedGrowth := growthRE.FindStringSubmatch(growthText)
			if natural := naturalRE.FindStringSubmatch(growthText); natural != nil {
				reportedGrowth = natural
			}
			metric.YTDYoY, err = rate(reportedGrowth)
			if err != nil {
				return item, err
			}
			metric.TaxRefundAdjustedYTDYoY, err = rate(taxRefundRE.FindStringSubmatch(growthText))
			if err != nil {
				return item, err
			}
			metric.ComparableYTDYoY, err = rate(comparableRE.FindStringSubmatch(growthText))
			if err != nil {
				return item, err
			}
			if metric.YTDYoY == nil {
				if !strings.Contains(growthText, "持平") {
					return item, fmt.Errorf("财政官方累计同比无法解析: %s", s.name)
				}
				metric.Note = "官方仅文字表述持平，未给出精确累计同比，保留空值"
				item.Status = "missing-values"
			}
		}
		if metric.YTDAmount == nil {
			return item, fmt.Errorf("财政发布稿缺少全国累计指标: %s", s.name)
		}
		item.Metrics = append(item.Metrics, metric)
	}
	return item, nil
}
