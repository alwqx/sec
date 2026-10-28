package federalreserve

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	statementPath = regexp.MustCompile(`^/newsevents/(?:pressreleases/monetary|press/monetary/)(\d{8})[a-z]\.htm$`)
	// 从实际决定段落提取区间，兼容整数、小数及1-1/4、1/4等官方分数写法。
	statementRange = regexp.MustCompile(`(?i)target range for the federal funds rate[^;]{0,180}?\b(\d+(?:[ -]\d+/\d+|/\d+|\.\d+)?)\s+(?:to|and)\s+(\d+(?:[ -]\d+/\d+|/\d+|\.\d+)?)\s+percent`)
	releaseTime    = regexp.MustCompile(`(?i)^For release at (\d{1,2}):(\d{2})\s*([ap])\.m\.\s+(EST|EDT)$`)
)

type statementLink struct {
	date string
	url  string
}

func parseCalendar(body []byte, address string) ([]statementLink, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	var links []statementLink
	seen := map[string]bool{}
	doc.Find("a[href]").Each(func(_ int, node *goquery.Selection) {
		label := strings.ToLower(clean(node.Text()))
		isStatement := label == "statement" || label == "fomc statement"
		if label == "html" {
			// HTML链接必须位于正式Statement栏；同日长期目标、缩表计划等公告不是利率声明候选。
			node.Parent().ChildrenFiltered("strong").Each(func(_ int, header *goquery.Selection) {
				if strings.EqualFold(strings.TrimSuffix(clean(header.Text()), ":"), "Statement") {
					isStatement = true
				}
			})
		}
		if !isStatement {
			return
		}
		href, _ := node.Attr("href")
		ref, err := url.Parse(href)
		if err != nil {
			return
		}
		ref = base.ResolveReference(ref)
		if ref.Scheme != "https" || ref.Host != "www.federalreserve.gov" || ref.RawQuery != "" || ref.Fragment != "" {
			return
		}
		match := statementPath.FindStringSubmatch(ref.Path)
		if match == nil || seen[ref.String()] {
			return
		}
		date, err := time.Parse("20060102", match[1])
		if err != nil {
			return
		}
		seen[ref.String()] = true
		links = append(links, statementLink{date.Format("2006-01-02"), ref.String()})
	})
	if len(links) == 0 {
		return nil, fmt.Errorf("美联储会议页面缺少可核验的官方声明链接")
	}
	return links, nil
}

type parsedStatement struct {
	date         string
	published    *time.Time
	lower        float64
	upper        float64
	raw          StatementRaw
	availability string
}

func parseStatement(body []byte) (*parsedStatement, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	article := doc.Find("#article")
	raw := StatementRaw{Date: clean(article.Find(".article__time").First().Text()), ReleaseTime: clean(article.Find(".releaseTime").First().Text())}
	date, err := time.Parse("January 2, 2006", raw.Date)
	if err != nil {
		return nil, fmt.Errorf("官方声明缺少可解析的公布日期")
	}
	var bounds []string
	article.Find("p").EachWithBreak(func(_ int, paragraph *goquery.Selection) bool {
		text := clean(paragraph.Text())
		normalized := strings.NewReplacer("‑", "-", "–", "-", "−", "-", "¼", "1/4", "½", "1/2", "¾", "3/4").Replace(text)
		bounds = statementRange.FindStringSubmatch(normalized)
		if bounds != nil {
			raw.Decision = text
			return false
		}
		return true
	})
	if bounds == nil {
		return nil, fmt.Errorf("官方声明未找到可核验的目标区间决定")
	}
	lower, err := fraction(bounds[1])
	if err != nil {
		return nil, err
	}
	upper, err := fraction(bounds[2])
	if err != nil || !finite(lower) || !finite(upper) || lower < 0 || lower >= upper {
		return nil, fmt.Errorf("官方声明目标区间无效")
	}
	parsed := &parsedStatement{date: date.Format("2006-01-02"), lower: lower, upper: upper, raw: raw, availability: "date-only"}
	if stamp := releaseTime.FindStringSubmatch(raw.ReleaseTime); stamp != nil {
		hour, _ := strconv.Atoi(stamp[1])
		minute, _ := strconv.Atoi(stamp[2])
		if hour < 1 || hour > 12 || minute > 59 {
			return nil, fmt.Errorf("官方声明公布时间无效")
		}
		hour %= 12
		if strings.EqualFold(stamp[3], "p") {
			hour += 12
		}
		offset := -5 * 3600
		if strings.EqualFold(stamp[4], "EDT") {
			offset = -4 * 3600
		}
		published := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, time.FixedZone(strings.ToUpper(stamp[4]), offset))
		parsed.published, parsed.availability = &published, "available"
	}
	return parsed, nil
}

func fraction(value string) (float64, error) {
	parts := strings.Fields(strings.ReplaceAll(value, "-", " "))
	result := 0.0
	for _, part := range parts {
		numbers := strings.Split(part, "/")
		first, err := strconv.ParseFloat(numbers[0], 64)
		if err != nil {
			return 0, fmt.Errorf("无效官方利率分数: %s", value)
		}
		if len(numbers) == 2 {
			denominator, err := strconv.ParseFloat(numbers[1], 64)
			if err != nil || denominator <= 0 {
				return 0, fmt.Errorf("无效官方利率分母: %s", value)
			}
			first /= denominator
		}
		result += first
	}
	return result, nil
}

type releaseResolver struct {
	client       *Client
	current      []statementLink
	currentError error
	loaded       bool
	years        map[int][]statementLink
	yearErrors   map[int]error
}

func newReleaseResolver(client *Client) *releaseResolver {
	return &releaseResolver{client: client, years: map[int][]statementLink{}, yearErrors: map[int]error{}}
}

func (r *releaseResolver) links(ctx context.Context, year int) ([]statementLink, error) {
	if !r.loaded {
		r.loaded = true
		body, _, err := r.client.fetch(ctx, r.client.calendar, func(body []byte) error { _, err := parseCalendar(body, r.client.calendar); return err })
		r.currentError = err
		if err == nil {
			r.current, r.currentError = parseCalendar(body, r.client.calendar)
		}
	}
	for _, link := range r.current {
		if strings.HasPrefix(link.date, strconv.Itoa(year)+"-") {
			return r.current, nil
		}
	}
	if links, exists := r.years[year]; exists {
		return links, r.yearErrors[year]
	}
	address := fmt.Sprintf(r.client.history, year)
	body, _, err := r.client.fetch(ctx, address, func(body []byte) error { _, err := parseCalendar(body, address); return err })
	if err == nil {
		r.years[year], err = parseCalendar(body, address)
	} else {
		r.years[year] = nil
	}
	if err != nil && r.currentError != nil {
		err = fmt.Errorf("会议日历不可用（%v）；历史日历不可用（%w）", r.currentError, err)
	}
	r.yearErrors[year] = err
	return r.years[year], err
}

func (r *releaseResolver) enrich(ctx context.Context, event *TargetRateEvent) error {
	date, _ := time.Parse("2006-01-02", event.EffectiveDate)
	links, err := r.links(ctx, date.Year())
	if err != nil {
		return err
	}
	var candidates []statementLink
	for _, link := range links {
		// URL日期只用于筛选候选；公布日期必须再由声明正文核验。
		if link.date <= event.EffectiveDate && link.date >= date.AddDate(0, 0, -7).Format("2006-01-02") {
			candidates = append(candidates, link)
		}
	}
	if len(candidates) == 0 {
		return fmt.Errorf("官方日历未找到生效日前7天内的声明链接")
	}
	var matched *parsedStatement
	var matchedURL string
	var matchedAt time.Time
	var failures []string
	for _, candidate := range candidates {
		body, fetched, err := r.client.fetch(ctx, candidate.url, func(body []byte) error { _, err := parseStatement(body); return err })
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		statement, err := parseStatement(body)
		if err != nil {
			return err
		}
		if statement.date != candidate.date || statement.date > event.EffectiveDate || statement.lower != event.LowerPercent || statement.upper != event.UpperPercent {
			continue
		}
		if matched != nil {
			return fmt.Errorf("多个官方声明与目标区间匹配，无法唯一确定公布信息")
		}
		matched, matchedURL, matchedAt = statement, candidate.url, fetched
	}
	if matched == nil || len(failures) > 0 {
		return fmt.Errorf("无法完整核验对应声明及目标区间%s", failureDetail(failures))
	}
	event.AnnouncedDate, event.PublishedAt = &matched.date, matched.published
	event.StatementSourceURL, event.StatementFetchedAt, event.StatementRaw = matchedURL, &matchedAt, &matched.raw
	event.ReleaseStatus = matched.availability
	event.ReleaseNote = "公布日期和时间来自匹配目标区间的官方声明；生效日期来自官方调整表"
	if matched.published == nil {
		event.ReleaseNote = "公布日期来自匹配目标区间的官方声明；声明未提供可解析的具体公布时间，不推断时刻"
	}
	return nil
}

func failureDetail(failures []string) string {
	if len(failures) == 0 {
		return ""
	}
	return "：" + strings.Join(failures, "；")
}
