package pbc

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/utils"
)

const (
	ReverseRepoHistoryCoverageStart   = "20150122"
	ReverseRepoHistoryVerifiedThrough = "20240719"
	reverseRepoHistoryCutoff          = "20240722"
	reverseRepoHistoryChannelID       = "2845"
	reverseRepoHistoryOrigin          = "https://www.chinamoney.com.cn"
)

//go:embed data/reverse-repo-history.json
var embeddedReverseRepoHistory []byte

// ReverseRepoHistoryEvent 是一次实际开展的7天期逆回购操作。发布时间和操作日分别保留；正文没有明确操作日时不推断。
type ReverseRepoHistoryEvent struct {
	PublicationDate string  `json:"publication_date"`
	PublishedAt     string  `json:"published_at"`
	OperationDate   string  `json:"operation_date,omitempty"`
	Term            string  `json:"term"`
	AwardedRateRaw  string  `json:"awarded_rate_raw"`
	RatePercent     float64 `json:"rate_percent"`
	Publisher       string  `json:"publisher"`
	Host            string  `json:"host"`
	SourceURL       string  `json:"source_url"`
	Title           string  `json:"title"`
}

type reverseRepoHistorySnapshot struct {
	VerifiedThrough string                    `json:"verified_through"`
	Source          string                    `json:"source"`
	Publisher       string                    `json:"publisher"`
	Host            string                    `json:"host"`
	Events          []ReverseRepoHistoryEvent `json:"events"`
}

type ReverseRepoHistoryRequest struct{ Begin, End string }

func (r ReverseRepoHistoryRequest) Validate() error {
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
	if r.Begin < ReverseRepoHistoryCoverageStart || r.End > ReverseRepoHistoryVerifiedThrough {
		return fmt.Errorf("七天逆回购中标利率历史覆盖范围为 %s 至 %s（公告公布日）", ReverseRepoHistoryCoverageStart, ReverseRepoHistoryVerifiedThrough)
	}
	return nil
}

type ReverseRepoHistoryResult struct {
	Region          string                    `json:"region"`
	Currency        string                    `json:"currency"`
	Indicator       string                    `json:"indicator"`
	Unit            string                    `json:"unit"`
	Begin           string                    `json:"begin"`
	End             string                    `json:"end"`
	DateBasis       string                    `json:"date_basis"`
	CoverageStart   string                    `json:"coverage_start"`
	VerifiedThrough string                    `json:"verified_through"`
	UpdateStatus    string                    `json:"update_status"`
	TotalEvents     int                       `json:"total_events"`
	Events          []ReverseRepoHistoryEvent `json:"events"`
}

func loadReverseRepoHistory() (reverseRepoHistorySnapshot, error) {
	var snapshot reverseRepoHistorySnapshot
	if err := json.Unmarshal(embeddedReverseRepoHistory, &snapshot); err != nil {
		return snapshot, fmt.Errorf("读取七天逆回购历史快照失败: %w", err)
	}
	if err := validateReverseRepoHistorySnapshot(snapshot); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func validateReverseRepoHistorySnapshot(snapshot reverseRepoHistorySnapshot) error {
	if snapshot.VerifiedThrough != ReverseRepoHistoryVerifiedThrough || len(snapshot.Events) == 0 || snapshot.Source != "https://www.chinamoney.com.cn/chinese/scggyhywgg/" || snapshot.Publisher != "中国人民银行公开市场业务操作室" || snapshot.Host != "中国货币网" {
		return fmt.Errorf("七天逆回购历史快照检查点无效")
	}
	for i, event := range snapshot.Events {
		date := strings.ReplaceAll(event.PublicationDate, "-", "")
		source, sourceErr := url.Parse(event.SourceURL)
		rateText := strings.ReplaceAll(event.AwardedRateRaw, " ", "")
		rateText = strings.TrimSuffix(rateText, "%")
		rate, rateErr := strconv.ParseFloat(rateText, 64)
		publishedAt, publishedErr := time.Parse("2006-01-02 15:04", event.PublishedAt)
		operationDateValid := event.OperationDate == ""
		if event.OperationDate != "" {
			_, operationDateErr := time.Parse("2006-01-02", event.OperationDate)
			operationDateValid = operationDateErr == nil
		}
		if len(date) != 8 || date < ReverseRepoHistoryCoverageStart || date > snapshot.VerifiedThrough || event.Term != "7天" || event.RatePercent <= 0 || !reverseRepoHistoryRateRE.MatchString(event.AwardedRateRaw) || rateErr != nil || rate != event.RatePercent || sourceErr != nil || source.Scheme != "https" || source.Host != "www.chinamoney.com.cn" || !strings.HasPrefix(source.Path, "/chinese/scggyhywgggksccz/") || event.Publisher != "中国人民银行公开市场业务操作室" || event.Host != "中国货币网" || !strings.HasPrefix(event.Title, "公开市场业务交易公告") || publishedErr != nil || publishedAt.Format("2006-01-02") != event.PublicationDate || !operationDateValid {
			return fmt.Errorf("七天逆回购历史快照第%d条语义校验失败", i+1)
		}
		if i > 0 && snapshot.Events[i-1].PublicationDate > event.PublicationDate {
			return fmt.Errorf("七天逆回购历史快照未按公布日期排序")
		}
	}
	return nil
}

// ReverseRepoHistory 从内置逐笔核验快照查询历史；Refresh 只检查政策机制切换前的新增目录项。
func (c *Client) ReverseRepoHistory(ctx context.Context, req ReverseRepoHistoryRequest) (ReverseRepoHistoryResult, error) {
	if err := req.Validate(); err != nil {
		return ReverseRepoHistoryResult{}, err
	}
	snapshot, err := loadReverseRepoHistory()
	if err != nil {
		return ReverseRepoHistoryResult{}, err
	}
	result := ReverseRepoHistoryResult{Region: "CN", Currency: "CNY", Indicator: "7-day-reverse-repo-awarded-rate-history", Unit: "%", Begin: req.Begin, End: req.End, DateBasis: "publication-date", CoverageStart: ReverseRepoHistoryCoverageStart, VerifiedThrough: snapshot.VerifiedThrough, UpdateStatus: "embedded-history", Events: []ReverseRepoHistoryEvent{}}
	for _, event := range snapshot.Events {
		date := strings.ReplaceAll(event.PublicationDate, "-", "")
		if date >= req.Begin && date <= req.End {
			result.Events = append(result.Events, event)
		}
	}
	if c != nil && c.Refresh {
		updates, status, err := c.checkReverseRepoHistoryDirectory(ctx, snapshot.VerifiedThrough)
		if err != nil {
			return ReverseRepoHistoryResult{}, err
		}
		result.UpdateStatus = status
		if len(updates) > 0 {
			result.UpdateStatus = "incremental-events-found"
		}
		for _, event := range updates {
			date := strings.ReplaceAll(event.PublicationDate, "-", "")
			if date >= req.Begin && date <= req.End {
				result.Events = append(result.Events, event)
			}
		}
		for _, event := range updates {
			date := strings.ReplaceAll(event.PublicationDate, "-", "")
			if date > result.VerifiedThrough {
				result.VerifiedThrough = date
			}
		}
	}
	sortReverseRepoHistory(result.Events)
	result.TotalEvents = len(result.Events)
	return result, nil
}

type reverseRepoNoticePage struct {
	Data struct {
		PageTotalSize int `json:"pageTotalSize"`
	} `json:"data"`
	Records []struct {
		Title       string `json:"title"`
		ReleaseDate string `json:"releaseDate"`
		DraftPath   string `json:"draftPath"`
	} `json:"records"`
}

// checkReverseRepoHistoryDirectory checks only announcements between the archive checkpoint and the July 2024 policy-mechanism change.
func (c *Client) checkReverseRepoHistoryDirectory(ctx context.Context, verifiedThrough string) ([]ReverseRepoHistoryEvent, string, error) {
	updates := []ReverseRepoHistoryEvent{}
	for pageNo := 1; pageNo <= 100; pageNo++ {
		address := fmt.Sprintf("%s/ags/ms/cm-s-notice-query/contents?pageNo=%d&pageSize=15&channelId=%s", reverseRepoHistoryOrigin, pageNo, reverseRepoHistoryChannelID)
		resp, err := utils.MakeRequest(ctx, http.MethodPost, address, http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}}, bytes.NewReader(nil), 30*time.Second)
		if err != nil {
			return nil, "", fmt.Errorf("请求中国货币网公开市场操作公告目录失败: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return nil, "", readErr
		}
		if closeErr != nil {
			return nil, "", closeErr
		}
		if resp.StatusCode != http.StatusOK || len(body) > 2<<20 {
			return nil, "", fmt.Errorf("中国货币网公告目录返回 HTTP %d 或响应过大", resp.StatusCode)
		}
		var directory reverseRepoNoticePage
		if err := json.Unmarshal(body, &directory); err != nil {
			return nil, "", fmt.Errorf("解析中国货币网公告目录失败: %w", err)
		}
		if len(directory.Records) == 0 || directory.Data.PageTotalSize == 0 {
			return nil, "", fmt.Errorf("中国货币网公开市场操作公告目录为空或格式变化")
		}
		olderThanCutoff := false
		for _, notice := range directory.Records {
			date := strings.ReplaceAll(notice.ReleaseDate, "-", "")
			if len(date) != 8 {
				return nil, "", fmt.Errorf("公告目录缺少有效公布日期: %s", notice.Title)
			}
			if date < reverseRepoHistoryCutoff {
				olderThanCutoff = true
			}
			// 只允许在已核验基线之后且固定利率机制切换之前发现旧口径事件。
			if date > verifiedThrough && date < reverseRepoHistoryCutoff {
				// 公告目录本身不含中标利率；不把目录项当作观察值。
				// 发现此类项目时要求后续显式解析正文，而不是推算或填充。
				if notice.DraftPath == "" || !strings.HasPrefix(notice.DraftPath, "/chinese/scggyhywgggksccz/") {
					return nil, "", fmt.Errorf("新增旧口径公告正文链接不符合预期: %s", notice.Title)
				}
				source := reverseRepoHistoryOrigin + notice.DraftPath
				resp, err := utils.MakeRequest(ctx, http.MethodGet, source, nil, nil, 30*time.Second)
				if err != nil {
					return nil, "", fmt.Errorf("请求新增逆回购公告正文失败: %w", err)
				}
				raw, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
				closeErr := resp.Body.Close()
				if readErr != nil {
					return nil, "", readErr
				}
				if closeErr != nil {
					return nil, "", closeErr
				}
				if resp.StatusCode != http.StatusOK || len(raw) > 4<<20 {
					return nil, "", fmt.Errorf("新增逆回购公告正文返回 HTTP %d 或响应过大", resp.StatusCode)
				}
				event, err := parseReverseRepoHistoryNotice(raw, source, notice.ReleaseDate)
				if err != nil {
					return nil, "", err
				}
				if event != nil {
					updates = append(updates, *event)
				}
			}
		}
		if olderThanCutoff || pageNo >= directory.Data.PageTotalSize {
			return updates, "sealed-at-2024-07-19", nil
		}
	}
	return nil, "", fmt.Errorf("扫描中国货币网公告目录超过100页，拒绝静默截断")
}

var reverseRepoHistoryRateRE = regexp.MustCompile(`^\s*([0-9]+(?:\.[0-9]+)?)\s*%\s*$`)

func parseReverseRepoHistoryNotice(raw []byte, source string, publicationDate string) (*ReverseRepoHistoryEvent, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(source)
	if err != nil || parsedURL.Host != "www.chinamoney.com.cn" || parsedURL.Scheme != "https" {
		return nil, fmt.Errorf("非中国货币网公告链接: %s", source)
	}
	title := compact(doc.Find(".title-heading").First().Text())
	if !strings.HasPrefix(title, "公开市场业务交易公告") {
		return nil, fmt.Errorf("非公开市场业务交易公告: %s", title)
	}
	publishedAt := strings.TrimSpace(doc.Find(".article-a-toolbar .san-col-4 span").First().Text())
	content := doc.Find(".article-a-content").First()
	if content.Length() == 0 {
		content = doc.Find("#ewebeditor_content").First()
	}
	if content.Length() == 0 {
		return nil, fmt.Errorf("公告正文为空: %s", source)
	}
	var rawRate string
	content.Find("table tr").EachWithBreak(func(_ int, row *goquery.Selection) bool {
		cells := row.ChildrenFiltered("th,td")
		if cells.Length() < 3 || !regexp.MustCompile(`^\s*7\s*天\s*$`).MatchString(strings.TrimSpace(cells.Eq(0).Text())) {
			return true
		}
		candidate := strings.TrimSpace(cells.Eq(2).Text())
		if reverseRepoHistoryRateRE.MatchString(candidate) {
			rawRate = regexp.MustCompile(`\s+`).ReplaceAllString(candidate, "")
		}
		return rawRate == ""
	})
	if rawRate == "" {
		return nil, nil // 目录公告没有明确的7天中标利率，不生成观察值。
	}
	rate, _ := strconv.ParseFloat(strings.TrimSuffix(rawRate, "%"), 64)
	operationDate := ""
	text := regexp.MustCompile(`\s+`).ReplaceAllString(content.Text(), "")
	if match := regexp.MustCompile(`(20\d{2})年(\d{1,2})月(\d{1,2})日人民银行`).FindStringSubmatch(text); len(match) == 4 {
		if parsed, err := time.Parse("2006-1-2", fmt.Sprintf("%s-%s-%s", match[1], match[2], match[3])); err == nil {
			operationDate = parsed.Format("2006-01-02")
		}
	}
	return &ReverseRepoHistoryEvent{PublicationDate: publicationDate, PublishedAt: publishedAt, OperationDate: operationDate, Term: "7天", AwardedRateRaw: rawRate, RatePercent: rate, Publisher: "中国人民银行公开市场业务操作室", Host: "中国货币网", SourceURL: source, Title: title}, nil
}

func sortReverseRepoHistory(events []ReverseRepoHistoryEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].PublicationDate == events[j].PublicationDate {
			return events[i].PublishedAt < events[j].PublishedAt
		}
		return events[i].PublicationDate < events[j].PublicationDate
	})
}
