package fiscaldata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const auctionsURL = "https://api.fiscaldata.treasury.gov/services/api/fiscal_service/v1/accounting/od/auctions_query"

// AuctionsRequest 默认以官方发行日期筛选；拍卖日期作为独立筛选口径。
type AuctionsRequest struct{ Begin, End, DateBasis string }

func (r *AuctionsRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("拍卖查询请求不能为空")
	}
	if r.DateBasis != "" && r.DateBasis != "issue" && r.DateBasis != "auction" {
		return fmt.Errorf("日期口径可选 issue（发行日）或 auction（拍卖日）")
	}
	for _, v := range []string{r.Begin, r.End} {
		d, e := time.Parse("20060102", v)
		if e != nil || d.Format("20060102") != v {
			return fmt.Errorf("拍卖日期须为YYYYMMDD：%s", v)
		}
	}
	if r.Begin < "19980727" || r.Begin > r.End || r.End > time.Now().UTC().Format("20060102") {
		return fmt.Errorf("拍卖查询支持19980727至UTC今天，开始日期不能晚于截至日期")
	}
	return nil
}

type AuctionAmounts struct {
	Offering        *float64 `json:"offering_usd"`
	TotalAccepted   *float64 `json:"total_accepted_usd"`
	Competitive     *float64 `json:"competitive_accepted_usd"`
	Noncompetitive  *float64 `json:"noncompetitive_accepted_usd"`
	SOMA            *float64 `json:"soma_accepted_usd"`
	FIMA            *float64 `json:"fima_noncompetitive_accepted_usd"`
	PrimaryDealers  *float64 `json:"primary_dealer_accepted_usd"`
	DirectBidders   *float64 `json:"direct_bidder_accepted_usd"`
	IndirectBidders *float64 `json:"indirect_bidder_accepted_usd"`
}
type AuctionObservation struct {
	CUSIP             string            `json:"cusip"`
	SecurityType      string            `json:"security_type"`
	Term              string            `json:"security_term"`
	AnnouncementDate  *string           `json:"announcement_date"`
	AuctionDate       string            `json:"auction_date"`
	IssueDate         string            `json:"issue_date"`
	MaturityDate      string            `json:"maturity_date"`
	OriginalIssueDate *string           `json:"original_issue_date"`
	Reopening         *bool             `json:"reopening"`
	FloatingRate      *bool             `json:"floating_rate"`
	InflationIndexed  *bool             `json:"inflation_indexed"`
	SOMAIncluded      *bool             `json:"soma_inside_offering_amount"`
	FIMAIncluded      *bool             `json:"fima_competitive_inside_offering_amount"`
	Amounts           AuctionAmounts    `json:"amounts"`
	PricePer100       *float64          `json:"price_per_100"`
	Source            string            `json:"source"`
	FetchedAt         time.Time         `json:"fetched_at"`
	Raw               map[string]string `json:"raw_values"`
}
type AuctionsResult struct {
	Region       string               `json:"region"`
	Indicator    string               `json:"indicator"`
	Frequency    string               `json:"frequency"`
	Market       string               `json:"market"`
	DateBasis    string               `json:"date_basis"`
	AmountBasis  string               `json:"amount_basis"`
	AmountUnit   string               `json:"amount_unit"`
	Begin        string               `json:"begin"`
	End          string               `json:"end"`
	FirstDate    string               `json:"first_date"`
	LastDate     string               `json:"last_date"`
	Source       string               `json:"source"`
	FetchedAt    time.Time            `json:"fetched_at"`
	PublishedAt  *time.Time           `json:"published_at"`
	RevisionID   *string              `json:"revision_id"`
	DataVersion  string               `json:"data_version"`
	Observations []AuctionObservation `json:"observations"`
	Note         string               `json:"note"`
}

func optionalDate(raw string) (*string, error) {
	if raw == "" || raw == "null" {
		return nil, nil
	}
	d, e := time.Parse("2006-01-02", raw)
	if e != nil || d.Format("2006-01-02") != raw {
		return nil, fmt.Errorf("拍卖数据日期无效：%s", raw)
	}
	return &raw, nil
}
func optionalBool(raw string) (*bool, error) {
	switch raw {
	case "", "null":
		return nil, nil
	case "Yes":
		b := true
		return &b, nil
	case "No":
		b := false
		return &b, nil
	}
	return nil, fmt.Errorf("拍卖标记无效：%s", raw)
}

var cusipPattern = regexp.MustCompile(`^[A-Z0-9]{9}$`)

func auctionKey(r AuctionObservation, basis string) string {
	if basis == "auction_date" {
		return r.AuctionDate + "/" + r.CUSIP + "/" + r.IssueDate
	}
	return r.IssueDate + "/" + r.CUSIP + "/" + r.AuctionDate
}

func parseAuctions(raw []byte, req *AuctionsRequest, page int) (envelope, []AuctionObservation, error) {
	var env envelope
	env.Meta.Total, env.Meta.Count, env.Meta.Pages = -1, -1, -1
	if e := json.Unmarshal(raw, &env); e != nil {
		return env, nil, fmt.Errorf("拍卖结果响应无效：%w", e)
	}
	if env.Data == nil || env.Meta.Total < 0 || env.Meta.Pages > 300 || env.Meta.Pages != (env.Meta.Total+pageSize-1)/pageSize || page > max(1, env.Meta.Pages) || env.Meta.Count != len(env.Data) || len(env.Data) != min(pageSize, env.Meta.Total-(page-1)*pageSize) {
		return env, nil, fmt.Errorf("拍卖结果分页计数无效")
	}
	rows := []AuctionObservation{}
	for _, raw := range env.Data {
		item := AuctionObservation{CUSIP: raw["cusip"], SecurityType: raw["security_type"], Term: raw["security_term"], Raw: raw}
		if !cusipPattern.MatchString(item.CUSIP) || strings.TrimSpace(item.Term) == "" || item.Term == "null" {
			return env, nil, fmt.Errorf("拍卖CUSIP或期限无效")
		}
		switch item.SecurityType {
		case "Bill", "Note", "Bond":
		default:
			return env, nil, fmt.Errorf("拍卖证券类型变化：%s", item.SecurityType)
		}
		for _, f := range []struct {
			k string
			p *string
		}{{"auction_date", &item.AuctionDate}, {"issue_date", &item.IssueDate}, {"maturity_date", &item.MaturityDate}} {
			v, e := optionalDate(raw[f.k])
			if e != nil || v == nil {
				return env, nil, fmt.Errorf("拍卖缺少有效%s", f.k)
			}
			*f.p = *v
		}
		if item.AuctionDate > item.IssueDate || item.IssueDate >= item.MaturityDate {
			return env, nil, fmt.Errorf("拍卖、发行及到期日期顺序异常")
		}
		date := item.IssueDate
		if req.DateBasis == "auction" {
			date = item.AuctionDate
		}
		period := strings.ReplaceAll(date, "-", "")
		if period < req.Begin || period > req.End {
			return env, nil, fmt.Errorf("拍卖结果日期超出查询口径区间")
		}
		for _, f := range []struct {
			k string
			p **string
		}{{"announcemt_date", &item.AnnouncementDate}, {"original_issue_date", &item.OriginalIssueDate}} {
			v, e := optionalDate(raw[f.k])
			if e != nil {
				return env, nil, e
			}
			*f.p = v
		}
		if item.AnnouncementDate != nil && *item.AnnouncementDate > item.AuctionDate || item.OriginalIssueDate != nil && *item.OriginalIssueDate > item.IssueDate {
			return env, nil, fmt.Errorf("拍卖公告日或原发行日异常")
		}
		for _, f := range []struct {
			k string
			p **bool
		}{{"reopening", &item.Reopening}, {"floating_rate", &item.FloatingRate}, {"inflation_index_security", &item.InflationIndexed}, {"soma_included", &item.SOMAIncluded}, {"fima_included", &item.FIMAIncluded}} {
			v, e := optionalBool(raw[f.k])
			if e != nil {
				return env, nil, e
			}
			*f.p = v
		}
		for _, f := range []struct {
			k string
			p **float64
		}{{"offering_amt", &item.Amounts.Offering}, {"total_accepted", &item.Amounts.TotalAccepted}, {"comp_accepted", &item.Amounts.Competitive}, {"noncomp_accepted", &item.Amounts.Noncompetitive}, {"soma_accepted", &item.Amounts.SOMA}, {"fima_noncomp_accepted", &item.Amounts.FIMA}, {"primary_dealer_accepted", &item.Amounts.PrimaryDealers}, {"direct_bidder_accepted", &item.Amounts.DirectBidders}, {"indirect_bidder_accepted", &item.Amounts.IndirectBidders}, {"price_per100", &item.PricePer100}} {
			if _, exists := raw[f.k]; !exists {
				return env, nil, fmt.Errorf("拍卖响应缺少%s字段，不能判作历史空值", f.k)
			}
			v, e := number(raw[f.k])
			if e != nil || v != nil && *v < 0 {
				return env, nil, fmt.Errorf("拍卖%s数值无效", f.k)
			}
			*f.p = v
		}
		if item.PricePer100 != nil && *item.PricePer100 <= 0 {
			return env, nil, fmt.Errorf("拍卖价格须大于零")
		}
		rows = append(rows, item)
	}
	return env, rows, nil
}

func (c *Client) Auctions(ctx context.Context, req *AuctionsRequest) (*AuctionsResult, error) {
	if e := req.Validate(); e != nil {
		return nil, e
	}
	basis := "issue_date"
	sortFields := "issue_date,cusip,auction_date"
	if req.DateBasis == "auction" {
		basis = "auction_date"
		sortFields = "auction_date,cusip,issue_date"
	}
	begin, _ := time.Parse("20060102", req.Begin)
	end, _ := time.Parse("20060102", req.End)
	result := &AuctionsResult{Region: "US", Indicator: "treasury_auctions", Frequency: "event", Market: "marketable", DateBasis: basis, AmountBasis: "par value", AmountUnit: "USD", Begin: req.Begin, End: req.End, Source: auctionsURL, DataVersion: "current-visible-revision", Observations: []AuctionObservation{}, Note: "美国财政部可流通证券拍卖结果，按官方发行日或拍卖日分别筛选；不以公告日代替发行日，不把原始发行日用于筛选再发行。CUSIP+发行日+拍卖日标识事件，同一CUSIP可有多次再发行。金额为美元面值，不是百万美元、现金募集收入或净融资。SOMA/FIMA、竞争性/非竞争性与投标人分项保留上游口径，存在包含或重叠关系，不任意加减合计；未知标记及历史字段缺口为null。原始利率、价格、期限及文件名见raw_values。issue_date为上游官方发行日期，不是单笔实际到账确认；按拍卖日查询可包含尚未到发行日的结果。当前可见历史不是当时可得版本，公布时分与修订编号保持null。"}
	total, pages := -1, 0
	previous := ""
	for page := 1; ; page++ {
		q := url.Values{"filter": {basis + ":gte:" + begin.Format("2006-01-02") + "," + basis + ":lte:" + end.Format("2006-01-02")}, "sort": {sortFields}, "page[size]": {strconv.Itoa(pageSize)}, "page[number]": {strconv.Itoa(page)}}
		address := auctionsURL + "?" + q.Encode()
		var env envelope
		var rows []AuctionObservation
		fetched, e := c.fetch(ctx, address, func(raw []byte) error {
			var e error
			env, rows, e = parseAuctions(raw, req, page)
			if e != nil {
				return e
			}
			if total >= 0 && (env.Meta.Total != total || env.Meta.Pages != pages) {
				return fmt.Errorf("拍卖分页期间总数变化")
			}
			key := previous
			for _, r := range rows {
				next := auctionKey(r, basis)
				if next <= key {
					return fmt.Errorf("拍卖事件重复或排序变化")
				}
				key = next
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
		total, pages = env.Meta.Total, env.Meta.Pages
		for _, r := range rows {
			r.Source, r.FetchedAt = address, fetched
			result.Observations = append(result.Observations, r)
			previous = auctionKey(r, basis)
			date := r.IssueDate
			if basis == "auction_date" {
				date = r.AuctionDate
			}
			if result.FirstDate == "" {
				result.FirstDate = date
			}
			result.LastDate = date
		}
		if fetched.After(result.FetchedAt) {
			result.FetchedAt = fetched
		}
		if page >= pages {
			break
		}
	}
	return result, nil
}
