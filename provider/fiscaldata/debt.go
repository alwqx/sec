// Package fiscaldata 采集美国财政部 Fiscal Data 的公开财政与债务数据。
package fiscaldata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const debtURL = "https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions"
const pageSize = 1000

type Client struct {
	CacheDir string
	Refresh  bool
}

func NewClient(dir string) *Client { return &Client{CacheDir: dir} }

// DebtRequest 按DTS记录日期查询，不能将其替换为拍卖公告日。
type DebtRequest struct{ Begin, End string }

func (r *DebtRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("国债交易请求不能为空")
	}
	for _, v := range []string{r.Begin, r.End} {
		d, e := time.Parse("20060102", v)
		if e != nil || d.Format("20060102") != v {
			return fmt.Errorf("国债交易日期须为YYYYMMDD：%s", v)
		}
	}
	if r.Begin > r.End || r.Begin < "20051003" || r.End > time.Now().UTC().Format("20060102") {
		return fmt.Errorf("国债交易支持20051003至当前UTC日期，开始日期不能晚于截至日期")
	}
	return nil
}

type DebtRow struct {
	Date        string            `json:"date"`
	Transaction string            `json:"transaction"`
	Market      string            `json:"market"`
	Security    string            `json:"security"`
	Description string            `json:"description"`
	Today       *float64          `json:"today_million_usd"`
	MTD         *float64          `json:"month_to_date_million_usd"`
	FYTD        *float64          `json:"fiscal_year_to_date_million_usd"`
	Raw         map[string]string `json:"raw_values"`
	Source      string            `json:"source"`
	FetchedAt   time.Time         `json:"fetched_at"`
}
type DebtSummary struct {
	Date        string   `json:"date"`
	Market      string   `json:"market"`
	Issues      *float64 `json:"issues_million_usd"`
	Redemptions *float64 `json:"redemptions_million_usd"`
	Net         *float64 `json:"net_recorded_issuance_million_usd"`
	IsDerived   bool     `json:"is_derived"`
	InputRows   int      `json:"input_rows"`
	// DuplicateRows 是未计入汇总的上游重复行数，这些行仍保留在Rows中。
	DuplicateRows int `json:"duplicate_rows_excluded,omitempty"`
}
type DebtResult struct {
	Region      string        `json:"region"`
	Indicator   string        `json:"indicator"`
	Frequency   string        `json:"frequency"`
	DateBasis   string        `json:"date_basis"`
	Unit        string        `json:"unit"`
	Begin       string        `json:"begin"`
	End         string        `json:"end"`
	Source      string        `json:"source"`
	FetchedAt   time.Time     `json:"fetched_at"`
	PublishedAt *time.Time    `json:"published_at"`
	RevisionID  *string       `json:"revision_id"`
	DataVersion string        `json:"data_version"`
	Rows        []DebtRow     `json:"rows"`
	Summaries   []DebtSummary `json:"summaries"`
	Note        string        `json:"note"`
}

type envelope struct {
	Data []map[string]string `json:"data"`
	Meta struct {
		Count   int               `json:"count"`
		Total   int               `json:"total-count"`
		Pages   int               `json:"total-pages"`
		Formats map[string]string `json:"dataFormats"`
	} `json:"meta"`
}

func number(raw string) (*float64, error) {
	if raw == "null" || raw == "" {
		return nil, nil
	}
	n, e := strconv.ParseFloat(raw, 64)
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("财政部交易金额无效：%s", raw)
	}
	return &n, nil // 官方更正、通胀及利息调整可能为负数，不能拒绝或取绝对值。
}

func parsePage(raw []byte, begin, end string, page int) (envelope, []DebtRow, error) {
	var env envelope
	env.Meta.Total, env.Meta.Count, env.Meta.Pages = -1, -1, -1
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, nil, fmt.Errorf("财政部债务响应无效：%w", err)
	}
	if env.Data == nil || env.Meta.Total < 0 || env.Meta.Pages > 300 || env.Meta.Pages != (env.Meta.Total+pageSize-1)/pageSize || page > max(1, env.Meta.Pages) || env.Meta.Count != len(env.Data) || len(env.Data) != min(pageSize, env.Meta.Total-(page-1)*pageSize) {
		return env, nil, fmt.Errorf("财政部债务分页计数无效")
	}
	for _, k := range []string{"transaction_today_amt", "transaction_mtd_amt", "transaction_fytd_amt"} {
		if env.Meta.Formats[k] != "$1,000,000" {
			return env, nil, fmt.Errorf("财政部交易金额单位变化")
		}
	}
	rows := []DebtRow{}
	for _, r := range env.Data {
		d, e := time.Parse("2006-01-02", r["record_date"])
		line, le := strconv.Atoi(r["src_line_nbr"])
		if e != nil || d.Format("20060102") < begin || d.Format("20060102") > end || le != nil || line <= 0 || r["table_nm"] != "Public Debt Transactions" || (r["table_nbr"] != "IIIA" && r["table_nbr"] != "III-A") {
			return env, nil, fmt.Errorf("财政部交易日期或表标识无效")
		}
		if (r["transaction_type"] != "Issues" && r["transaction_type"] != "Redemptions") || (r["security_market"] != "Marketable" && r["security_market"] != "Nonmarketable") || strings.TrimSpace(r["security_type"]) == "" || strings.Contains(strings.ToLower(r["security_type"]), "total") {
			return env, nil, fmt.Errorf("财政部交易分类变化，不能安全汇总")
		}
		item := DebtRow{Date: r["record_date"], Transaction: r["transaction_type"], Market: r["security_market"], Security: r["security_type"], Description: r["security_type_desc"], Raw: r}
		for _, f := range []struct {
			k string
			p **float64
		}{{"transaction_today_amt", &item.Today}, {"transaction_mtd_amt", &item.MTD}, {"transaction_fytd_amt", &item.FYTD}} {
			v, ok := r[f.k]
			if !ok {
				return env, nil, fmt.Errorf("财政部交易缺少金额字段")
			}
			n, err := number(v)
			if err != nil {
				return env, nil, err
			}
			*f.p = n
		}
		rows = append(rows, item)
	}
	return env, rows, nil
}

type cachedPage struct {
	URL       string    `json:"url"`
	FetchedAt time.Time `json:"fetched_at"`
	Body      []byte    `json:"body"`
}

func (c *Client) fetch(ctx context.Context, address string, validate func([]byte) error) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	key := sha256.Sum256([]byte(address))
	path := filepath.Join(c.CacheDir, hex.EncodeToString(key[:])+".json")
	if c.CacheDir != "" && !c.Refresh {
		if raw, e := os.ReadFile(path); e == nil {
			var old cachedPage
			if json.Unmarshal(raw, &old) == nil && old.URL == address && time.Since(old.FetchedAt) >= 0 && time.Since(old.FetchedAt) < 6*time.Hour && validate(old.Body) == nil {
				return old.FetchedAt, nil
			}
		}
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, address, nil, nil, 30*time.Second)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("美国财政部返回HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil {
		return time.Time{}, err
	}
	if len(raw) > 16<<20 {
		return time.Time{}, fmt.Errorf("财政部响应超过16MiB")
	}
	if err := validate(raw); err != nil {
		return time.Time{}, err
	}
	fetched := time.Now().UTC()
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0755); err != nil {
			return time.Time{}, err
		}
		data, err := json.Marshal(cachedPage{URL: address, FetchedAt: fetched, Body: raw})
		if err != nil {
			return time.Time{}, err
		}
		f, err := os.CreateTemp(c.CacheDir, ".debt-*")
		if err != nil {
			return time.Time{}, err
		}
		defer os.Remove(f.Name())
		_, we := f.Write(data)
		ce := f.Close()
		if we != nil {
			return time.Time{}, we
		}
		if ce != nil {
			return time.Time{}, ce
		}
		if err := os.Rename(f.Name(), path); err != nil {
			return time.Time{}, err
		}
	}
	return fetched, nil
}

func (c *Client) Debt(ctx context.Context, req *DebtRequest) (*DebtResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	begin, _ := time.Parse("20060102", req.Begin)
	end, _ := time.Parse("20060102", req.End)
	result := &DebtResult{Region: "US", Indicator: "treasury_debt_transactions", Frequency: "daily", DateBasis: "DTS record date", Unit: "百万美元", Begin: req.Begin, End: req.End, Source: debtURL, DataVersion: "current-visible-revision", Rows: []DebtRow{}, Summaries: []DebtSummary{}, Note: "财政部DTS表IIIA账面发行与赎回，按记录日、可流通/非流通分别保留。金额含官方更正、利息和通胀调整，允许负值，不等于纯现金毛发行或拍卖募集额。汇总为同日同市场全部明细日值之和，净记账发行=发行−赎回，均为计算值；不代表市场公众净融资或财政赤字。不累加月累计/财年累计，不用债务余额差分。财年10月开始；周末、假期及无记录日不补。尚未采集CUSIP结算事件、拍卖分配和SOMA分项，不按公告日推断结算日。公布时间与修订编号不可得，保持null。"}
	previousDate := ""
	previousLine := 0
	total := -1
	pages := 0
	for page := 1; ; page++ {
		if page > 300 {
			return nil, fmt.Errorf("财政部债务查询超过300页")
		}
		q := url.Values{"filter": {"record_date:gte:" + begin.Format("2006-01-02") + ",record_date:lte:" + end.Format("2006-01-02")}, "sort": {"record_date,src_line_nbr"}, "page[size]": {strconv.Itoa(pageSize)}, "page[number]": {strconv.Itoa(page)}}
		address := debtURL + "?" + q.Encode()
		var env envelope
		var rows []DebtRow
		fetched, err := c.fetch(ctx, address, func(raw []byte) error {
			var err error
			env, rows, err = parsePage(raw, req.Begin, req.End, page)
			if err != nil {
				return err
			}
			if total >= 0 && (env.Meta.Total != total || env.Meta.Pages != pages) {
				return fmt.Errorf("财政部债务分页总数变化")
			}
			date, line := previousDate, previousLine
			for _, r := range rows {
				n, _ := strconv.Atoi(r.Raw["src_line_nbr"])
				if r.Date < date || r.Date == date && n <= line {
					return fmt.Errorf("财政部交易重复或排序变化")
				}
				date, line = r.Date, n
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		total, pages = env.Meta.Total, env.Meta.Pages
		for _, r := range rows {
			r.Source, r.FetchedAt = address, fetched
			result.Rows = append(result.Rows, r)
			previousDate = r.Date
			previousLine, _ = strconv.Atoi(r.Raw["src_line_nbr"])
		}
		if fetched.After(result.FetchedAt) {
			result.FetchedAt = fetched
		}
		if page >= pages {
			break
		}
	}
	groups := map[string][]DebtRow{}
	keys := []string{}
	uniqueDebt, _ := UniqueDebtRows(result.Rows)
	repeats := map[string]int{}
	counted := map[string]int{}
	for _, r := range result.Rows {
		counted[r.Date+"/"+r.Market]++
	}
	for _, r := range uniqueDebt {
		repeats[r.Date+"/"+r.Market]++
	}
	for _, r := range uniqueDebt {
		key := r.Date + "/" + r.Market
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], r)
	}
	sort.Strings(keys)
	for _, key := range keys {
		rows := groups[key]
		s := DebtSummary{Date: rows[0].Date, Market: rows[0].Market, IsDerived: true, InputRows: len(rows), DuplicateRows: counted[key] - repeats[key]}
		for _, kind := range []string{"Issues", "Redemptions"} {
			sum := 0.0
			found, valid := false, true
			for _, r := range rows {
				if r.Transaction != kind {
					continue
				}
				found = true
				if r.Today == nil {
					valid = false
				} else {
					sum += *r.Today
				}
			}
			if math.IsInf(sum, 0) || math.IsNaN(sum) {
				return nil, fmt.Errorf("财政部交易汇总溢出")
			}
			if found && valid {
				if kind == "Issues" {
					s.Issues = &sum
				} else {
					s.Redemptions = &sum
				}
			}
		}
		if s.Issues != nil && s.Redemptions != nil {
			v := *s.Issues - *s.Redemptions
			if math.IsInf(v, 0) {
				return nil, fmt.Errorf("财政部净记账发行溢出")
			}
			s.Net = &v
		}
		result.Summaries = append(result.Summaries, s)
	}
	return result, nil
}
