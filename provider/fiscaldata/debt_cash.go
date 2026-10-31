package fiscaldata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const cashTotalsURL = "https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/dts/deposits_withdrawals_operating_cash"
const cashAdjustmentsURL = "https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/dts/adjustment_public_debt_transactions_cash_basis"

type CashAmounts struct {
	Today *float64 `json:"today_million_usd"`
	MTD   *float64 `json:"month_to_date_million_usd"`
	FYTD  *float64 `json:"fiscal_year_to_date_million_usd"`
}
type CashRow struct {
	CashAmounts
	Date        string            `json:"date"`
	Transaction string            `json:"transaction"`
	Category    string            `json:"category"`
	Description string            `json:"description"`
	Source      string            `json:"source"`
	FetchedAt   time.Time         `json:"fetched_at"`
	Raw         map[string]string `json:"raw_values"`
}
type CashObservation struct {
	Date         string      `json:"date"`
	Issues       *CashRow    `json:"issues"`
	Redemptions  *CashRow    `json:"redemptions"`
	Net          CashAmounts `json:"net_cash_issuance"`
	NetIsDerived bool        `json:"net_is_derived"`
	NetFormula   string      `json:"net_formula"`
	Adjustments  []CashRow   `json:"adjustments"`
	Note         string      `json:"note,omitempty"`
}
type CashResult struct {
	Region       string            `json:"region"`
	Indicator    string            `json:"indicator"`
	Frequency    string            `json:"frequency"`
	Basis        string            `json:"basis"`
	Unit         string            `json:"unit"`
	Begin        string            `json:"begin"`
	End          string            `json:"end"`
	Sources      []string          `json:"sources"`
	FetchedAt    time.Time         `json:"fetched_at"`
	PublishedAt  *time.Time        `json:"published_at"`
	RevisionID   *string           `json:"revision_id"`
	DataVersion  string            `json:"data_version"`
	Observations []CashObservation `json:"observations"`
	Note         string            `json:"note"`
}

type cashSpec struct {
	address, category, transaction string
	adjustments                    bool
}

func cashSpecs() []cashSpec {
	specs := []cashSpec{}
	// 官方in过滤对含括号的类别不能可靠匹配，分别使用已在线核验的精确eq查询。
	for _, suffix := range []string{"IIIB", "III-B"} {
		specs = append(specs, cashSpec{address: cashTotalsURL, category: "Public Debt Cash Issues (Table " + suffix + ")", transaction: "Issues"}, cashSpec{address: cashTotalsURL, category: "Public Debt Cash Redemp. (Table " + suffix + ")", transaction: "Redemptions"})
	}
	return append(specs, cashSpec{address: cashAdjustmentsURL, adjustments: true})
}

func parseCashPage(raw []byte, req *DebtRequest, spec cashSpec, page int) (envelope, []CashRow, error) {
	var env envelope
	env.Meta.Total, env.Meta.Count, env.Meta.Pages = -1, -1, -1
	if e := json.Unmarshal(raw, &env); e != nil {
		return env, nil, fmt.Errorf("现金债务数据响应无效：%w", e)
	}
	if env.Data == nil || env.Meta.Total < 0 || env.Meta.Pages > 300 || env.Meta.Pages != (env.Meta.Total+pageSize-1)/pageSize || page > max(1, env.Meta.Pages) || env.Meta.Count != len(env.Data) || len(env.Data) != min(pageSize, env.Meta.Total-(page-1)*pageSize) {
		return env, nil, fmt.Errorf("现金债务数据分页计数无效")
	}
	prefix := "transaction"
	if spec.adjustments {
		prefix = "adj"
	}
	for _, suffix := range []string{"today", "mtd", "fytd"} {
		if env.Meta.Formats[prefix+"_"+suffix+"_amt"] != "$1,000,000" {
			return env, nil, fmt.Errorf("现金债务数据金额单位变化")
		}
	}
	rows := []CashRow{}
	for _, r := range env.Data {
		date, e := time.Parse("2006-01-02", r["record_date"])
		line, le := strconv.Atoi(r["src_line_nbr"])
		if e != nil || date.Format("20060102") < req.Begin || date.Format("20060102") > req.End || le != nil || line <= 0 {
			return env, nil, fmt.Errorf("现金债务数据日期或行号无效")
		}
		item := CashRow{Date: r["record_date"], Raw: r}
		if spec.adjustments {
			if (r["table_nbr"] != "IIIB" && r["table_nbr"] != "III-B") || r["table_nm"] != "Adjustment of Public Debt Transactions to Cash Basis" || strings.TrimSpace(r["adj_type"]) == "" || r["adj_type"] == "null" {
				return env, nil, fmt.Errorf("现金债务调整表标识变化")
			}
			switch r["transaction_type"] {
			case "Issues", "Public Debt Cash Issues":
				item.Transaction = "Issues"
			case "Redemptions", "Public Debt Cash Redemptions":
				item.Transaction = "Redemptions"
			default:
				return env, nil, fmt.Errorf("现金债务调整交易类型变化")
			}
			item.Category, item.Description = r["adj_type"], r["adj_type_desc"]
		} else {
			expected := "Deposits"
			if spec.transaction == "Redemptions" {
				expected = "Withdrawals"
			}
			if r["table_nbr"] != "II" || r["table_nm"] != "Deposits and Withdrawals of Operating Cash" || r["transaction_catg"] != spec.category || r["transaction_type"] != expected || (r["account_type"] != "Treasury General Account (TGA)" && r["account_type"] != "Federal Reserve Account") {
				return env, nil, fmt.Errorf("官方现金债务总额类别或账户变化")
			}
			item.Transaction, item.Category, item.Description = spec.transaction, r["transaction_catg"], r["transaction_catg_desc"]
		}
		for _, f := range []struct {
			k string
			p **float64
		}{{prefix + "_today_amt", &item.Today}, {prefix + "_mtd_amt", &item.MTD}, {prefix + "_fytd_amt", &item.FYTD}} {
			text, ok := r[f.k]
			if !ok {
				return env, nil, fmt.Errorf("现金债务数据缺少金额字段%s", f.k)
			}
			v, e := number(text)
			if e != nil {
				return env, nil, e
			}
			*f.p = v
		}
		rows = append(rows, item)
	}
	return env, rows, nil
}

func (c *Client) cashRows(ctx context.Context, req *DebtRequest, spec cashSpec) ([]CashRow, time.Time, error) {
	begin, _ := time.Parse("20060102", req.Begin)
	end, _ := time.Parse("20060102", req.End)
	result := []CashRow{}
	previousDate := ""
	previousLine := 0
	total, pages := -1, 0
	latest := time.Time{}
	for page := 1; ; page++ {
		filter := "record_date:gte:" + begin.Format("2006-01-02") + ",record_date:lte:" + end.Format("2006-01-02")
		if !spec.adjustments {
			filter += ",transaction_catg:eq:" + spec.category
		}
		q := url.Values{"filter": {filter}, "sort": {"record_date,src_line_nbr"}, "page[size]": {strconv.Itoa(pageSize)}, "page[number]": {strconv.Itoa(page)}}
		address := spec.address + "?" + q.Encode()
		var env envelope
		var rows []CashRow
		fetched, e := c.fetch(ctx, address, func(raw []byte) error {
			var e error
			env, rows, e = parseCashPage(raw, req, spec, page)
			if e != nil {
				return e
			}
			if total >= 0 && (env.Meta.Total != total || env.Meta.Pages != pages) {
				return fmt.Errorf("现金债务数据分页期间总数变化")
			}
			date, line := previousDate, previousLine
			for _, r := range rows {
				n, _ := strconv.Atoi(r.Raw["src_line_nbr"])
				if r.Date < date || r.Date == date && n <= line {
					return fmt.Errorf("现金债务数据重复或排序变化")
				}
				date, line = r.Date, n
			}
			return nil
		})
		if e != nil {
			return nil, time.Time{}, e
		}
		total, pages = env.Meta.Total, env.Meta.Pages
		for _, r := range rows {
			r.Source, r.FetchedAt = address, fetched
			result = append(result, r)
			previousDate = r.Date
			previousLine, _ = strconv.Atoi(r.Raw["src_line_nbr"])
		}
		if fetched.After(latest) {
			latest = fetched
		}
		if page >= pages {
			break
		}
	}
	return result, latest, nil
}

// DebtCash 读取官方现金总额；调整明细只溯源，不自行重建官方总额。
func (c *Client) DebtCash(ctx context.Context, req *DebtRequest) (*CashResult, error) {
	if e := req.Validate(); e != nil {
		return nil, e
	}
	result := &CashResult{Region: "US", Indicator: "treasury_debt_cash", Frequency: "daily", Basis: "DTS cash basis; record date", Unit: "百万美元", Begin: req.Begin, End: req.End, Sources: []string{cashTotalsURL, cashAdjustmentsURL}, DataVersion: "current-visible-revision", Observations: []CashObservation{}, Note: "现金发行和现金赎回直接读取DTS表II转引IIIB的官方总额，不用IIIA面值、拍卖面值或调整项自行拼算。净现金发行=官方现金发行−官方现金赎回，日值、月累计、财年累计各自作差并标为计算值；不累加累计字段。IIIB行含基准及调整，(-)是官方应用方向，原始金额可能为负数，未擅自翻转或加总。旧版Federal Reserve Account与现行TGA原名保留；不混加账户。周末、假期和无源记录日不补，缺失与零区分。不是逐券实际到账确认，也不推断剔除SOMA后的公众净融资或财政赤字。当前可见历史可能修订，公布时分与修订编号为null。"}
	byDate := map[string]*CashObservation{}
	for _, spec := range cashSpecs() {
		rows, fetched, e := c.cashRows(ctx, req, spec)
		if e != nil {
			return nil, e
		}
		if fetched.After(result.FetchedAt) {
			result.FetchedAt = fetched
		}
		for _, row := range rows {
			item := byDate[row.Date]
			if item == nil {
				item = &CashObservation{Date: row.Date, NetIsDerived: true, NetFormula: "official_cash_issues - official_cash_redemptions", Adjustments: []CashRow{}}
				byDate[row.Date] = item
			}
			if spec.adjustments {
				item.Adjustments = append(item.Adjustments, row)
				continue
			}
			target := &item.Issues
			if row.Transaction == "Redemptions" {
				target = &item.Redemptions
			}
			if *target != nil {
				return nil, fmt.Errorf("同日官方现金债务总额重复，不能混加新旧类别或账户")
			}
			copy := row
			*target = &copy
		}
	}
	dates := []string{}
	for date := range byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		item := byDate[date]
		if item.Issues == nil || item.Redemptions == nil {
			item.Note = "官方现金发行或赎回总额缺失，未推算缺失一侧"
		} else {
			for _, f := range []struct {
				a, b *float64
				p    **float64
			}{{item.Issues.Today, item.Redemptions.Today, &item.Net.Today}, {item.Issues.MTD, item.Redemptions.MTD, &item.Net.MTD}, {item.Issues.FYTD, item.Redemptions.FYTD, &item.Net.FYTD}} {
				if f.a != nil && f.b != nil {
					v := *f.a - *f.b
					if math.IsInf(v, 0) || math.IsNaN(v) {
						return nil, fmt.Errorf("债务净现金流计算溢出")
					}
					*f.p = &v
				}
			}
		}
		if len(item.Adjustments) == 0 {
			if item.Note != "" {
				item.Note += "；"
			}
			item.Note += "IIIB调整明细未列示，保留可用官方现金总额"
		}
		result.Observations = append(result.Observations, *item)
	}
	return result, nil
}
