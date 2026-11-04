// Package edgar 读取美国证监会 EDGAR 的官方数据：股票代码对照表、公司申报列表和申报原文。
// SEC 要求自动程序在 User-Agent 里写明身份和联系邮箱，并限制每秒最多 10 个请求。
// 文档：https://www.sec.gov/search-filings/edgar-application-programming-interfaces
package edgar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/version"
	"github.com/alwqx/sec/watchlist"
)

const (
	tickersURL     = "https://www.sec.gov/files/company_tickers.json"
	submissionsURL = "https://data.sec.gov/submissions/"
	archivesURL    = "https://www.sec.gov/Archives/edgar/data/"
	// tickersTTL 是代码对照表的缓存时间，SEC 每天更新一次。
	tickersTTL = 24 * time.Hour
	// minInterval 让请求不超过每秒 10 个。
	minInterval = 110 * time.Millisecond
)

// ErrNotFound 表示 SEC 没有这个股票代码，比如只在美国上市交易、不向 SEC 申报的证券。
var ErrNotFound = errors.New("SEC EDGAR 没有这个股票代码")

// Client 访问 EDGAR。Email 是写在 User-Agent 里的联系邮箱。
type Client struct {
	Email string
	// CacheDir 为空时使用 ~/.sec/cache/edgar。
	CacheDir string
}

var throttle struct {
	sync.Mutex
	last time.Time
}

// wait 保证两次请求之间至少间隔 minInterval，多个 goroutine 共用。
func wait(ctx context.Context) error {
	throttle.Lock()
	next := throttle.last.Add(minInterval)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	throttle.last = next
	throttle.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Until(next)):
		return nil
	}
}

func (c *Client) userAgent() string { return "alwqx-sec/" + version.Version + " " + c.Email }

// get 请求 EDGAR，返回正文。403 时说明多半是联系邮箱缺失或被拒绝。
func (c *Client) get(ctx context.Context, link string) ([]byte, error) {
	if c.Email == "" {
		return nil, errors.New("没有配置 SEC 联系邮箱，请用 sec config set sec-edgar-email 设置")
	}
	if err := wait(ctx); err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("User-Agent", c.userAgent())
	resp, err := utils.MakeRequest(ctx, http.MethodGet, link, headers, nil, 60*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("SEC EDGAR 拒绝了请求（HTTP 403），请确认联系邮箱 %s 有效，或稍后再试", c.Email)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("SEC EDGAR 找不到 %s（HTTP 404）", link)
	}
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) cacheDir() (string, error) {
	if c.CacheDir != "" {
		return c.CacheDir, nil
	}
	return utils.SecDir("cache", "edgar")
}

// Company 是一家向 SEC 申报的公司。
type Company struct {
	CIK    int    `json:"cik"`
	Ticker string `json:"ticker"`
	Name   string `json:"name"`
}

// NormalizeTicker 把 $AAPL、brk.b 这样的写法转成 SEC 的格式：大写，类别股用连字符，比如 BRK-B。
func NormalizeTicker(ticker string) string {
	ticker = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ticker), "$")))
	return strings.ReplaceAll(ticker, ".", "-")
}

// tickerCache 在进程内保存解析好的对照表，Desktop 翻页时不用每次读取、解析约 1 MB 的文件。
var tickerCache struct {
	sync.Mutex
	dir       string
	loadedAt  time.Time
	companies map[string]Company
}

// tickers 读取代码对照表：进程内和磁盘都缓存 24 小时。缓存过期时重新下载，下载失败时用旧缓存；
// 磁盘缓存解析失败（比如写到一半）时也重新下载。
func (c *Client) tickers(ctx context.Context) (map[string]Company, error) {
	dir, err := c.cacheDir()
	if err != nil {
		return nil, err
	}
	tickerCache.Lock()
	defer tickerCache.Unlock()
	if tickerCache.dir == dir && tickerCache.companies != nil && time.Since(tickerCache.loadedAt) < tickersTTL {
		return tickerCache.companies, nil
	}
	path := filepath.Join(dir, "company_tickers.json")
	var cached map[string]Company
	info, statErr := os.Stat(path)
	if data, err := os.ReadFile(path); err == nil {
		cached, _ = parseTickers(data)
	}
	companies, loadedAt := cached, time.Time{}
	if statErr == nil {
		loadedAt = info.ModTime()
	}
	if cached == nil || time.Since(loadedAt) > tickersTTL {
		fresh, err := c.get(ctx, tickersURL)
		var parsed map[string]Company
		if err == nil {
			parsed, err = parseTickers(fresh)
		}
		switch {
		case err == nil:
			companies, loadedAt = parsed, time.Now()
			if mkErr := os.MkdirAll(dir, 0700); mkErr == nil {
				_ = watchlist.AtomicWrite(path, fresh)
			}
		case cached == nil:
			return nil, fmt.Errorf("下载 SEC 股票代码对照表失败：%w", err)
		}
	}
	tickerCache.dir, tickerCache.loadedAt, tickerCache.companies = dir, loadedAt, companies
	return companies, nil
}

// parseTickers 解析对照表。文件是以 "0"、"1"… 为键的对象，按 SEC 的排列顺序处理：
// 同一个代码对应多家公司时保留排在前面的，结果不随 map 的遍历顺序变化。
func parseTickers(data []byte) (map[string]Company, error) {
	var raw map[string]struct {
		CIK    int    `json:"cik_str"`
		Ticker string `json:"ticker"`
		Title  string `json:"title"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("SEC 股票代码对照表格式无效：%w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("SEC 股票代码对照表为空")
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, errA := strconv.Atoi(keys[i])
		b, errB := strconv.Atoi(keys[j])
		if errA != nil || errB != nil {
			return keys[i] < keys[j]
		}
		return a < b
	})
	companies := make(map[string]Company, len(raw))
	for _, key := range keys {
		item := raw[key]
		ticker := NormalizeTicker(item.Ticker)
		if _, ok := companies[ticker]; !ok {
			companies[ticker] = Company{CIK: item.CIK, Ticker: ticker, Name: item.Title}
		}
	}
	return companies, nil
}

// Lookup 按股票代码查找公司。
func (c *Client) Lookup(ctx context.Context, ticker string) (Company, error) {
	companies, err := c.tickers(ctx)
	if err != nil {
		return Company{}, err
	}
	company, ok := companies[NormalizeTicker(ticker)]
	if !ok {
		return Company{}, fmt.Errorf("%w：%s", ErrNotFound, ticker)
	}
	return company, nil
}

// Filing 是一份申报。
type Filing struct {
	Form        string `json:"form"`
	FilingDate  string `json:"filing_date"`
	ReportDate  string `json:"report_date"` // 报告期末，8-K 等没有报告期的申报为空
	Accession   string `json:"accession"`
	Items       string `json:"items"`       // 8-K 事项编号，比如 2.02,9.01
	Description string `json:"description"` // 中文说明，见 Describe；不认识的类型用 SEC 的原文说明
	Size        int64  `json:"size"`
	URL         string `json:"url"`       // 主文件
	IndexURL    string `json:"index_url"` // 申报目录，包含全部附件
}

// Profile 是申报列表里的公司信息。
type Profile struct {
	CIK           int          `json:"cik"`
	Name          string       `json:"name"`
	Tickers       []string     `json:"tickers"`
	Exchanges     []string     `json:"exchanges"`
	Industry      string       `json:"industry"`
	SIC           string       `json:"sic"`
	FiscalYearEnd string       `json:"fiscal_year_end"` // MMDD
	Category      string       `json:"category"`        // 申报人类别，比如 Large accelerated filer
	Incorporation string       `json:"state_of_incorporation"`
	Address       string       `json:"business_address"`
	Phone         string       `json:"phone"`
	Website       string       `json:"website"`
	FormerNames   []FormerName `json:"former_names"`
}

// FormerName 是公司曾用名，日期为 YYYY-MM-DD。
type FormerName struct {
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

type address struct {
	Street1, Street2, City, StateOrCountry, ZipCode string
	Country                                         string
}

func (a address) String() string {
	parts := []string{}
	for _, part := range []string{a.Street1, a.Street2, a.City, strings.TrimSpace(a.StateOrCountry + " " + a.ZipCode), a.Country} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

// recentFilings 是 submissions 中按列存储的申报列表。
type recentFilings struct {
	Accession       []string `json:"accessionNumber"`
	FilingDate      []string `json:"filingDate"`
	ReportDate      []string `json:"reportDate"`
	Form            []string `json:"form"`
	Size            []int64  `json:"size"`
	PrimaryDocument []string `json:"primaryDocument"`
	Description     []string `json:"primaryDocDescription"`
	Items           []string `json:"items"`
}

type submissions struct {
	CIK           string   `json:"cik"`
	Name          string   `json:"name"`
	Tickers       []string `json:"tickers"`
	Exchanges     []string `json:"exchanges"`
	Industry      string   `json:"sicDescription"`
	SIC           string   `json:"sic"`
	FiscalYearEnd string   `json:"fiscalYearEnd"`
	Category      string   `json:"category"`
	Incorporation string   `json:"stateOfIncorporationDescription"`
	Phone         string   `json:"phone"`
	Website       string   `json:"website"`
	Addresses     struct {
		Business address `json:"business"`
	} `json:"addresses"`
	FormerNames []struct {
		Name     string `json:"name"`
		From, To string
	} `json:"formerNames"`
	Filings struct {
		Recent recentFilings `json:"recent"`
		Files  []struct {
			Name       string `json:"name"`
			FilingFrom string `json:"filingFrom"`
		} `json:"files"`
	} `json:"filings"`
}

// DefaultForms 是默认列出的申报类型：年报、季报、重大事项、外国公司年报和临时报告、委托书、上市招股书。
// Form 4 等内部人交易数量很多，需要时单独指定。CLI 和 Desktop 共用。
var DefaultForms = []string{"10-K", "10-Q", "8-K", "20-F", "6-K", "40-F", "DEF 14A", "S-1", "F-1"}

// maxOlderPages 是没有指定开始日期时，为凑够结果最多再读的历史分页数。
// 高频申报的公司（比如高盛一年 1.6 万份）最近列表只覆盖约一年，按类型筛选时需要往前读。
const maxOlderPages = 10

// FilingQuery 筛选申报。Forms 为空时不按类型筛选；指定 10-K 时也包含修订版 10-K/A。日期为 YYYY-MM-DD，留空不限。
// Want 是希望得到的条数：最近列表里不够时继续读历史分页，最多 maxOlderPages 页；指定 Begin 时一直读到该日期。
type FilingQuery struct {
	Forms      []string
	Begin, End string
	Want       int
}

// FilingsResult 是查询结果。Truncated 表示读到分页上限时还有更早的申报没有读，结果可能不全。
type FilingsResult struct {
	Profile   *Profile
	Filings   []Filing
	Truncated bool
}

func (q FilingQuery) match(form, date string) bool {
	if (q.Begin != "" && date < q.Begin) || (q.End != "" && date > q.End) {
		return false
	}
	if len(q.Forms) == 0 {
		return true
	}
	for _, want := range q.Forms {
		if strings.EqualFold(form, want) || strings.EqualFold(form, want+"/A") {
			return true
		}
	}
	return false
}

func (r recentFilings) filings(cik int, q FilingQuery) []Filing {
	result := []Filing{}
	for i := range r.Accession {
		if i >= len(r.Form) || i >= len(r.FilingDate) || !q.match(r.Form[i], r.FilingDate[i]) {
			continue
		}
		f := Filing{Form: r.Form[i], FilingDate: r.FilingDate[i], Accession: r.Accession[i]}
		if i < len(r.ReportDate) {
			f.ReportDate = r.ReportDate[i]
		}
		if i < len(r.Items) {
			f.Items = r.Items[i]
		}
		if f.Description = Describe(f.Form, f.Items); f.Description == "" && i < len(r.Description) {
			f.Description = r.Description[i]
		}
		if i < len(r.Size) {
			f.Size = r.Size[i]
		}
		folder := archivesURL + strconv.Itoa(cik) + "/" + strings.ReplaceAll(f.Accession, "-", "") + "/"
		f.IndexURL = folder + f.Accession + "-index.htm"
		f.URL = f.IndexURL
		if i < len(r.PrimaryDocument) && r.PrimaryDocument[i] != "" {
			f.URL = folder + r.PrimaryDocument[i]
		}
		result = append(result, f)
	}
	return result
}

func (data *submissions) profile(cik int) *Profile {
	p := &Profile{CIK: cik, Name: data.Name, Tickers: data.Tickers, Exchanges: unique(data.Exchanges), Industry: data.Industry, SIC: data.SIC,
		FiscalYearEnd: data.FiscalYearEnd, Category: data.Category, Incorporation: data.Incorporation, Address: data.Addresses.Business.String(),
		Phone: data.Phone, Website: data.Website, FormerNames: []FormerName{}}
	day := func(text string) string {
		if len(text) >= 10 {
			return text[:10]
		}
		return text
	}
	for _, name := range data.FormerNames {
		p.FormerNames = append(p.FormerNames, FormerName{name.Name, day(name.From), day(name.To)})
	}
	return p
}

// Profile 返回公司信息。和 Filings 读的是同一个文件，只需要公司信息时用它。
func (c *Client) Profile(ctx context.Context, cik int) (*Profile, error) {
	body, err := c.get(ctx, fmt.Sprintf("%sCIK%010d.json", submissionsURL, cik))
	if err != nil {
		return nil, err
	}
	var data submissions
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("SEC 申报列表格式无效：%w", err)
	}
	return data.profile(cik), nil
}

// Filings 返回公司的申报，按申报日期从新到旧。SEC 的申报列表默认只有最近约 1000 份或一年，
// 查询开始日期更早，或按类型筛选后不够 Want 条时，再读取历史分页。
func (c *Client) Filings(ctx context.Context, cik int, q FilingQuery) (*FilingsResult, error) {
	body, err := c.get(ctx, fmt.Sprintf("%sCIK%010d.json", submissionsURL, cik))
	if err != nil {
		return nil, err
	}
	var data submissions
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("SEC 申报列表格式无效：%w", err)
	}
	result := &FilingsResult{Profile: data.profile(cik)}
	result.Filings = data.Filings.Recent.filings(cik, q)
	oldest := ""
	if dates := data.Filings.Recent.FilingDate; len(dates) > 0 {
		oldest = dates[len(dates)-1]
	}
	needOlder := func() bool {
		if q.Begin != "" {
			return oldest != "" && q.Begin < oldest
		}
		return q.Want > 0 && len(result.Filings) < q.Want
	}
	for i, file := range data.Filings.Files {
		if !needOlder() {
			break
		}
		if q.Begin == "" && i >= maxOlderPages {
			result.Truncated = true
			break
		}
		body, err := c.get(ctx, submissionsURL+file.Name)
		if err != nil {
			return nil, err
		}
		var older recentFilings
		if err := json.Unmarshal(body, &older); err != nil {
			return nil, fmt.Errorf("SEC 历史申报列表格式无效：%w", err)
		}
		result.Filings = append(result.Filings, older.filings(cik, q)...)
		// 历史分页从新到旧，filingFrom 是该页最早的申报日期。
		if file.FilingFrom != "" {
			oldest = file.FilingFrom
		}
	}
	sort.SliceStable(result.Filings, func(i, j int) bool { return result.Filings[i].FilingDate > result.Filings[j].FilingDate })
	return result, nil
}

// IsDocumentURL 判断链接是否是 SEC 申报目录下的文件：https、www.sec.gov、路径在 /Archives/edgar/data/ 下，
// 不含 .. 等需要规范化的部分。
func IsDocumentURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.sec.gov" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return path.Clean(u.Path) == u.Path && !strings.Contains(u.Path, "..") && !strings.Contains(u.EscapedPath(), "%") && strings.HasPrefix(u.Path, "/Archives/edgar/data/")
}

// Download 下载申报文件，只允许 sec.gov 的申报目录。
func (c *Client) Download(ctx context.Context, link string) ([]byte, error) {
	if !IsDocumentURL(link) {
		return nil, fmt.Errorf("不是 SEC 申报文件地址：%s", link)
	}
	return c.get(ctx, link)
}

// unique 去掉重复项，保持原顺序。交易所列表按证券排列，同一交易所上市的优先股会重复出现。
func unique(items []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, item := range items {
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}
