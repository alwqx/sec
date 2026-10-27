package pbc

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

const lprPath = "/zhengcehuobisi/125207/125213/125440/3876551/index.html"

// LPRCoverageStart 为2019年报价机制改革后的首个发布月。
const LPRCoverageStart = "201908"

type LPRRequest struct{ Begin, End string }

func (r LPRRequest) Validate() error {
	if err := (FinancingRequest{Begin: r.Begin, End: r.End}).Validate(); err != nil {
		return err
	}
	if r.Begin < LPRCoverageStart {
		return fmt.Errorf("LPR 月度双期限报价当前覆盖自 201908 起，不与改革前口径拼接")
	}
	return nil
}

// LPRObservation 保留月度报价及其与相邻上月发布值的变化，不将缺失月份补为零。
type LPRObservation struct {
	Region               string     `json:"region"`
	Period               string     `json:"period"`
	QuoteDate            string     `json:"quote_date"`
	OneYearRate          *float64   `json:"one_year_rate_percent"`
	FiveYearPlusRate     *float64   `json:"five_year_plus_rate_percent"`
	OneYearChangeBP      *float64   `json:"one_year_change_basis_points"`
	FiveYearPlusChangeBP *float64   `json:"five_year_plus_change_basis_points"`
	ChangeMethod         string     `json:"change_method"`
	ChangeStatus         string     `json:"change_status"`
	Unit                 string     `json:"unit"`
	Definition           string     `json:"definition"`
	PublishedAt          string     `json:"published_at"`
	PublishedPrecision   string     `json:"published_precision"`
	Source               string     `json:"source"`
	FetchedAt            *time.Time `json:"fetched_at"`
	PreviousQuoteDate    string     `json:"previous_quote_date,omitempty"`
	PreviousSource       string     `json:"previous_source,omitempty"`
	Validity             string     `json:"validity"`
	Note                 string     `json:"note,omitempty"`
}

var lprTitleDate = regexp.MustCompile(`^(\d{4})年(\d{1,2})月(\d{1,2})日全国银行间同业拆借中心受权公布贷款市场报价利率[（(]LPR[）)]`)
var lprBodyDate = regexp.MustCompile(`(\d{4})年(\d{1,2})月(\d{1,2})日贷款市场报价利率[（(]LPR[）)]为[:：]`)
var lprOneYear = regexp.MustCompile(`(?:^|[^0-9])1年期LPR为([0-9.]+)[%％]`)
var lprFiveYear = regexp.MustCompile(`5年期以上LPR为([0-9.]+)[%％]`)
var lprPageNumbers = regexp.MustCompile(`当前页[:：](\d+)/(\d+)`)

func lprDate(match []string) (string, error) {
	year, _ := strconv.Atoi(match[1])
	month, _ := strconv.Atoi(match[2])
	day, _ := strconv.Atoi(match[3])
	date := fmt.Sprintf("%04d%02d%02d", year, month, day)
	if _, err := time.Parse("20060102", date); err != nil {
		return "", fmt.Errorf("无效 LPR 报价日期: %s", date)
	}
	return date, nil
}

func parseLPR(doc *goquery.Document, expectedDate string) (LPRObservation, error) {
	result := LPRObservation{}
	body := doc.Find("#zoom")
	if body.Length() != 1 {
		return result, fmt.Errorf("LPR 公告缺少唯一正文")
	}
	text := compact(body.Text())
	dates := lprBodyDate.FindAllStringSubmatch(text, -1)
	if len(dates) != 1 {
		return result, fmt.Errorf("LPR 公告缺少唯一报价日期")
	}
	date, err := lprDate(dates[0])
	if err != nil {
		return result, err
	}
	if date != expectedDate {
		return result, fmt.Errorf("LPR 正文报价日期与目录标题不一致: %s / %s", date, expectedDate)
	}
	result.QuoteDate, result.Period = date, date[:6]
	for _, part := range []struct {
		pattern *regexp.Regexp
		value   **float64
	}{{lprOneYear, &result.OneYearRate}, {lprFiveYear, &result.FiveYearPlusRate}} {
		matches := part.pattern.FindAllStringSubmatch(text, -1)
		if len(matches) != 1 {
			return result, fmt.Errorf("LPR 公告缺少唯一的1年期或5年期以上报价")
		}
		n, err := strconv.ParseFloat(matches[0][1], 64)
		if err != nil || n < 0 || n > 100 {
			return result, fmt.Errorf("无效 LPR 利率")
		}
		*part.value = &n
	}
	pubDate := doc.Find(`meta[name="PubDate"]`).AttrOr("content", "")
	if len(pubDate) < 10 {
		return result, fmt.Errorf("LPR 公告缺少公布日期")
	}
	pubDate = pubDate[:10]
	if _, err := time.Parse("2006-01-02", pubDate); err != nil {
		return result, fmt.Errorf("LPR 公布日期无效")
	}
	result.PublishedAt, result.PublishedPrecision = pubDate, "day"
	if stamp := strings.TrimSpace(doc.Find("#shijian").Text()); stamp != "" {
		if _, err := time.Parse("2006-01-02 15:04:05", stamp); err != nil || !strings.HasPrefix(stamp, pubDate) {
			return result, fmt.Errorf("LPR 公布时间无效或与公布日期不一致")
		}
		result.PublishedAt, result.PublishedPrecision = strings.Replace(stamp, " ", "T", 1)+"+08:00", "second"
	}
	if !strings.Contains(text, "以上LPR在下一次发布LPR之前有效") {
		return result, fmt.Errorf("LPR 公告缺少可识别的有效期说明")
	}
	result.Validity = "until-next-release"
	return result, nil
}

func (c *Client) lprReports(ctx context.Context, earliest, end string) (map[string]LPRObservation, error) {
	reports := map[string]LPRObservation{}
	seenPages, seenLinks := map[string]bool{}, map[string]bool{}
	address := c.origin + lprPath
	for page := 0; page < 100; page++ {
		if seenPages[address] {
			return nil, fmt.Errorf("LPR 目录分页链接循环")
		}
		seenPages[address] = true
		doc, _, err := c.get(ctx, address)
		if err != nil {
			return nil, err
		}
		older, count := false, 0
		var parseErr error
		doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if parseErr != nil {
				return
			}
			title := compact(a.AttrOr("title", a.Text()))
			match := lprTitleDate.FindStringSubmatch(title)
			if match == nil {
				return // 排除报价行名单、改革问答和栏目导航。
			}
			count++
			date, err := lprDate(match)
			if err != nil {
				parseErr = err
				return
			}
			period := date[:6]
			if period < earliest {
				older = true
				return
			}
			if period > end {
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
			reportDoc, fetched, err := c.get(ctx, source)
			if err != nil {
				parseErr = err
				return
			}
			report, err := parseLPR(reportDoc, date)
			if err != nil {
				parseErr = fmt.Errorf("%s: %w", source, err)
				return
			}
			if previous, ok := reports[period]; ok {
				if previous.QuoteDate != report.QuoteDate || *previous.OneYearRate != *report.OneYearRate || *previous.FiveYearPlusRate != *report.FiveYearPlusRate {
					parseErr = fmt.Errorf("LPR 同月公告冲突: %s", period)
				}
				return
			}
			report.Source, report.FetchedAt = source, &fetched
			reports[period] = report
		})
		if parseErr != nil {
			return nil, parseErr
		}
		if count == 0 {
			return nil, fmt.Errorf("LPR 目录为空或公告标题格式变化")
		}
		if older {
			return reports, nil
		}
		next := ""
		var nextErr error
		doc.Find("a").Each(func(_ int, a *goquery.Selection) {
			if compact(a.Text()) != "下一页" {
				return
			}
			href := a.AttrOr("tagname", a.AttrOr("href", ""))
			if href != "" && !strings.HasPrefix(href, "[") {
				next, nextErr = c.resolve(address, href)
			}
		})
		if nextErr != nil {
			return nil, nextErr
		}
		if next == "" {
			numbers := lprPageNumbers.FindStringSubmatch(compact(doc.Text()))
			if numbers != nil && numbers[1] == numbers[2] {
				return reports, nil
			}
			return nil, fmt.Errorf("LPR 目录缺少下一页且无法确认已到末页")
		}
		address = next
	}
	return nil, fmt.Errorf("LPR 公告超过100页")
}

// LPR 采集改革后的双期限月度报价，并按相邻月份计算变化；不跨缺口或旧机制计算。
func (c *Client) LPR(ctx context.Context, req LPRRequest) ([]LPRObservation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	start, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	earliest := start.AddDate(0, -1, 0).Format("200601")
	if earliest < LPRCoverageStart {
		earliest = LPRCoverageStart
	}
	reports, err := c.lprReports(ctx, earliest, req.End)
	if err != nil {
		return nil, err
	}
	items := []LPRObservation{}
	for date := start; !date.After(end); date = date.AddDate(0, 1, 0) {
		period := date.Format("200601")
		item, found := reports[period]
		item.Region, item.Period, item.Unit, item.Definition = "CN", period, "%", "lpr-reform-2019"
		item.ChangeMethod, item.ChangeStatus = "unavailable", "unknown"
		if !found {
			item.ChangeStatus, item.Note = "missing-release", "当前LPR目录未检索到该月份公告；报价和变化保留空值"
		} else if previous, ok := reports[date.AddDate(0, -1, 0).Format("200601")]; ok {
			one := math.Round((*item.OneYearRate-*previous.OneYearRate)*100*1e6) / 1e6
			five := math.Round((*item.FiveYearPlusRate-*previous.FiveYearPlusRate)*100*1e6) / 1e6
			item.OneYearChangeBP, item.FiveYearPlusChangeBP = &one, &five
			item.PreviousQuoteDate, item.PreviousSource = previous.QuoteDate, previous.Source
			item.ChangeMethod, item.ChangeStatus = "difference-of-adjacent-monthly-releases", "unchanged"
			if one != 0 || five != 0 {
				item.ChangeStatus = "changed"
			}
		} else {
			item.Note = "缺少相邻上月同口径报价，基点变化不计算"
		}
		items = append(items, item)
	}
	return items, nil
}
