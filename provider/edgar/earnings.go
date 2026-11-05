package edgar

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// earningsCacheVersion 是利润表解析结果的缓存版本，解析规则变化时加一，旧缓存不再使用、重新读取。
const earningsCacheVersion = "v2"

// ClassEarnings 是一份 10-K 利润表上一类股票一个财年的每股收益和加权股数。Member 是 XBRL 的类别成员，
// 比如 us-gaap_CommonClassAMember；EPS、Shares 优先用稀释数，只列基本数的（比如伯克希尔，两者相同）用基本数。
type ClassEarnings struct {
	End    string  `json:"end"`
	Filed  string  `json:"filed"`
	Member string  `json:"member"`
	EPS    float64 `json:"eps"`
	Shares float64 `json:"shares"`
}

// EarningsResult 是 ClassEarnings 的结果，按截止日升序。Failed 是读取失败、被跳过的年报数。
type EarningsResult struct {
	Earnings []ClassEarnings
	Failed   int
}

// ClassEarnings 读取 since 之后各份 10-K 利润表上按类别列出的每股收益和加权股数。伯克希尔、Visa 这类公司的每股收益
// 只按类别申报，companyfacts 不收录；利润表页从申报目录的 FilingSummary.xml 找到。解析结果按申报号缓存。
// 个别年报读取失败时跳过并计入 Failed；全部失败或上下文被取消时返回错误。
func (c *Client) ClassEarnings(ctx context.Context, cik int, since string) (*EarningsResult, error) {
	result, err := c.Filings(ctx, cik, FilingQuery{Forms: []string{"10-K"}, Begin: since})
	if err != nil {
		return nil, err
	}
	dir, err := c.cacheDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "earnings", earningsCacheVersion, strconv.Itoa(cik))
	all, failed, firstErr := readFilings(result.Filings, func(f Filing) ([]ClassEarnings, error) {
		return cachedFiling(dir, f, func() ([]ClassEarnings, bool, error) {
			earnings, err := c.filingEarnings(ctx, cik, f)
			return earnings, len(earnings) > 0, err
		})
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	earnings := []ClassEarnings{}
	for _, list := range all {
		earnings = append(earnings, list...)
	}
	if len(earnings) == 0 && firstErr != nil {
		return nil, fmt.Errorf("读取 SEC 年报利润表失败：%w", firstErr)
	}
	sort.SliceStable(earnings, func(i, j int) bool {
		if earnings[i].End != earnings[j].End {
			return earnings[i].End < earnings[j].End
		}
		if earnings[i].Filed != earnings[j].Filed {
			return earnings[i].Filed < earnings[j].Filed
		}
		return earnings[i].Member < earnings[j].Member
	})
	return &EarningsResult{Earnings: earnings, Failed: failed}, nil
}

// filingEarnings 读取一份 10-K 的利润表页。没有 FilingSummary.xml、找不到利润表（HTTP 404）时返回空。
func (c *Client) filingEarnings(ctx context.Context, cik int, f Filing) ([]ClassEarnings, error) {
	var notFound notFoundError
	body, err := c.get(ctx, filingFile(cik, f, "FilingSummary.xml"))
	if errors.As(err, &notFound) {
		return []ClassEarnings{}, nil
	}
	if err != nil {
		return nil, err
	}
	page := incomeStatementPage(body)
	if page == "" {
		return []ClassEarnings{}, nil
	}
	body, err = c.get(ctx, filingFile(cik, f, page))
	if errors.As(err, &notFound) {
		return []ClassEarnings{}, nil
	}
	if err != nil {
		return nil, err
	}
	earnings := parseClassEarnings(string(body))
	for i := range earnings {
		earnings[i].Filed = f.FilingDate
	}
	return earnings, nil
}

// incomeStatementPage 从 FilingSummary.xml 找到利润表页（比如 R4.htm）：报表类别里名称含 income、operations 或 earnings 的第一页，
// 不要附注括号页（Parenthetical），也不要股东权益表、现金流量表、资产负债表（比如 "Shareholders' Equity and Retained Earnings"）；
// 有单独的利润表时不用合并了综合收益的表。
func incomeStatementPage(summary []byte) string {
	var doc struct {
		Reports []struct {
			HTMLFileName string `xml:"HtmlFileName"`
			LongName     string `xml:"LongName"`
			MenuCategory string `xml:"MenuCategory"`
		} `xml:"MyReports>Report"`
	}
	if xml.Unmarshal(summary, &doc) != nil {
		return ""
	}
	combined := ""
	for _, r := range doc.Reports {
		name := strings.ToLower(r.LongName)
		if r.MenuCategory != "Statements" || r.HTMLFileName == "" ||
			!(strings.Contains(name, "income") || strings.Contains(name, "operations") || strings.Contains(name, "earnings")) {
			continue
		}
		if strings.Contains(name, "parenthetical") || strings.Contains(name, "equity") || strings.Contains(name, "retained") ||
			strings.Contains(name, "cash flow") || strings.Contains(name, "balance") {
			continue
		}
		if !strings.Contains(name, "comprehensive") {
			return r.HTMLFileName
		}
		if combined == "" {
			combined = r.HTMLFileName
		}
	}
	return combined
}

// earningsRows 是每股收益和加权股数的候选科目，按优先级排列。
var (
	epsConcepts    = []string{"us-gaap_EarningsPerShareDiluted", "us-gaap_EarningsPerShareBasicAndDiluted", "us-gaap_EarningsPerShareBasic"}
	sharesConcepts = []string{"us-gaap_WeightedAverageNumberOfDilutedSharesOutstanding", "us-gaap_WeightedAverageNumberOfShareOutstandingBasicAndDiluted", "us-gaap_WeightedAverageNumberOfSharesOutstandingBasic"}
)

// amountPattern 匹配利润表里的数值，比如 "$ 10.20"、"1,966"、"$ (10.63)"，末尾可以带脚注标记，比如 "$ 5.63 [1]"。
var amountPattern = regexp.MustCompile(`^\$?\s*(\()?\s*([0-9][0-9,]*(\.[0-9]+)?)\s*\)?(\s*\[\d+\])*$`)

// headerUnitPattern 匹配不单独注明股数单位的表头，比如 "In Millions, except Per Share data"，这时股数也是这个单位。
var headerUnitPattern = regexp.MustCompile(`(?i)\bin (thousands|millions), except per share`)

// parseClassEarnings 解析利润表页：每个股票类别以 "类别成员" 行开头，下面是该类别的每股收益和加权股数；其他维度下的行不计入。
// 列的截止日见 headerDates；表头注明的股数单位（比如 "shares in Millions"）只用于股数。每股收益和股数都有的才返回。
func parseClassEarnings(page string) []ClassEarnings {
	text := func(raw string) string {
		return strings.TrimSpace(html.UnescapeString(tagPattern.ReplaceAllString(raw, "")))
	}
	rows := rowPattern.FindAllString(page, -1)
	header := 0
	for header < len(rows) && !defrefPattern.MatchString(rows[header]) {
		header++
	}
	dates, unit := headerDates(rows[:header], text)
	if unit == 1 {
		for _, row := range rows[:header] {
			if m := headerUnitPattern.FindStringSubmatch(text(row)); m != nil {
				unit = map[string]float64{"thousands": 1e3, "millions": 1e6}[strings.ToLower(m[1])]
				break
			}
		}
	}
	type key struct{ member, end string }
	type values struct {
		eps, shares         float64
		epsRank, sharesRank int // 候选科目的序号，越小越优先；0 表示还没有
	}
	found, order := map[key]*values{}, []key{}
	member := ""
	for _, row := range rows[header:] {
		m := defrefPattern.FindStringSubmatch(row)
		if m == nil {
			continue
		}
		name := m[1]
		if strings.Contains(name, "Axis=") {
			member = ""
			if rest, ok := strings.CutPrefix(name, classAxis); ok && !strings.Contains(rest, "Axis=") {
				member = rest
			}
			continue
		}
		epsRank, sharesRank := rank(epsConcepts, name), rank(sharesConcepts, name)
		if member == "" || (epsRank == 0 && sharesRank == 0) {
			continue
		}
		cells := cellPattern.FindAllStringSubmatch(row, -1)
		for j := 1; j < len(cells); j++ {
			end, ok := dates[j]
			number, valid := amount(text(cells[j][1]))
			if !ok || !valid {
				continue
			}
			k := key{member, end}
			v := found[k]
			if v == nil {
				v = &values{}
				found[k] = v
				order = append(order, k)
			}
			if epsRank > 0 && (v.epsRank == 0 || epsRank < v.epsRank) {
				v.eps, v.epsRank = number, epsRank
			}
			if sharesRank > 0 && (v.sharesRank == 0 || sharesRank < v.sharesRank) {
				v.shares, v.sharesRank = number*unit, sharesRank
			}
		}
	}
	result := []ClassEarnings{}
	for _, k := range order {
		if v := found[k]; v.epsRank > 0 && v.sharesRank > 0 && v.shares > 0 {
			result = append(result, ClassEarnings{End: k.end, Member: k.member, EPS: v.eps, Shares: v.shares})
		}
	}
	return result
}

// rank 返回 name 在候选科目中的序号（从 1 开始），不是候选科目时返回 0。
func rank(concepts []string, name string) int {
	for i, concept := range concepts {
		if concept == name {
			return i + 1
		}
	}
	return 0
}

// amount 解析利润表里的数值，括号表示负数。
func amount(text string) (float64, bool) {
	m := amountPattern.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", ""), 64)
	if err != nil {
		return 0, false
	}
	if m[1] != "" {
		v = -v
	}
	return v, true
}
