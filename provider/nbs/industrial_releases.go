package nbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alwqx/sec/utils"
)

type industrialRelease struct {
	Title     string `json:"title"`
	URL       string `json:"external_link"`
	Published string `json:"pb_time"`
}

// 月报标题有“9月份”和“1—4月份”两种形式，均指向该报告的截至月。
var industrialTitlePattern = regexp.MustCompile(`^(\d{4})年(?:1[—－–\-至])?(\d{1,2})月份.*规模以上工业增加值`)
var releaseYearPattern = regexp.MustCompile(`^(\d{4})年$`)
var releaseMonthPattern = regexp.MustCompile(`^(\d{1,2})月$`)

func releasePeriod(title string) string {
	m := industrialTitlePattern.FindStringSubmatch(compact(title))
	if m == nil {
		return ""
	}
	n, _ := strconv.Atoi(m[2])
	if n < 1 || n > 12 {
		return ""
	}
	return fmt.Sprintf("%s%02d", m[1], n)
}

func (c *Client) supplementIndustrialMoM(ctx context.Context, items []IndustrialObservation) error {
	seenURLs := map[string]bool{}
	for page := 1; page <= 100; page++ {
		var feed struct {
			Data []industrialRelease `json:"data"`
		}
		_, err := c.request(ctx, "/new/queryCMSArticles", url.Values{"code": {"zxfb"}, "pagenum": {strconv.Itoa(page)}, "pageSize": {"100"}}, nil, &feed)
		if err != nil {
			return fmt.Errorf("查询工业增加值发布稿失败: %w", err)
		}
		if len(feed.Data) == 0 {
			break
		}
		sort.SliceStable(feed.Data, func(i, j int) bool { return feed.Data[i].Published > feed.Data[j].Published })
		olderThanRange := false
		for _, release := range feed.Data {
			period := releasePeriod(release.Title)
			if period == "" {
				continue
			}
			if period < items[0].Period {
				olderThanRange = true
				continue
			}
			if seenURLs[release.URL] {
				continue
			}
			seenURLs[release.URL] = true
			end, _ := time.Parse("200601", period)
			windowStart := end.AddDate(0, -12, 0).Format("200601")
			needed := false
			for _, item := range items {
				if item.MoM == nil && item.Period >= windowStart && item.Period <= period {
					needed = true
					break
				}
			}
			if !needed {
				continue
			}
			published, err := time.ParseInLocation("2006-01-02 15:04", release.Published, time.FixedZone("CST", 8*60*60))
			if err != nil {
				return fmt.Errorf("无效的工业增加值公布时间: %s", release.Published)
			}
			rates, fetched, err := c.industrialReleaseRates(ctx, release.URL)
			if err != nil {
				return err
			}
			for i := range items {
				item := &items[i]
				if item.MoM != nil {
					continue
				} // 按公布时间从新到旧，保留首次找到的最新可用修订。
				if rate, ok := rates[item.Period]; ok {
					value := rate
					item.MoM = &value
					item.MoMSource = release.URL
					item.MoMPublishedAt = &published
					item.MoMFetchedAt = &fetched
				}
			}
		}
		complete := true
		for _, item := range items {
			if item.MoM == nil {
				complete = false
				break
			}
		}
		if complete {
			return nil
		}
		if olderThanRange || len(feed.Data) < 100 {
			break
		}
		if page == 100 {
			return fmt.Errorf("工业增加值发布稿超过 100 页，请缩小查询范围")
		}
	}
	for i := range items {
		if items[i].MoM == nil {
			if items[i].Note != "" {
				items[i].Note += "；"
			}
			items[i].Note += IndustrialMoMMissingNote
		}
	}
	return nil
}

func (c *Client) industrialReleaseRates(ctx context.Context, source string) (map[string]float64, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Host != "www.stats.gov.cn" || u.User != nil {
		return nil, time.Time{}, fmt.Errorf("无效的国家统计局发布稿链接")
	}
	key := sha256.Sum256([]byte("industrial-release\n" + source))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, err := os.ReadFile(path); err == nil {
			var entry cacheEntry
			var html string
			if json.Unmarshal(raw, &entry) == nil && time.Since(entry.FetchedAt) >= 0 && time.Since(entry.FetchedAt) < 6*time.Hour && json.Unmarshal(entry.Body, &html) == nil {
				if rates, err := parseIndustrialMoM(html); err == nil {
					return rates, entry.FetchedAt, nil
				}
			}
		}
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, source, nil, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("请求工业增加值发布稿失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("工业增加值发布稿返回 HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, time.Time{}, err
	}
	rates, err := parseIndustrialMoM(string(raw))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("解析工业增加值环比失败（%s）: %w", source, err)
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		body, _ := json.Marshal(string(raw))
		if err := c.saveCache(path, cacheEntry{FetchedAt: fetched, Body: body}); err != nil {
			return nil, time.Time{}, err
		}
	}
	return rates, fetched, nil
}

// parseIndustrialMoM 仅解析带“环比增速”标题的表格，处理跨行年份及桌面/移动重复表格。
func parseIndustrialMoM(html string) (map[string]float64, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	rates := map[string]float64{}
	var parseErr error
	doc.Find("table").Each(func(_ int, table *goquery.Selection) {
		if !strings.Contains(compact(table.Text()), "环比增速") {
			return
		}
		year := ""
		table.Find("tr").Each(func(_ int, row *goquery.Selection) {
			if parseErr != nil {
				return
			}
			var cells []string
			row.ChildrenFiltered("td,th").Each(func(_ int, cell *goquery.Selection) { cells = append(cells, compact(cell.Text())) })
			month := 0
			for _, cell := range cells {
				if m := releaseYearPattern.FindStringSubmatch(cell); m != nil {
					year = m[1]
				}
				if m := releaseMonthPattern.FindStringSubmatch(cell); m != nil {
					month, _ = strconv.Atoi(m[1])
				}
			}
			if month == 0 {
				return
			}
			if year == "" || month > 12 || len(cells) < 2 {
				parseErr = fmt.Errorf("环比表月份或年份无效")
				return
			}
			text := strings.TrimSuffix(strings.TrimSuffix(cells[len(cells)-1], "%"), "％")
			raw, _ := json.Marshal(text)
			n, err := parseNumber(raw)
			if err != nil {
				parseErr = err
				return
			}
			if n == nil {
				return
			}
			period := fmt.Sprintf("%s%02d", year, month)
			if old, ok := rates[period]; ok && old != *n {
				parseErr = fmt.Errorf("环比表同月数值冲突: %s", period)
				return
			}
			rates[period] = *n
		})
	})
	if parseErr != nil {
		return nil, parseErr
	}
	if len(rates) == 0 {
		return nil, fmt.Errorf("发布稿缺少可解析的环比增速表")
	}
	return rates, nil
}
