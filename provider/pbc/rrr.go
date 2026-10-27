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

const rrrNewsPath = "/goutongjiaoliu/113456/113469/index.html"

// RRRCoverageStart 是本采集器覆盖的人民币准备金率决策公告起点。
const RRRCoverageStart = "20200101"

type RRRRequest struct{ Begin, End string }

func (r RRRRequest) Validate() error {
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
	if r.Begin < RRRCoverageStart {
		return fmt.Errorf("人民币存款准备金率调整公告当前覆盖自 20200101 起")
	}
	return nil
}

// RRREvent 按生效日及适用机构拆分；幅度为有符号百分点，负值表示下调。
type RRREvent struct {
	EffectiveDate      string    `json:"effective_date"`
	EffectiveDateBasis string    `json:"effective_date_basis"`
	ScopeType          string    `json:"scope_type"`
	Scope              string    `json:"scope"`
	Exclusions         string    `json:"exclusions,omitempty"`
	Additional         bool      `json:"additional"`
	ChangePPLower      float64   `json:"change_percentage_points_lower"`
	ChangePPUpper      float64   `json:"change_percentage_points_upper"`
	TrancheIndex       int       `json:"tranche_index"`
	TrancheCount       int       `json:"tranche_count"`
	PublishedAt        string    `json:"published_at"`
	PublishedPrecision string    `json:"published_precision"`
	Title              string    `json:"title"`
	Source             string    `json:"source"`
	FetchedAt          time.Time `json:"fetched_at"`
	AnnouncementText   string    `json:"announcement_text"`
}

type RRRResult struct {
	Region                 string     `json:"region"`
	Currency               string     `json:"currency"`
	Indicator              string     `json:"indicator"`
	Unit                   string     `json:"unit"`
	Begin                  string     `json:"begin"`
	End                    string     `json:"end"`
	CoverageStart          string     `json:"coverage_start"`
	CoverageBasis          string     `json:"coverage_basis"`
	Events                 []RRREvent `json:"events"`
	ListSource             string     `json:"list_source"`
	ListFetchedAt          *time.Time `json:"list_fetched_at"`
	HistoryVerifiedThrough string     `json:"history_verified_through"`
	HistoryEventCount      int        `json:"history_event_count"`
	CachedUpdateCount      int        `json:"cached_update_count"`
	UpdatesCheckedThrough  string     `json:"updates_checked_through"`
	UpdateStatus           string     `json:"update_status"`
	ScannedPages           int        `json:"scanned_pages"`
	ScannedAnnouncements   int        `json:"scanned_announcements"`
	Note                   string     `json:"note"`
}

var rrrMainChange = regexp.MustCompile(`(下调|降低|上调|提高)([^。；;，,]*?)存款准备金率([0-9.]+)(?:至([0-9.]+))?个百分点`)
var rrrDirectSubject = regexp.MustCompile(`(?:下调|降低|上调|提高)[^。；;，,]*?存款准备金率`)
var rrrTargetChange = regexp.MustCompile(`定向降准([0-9.]+)(?:至([0-9.]+))?个百分点`)
var rrrAdditionalChange = regexp.MustCompile(`对(.+?)[，,]在(?:下调|降低)存款准备金率([0-9.]+)个百分点的基础上[，,]再额外(?:多降|下调|降低)([0-9.]+)个百分点`)
var rrrPhasedChange = regexp.MustCompile(`每次(下调|降低|上调|提高)([0-9.]+)个百分点`)
var rrrMagnitude = regexp.MustCompile(`[0-9.]+(?:至[0-9.]+)?个百分点`)
var rrrDate = regexp.MustCompile(`(?:(\d{4})年)?(\d{1,2})月(\d{1,2})日`)
var rrrEffectiveDate = regexp.MustCompile(`(?:从|自|于)(?:(\d{4})年)?(\d{1,2})月(\d{1,2})日`)
var rrrSentence = regexp.MustCompile(`[。；;]`)
var rrrExclusion = regexp.MustCompile(`^[（(]([^）)]*)[）)]`)
var rrrTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

func isRRRDecision(title string) bool {
	return strings.HasPrefix(title, "中国人民银行决定") && !strings.Contains(title, "外汇") && (strings.Contains(title, "存款准备金率") || strings.Contains(title, "降准"))
}

func rrrDateValue(match []string, pubDate time.Time) (string, error) {
	year := pubDate.Year()
	if match[1] != "" {
		year, _ = strconv.Atoi(match[1])
	}
	month, _ := strconv.Atoi(match[2])
	day, _ := strconv.Atoi(match[3])
	date := fmt.Sprintf("%04d%02d%02d", year, month, day)
	if _, err := time.Parse("20060102", date); err != nil || (match[1] == "" && date < pubDate.Format("20060102")) {
		return "", fmt.Errorf("准备金率生效日期无效或省略年份有歧义: %s", date)
	}
	return date, nil
}

func rrrChange(action, lower, upper string) (float64, float64, error) {
	lo, err := strconv.ParseFloat(lower, 64)
	if err != nil || lo <= 0 || lo > 100 {
		return 0, 0, fmt.Errorf("准备金率调整幅度无效: %s", lower)
	}
	hi := lo
	if upper != "" {
		hi, err = strconv.ParseFloat(upper, 64)
		if err != nil || hi < lo || hi > 100 {
			return 0, 0, fmt.Errorf("准备金率调整幅度区间无效")
		}
	}
	if action == "下调" || action == "降低" {
		return -hi, -lo, nil
	}
	return lo, hi, nil
}

func rrrScopeBefore(prefix string) string {
	index := strings.LastIndex(prefix, "对")
	if index < 0 {
		return ""
	}
	scope := strings.TrimPrefix(prefix[index+len("对"):], "于")
	for _, suffix := range []string{"定向", "再额外", "额外", "再"} {
		scope = strings.TrimSuffix(scope, suffix)
	}
	return strings.Trim(scope, "，,：:")
}

func parseRRR(doc *goquery.Document, title string) ([]RRREvent, error) {
	body := doc.Find("#zoom")
	if body.Length() != 1 {
		return nil, fmt.Errorf("准备金率公告缺少唯一正文")
	}
	text := compact(body.Text())
	pub := doc.Find(`meta[name="PubDate"]`).AttrOr("content", "")
	if len(pub) < 10 {
		return nil, fmt.Errorf("准备金率公告缺少公布日期")
	}
	pub = pub[:10]
	pubDate, err := time.Parse("2006-01-02", pub)
	if err != nil {
		return nil, fmt.Errorf("准备金率公告公布日期无效")
	}
	base := RRREvent{Title: title, PublishedAt: pub, PublishedPrecision: "day", AnnouncementText: text, TrancheIndex: 1, TrancheCount: 1}
	stamp := strings.TrimSpace(doc.Find("#shijian").Text())
	if stamp == "" {
		doc.Find(`td.hui12[align="right"]`).Each(func(_ int, cell *goquery.Selection) {
			if value := strings.TrimSpace(cell.Text()); rrrTimestamp.MatchString(value) {
				stamp = value
			}
		})
	}
	if stamp != "" {
		if _, err := time.Parse("2006-01-02 15:04:05", stamp); err != nil || !strings.HasPrefix(stamp, pub) {
			return nil, fmt.Errorf("准备金率公告公布时间无效或日期不一致")
		}
		base.PublishedAt, base.PublishedPrecision = strings.Replace(stamp, " ", "T", 1)+"+08:00", "second"
	}
	statements := rrrSentence.Split(text, -1)
	sharedDates := map[string]bool{}
	for _, statement := range statements {
		if !strings.Contains(statement, "存款准备金率") && !strings.Contains(statement, "定向降准") {
			continue
		}
		if strings.Contains(statement, "分两次") {
			continue
		}
		for _, match := range rrrEffectiveDate.FindAllStringSubmatch(statement, -1) {
			date, err := rrrDateValue(match, pubDate)
			if err != nil {
				return nil, err
			}
			sharedDates[date] = true
		}
	}
	var events []RRREvent
	for _, statement := range statements {
		if (!strings.Contains(statement, "存款准备金率") && !strings.Contains(statement, "定向降准")) || len(rrrMagnitude.FindAllString(statement, -1)) == 0 {
			continue
		}
		effective, basis := "", "explicit-date"
		if match := rrrEffectiveDate.FindStringSubmatch(statement); match != nil {
			effective, err = rrrDateValue(match, pubDate)
			if err != nil {
				return nil, err
			}
		} else if strings.Contains(statement, "即日起") {
			effective, basis = pubDate.Format("20060102"), "immediate-on-publication-date"
		} else if len(sharedDates) == 1 {
			for date := range sharedDates {
				effective = date
			}
			basis = "shared-announcement-effective-date"
		}
		if effective == "" {
			return nil, fmt.Errorf("准备金率调整缺少可确认的生效日期")
		}
		makeEvent := func(scope, action, lower, upper string, additional bool) (RRREvent, error) {
			event := base
			event.EffectiveDate, event.EffectiveDateBasis = effective, basis
			event.Scope, event.Additional, event.ScopeType = strings.Trim(scope, "，,：:"), additional, "targeted"
			if event.Scope == "金融机构" || event.Scope == "金融机构人民币" {
				event.ScopeType = "broad"
			}
			if event.Scope == "" {
				return event, fmt.Errorf("准备金率调整无法确认适用机构范围")
			}
			event.ChangePPLower, event.ChangePPUpper, err = rrrChange(action, lower, upper)
			return event, err
		}
		if match := rrrAdditionalChange.FindStringSubmatch(statement); match != nil {
			baseline, baselineErr := strconv.ParseFloat(match[2], 64)
			if baselineErr != nil || len(events) == 0 || events[0].ScopeType != "broad" || events[0].ChangePPLower != events[0].ChangePPUpper || math.Abs(events[0].ChangePPLower+baseline) > 1e-8 {
				return nil, fmt.Errorf("额外降准引用的全面降准幅度不一致")
			}
			event, err := makeEvent(rrrScopeBefore("对"+match[1]), "下调", match[3], "", true)
			if err != nil {
				return nil, err
			}
			events = append(events, event)
			continue // 基础降幅已在全面调整中保存，不再计为本机构的额外降幅。
		}
		before := len(events)
		mainMatches := rrrMainChange.FindAllStringSubmatchIndex(statement, -1)
		subjects := 0
		for _, subject := range rrrDirectSubject.FindAllString(statement, -1) {
			if strings.Contains(subject, "加权平均") {
				continue
			}
			subjects++
		}
		if subjects != len(mainMatches) {
			return nil, fmt.Errorf("准备金率直接调整幅度未完整识别")
		}
		for _, match := range mainMatches {
			scope := statement[match[4]:match[5]]
			prefix := statement[:match[0]]
			if scope == "" {
				scope = rrrScopeBefore(prefix)
			}
			upper := ""
			if match[8] >= 0 {
				upper = statement[match[8]:match[9]]
			}
			event, err := makeEvent(scope, statement[match[2]:match[3]], statement[match[6]:match[7]], upper, strings.Contains(prefix, "额外"))
			if err != nil {
				return nil, err
			}
			if exclusion := rrrExclusion.FindStringSubmatch(statement[match[1]:]); exclusion != nil {
				event.Exclusions = exclusion[1]
			}
			events = append(events, event)
		}
		for _, match := range rrrTargetChange.FindAllStringSubmatchIndex(statement, -1) {
			prefix := statement[:match[0]]
			upper := ""
			if match[4] >= 0 {
				upper = statement[match[4]:match[5]]
			}
			event, err := makeEvent(rrrScopeBefore(prefix), "下调", statement[match[2]:match[3]], upper, strings.Contains(prefix, "额外"))
			if err != nil {
				return nil, err
			}
			events = append(events, event)
		}
		if strings.Contains(statement, "分两次") {
			parts := rrrPhasedChange.FindAllStringSubmatch(statement, -1)
			dates := rrrDate.FindAllStringSubmatch(statement, -1)
			if len(events)-before != 1 || len(parts) != 1 || len(dates) != 2 || len(rrrMagnitude.FindAllString(statement, -1)) != 2 {
				return nil, fmt.Errorf("分批降准日期或幅度无法识别")
			}
			total := events[before]
			lo, hi, err := rrrChange(parts[0][1], parts[0][2], "")
			if err != nil || math.Abs(lo*2-total.ChangePPLower) > 1e-8 || total.ChangePPLower != total.ChangePPUpper {
				return nil, fmt.Errorf("分批降准幅度与总调整不一致")
			}
			events = events[:before]
			for i, date := range dates {
				event := total
				event.EffectiveDate, err = rrrDateValue(date, pubDate)
				if err != nil {
					return nil, err
				}
				event.EffectiveDateBasis = "explicit-phased-dates"
				event.ChangePPLower, event.ChangePPUpper = lo, hi
				event.TrancheIndex, event.TrancheCount = i+1, 2
				events = append(events, event)
			}
		} else if len(events)-before != len(rrrMagnitude.FindAllString(statement, -1)) {
			return nil, fmt.Errorf("准备金率调整语句未完整识别")
		}
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("准备金率决策公告未识别到调整")
	}
	return events, nil
}

// scanRRR 只扫描检查点当日及之后发布的新决策；同日重扫以免漏掉稍后发布的公告。
func (c *Client) scanRRR(ctx context.Context, cutoff string, knownSources map[string]bool) (RRRResult, error) {
	result := RRRResult{Events: []RRREvent{}, ListSource: c.origin + rrrNewsPath}
	address := result.ListSource
	seenPages, seenLinks, seenEvents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for source := range knownSources {
		seenLinks[source] = true
	}
	// 目录实时读取，避免分页缓存错位后仍推进持久检查点；只需检查最新的少量页面。
	listingClient := *c
	listingClient.Refresh = true
	for page := 0; page < 500; page++ {
		if seenPages[address] {
			return result, fmt.Errorf("准备金率新闻目录分页循环")
		}
		seenPages[address] = true
		doc, fetched, err := listingClient.get(ctx, address)
		if err != nil {
			return result, err
		}
		if page == 0 {
			result.ListFetchedAt = &fetched
		}
		result.ScannedPages = page + 1
		maxDate, count := "", 0
		var parseErr error
		doc.Find(".newslist_style a[href]").Each(func(_ int, a *goquery.Selection) {
			if parseErr != nil {
				return
			}
			date := strings.ReplaceAll(repoListingDate.FindString(a.Closest("td").Find(".hui12").Text()), "-", "")
			if _, err := time.Parse("20060102", date); err != nil {
				parseErr = fmt.Errorf("新闻目录缺少有效日期")
				return
			}
			count++
			if date > maxDate {
				maxDate = date
			}
			title := compact(a.AttrOr("title", a.Text()))
			if date < cutoff || !isRRRDecision(title) {
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
			events, err := parseRRR(report, title)
			if err != nil {
				parseErr = fmt.Errorf("%s: %w", source, err)
				return
			}
			result.ScannedAnnouncements++
			for _, event := range events {
				event.Source, event.FetchedAt = source, fetched
				key := fmt.Sprintf("%s|%s|%s|%g|%g|%t", event.EffectiveDate, event.Scope, event.Exclusions, event.ChangePPLower, event.ChangePPUpper, event.Additional)
				if !seenEvents[key] {
					seenEvents[key] = true
					result.Events = append(result.Events, event)
				}
			}
		})
		if parseErr != nil {
			return result, parseErr
		}
		if count == 0 {
			return result, fmt.Errorf("新闻目录为空或版式变化")
		}
		// 全页均早于起点才停止，避免置顶旧新闻造成提前结束。
		if maxDate < cutoff {
			sort.SliceStable(result.Events, func(i, j int) bool { return result.Events[i].EffectiveDate < result.Events[j].EffectiveDate })
			return result, nil
		}
		next := ""
		var nextErr error
		doc.Find("a").Each(func(_ int, a *goquery.Selection) {
			if compact(a.Text()) == "下一页" {
				href := a.AttrOr("tagname", a.AttrOr("href", ""))
				if href != "" && !strings.HasPrefix(href, "[") {
					next, nextErr = c.resolve(address, href)
				}
			}
		})
		if nextErr != nil {
			return result, nextErr
		}
		if next == "" {
			return result, fmt.Errorf("新闻目录尚未越过增量检查点且缺少下一页")
		}
		address = next
	}
	return result, fmt.Errorf("准备金率新闻目录超过500页")
}
