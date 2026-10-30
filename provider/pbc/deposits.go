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

// DepositRequest 查询全国住户人民币存款，月份范围包含端点。
type DepositRequest struct{ Begin, End string }

func (r *DepositRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("住户存款查询请求不能为空")
	}
	if err := (FinancingRequest{r.Begin, r.End}).Validate(); err != nil {
		return err
	}
	if r.Begin < "202101" {
		return fmt.Errorf("住户存款查询自202101起，2020年表仅用于同比基期")
	}
	if r.End > time.Now().In(time.FixedZone("CST", 8*3600)).Format("200601") {
		return fmt.Errorf("截至月份不能晚于当前北京时间月份")
	}
	return nil
}

type DepositBalance struct {
	Period       string            `json:"period"`
	Balance      *float64          `json:"balance"`
	Demand       *float64          `json:"demand"`
	TimeAndOther *float64          `json:"time_and_other"`
	RawValues    map[string]string `json:"raw_values"`
	Source       string            `json:"source"`
	FetchedAt    *time.Time        `json:"fetched_at"`
	Notes        []string          `json:"notes"`
}

type DepositObservation struct {
	DepositBalance
	YearAgoBase            DepositBalance `json:"year_ago_base"`
	ComputedYoY            *float64       `json:"computed_yoy_percent"`
	ReportedHouseholdYoY   *float64       `json:"reported_household_yoy_percent"`
	MonthlyFlow            *float64       `json:"monthly_flow"`
	YTDFlow                *float64       `json:"ytd_flow"`
	MonthlyFlowMethod      string         `json:"monthly_flow_method"`
	MonthlyFlowApproximate bool           `json:"monthly_flow_approximate"`
	ReportSource           string         `json:"report_source"`
	ReportPublishedDate    string         `json:"report_published_date"`
	ReportFetchedAt        *time.Time     `json:"report_fetched_at"`
	PreviousReportSource   string         `json:"previous_report_source"`
	RawReportText          string         `json:"raw_report_text"`
	ReportNotes            []string       `json:"report_notes"`
	Note                   string         `json:"note"`
}

type DepositResult struct {
	Region             string               `json:"region"`
	Frequency          string               `json:"frequency"`
	Unit               string               `json:"unit"`
	Currency           string               `json:"currency"`
	Measurement        string               `json:"measurement"`
	Population         string               `json:"population"`
	SeasonallyAdjusted bool                 `json:"seasonally_adjusted"`
	Annualized         bool                 `json:"annualized"`
	Begin              string               `json:"begin"`
	End                string               `json:"end"`
	DataVersion        string               `json:"data_version"`
	RevisionID         *string              `json:"revision_id"`
	BalancePublishedAt *time.Time           `json:"balance_published_at"`
	Observations       []DepositObservation `json:"observations"`
	Note               string               `json:"note"`
}

func parseDepositBalances(doc *goquery.Document, year int) (map[string]DepositBalance, error) {
	values := map[string]DepositBalance{}
	text := compact(doc.Text())
	if !strings.Contains(text, "金融机构人民币信贷收支表") || strings.Contains(text, "存款类金融机构人民币信贷收支表") {
		return nil, fmt.Errorf("非金融机构人民币信贷收支表")
	}
	if u := unitPattern.FindStringSubmatch(text); u == nil || u[1] != "亿元" {
		return nil, fmt.Errorf("住户存款表单位不是亿元")
	}
	grid, err := tableGrid(doc.Find("table").First())
	if err != nil {
		return nil, err
	}
	months := map[int]string{}
	notes := []string{}
	seen := map[string]bool{}
	inHouseholds := false
	notesStarted := false
	for _, row := range grid {
		if len(row) == 0 {
			continue
		}
		if strings.Contains(row[0], "项目") {
			for col, header := range row {
				p := monthCode(header)
				if p == "" {
					continue
				}
				if p[:4] != strconv.Itoa(year) {
					return nil, fmt.Errorf("住户存款表年份不符: %s", p)
				}
				for _, old := range months {
					if old == p {
						return nil, fmt.Errorf("住户存款表月份重复: %s", p)
					}
				}
				months[col] = p
			}
		}
		label := row[0]
		if strings.HasPrefix(label, "注") {
			notesStarted = true
		}
		if notesStarted {
			if label != "" {
				notes = append(notes, label)
			}
			continue
		}
		key := ""
		switch {
		case strings.Contains(label, "住户存款"):
			inHouseholds = true
			key = "balance"
		case inHouseholds && strings.Contains(label, "定期及其他存款"):
			key = "time-and-other"
		case inHouseholds && strings.Contains(label, "活期存款"):
			key = "demand"
		case inHouseholds && label != "":
			inHouseholds = false
		}
		if key == "" {
			continue
		}
		if len(months) == 0 {
			return nil, fmt.Errorf("住户存款表缺少月份")
		}
		if seen[key] {
			return nil, fmt.Errorf("住户存款表分项重复: %s", key)
		}
		seen[key] = true
		for col, p := range months {
			n, err := number(row[col])
			if err != nil {
				return nil, err
			}
			if n != nil && *n < 0 {
				return nil, fmt.Errorf("住户存款余额不能为负")
			}
			v, ok := values[p]
			if !ok {
				v = DepositBalance{Period: p, RawValues: map[string]string{}}
			}
			v.RawValues[key] = row[col]
			switch key {
			case "balance":
				v.Balance = n
			case "demand":
				v.Demand = n
			case "time-and-other":
				v.TimeAndOther = n
			}
			values[p] = v
		}
	}
	for _, key := range []string{"balance", "demand", "time-and-other"} {
		if !seen[key] {
			return nil, fmt.Errorf("住户存款表缺少分项: %s", key)
		}
	}
	for p, v := range values {
		v.Notes = notes
		values[p] = v
	}
	return values, nil
}

type depositReport struct {
	Period, Source, PublishedDate string
	FetchedAt                     time.Time
	Monthly, YTD, YoY             *float64
	RawText                       string
	Notes                         []string
}

var depositBalancePattern = regexp.MustCompile(`人民币存款余额(?:为)?([0-9.,]+)(万亿元|亿元)`)
var householdDepositYoYPattern = regexp.MustCompile(`住户(?:部门)?存款余额(?:为)?([0-9.,]+)(万亿元|亿元)[，,]同比(增长|下降)([0-9.]+)%`)
var depositFlowStartPattern = regexp.MustCompile(`(?:当月|本月|\d{1,2}月份?|全年|上半年|一季度|第一季度|前三季度|前[一二三四五六七八九十两0-9]+个?月|1[—–\-]\d{1,2}月份?)[，,]?人民币存款(?:增加|减少)`)
var monthlyDepositPrefix = regexp.MustCompile(strings.ReplaceAll(monthlyPrefixPattern.String(), "贷款", "存款"))
var ytdDepositPrefix = regexp.MustCompile(strings.ReplaceAll(ytdPrefixPattern.String(), "贷款", "存款"))
var explicitDepositMonth = regexp.MustCompile(`^(\d{1,2})月份?`)

func parseDepositReport(doc *goquery.Document, period string) (depositReport, error) {
	result := depositReport{Period: period, PublishedDate: doc.Find(`meta[name="PubDate"]`).AttrOr("content", "")}
	body := doc.Find("#zoom")
	if body.Length() == 0 {
		body = doc.Find("body")
	}
	inSection := false
	parts := []string{}
	body.Find("p").Each(func(_ int, p *goquery.Selection) {
		text := compact(p.Text())
		if strings.HasPrefix(text, "注") {
			if strings.Contains(text, "金融统计范围") {
				result.Notes = append(result.Notes, text)
			}
			inSection = false
			return
		}
		if reportSectionPattern.MatchString(text) {
			inSection = strings.Contains(text, "人民币存款")
			return
		}
		if depositBalancePattern.MatchString(text) {
			inSection = true
		}
		if inSection {
			if strings.Contains(text, "外币存款余额") && !strings.Contains(text, "人民币存款余额") {
				inSection = false
				return
			}
			parts = append(parts, text)
		}
	})
	result.RawText = strings.Join(parts, "")
	if !depositBalancePattern.MatchString(result.RawText) {
		return result, fmt.Errorf("报告缺少人民币存款余额段落")
	}
	_, result.YoY = parseMoneyPair(householdDepositYoYPattern, result.RawText)
	starts := depositFlowStartPattern.FindAllStringIndex(result.RawText, -1)
	for i, span := range starts {
		end := len(result.RawText)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		text := result.RawText[span[0]:end]
		value, err := amountInReport(text, `住户(?:部门)?存款`)
		if err != nil {
			return result, err
		}
		if value == nil {
			continue
		}
		target := &result.YTD
		if monthlyDepositPrefix.MatchString(text) {
			if m := explicitDepositMonth.FindStringSubmatch(text); m != nil {
				n, _ := strconv.Atoi(m[1])
				if fmt.Sprintf("%02d", n) != period[4:] {
					return result, fmt.Errorf("住户存款报告月度期间不符")
				}
			}
			target = &result.Monthly
		} else if !ytdDepositPrefix.MatchString(text) {
			return result, fmt.Errorf("无法确认住户存款新增额所属期间")
		}
		if *target != nil && **target != *value {
			return result, fmt.Errorf("住户存款报告新增额冲突")
		}
		*target = value
		if strings.HasSuffix(period, "01") {
			result.YTD = value
			result.Monthly = value
		}
	}
	for _, n := range []*float64{result.Monthly, result.YTD, result.YoY} {
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0)) {
			return result, fmt.Errorf("住户存款报告值超出有限范围")
		}
	}
	return result, nil
}

// RMBDeposits 将期末余额与报告流量分开；余额差额不当作当月新增。
func (c *Client) RMBDeposits(ctx context.Context, req *DepositRequest) (*DepositResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	reports := map[string]depositReport{}
	err := c.walkFinancialReports(ctx, LoanRequest{req.Begin, req.End}, func(period, source string) error {
		var report depositReport
		doc, fetched, err := c.getValidated(ctx, source, func(doc *goquery.Document) error { var e error; report, e = parseDepositReport(doc, period); return e })
		if err != nil {
			return err
		}
		// 缓存命中和下载路径均已校验，保留各份报告的原始所属期与公布日期。
		report, err = parseDepositReport(doc, period)
		if err != nil {
			return err
		}
		report.Source = source
		report.FetchedAt = fetched
		reports[period] = report
		return nil
	})
	if err != nil {
		return nil, err
	}
	root, _, err := c.get(ctx, c.origin+statisticsPath)
	if err != nil {
		return nil, err
	}
	first, _ := strconv.Atoi(req.Begin[:4])
	last, _ := strconv.Atoi(req.End[:4])
	balances := map[string]DepositBalance{}
	for year := first - 1; year <= last; year++ {
		address, err := c.loanTableURL(ctx, year, root)
		if err != nil {
			return nil, err
		}
		doc, fetched, err := c.getValidated(ctx, address, func(doc *goquery.Document) error { _, err := parseDepositBalances(doc, year); return err })
		if err != nil {
			return nil, err
		}
		values, err := parseDepositBalances(doc, year)
		if err != nil {
			return nil, err
		}
		for p, v := range values {
			v.Source = address
			v.FetchedAt = &fetched
			balances[p] = v
		}
	}
	result := &DepositResult{Region: "CN", Frequency: "monthly", Unit: "亿元", Currency: "CNY", Measurement: "month-end-stock", Population: "全国金融机构住户人民币存款", Begin: req.Begin, End: req.End, DataVersion: "current-visible-revision", Observations: []DepositObservation{}, Note: "期末存款余额为名义存量，不是居民收入或新增储蓄；同比按严格上年同月余额计算、未调整统计范围或历史修订，不能当作官方可比同比；报告未公布住户同比时为null，不套用全部人民币存款同比。2023年起部分非存款类机构纳入金融统计范围，保留原表注释。新增从报告提取，不以余额差额代替；相邻累计报告之差为已四舍五入数据的近似值。未季调、未年化；余额表逐期公布时间和历史修订编号不可得。"}
	value := func(p string) DepositBalance {
		if v, ok := balances[p]; ok {
			return v
		}
		return DepositBalance{Period: p, RawValues: map[string]string{}}
	}
	begin, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	for date := begin; !date.After(end); date = date.AddDate(0, 1, 0) {
		p := date.Format("200601")
		item := DepositObservation{DepositBalance: value(p), YearAgoBase: value(date.AddDate(-1, 0, 0).Format("200601")), MonthlyFlowMethod: "unavailable"}
		if item.Balance != nil && item.YearAgoBase.Balance != nil && *item.YearAgoBase.Balance != 0 {
			n := (*item.Balance / *item.YearAgoBase.Balance - 1) * 100
			if math.IsInf(n, 0) || math.IsNaN(n) {
				return nil, fmt.Errorf("住户存款计算同比超出有限范围")
			}
			item.ComputedYoY = &n
		}
		report, ok := reports[p]
		if ok {
			item.YTDFlow = report.YTD
			item.ReportedHouseholdYoY = report.YoY
			item.ReportSource = report.Source
			item.ReportPublishedDate = report.PublishedDate
			item.ReportFetchedAt = &report.FetchedAt
			item.RawReportText = report.RawText
			item.ReportNotes = report.Notes
			previous := reports[date.AddDate(0, -1, 0).Format("200601")]
			currentLoan := loanReport{Period: p, Monthly: map[string]*float64{"households": report.Monthly}, YTD: map[string]*float64{"households": report.YTD}}
			previousLoan := loanReport{Period: previous.Period, YTD: map[string]*float64{"households": previous.YTD}}
			item.MonthlyFlow, item.MonthlyFlowMethod, item.MonthlyFlowApproximate = monthlyLoanFlow(p, "households", currentLoan, previousLoan)
			if item.MonthlyFlow != nil && (math.IsNaN(*item.MonthlyFlow) || math.IsInf(*item.MonthlyFlow, 0)) {
				return nil, fmt.Errorf("住户存款月度变化超出有限范围")
			}
			if item.MonthlyFlowApproximate {
				item.PreviousReportSource = previous.Source
			}
		}
		notes := []string{}
		if item.Balance == nil {
			notes = append(notes, "余额缺失")
		}
		if item.ComputedYoY == nil {
			notes = append(notes, "严格同比基数缺失或为零")
		}
		if !ok {
			notes = append(notes, "目录未检索到报告")
		} else if item.MonthlyFlow == nil {
			notes = append(notes, "报告未提供可用月度值或相邻累计基数")
		}
		item.Note = strings.Join(notes, "；")
		result.Observations = append(result.Observations, item)
	}
	return result, nil
}
