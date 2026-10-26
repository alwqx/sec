package pbc

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type LoanRequest struct{ Begin, End string }

func (r LoanRequest) Validate() error { return (FinancingRequest{r.Begin, r.End}).Validate() }

type LoanComponent struct {
	Key                    string   `json:"key"`
	Name                   string   `json:"name"`
	Balance                *float64 `json:"balance"`
	MonthlyFlow            *float64 `json:"monthly_flow"`
	YTDFlow                *float64 `json:"ytd_flow"`
	MonthlyFlowMethod      string   `json:"monthly_flow_method"`
	MonthlyFlowApproximate bool     `json:"monthly_flow_approximate"`
}

// LoanObservation 分开保存精确余额表、报告同比及新增额，累计报告差额标记为近似计算值。
type LoanObservation struct {
	Region                 string          `json:"region"`
	Period                 string          `json:"period"`
	YTDPeriod              string          `json:"ytd_period"`
	Balance                *float64        `json:"balance"`
	ReportedBalance        *float64        `json:"reported_balance"`
	YoY                    *float64        `json:"yoy_percent"`
	MonthlyFlow            *float64        `json:"monthly_flow"`
	YTDFlow                *float64        `json:"ytd_flow"`
	MonthlyFlowMethod      string          `json:"monthly_flow_method"`
	MonthlyFlowApproximate bool            `json:"monthly_flow_approximate"`
	Unit                   string          `json:"unit"`
	Components             []LoanComponent `json:"components"`
	BalanceSource          string          `json:"balance_source"`
	BalanceFetchedAt       time.Time       `json:"balance_fetched_at"`
	ReportSource           string          `json:"report_source,omitempty"`
	ReportPublishedDate    string          `json:"report_published_date,omitempty"`
	ReportFetchedAt        *time.Time      `json:"report_fetched_at"`
	PreviousReportSource   string          `json:"previous_report_source,omitempty"`
	BalanceNotes           []string        `json:"balance_notes"`
	ReportNotes            []string        `json:"report_notes"`
	Note                   string          `json:"note,omitempty"`
}

var loanComponents = []struct{ key, name string }{
	{"total", "各项人民币贷款"}, {"households", "住户贷款"}, {"households-short", "住户短期贷款"}, {"households-long", "住户中长期贷款"},
	{"enterprises", "企（事）业单位贷款"}, {"enterprises-short", "企（事）业单位短期贷款"}, {"enterprises-long", "企（事）业单位中长期贷款"}, {"enterprise-bills", "企业票据融资"},
}

func (c *Client) loanTableURL(ctx context.Context, year int, root *goquery.Document) (string, error) {
	yearURL, err := c.link(root, c.origin+statisticsPath, strconv.Itoa(year)+"年统计数据")
	if err != nil {
		return "", err
	}
	doc, _, err := c.get(ctx, yearURL)
	if err != nil {
		return "", err
	}
	// 官方标题附英文，限定前缀并排除嵌套布局节点。
	category := ""
	doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		if strings.HasPrefix(compact(a.Text()), "金融机构信贷收支统计") {
			href, _ := a.Attr("href")
			u, e := c.resolve(yearURL, href)
			if e == nil && category == "" {
				category = u
			}
		}
	})
	if category == "" {
		return "", fmt.Errorf("%d 年目录缺少金融机构信贷收支统计", year)
	}
	doc, _, err = c.get(ctx, category)
	if err != nil {
		return "", err
	}
	result := ""
	var linkErr error
	doc.Find("tr").Each(func(_ int, row *goquery.Selection) {
		match := false
		row.ChildrenFiltered("td,th").Each(func(_ int, cell *goquery.Selection) {
			if cell.Find("table").Length() == 0 && strings.HasPrefix(compact(cell.Text()), "金融机构人民币信贷收支表") {
				match = true
			}
		})
		if !match {
			return
		}
		row.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href, _ := a.Attr("href")
			if !strings.HasSuffix(strings.ToLower(href), ".htm") {
				return
			}
			u, e := c.resolve(category, href)
			if e != nil {
				linkErr = e
				return
			}
			if result != "" && result != u {
				linkErr = fmt.Errorf("人民币贷款余额表链接重复")
				return
			}
			result = u
		})
	})
	if linkErr != nil {
		return "", linkErr
	}
	if result == "" {
		return "", fmt.Errorf("%d 年目录缺少金融机构人民币信贷收支 HTML 表", year)
	}
	return result, nil
}

type loanBalances struct {
	Values map[string]map[string]*float64
	Notes  []string
}

func parseLoanBalances(doc *goquery.Document, year int) (loanBalances, error) {
	result := loanBalances{Values: map[string]map[string]*float64{}}
	if !strings.Contains(compact(doc.Text()), "金融机构人民币信贷收支表") {
		return result, fmt.Errorf("非金融机构人民币信贷收支表")
	}
	unit := unitPattern.FindStringSubmatch(compact(doc.Text()))
	if unit == nil || unit[1] != "亿元" {
		return result, fmt.Errorf("贷款余额表单位不是亿元")
	}
	grid, err := tableGrid(doc.Find("table").First())
	if err != nil {
		return result, err
	}
	var months []string
	inLoans := false
	department := ""
	seen := map[string]bool{}
	for _, row := range grid {
		if len(row) == 0 {
			continue
		}
		for _, cell := range row {
			if period := monthCode(cell); period != "" && strings.HasPrefix(period, strconv.Itoa(year)) {
				months = row
				break
			}
		}
		label := row[0]
		if strings.HasPrefix(label, "注") || strings.Contains(label, "金融机构包括") || strings.Contains(label, "2023年1月") {
			result.Notes = append(result.Notes, label)
		}
		key := ""
		switch {
		case strings.Contains(label, "一、各项贷款"):
			inLoans = true
			department = ""
			key = "total"
		case strings.HasPrefix(label, "二、"):
			inLoans = false
		case !inLoans:
			continue
		case strings.Contains(label, "住户贷款"):
			department = "households"
			key = department
		case strings.Contains(label, "企（事）业单位贷款") || strings.Contains(label, "非金融企业及机关团体贷款"):
			department = "enterprises"
			key = department
		case strings.Contains(label, "非银行业金融机构贷款"):
			department = ""
		case strings.Contains(label, "短期贷款") && department != "":
			key = department + "-short"
		case strings.Contains(label, "中长期贷款") && department != "":
			key = department + "-long"
		case strings.Contains(label, "票据融资") && department == "enterprises":
			key = "enterprise-bills"
		}
		if key == "" {
			continue
		}
		if months == nil {
			return result, fmt.Errorf("贷款余额表缺少月份表头")
		}
		if seen[key] {
			return result, fmt.Errorf("贷款余额分项重复: %s", key)
		}
		seen[key] = true
		for col, header := range months {
			period := monthCode(header)
			if period == "" || !strings.HasPrefix(period, strconv.Itoa(year)) {
				continue
			}
			n, err := number(row[col])
			if err != nil {
				return result, err
			}
			if n != nil && *n < 0 {
				return result, fmt.Errorf("贷款余额不能为负")
			}
			if result.Values[period] == nil {
				result.Values[period] = map[string]*float64{}
			}
			result.Values[period][key] = n
		}
	}
	for _, component := range loanComponents {
		if !seen[component.key] {
			return result, fmt.Errorf("贷款余额表缺少 %s", component.name)
		}
	}
	return result, nil
}

// RMBLoans 精确余额来自年度表，官方同比和新增额来自金融统计数据报告。
func (c *Client) RMBLoans(ctx context.Context, req LoanRequest) ([]LoanObservation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	reports, err := c.loanReports(ctx, req)
	if err != nil {
		return nil, err
	}
	root, _, err := c.get(ctx, c.origin+statisticsPath)
	if err != nil {
		return nil, err
	}
	first, _ := strconv.Atoi(req.Begin[:4])
	last, _ := strconv.Atoi(req.End[:4])
	var items []LoanObservation
	for year := first; year <= last; year++ {
		address, err := c.loanTableURL(ctx, year, root)
		if err != nil {
			return nil, err
		}
		doc, fetched, err := c.get(ctx, address)
		if err != nil {
			return nil, err
		}
		balances, err := parseLoanBalances(doc, year)
		if err != nil {
			return nil, fmt.Errorf("%d 年贷款表: %w", year, err)
		}
		var periods []string
		for period := range balances.Values {
			if period >= req.Begin && period <= req.End {
				periods = append(periods, period)
			}
		}
		sort.Strings(periods)
		for _, period := range periods {
			values := balances.Values[period]
			if values["total"] == nil {
				continue
			}
			item := LoanObservation{Region: "CN", Period: period, YTDPeriod: period[:4] + "01-" + period, Balance: values["total"], Unit: "亿元", BalanceSource: address, BalanceFetchedAt: fetched, BalanceNotes: balances.Notes}
			report, ok := reports[period]
			if ok {
				item.ReportSource = report.Source
				item.ReportPublishedDate = report.PublishedDate
				item.ReportFetchedAt = &report.FetchedAt
				item.ReportedBalance = report.Balance
				item.YoY = report.YoY
				item.YTDFlow = report.YTD["total"]
				item.ReportNotes = report.Notes
			}
			date, _ := time.Parse("200601", period)
			previous := reports[date.AddDate(0, -1, 0).Format("200601")]
			if date.Month() != time.January {
				item.PreviousReportSource = previous.Source
			}
			item.MonthlyFlow, item.MonthlyFlowMethod, item.MonthlyFlowApproximate = monthlyLoanFlow(period, "total", report, previous)
			for _, component := range loanComponents[1:] {
				flow, method, approx := monthlyLoanFlow(period, component.key, report, previous)
				item.Components = append(item.Components, LoanComponent{Key: component.key, Name: component.name, Balance: values[component.key], MonthlyFlow: flow, YTDFlow: report.YTD[component.key], MonthlyFlowMethod: method, MonthlyFlowApproximate: approx})
			}
			if !ok {
				item.Note = "未检索到该月官方金融统计报告，同比及新增额保留缺失"
			} else if item.MonthlyFlowApproximate {
				item.Note = "当月新增由相邻报告的已四舍五入累计额相减，属于近似计算值"
			} else if item.MonthlyFlow == nil {
				item.Note = "缺少月度新增或相邻累计报告，不以余额差额替代"
			}
			items = append(items, item)
		}
	}
	return items, nil
}
