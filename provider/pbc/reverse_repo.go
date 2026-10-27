package pbc

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const reverseRepoPath = "/zhengcehuobisi/125207/125213/125431/125469/index.html"

// ReverseRepoCoverageStart 是固定利率、数量招标机制及本采集器的覆盖起点。
const ReverseRepoCoverageStart = "20240722"

// ReverseRepoRequest 按生效日查询固定利率机制下的七天逆回购利率调整。
type ReverseRepoRequest struct{ Begin, End string }

func (r ReverseRepoRequest) Validate() error {
	for _, date := range []string{r.Begin, r.End} {
		if len(date) != 8 {
			return fmt.Errorf("日期必须为 YYYYMMDD: %s", date)
		}
		if _, err := time.Parse("20060102", date); err != nil {
			return fmt.Errorf("无效日期: %s", date)
		}
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始日期不能晚于结束日期")
	}
	if r.Begin < ReverseRepoCoverageStart {
		return fmt.Errorf("七天逆回购政策利率公告当前覆盖自 20240722 起；此前中标利率历史尚未接入")
	}
	return nil
}

// ReverseRepoEvent 的前后利率均直接取自公告，基点变化由两者计算。
type ReverseRepoEvent struct {
	EffectiveDate      string    `json:"effective_date"`
	EffectiveDateBasis string    `json:"effective_date_basis"`
	PublishedAt        string    `json:"published_at"`
	PublishedPrecision string    `json:"published_precision"`
	PreviousRate       float64   `json:"previous_rate_percent"`
	Rate               float64   `json:"rate_percent"`
	ChangeBasisPoints  float64   `json:"change_basis_points"`
	Title              string    `json:"title"`
	Source             string    `json:"source"`
	FetchedAt          time.Time `json:"fetched_at"`
	AnnouncementText   string    `json:"announcement_text"`
}

// ReverseRepoResult 将区间内调整和区间前最后一项公告分开，空事件不等于利率为零。
type ReverseRepoResult struct {
	Region        string             `json:"region"`
	Indicator     string             `json:"indicator"`
	TenorDays     int                `json:"tenor_days"`
	Unit          string             `json:"unit"`
	Begin         string             `json:"begin"`
	End           string             `json:"end"`
	CoverageStart string             `json:"coverage_start"`
	PreviousEvent *ReverseRepoEvent  `json:"previous_event"`
	Events        []ReverseRepoEvent `json:"events"`
	ListSource    string             `json:"list_source"`
	ListFetchedAt time.Time          `json:"list_fetched_at"`
	Note          string             `json:"note"`
}

var repoListingDate = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
var repoRateChange = regexp.MustCompile(`公开市场7天(?:期)?逆回购操作利率由(?:此前的)?([0-9.]+)[%％](?:调整为|下调至|上调至|调降至|调升至)([0-9.]+)[%％]`)
var repoDirectChange = regexp.MustCompile(`公开市场7天(?:期)?逆回购操作利率(?:由|调整|下调|上调|调降|调升)`)
var repoEffectiveDate = regexp.MustCompile(`(?:从|自)(?:(\d{4})年)?(\d{1,2})月(\d{1,2})日起`)

func parseReverseRepoEvent(doc *goquery.Document) (*ReverseRepoEvent, error) {
	body := doc.Find("#zoom")
	if body.Length() != 1 {
		return nil, fmt.Errorf("公开市场公告缺少唯一正文")
	}
	text := compact(body.Text())
	matches := repoRateChange.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		if repoDirectChange.MatchString(text) {
			return nil, fmt.Errorf("七天逆回购利率调整格式无法识别")
		}
		return nil, nil // 临时隔夜工具加减点、14天操作及其他业务公告不属于本指标。
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("公告存在多项七天逆回购利率调整")
	}
	date := doc.Find(`meta[name="PubDate"]`).AttrOr("content", "")
	if len(date) < 10 {
		return nil, fmt.Errorf("利率调整公告缺少公布日期")
	}
	date = date[:10]
	pubDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		return nil, fmt.Errorf("无效公告公布日期: %s", date)
	}
	event := &ReverseRepoEvent{PublishedAt: date, PublishedPrecision: "day", Title: strings.TrimSpace(doc.Find("h2").First().Text()), AnnouncementText: text}
	if timestamp := strings.TrimSpace(doc.Find("#shijian").Text()); timestamp != "" {
		if _, err := time.ParseInLocation("2006-01-02 15:04:05", timestamp, time.FixedZone("CST", 8*60*60)); err != nil || !strings.HasPrefix(timestamp, date) {
			return nil, fmt.Errorf("公告日期和时间无效或不一致: %s", timestamp)
		}
		event.PublishedAt, event.PublishedPrecision = strings.Replace(timestamp, " ", "T", 1)+"+08:00", "second"
	}
	if event.Title == "" {
		event.Title = doc.Find(`meta[name="ArticleTitle"]`).AttrOr("content", "")
	}
	// 只读取包含直接利率调整的段落，避免借用其他操作或历史回顾的生效日期。
	paragraph := ""
	body.Find("p").Each(func(_ int, p *goquery.Selection) {
		if repoRateChange.MatchString(compact(p.Text())) {
			paragraph = compact(p.Text())
		}
	})
	if m := repoEffectiveDate.FindStringSubmatch(paragraph); m != nil {
		year := pubDate.Year()
		if m[1] != "" {
			year, _ = strconv.Atoi(m[1])
		}
		month, _ := strconv.Atoi(m[2])
		day, _ := strconv.Atoi(m[3])
		event.EffectiveDate = fmt.Sprintf("%04d%02d%02d", year, month, day)
		if _, err := time.Parse("20060102", event.EffectiveDate); err != nil {
			return nil, fmt.Errorf("无效利率生效日期: %s", event.EffectiveDate)
		}
		if m[1] == "" && event.EffectiveDate < pubDate.Format("20060102") {
			return nil, fmt.Errorf("公告未写明生效年份且日期早于公布日期")
		}
		event.EffectiveDateBasis = "explicit-date"
	} else if strings.Contains(paragraph, "即日起") {
		event.EffectiveDate, event.EffectiveDateBasis = pubDate.Format("20060102"), "immediate-on-publication-date"
	} else {
		return nil, fmt.Errorf("七天逆回购利率调整缺少可确认的生效日期")
	}
	event.PreviousRate, err = strconv.ParseFloat(matches[0][1], 64)
	if err != nil {
		return nil, fmt.Errorf("无效调整前利率")
	}
	event.Rate, err = strconv.ParseFloat(matches[0][2], 64)
	if err != nil || event.Rate < 0 || event.Rate > 100 || event.PreviousRate < 0 || event.PreviousRate > 100 || event.Rate == event.PreviousRate {
		return nil, fmt.Errorf("利率调整前后值无效")
	}
	event.ChangeBasisPoints = math.Round((event.Rate-event.PreviousRate)*100*1e6) / 1e6
	return event, nil
}

// ReverseRepo 按官方业务公告采集政策利率事件，始终扫描覆盖起点之后的公告以保留查询前基准。
func (c *Client) ReverseRepo(ctx context.Context, req ReverseRepoRequest) (ReverseRepoResult, error) {
	result := ReverseRepoResult{Region: "CN", Indicator: "pbc-7-day-reverse-repo-policy-rate", TenorDays: 7, Unit: "%", Begin: req.Begin, End: req.End, CoverageStart: ReverseRepoCoverageStart, Events: []ReverseRepoEvent{}, ListSource: c.origin + reverseRepoPath, Note: "按官方生效日筛选；覆盖2024年7月22日固定利率机制以来公告。前后利率来自原文，基点变化为计算值；公布时间为网页当前元数据，非首次发布存档。"}
	if err := req.Validate(); err != nil {
		return result, err
	}
	address := result.ListSource
	seenPages, seenLinks := map[string]bool{}, map[string]bool{}
	var events []ReverseRepoEvent
	finished := false
	for page := 0; page < 100; page++ {
		if seenPages[address] {
			return result, fmt.Errorf("公开市场公告分页链接循环")
		}
		seenPages[address] = true
		doc, fetched, err := c.get(ctx, address)
		if err != nil {
			return result, err
		}
		if page == 0 {
			result.ListFetchedAt = fetched
		}
		count, older := 0, false
		var parseErr error
		doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if parseErr != nil {
				return
			}
			title := compact(a.Text())
			if !strings.HasPrefix(title, "公开市场业务公告") && !strings.HasPrefix(title, "中国人民银行公开市场业务公告") {
				return
			}
			if !strings.Contains(title, "第") || !strings.HasSuffix(title, "号") {
				return // 排除导航和目录标题。
			}
			date := repoListingDate.FindString(a.Closest("td").Text())
			if _, err := time.Parse("2006-01-02", date); err != nil {
				parseErr = fmt.Errorf("业务公告目录缺少有效日期: %s", title)
				return
			}
			count++
			if strings.ReplaceAll(date, "-", "") < ReverseRepoCoverageStart {
				older = true
				return
			}
			source, err := c.resolve(address, a.AttrOr("href", ""))
			if err != nil {
				parseErr = err
				return
			}
			if seenLinks[source] {
				return
			}
			seenLinks[source] = true
			report, fetched, err := c.get(ctx, source)
			if err != nil {
				parseErr = err
				return
			}
			event, err := parseReverseRepoEvent(report)
			if err != nil {
				parseErr = fmt.Errorf("%s: %w", source, err)
				return
			}
			if event != nil {
				event.Source, event.FetchedAt = source, fetched
				events = append(events, *event)
			}
		})
		if parseErr != nil {
			return result, parseErr
		}
		if count == 0 {
			return result, fmt.Errorf("公开市场业务公告目录为空或格式变化")
		}
		if older {
			finished = true
			break
		}
		next := ""
		var nextErr error
		doc.Find("a").Each(func(_ int, a *goquery.Selection) {
			if compact(a.Text()) != "下一页" {
				return
			}
			href := a.AttrOr("tagname", a.AttrOr("href", ""))
			if href == "" || strings.HasPrefix(href, "[") {
				return
			}
			next, nextErr = c.resolve(address, href)
		})
		if nextErr != nil {
			return result, nextErr
		}
		if next == "" {
			return result, fmt.Errorf("公告目录尚未到覆盖起点，缺少下一页链接")
		}
		address = next
	}
	if !finished {
		return result, fmt.Errorf("公开市场业务公告超过100页")
	}
	sort.Slice(events, func(i, j int) bool { return events[i].EffectiveDate < events[j].EffectiveDate })
	if len(events) == 0 || events[0].EffectiveDate != ReverseRepoCoverageStart {
		return result, fmt.Errorf("公告目录缺少2024年7月22日起始利率调整，无法确认覆盖")
	}
	for i := range events {
		event := events[i]
		if i > 0 && (event.EffectiveDate == events[i-1].EffectiveDate || event.PreviousRate != events[i-1].Rate) {
			return result, fmt.Errorf("利率公告重复、冲突或调整前后不连续: %s", event.EffectiveDate)
		}
		if event.EffectiveDate < req.Begin {
			copy := event
			result.PreviousEvent = &copy
		} else if event.EffectiveDate <= req.End {
			result.Events = append(result.Events, event)
		}
	}
	return result, nil
}
