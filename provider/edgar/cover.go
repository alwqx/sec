package edgar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/watchlist"
)

// coverWorkers 是同时下载封面页的数量，总请求速度仍受 wait 限制。
const coverWorkers = 4

// coverCacheVersion 是封面解析结果的缓存版本，解析规则变化时加一，旧缓存不再使用、重新读取。
const coverCacheVersion = "v2"

// coverRetryDays 内申报的封面读不到（HTTP 404，SEC 刚收到申报时 R 页面可能还没生成）或解析为空时不缓存，以后再读。
const coverRetryDays = 30

// CoverResult 是 CoverShares 的结果。Failed 是读取失败、被跳过的申报数，大于 0 时历史股数可能有缺口。
type CoverResult struct {
	Points []ClassShares
	Failed int
}

// ClassShares 是一次申报封面上按股票类别列出的流通股数：Date 是股数的统计日，Filed 是申报日。
type ClassShares struct {
	Date    string       `json:"date"`
	Filed   string       `json:"filed"`
	Classes []ShareClass `json:"classes"`
}

// ShareClass 是一类股票的流通股数。Member 是 XBRL 的类别成员，比如 us-gaap_CommonClassBMember，
// 不分类别时为空；Symbol 是封面上的交易代码，2019 年以前的封面没有。
type ShareClass struct {
	Member string  `json:"member"`
	Symbol string  `json:"symbol,omitempty"`
	Shares float64 `json:"shares"`
}

// CoverShares 读取 since 之后各份 10-K、10-Q 封面页（申报目录里的 R1.htm）上按类别列出的流通股数，按统计日升序。
// companyfacts 不含按类别申报的数值，伯克希尔、Visa 这类公司只能从每份申报读取。申报发布后不再变化，
// 解析结果按申报号缓存在磁盘上。个别申报读取失败时跳过并计入 Failed；全部失败或上下文被取消时返回错误。
func (c *Client) CoverShares(ctx context.Context, cik int, since string) (*CoverResult, error) {
	result, err := c.Filings(ctx, cik, FilingQuery{Forms: []string{"10-K", "10-Q"}, Begin: since})
	if err != nil {
		return nil, err
	}
	dir, err := c.cacheDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "covers", coverCacheVersion, strconv.Itoa(cik))
	var (
		mu       sync.Mutex
		points   []ClassShares
		failed   int
		firstErr error
		wg       sync.WaitGroup
	)
	jobs := make(chan Filing)
	for range coverWorkers {
		wg.Go(func() {
			for f := range jobs {
				p, err := c.coverShares(ctx, cik, dir, f)
				mu.Lock()
				if err != nil {
					failed++
					if firstErr == nil {
						firstErr = err
					}
				}
				if p != nil && len(p.Classes) > 0 {
					points = append(points, *p)
				}
				mu.Unlock()
			}
		})
	}
	for _, f := range result.Filings {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(points) == 0 && firstErr != nil {
		return nil, fmt.Errorf("读取 SEC 申报封面失败：%w", firstErr)
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Date != points[j].Date {
			return points[i].Date < points[j].Date
		}
		return points[i].Filed < points[j].Filed
	})
	return &CoverResult{Points: points, Failed: failed}, nil
}

// coverShares 读取一份申报的封面股数，先查缓存。较早的申报没有封面页（HTTP 404）或没有类别股数时也缓存空结果，
// 不再重复请求；coverRetryDays 内的申报不缓存空结果，R 页面生成后可以读到。
func (c *Client) coverShares(ctx context.Context, cik int, dir string, f Filing) (*ClassShares, error) {
	path := filepath.Join(dir, f.Accession+".json")
	if data, err := os.ReadFile(path); err == nil {
		var cached ClassShares
		if json.Unmarshal(data, &cached) == nil {
			return &cached, nil
		}
	}
	link := archivesURL + strconv.Itoa(cik) + "/" + strings.ReplaceAll(f.Accession, "-", "") + "/R1.htm"
	body, err := c.get(ctx, link)
	var notFound notFoundError
	if err != nil && !errors.As(err, &notFound) {
		return nil, err
	}
	p := &ClassShares{Filed: f.FilingDate, Classes: []ShareClass{}}
	if err == nil {
		p.Date, p.Classes = parseCover(string(body))
	}
	recent := true
	if filed, err := time.Parse(time.DateOnly, f.FilingDate); err == nil {
		recent = time.Since(filed) < coverRetryDays*24*time.Hour
	}
	if len(p.Classes) > 0 || !recent {
		if data, err := json.Marshal(p); err == nil && os.MkdirAll(dir, 0700) == nil {
			_ = watchlist.AtomicWrite(path, data)
		}
	}
	return p, nil
}

var (
	rowPattern    = regexp.MustCompile(`(?s)<tr[^>]*>.*?</tr>`)
	headerPattern = regexp.MustCompile(`(?s)<th([^>]*)>(.*?)</th>`)
	spanPattern   = regexp.MustCompile(`(rowspan|colspan)="?(\d+)`)
	cellPattern   = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	defrefPattern = regexp.MustCompile(`defref_([^']+)'`)
	tagPattern    = regexp.MustCompile(`<[^>]+>`)
	numberPattern = regexp.MustCompile(`^[0-9][0-9,]*(\.[0-9]+)?`)
)

// classAxis 是封面上区分股票类别的维度。
const classAxis = "us-gaap_StatementClassOfStockAxis="

// headerDates 按表头单元格的 rowspan、colspan 排出每一列的日期：列 0 是项目名，数值列从 1 开始。
// 常见的封面表头第一行是期间（比如 "6 Months Ended"），第二行是日期；也有把某一列的日期写在第一行、跨两行的。
// 同时读出表头注明的股数单位，比如 "shares in Thousands"。
func headerDates(rows []string, text func(string) string) (map[int]string, float64) {
	dates, unit := map[int]string{}, 1.0
	occupied := map[[2]int]bool{}
	for r, row := range rows {
		col := 0
		for _, h := range headerPattern.FindAllStringSubmatch(row, -1) {
			for occupied[[2]int{r, col}] {
				col++
			}
			rowspan, colspan := 1, 1
			for _, m := range spanPattern.FindAllStringSubmatch(h[1], -1) {
				if n, err := strconv.Atoi(m[2]); err == nil && n > 0 && n <= 50 {
					if m[1] == "rowspan" {
						rowspan = n
					} else {
						colspan = n
					}
				}
			}
			t := text(h[2])
			date, isDate := coverDate(t)
			for dr := range rowspan {
				for dc := range colspan {
					occupied[[2]int{r + dr, col + dc}] = true
					if isDate {
						dates[col+dc] = date
					}
				}
			}
			lower := strings.ToLower(t)
			if strings.Contains(lower, "shares in thousands") {
				unit = 1e3
			} else if strings.Contains(lower, "shares in millions") {
				unit = 1e6
			}
			col += colspan
		}
	}
	return dates, unit
}

// parseCover 解析封面页表格：数据行之前的行是表头（见 headerDates），每个类别以 "类别成员" 行开头，下面是该类别的流通股数和交易代码。
// 返回股数的统计日（各类别中最晚的一个）和各类别的股数；同一类别出现多次时保留第一次。
func parseCover(page string) (string, []ShareClass) {
	text := func(raw string) string {
		return strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(raw, "")))
	}
	rows := rowPattern.FindAllString(page, -1)
	header := 0
	for header < len(rows) && !defrefPattern.MatchString(rows[header]) {
		header++
	}
	dates, unit := headerDates(rows[:header], text)
	classes, index := []ShareClass{}, map[string]int{}
	member, skip, latest := "", false, ""
	classAt := func(member string) int {
		if k, ok := index[member]; ok {
			return k
		}
		index[member] = len(classes)
		classes = append(classes, ShareClass{Member: member})
		return len(classes) - 1
	}
	for _, row := range rows[header:] {
		m := defrefPattern.FindStringSubmatch(row)
		if m == nil {
			continue
		}
		name := m[1]
		cells := cellPattern.FindAllStringSubmatch(row, -1)
		switch {
		case strings.Contains(name, "Axis="):
			// 只认股票类别维度；其他维度（比如共同申报的子公司）下的股数不计入。
			member, skip = strings.TrimPrefix(name, classAxis), !strings.HasPrefix(name, classAxis) || strings.Contains(name[len(classAxis):], "Axis=")
		case name == "dei_EntityCommonStockSharesOutstanding" && !skip:
			for j := 1; j < len(cells); j++ {
				number := numberPattern.FindString(text(cells[j][1]))
				shares, err := strconv.ParseFloat(strings.ReplaceAll(number, ",", ""), 64)
				if err != nil || shares <= 0 {
					continue
				}
				if k := classAt(member); classes[k].Shares == 0 {
					classes[k].Shares = shares * unit
					if date, ok := dates[j]; ok {
						latest = max(latest, date)
					}
				}
				break
			}
		case name == "dei_TradingSymbol" && member != "" && !skip:
			// 交易代码可能写在股数前面，也可能在后面，也不一定在第一列数值里，取第一个非空的。
			if k := classAt(member); classes[k].Symbol == "" {
				for j := 1; j < len(cells); j++ {
					if symbol := text(cells[j][1]); symbol != "" {
						classes[k].Symbol = symbol
						break
					}
				}
			}
		}
	}
	// 只有交易代码、没有股数的类别（比如债券）去掉。
	result := []ShareClass{}
	for _, c := range classes {
		if c.Shares > 0 {
			result = append(result, c)
		}
	}
	return latest, result
}

// coverDate 解析封面表头的日期，比如 "Jul. 29, 2026"、"May 01, 2019"。
func coverDate(text string) (string, bool) {
	for _, layout := range []string{"Jan. 2, 2006", "Jan 2, 2006"} {
		if t, err := time.Parse(layout, text); err == nil {
			return t.Format(time.DateOnly), true
		}
	}
	return "", false
}
