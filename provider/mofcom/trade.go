package mofcom

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// TradeRequest 查询全国货物贸易月度值，开始与截至月份均包含在结果中。
type TradeRequest struct{ Begin, End string }

func (r *TradeRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("中国货物贸易查询请求不能为空")
	}
	begin, err := time.Parse("200601", r.Begin)
	if err != nil || begin.Format("200601") != r.Begin {
		return fmt.Errorf("无效开始月份 %q，请使用 YYYYMM", r.Begin)
	}
	end, err := time.Parse("200601", r.End)
	if err != nil || end.Format("200601") != r.End {
		return fmt.Errorf("无效截至月份 %q，请使用 YYYYMM", r.End)
	}
	if r.Begin > r.End {
		return fmt.Errorf("开始月份不能晚于截至月份")
	}
	if r.Begin < "201501" {
		return fmt.Errorf("中国海关货物贸易在线数据支持自201501起")
	}
	if r.End > time.Now().In(time.FixedZone("CST", 8*3600)).Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前北京时间月份")
	}
	return nil
}

// TradeAmount 保留当月和年初至当月累计的同币种值，不用汇率换算或累计差值补月值。
type TradeAmount struct {
	Monthly       *float64 `json:"monthly"`
	YearToDate    *float64 `json:"year_to_date"`
	MonthlyMoM    *float64 `json:"monthly_mom_percent,omitempty"`
	MonthlyYoY    *float64 `json:"monthly_yoy_percent"`
	YearToDateYoY *float64 `json:"year_to_date_yoy_percent"`
}

// TradeObservation 每月货物进出口官方公布总值，人民币和美元分开保留。
type TradeObservation struct {
	Period string               `json:"period"`
	USD    *TradeCurrencyValues `json:"usd,omitempty"`
	CNY    *TradeCurrencyValues `json:"cny,omitempty"`
	Note   string               `json:"note"`
}

type TradeCurrencyValues struct {
	Unit      string            `json:"unit"`
	Total     TradeAmount       `json:"total"`
	Exports   TradeAmount       `json:"exports"`
	Imports   TradeAmount       `json:"imports"`
	Balance   TradeAmount       `json:"balance"`
	Source    string            `json:"source"`
	FetchedAt time.Time         `json:"fetched_at"`
	RawValues map[string]string `json:"raw_values"`
}

// TradePartner 保留商务部页面展示的主要贸易伙伴累计统计；不是全体伙伴清单。
type TradePartner struct {
	Name          string            `json:"name"`
	Period        string            `json:"period"`
	Currency      string            `json:"currency"`
	Unit          string            `json:"unit"`
	TotalYTD      *float64          `json:"total_ytd"`
	ExportsYTD    *float64          `json:"exports_ytd"`
	ImportsYTD    *float64          `json:"imports_ytd"`
	TotalYTDYoY   *float64          `json:"total_ytd_yoy_percent"`
	ExportsYTDYoY *float64          `json:"exports_ytd_yoy_percent"`
	ImportsYTDYoY *float64          `json:"imports_ytd_yoy_percent"`
	Source        string            `json:"source"`
	FetchedAt     time.Time         `json:"fetched_at"`
	RawValues     map[string]string `json:"raw_values"`
}

type TradeResult struct {
	Region           string             `json:"region"`
	Measurement      string             `json:"measurement"`
	Frequency        string             `json:"frequency"`
	Begin            string             `json:"begin"`
	End              string             `json:"end"`
	FirstPeriod      string             `json:"first_period"`
	LastPeriod       string             `json:"last_period"`
	DataVersion      string             `json:"data_version"`
	Observations     []TradeObservation `json:"observations"`
	PartnerPeriod    string             `json:"partner_period"`
	PartnerNote      string             `json:"partner_note,omitempty"`
	PartnerSource    string             `json:"partner_source"`
	PartnerFetchedAt time.Time          `json:"partner_fetched_at"`
	Partners         []TradePartner     `json:"partners"`
	Note             string             `json:"note"`
}

type usdRow struct {
	Period        string                     `json:"trade_date"`
	Total         json.RawMessage            `json:"total_value"`
	TotalYoY      json.RawMessage            `json:"total_per"`
	TotalYTD      json.RawMessage            `json:"total_lj_value"`
	TotalYTDYoY   json.RawMessage            `json:"total_lj_per"`
	Exports       json.RawMessage            `json:"export_value"`
	ExportsYoY    json.RawMessage            `json:"export_per"`
	ExportsYTD    json.RawMessage            `json:"export_lj_value"`
	ExportsYTDYoY json.RawMessage            `json:"export_lj_per"`
	Imports       json.RawMessage            `json:"import_value"`
	ImportsYoY    json.RawMessage            `json:"import_per"`
	ImportsYTD    json.RawMessage            `json:"import_lj_value"`
	ImportsYTDYoY json.RawMessage            `json:"import_lj_per"`
	Balance       json.RawMessage            `json:"imexgap_value"`
	BalanceYoY    json.RawMessage            `json:"imexgap_per"`
	BalanceYTD    json.RawMessage            `json:"imexgap_lj_value"`
	BalanceYTDYoY json.RawMessage            `json:"imexgap_lj_per"`
	Raw           map[string]json.RawMessage `json:"-"`
}

type countryEnvelope struct {
	Rows []countryRow `json:"rows"`
}

type countryRow struct {
	Period        string                     `json:"trade_date"`
	Name          string                     `json:"type"`
	TotalYTD      json.RawMessage            `json:"total_lj_value"`
	TotalYTDYoY   json.RawMessage            `json:"total_lj_per"`
	ExportsYTD    json.RawMessage            `json:"export_lj_value"`
	ExportsYTDYoY json.RawMessage            `json:"export_lj_per"`
	ImportsYTD    json.RawMessage            `json:"import_lj_value"`
	ImportsYTDYoY json.RawMessage            `json:"import_lj_per"`
	Raw           map[string]json.RawMessage `json:"-"`
}

var cnyTitleRE = regexp.MustCompile(`^(\d{4})年(\d{1,2})月全国进出口总值表（人民币值）$`)

type articleRelease struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	IssueDate string `json:"issueDate"`
	Source    string `json:"source"`
	Period    string `json:"-"`
}

type listEnvelope struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Total   int              `json:"total"`
	Data    []articleRelease `json:"data"`
}

type articleEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		ID        int    `json:"id"`
		Name      string `json:"name"`
		IssueDate string `json:"issueDate"`
		Source    string `json:"source"`
		Content   string `json:"content"`
	} `json:"data"`
}

// Trade 获取人民币月值、美元月值及所选月份的美元伙伴累计值。
func (c *Client) Trade(ctx context.Context, req *TradeRequest) (*TradeResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	usd, usdFetched, err := c.usdRows(ctx, req.Begin, req.End)
	if err != nil {
		return nil, err
	}
	cny, err := c.cnyRows(ctx, req.Begin, req.End)
	if err != nil {
		return nil, err
	}
	form := url.Values{"date": {req.End}}
	partnerFetched := time.Time{}
	partnerRelease := fmt.Sprintf("https://data.mofcom.gov.cn/datamofcom/front/totalbycountry/query?date=%s", req.End)
	var partnerBytes []byte
	var partnerEnvelope countryEnvelope
	partnerValidate := func(raw []byte) error {
		var result countryEnvelope
		if err := json.Unmarshal(raw, &result); err != nil {
			return fmt.Errorf("解析海关伙伴统计失败: %w", err)
		}
		if result.Rows == nil {
			return fmt.Errorf("商务部主要伙伴统计缺少有效 rows 数组")
		}
		seen := map[string]bool{}
		for i := range result.Rows {
			row := &result.Rows[i]
			if row.Period != req.End || strings.TrimSpace(row.Name) == "" || seen[row.Name] {
				return fmt.Errorf("海关伙伴记录所属期无效或重复")
			}
			seen[row.Name] = true
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(mustJSON(row), &raw); err != nil {
				return err
			}
			row.Raw = raw
			for _, value := range []json.RawMessage{row.TotalYTD, row.TotalYTDYoY, row.ExportsYTD, row.ExportsYTDYoY, row.ImportsYTD, row.ImportsYTDYoY} {
				if _, err := parseNumber(value); err != nil {
					return err
				}
			}
		}
		partnerEnvelope = result
		return nil
	}
	partnerBytes, partnerFetched, err = c.request(ctx, http.MethodPost, partnerURL, "https://data.mofcom.gov.cn/hwmy/imexCountry.shtml", form, nil, partnerValidate)
	if err != nil {
		return nil, err
	}
	_ = partnerBytes
	result := &TradeResult{Region: "CN", Measurement: "customs-goods-trade", Frequency: "monthly", Begin: req.Begin, End: req.End, DataVersion: "current-visible-release", Observations: []TradeObservation{}, PartnerPeriod: req.End, PartnerSource: partnerRelease, PartnerFetchedAt: partnerFetched, Partners: []TradePartner{}, Note: "本命令仅含海关货物贸易，不含服务贸易。人民币月度金额来自海关总署发布稿（亿元），美元月值来自商务部数据中心转发海关统计（亿美元）；两币值独立采集，不做汇率换算。贸易伙伴为商务部页面列示的主要伙伴，不是全体国家/地区；伙伴数据为年初至当月累计美元值，未由累计差分构造月度值。未发布月份、缺失值和零值分开保留。"}
	date, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for !date.After(end) {
		period := date.Format("200601")
		item := TradeObservation{Period: period}
		if row, ok := usd[period]; ok {
			item.USD = &TradeCurrencyValues{Unit: "亿美元", Source: monthlyURL, FetchedAt: usdFetched, RawValues: rawUSD(row), Total: usdAmount(row.Total, row.TotalYTD, row.TotalYoY, row.TotalYTDYoY), Exports: usdAmount(row.Exports, row.ExportsYTD, row.ExportsYoY, row.ExportsYTDYoY), Imports: usdAmount(row.Imports, row.ImportsYTD, row.ImportsYoY, row.ImportsYTDYoY), Balance: usdAmount(row.Balance, row.BalanceYTD, row.BalanceYoY, row.BalanceYTDYoY)}
		}
		if row, ok := cny[period]; ok {
			item.CNY = &TradeCurrencyValues{Unit: "亿元人民币", Source: row.Source, FetchedAt: row.FetchedAt, RawValues: row.RawValues, Total: row.Total, Exports: row.Exports, Imports: row.Imports, Balance: row.Balance}
		}
		missing := []string{}
		if item.USD == nil {
			missing = append(missing, "美元月度来源该月无记录")
		}
		if item.CNY == nil {
			missing = append(missing, "人民币月报目录未找到该月报告")
		}
		item.Note = strings.Join(missing, "；")
		if item.USD != nil || item.CNY != nil {
			if result.FirstPeriod == "" || period < result.FirstPeriod {
				result.FirstPeriod = period
			}
			if period > result.LastPeriod {
				result.LastPeriod = period
			}
		}
		result.Observations = append(result.Observations, item)
		date = date.AddDate(0, 1, 0)
	}
	for _, row := range partnerEnvelope.Rows {
		result.Partners = append(result.Partners, TradePartner{Name: row.Name, Period: row.Period, Currency: "USD", Unit: "亿美元", TotalYTD: ptrNumber(row.TotalYTD), ExportsYTD: ptrNumber(row.ExportsYTD), ImportsYTD: ptrNumber(row.ImportsYTD), TotalYTDYoY: ptrNumber(row.TotalYTDYoY), ExportsYTDYoY: ptrNumber(row.ExportsYTDYoY), ImportsYTDYoY: ptrNumber(row.ImportsYTDYoY), Source: partnerRelease, FetchedAt: partnerFetched, RawValues: rawCountry(row)})
	}
	if len(result.Partners) == 0 {
		result.PartnerNote = fmt.Sprintf("商务部未提供%s月主要伙伴统计；保留可用的月度贸易数据", req.End)
	}
	return result, nil
}

type cnyRow struct {
	Period                           string
	Total, Exports, Imports, Balance TradeAmount
	Source                           string
	FetchedAt                        time.Time
	RawValues                        map[string]string
}

func (c *Client) usdRows(ctx context.Context, begin, end string) (map[string]usdRow, time.Time, error) {
	form := url.Values{"startDate": {begin}, "endDate": {end}}
	rows := map[string]usdRow{}
	validate := func(raw []byte) error { var err error; rows, err = parseUSD(raw, begin, end); return err }
	_, fetched, err := c.request(ctx, http.MethodPost, monthlyURL, "https://data.mofcom.gov.cn/hwmy/imexmonth.shtml", form, nil, validate)
	if err != nil {
		return nil, time.Time{}, err
	}
	return rows, fetched, nil
}

func parseUSD(raw []byte, begin, end string) (map[string]usdRow, error) {
	var envelope []json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope) != 2 {
		return nil, fmt.Errorf("商务部美元贸易响应结构无效")
	}
	var records []map[string]json.RawMessage
	if err := json.Unmarshal(envelope[0], &records); err != nil {
		return nil, fmt.Errorf("商务部美元月度记录无效")
	}
	rows := map[string]usdRow{}
	for _, record := range records {
		var row usdRow
		row.Raw = record
		if err := json.Unmarshal(mustJSON(record), &row); err != nil {
			return nil, err
		}
		if _, err := time.Parse("200601", row.Period); err != nil || len(row.Period) != 6 {
			return nil, fmt.Errorf("商务部美元月度所属期无效: %q", row.Period)
		}
		if row.Period < begin || row.Period > end {
			return nil, fmt.Errorf("商务部返回查询区间外月份: %s", row.Period)
		}
		if _, exists := rows[row.Period]; exists {
			return nil, fmt.Errorf("商务部美元月度所属期重复: %s", row.Period)
		}
		for _, value := range []json.RawMessage{row.Total, row.TotalYoY, row.TotalYTD, row.TotalYTDYoY, row.Exports, row.ExportsYoY, row.ExportsYTD, row.ExportsYTDYoY, row.Imports, row.ImportsYoY, row.ImportsYTD, row.ImportsYTDYoY, row.Balance, row.BalanceYoY, row.BalanceYTD, row.BalanceYTDYoY} {
			if _, err := parseNumber(value); err != nil {
				return nil, err
			}
		}
		rows[row.Period] = row
	}
	return rows, nil
}

func (c *Client) cnyRows(ctx context.Context, begin, end string) (map[string]cnyRow, error) {
	result := map[string]cnyRow{}
	start, _ := time.Parse("200601", begin)
	startIssueMonth := start.Format("200601")
	var total, page, pageSize int
	previousIssueDate := "9999-99-99"
	seenIDs := map[int]articleRelease{}
	for page = 1; page <= 300; page++ {
		var listing listEnvelope
		body := map[string]int{"menuId": 1, "page": page}
		validate := func(raw []byte) error {
			var response listEnvelope
			if err := json.Unmarshal(raw, &response); err != nil || response.Code != 0 || response.Total <= 0 || len(response.Data) == 0 {
				return fmt.Errorf("商务部人民币贸易报告目录无效")
			}
			for _, item := range response.Data {
				if _, err := time.Parse("2006-01-02", item.IssueDate); err != nil || item.ID <= 0 || strings.TrimSpace(item.Name) == "" {
					return fmt.Errorf("商务部人民币贸易目录记录无效")
				}
				if match := cnyTitleRE.FindStringSubmatch(item.Name); match != nil {
					month, _ := strconv.Atoi(match[2])
					item.Period = fmt.Sprintf("%s%02d", match[1], month)
					if month < 1 || month > 12 {
						return fmt.Errorf("商务部人民币贸易报告月份无效")
					}
					if item.Source != "" && item.Source != "海关总署" {
						return fmt.Errorf("人民币贸易目录匹配标题但来源不是海关总署: %s", item.Name)
					}
				}
			}
			listing = response
			return nil
		}
		_, _, err := c.request(ctx, http.MethodPost, listURL, "https://fdi.mofcom.gov.cn/come-datatongji-con.html", nil, body, validate)
		if err != nil {
			return nil, err
		}
		pagePeriods := map[string]bool{}
		pageIDs := map[int]articleRelease{}
		for _, item := range listing.Data {
			if item.IssueDate > previousIssueDate {
				return nil, fmt.Errorf("商务部人民币贸易目录分页顺序异常")
			}
			previousIssueDate = item.IssueDate
			if prior, exists := pageIDs[item.ID]; exists {
				if prior.Name != item.Name || prior.IssueDate != item.IssueDate || prior.Source != item.Source {
					return nil, fmt.Errorf("商务部人民币贸易目录同页记录 ID 冲突：%d", item.ID)
				}
				continue
			}
			pageIDs[item.ID] = item
			match := cnyTitleRE.FindStringSubmatch(item.Name)
			if match == nil {
				continue
			}
			month, _ := strconv.Atoi(match[2])
			period := fmt.Sprintf("%s%02d", match[1], month)
			if period >= begin && period <= end {
				if pagePeriods[period] {
					return nil, fmt.Errorf("商务部同月人民币贸易报告重复: %s", period)
				}
				pagePeriods[period] = true
			}
		}
		if total == 0 {
			total, pageSize = listing.Total, len(listing.Data)
		} else if listing.Total != total || len(listing.Data) > pageSize {
			return nil, fmt.Errorf("商务部人民币贸易目录分页期间总数或页面大小发生变化")
		}
		if page*pageSize < total && len(listing.Data) != pageSize {
			return nil, fmt.Errorf("商务部人民币贸易目录分页记录数量异常")
		}
		for _, item := range listing.Data {
			if prior, exists := seenIDs[item.ID]; exists {
				if prior.Name != item.Name || prior.IssueDate != item.IssueDate || prior.Source != item.Source {
					return nil, fmt.Errorf("商务部人民币贸易目录分页记录 ID 冲突：%d", item.ID)
				}
				continue
			}
			seenIDs[item.ID] = item
			match := cnyTitleRE.FindStringSubmatch(item.Name)
			if match == nil {
				continue
			}
			month, _ := strconv.Atoi(match[2])
			item.Period = fmt.Sprintf("%s%02d", match[1], month)
			if item.Period < begin || item.Period > end {
				continue
			}
			if _, exists := result[item.Period]; exists {
				return nil, fmt.Errorf("商务部同月人民币贸易报告重复: %s", item.Period)
			}
			row, err := c.cnyArticle(ctx, item)
			if err != nil {
				return nil, err
			}
			result[item.Period] = row
		}
		if hasOlderCNYReport(listing.Data, startIssueMonth) || page*pageSize >= total {
			break
		}
	}
	if page > 300 {
		return nil, fmt.Errorf("商务部人民币贸易目录超过300页")
	}
	return result, nil
}

func hasOlderCNYReport(items []articleRelease, begin string) bool {
	oldest := ""
	for _, item := range items {
		match := cnyTitleRE.FindStringSubmatch(item.Name)
		if match == nil {
			continue
		}
		month, err := strconv.Atoi(match[2])
		if err != nil || month < 1 || month > 12 {
			continue
		}
		period := fmt.Sprintf("%s%02d", match[1], month)
		if oldest == "" || period < oldest {
			oldest = period
		}
	}
	return oldest != "" && oldest < begin
}

func (c *Client) cnyArticle(ctx context.Context, release articleRelease) (cnyRow, error) {
	var article articleEnvelope
	validate := func(raw []byte) error {
		var response articleEnvelope
		if err := json.Unmarshal(raw, &response); err != nil || response.Code != 0 || response.Data.ID != release.ID || response.Data.Name != release.Name || response.Data.Source != "海关总署" || response.Data.IssueDate != release.IssueDate {
			return fmt.Errorf("商务部人民币报告元数据与目录不符: %s", release.Period)
		}
		row, err := parseCNYArticle(response.Data.Content, release.Period)
		if err != nil {
			return err
		}
		article = response
		_ = row
		return nil
	}
	address := articleURL + "?id=" + strconv.Itoa(release.ID)
	_, fetched, err := c.request(ctx, http.MethodGet, address, "https://fdi.mofcom.gov.cn/come-datatongji-con.html?id="+strconv.Itoa(release.ID), nil, nil, validate)
	if err != nil {
		return cnyRow{}, err
	}
	row, err := parseCNYArticle(article.Data.Content, release.Period)
	if err != nil {
		return cnyRow{}, err
	}
	row.Source, row.FetchedAt = address, fetched
	return row, nil
}

func parseCNYArticle(rawHTML, period string) (cnyRow, error) {
	row := cnyRow{Period: period, RawValues: map[string]string{}}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return row, err
	}
	table := doc.Find("table").First()
	if table.Length() == 0 || !strings.Contains(compactText(doc.Text()), "亿元人民币") {
		return row, fmt.Errorf("%s 人民币贸易报告缺少亿元人民币数据表", period)
	}
	month, _ := strconv.Atoi(period[4:])
	headers := []string{}
	var parseErr error
	table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		if parseErr != nil {
			return
		}
		cells := tr.ChildrenFiltered("th,td")
		if cells.Length() == 0 {
			cells = tr.Find("th,td")
		}
		values := []string{}
		cells.Each(func(_ int, cell *goquery.Selection) { values = append(values, compactText(cell.Text())) })
		if len(values) == 0 {
			return
		}
		if strings.Contains(values[0], "项目") {
			headers = values
			return
		}
		label := compactText(values[0])
		key := ""
		switch {
		case strings.Contains(label, "进出口总值") || label == "进出口总额":
			key = "total"
		case label == "出口" || strings.HasPrefix(label, "出口总值") || strings.HasPrefix(label, "出口额"):
			key = "exports"
		case label == "进口" || strings.HasPrefix(label, "进口总值") || strings.HasPrefix(label, "进口额"):
			key = "imports"
		case strings.Contains(label, "差额"):
			key = "balance"
		}
		if key == "" || len(headers) != len(values) {
			return
		}
		labelText := map[string]string{"total": "进出口总值", "exports": "出口", "imports": "进口", "balance": "差额"}[key]
		monthlyCol, ytdCol, momCol, yoyCol, ytdYoyCol := -1, -1, -1, -1, -1
		for col, header := range headers {
			if col == 0 {
				continue
			}
			if header == fmt.Sprintf("%d月", month) {
				monthlyCol = col
			}
			if header == fmt.Sprintf("1至%d月累计", month) || header == fmt.Sprintf("1—%d月累计", month) || header == fmt.Sprintf("1-%d月累计", month) {
				ytdCol = col
			}
			if strings.Contains(header, "环比") {
				momCol = col
			}
			if strings.Contains(header, "同比") && !strings.Contains(header, "累计") {
				yoyCol = col
			}
			if strings.Contains(header, "累计") && strings.Contains(header, "同比") {
				ytdYoyCol = col
			}
		}
		if monthlyCol < 0 || ytdCol < 0 {
			return
		}
		monthlyRaw, ytdRaw := values[monthlyCol], values[ytdCol]
		monthly, err1 := parseTextNumber(monthlyRaw)
		ytd, err2 := parseTextNumber(ytdRaw)
		if err1 != nil || err2 != nil {
			return
		}
		amount := TradeAmount{Monthly: monthly, YearToDate: ytd}
		if momCol >= 0 {
			amount.MonthlyMoM, parseErr = parseTextNumber(values[momCol])
			if parseErr != nil {
				parseErr = fmt.Errorf("%s %s环比字段无效: %w", period, labelText, parseErr)
				return
			}
			row.RawValues[key+"_monthly_mom_percent"] = values[momCol]
		}
		if yoyCol >= 0 {
			amount.MonthlyYoY, parseErr = parseTextNumber(values[yoyCol])
			if parseErr != nil {
				parseErr = fmt.Errorf("%s %s月同比字段无效: %w", period, labelText, parseErr)
				return
			}
			row.RawValues[key+"_monthly_yoy_percent"] = values[yoyCol]
		}
		if ytdYoyCol >= 0 {
			amount.YearToDateYoY, parseErr = parseTextNumber(values[ytdYoyCol])
			if parseErr != nil {
				parseErr = fmt.Errorf("%s %s累计同比字段无效: %w", period, labelText, parseErr)
				return
			}
			row.RawValues[key+"_ytd_yoy_percent"] = values[ytdYoyCol]
		}
		row.RawValues[key+"_monthly"] = monthlyRaw
		row.RawValues[key+"_ytd"] = ytdRaw
		switch key {
		case "total":
			row.Total = amount
		case "exports":
			row.Exports = amount
		case "imports":
			row.Imports = amount
		case "balance":
			row.Balance = amount
		}
	})
	if parseErr != nil {
		return row, parseErr
	}
	for _, item := range []struct {
		name  string
		value TradeAmount
	}{{"进出口总值", row.Total}, {"出口", row.Exports}, {"进口", row.Imports}} {
		if item.value.Monthly == nil || item.value.YearToDate == nil {
			return row, fmt.Errorf("%s 人民币贸易报告缺少%s当月或累计值", period, item.name)
		}
	}
	return row, nil
}

func parseNumber(raw json.RawMessage) (*float64, error) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return nil, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("海关统计值格式无效: %s", raw)
		}
		if text == "" || text == "-" {
			return nil, nil
		}
		text = strings.ReplaceAll(text, ",", "")
		n, err = strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("海关统计数值无效: %s", text)
		}
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("海关统计值不是有限数值")
	}
	return &n, nil
}

func parseTextNumber(text string) (*float64, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, ",", ""))
	if text == "" || text == "-" || text == "—" || text == "–" || text == "−" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("人民币贸易数值无效: %s", text)
	}
	return &n, nil
}

func usdAmount(monthly, ytd, monthlyYoY, ytdYoY json.RawMessage) TradeAmount {
	return TradeAmount{Monthly: ptrNumber(monthly), YearToDate: ptrNumber(ytd), MonthlyYoY: ptrNumber(monthlyYoY), YearToDateYoY: ptrNumber(ytdYoY)}
}

func ptrNumber(raw json.RawMessage) *float64 { value, _ := parseNumber(raw); return value }

func rawUSD(row usdRow) map[string]string {
	out := map[string]string{}
	for key, raw := range row.Raw {
		out[key] = strings.Trim(string(raw), `"`)
	}
	return out
}

func rawCountry(row countryRow) map[string]string {
	out := map[string]string{}
	for key, raw := range row.Raw {
		out[key] = strings.Trim(string(raw), `"`)
	}
	return out
}

func compactText(text string) string { return strings.Join(strings.Fields(text), "") }

func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
