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

const financialReportsPath = "/diaochatongjisi/116219/116225/index.html"

type loanReport struct {
	Period                string
	Balance, YoY          *float64
	M1Balance, M1YoY      *float64
	M2Balance, M2YoY      *float64
	Monthly, YTD          map[string]*float64
	Source, PublishedDate string
	FetchedAt             time.Time
	Notes                 []string
}

var financialTitlePattern = regexp.MustCompile(`^(\d{4})年(.*?)金融统计数据报告$`)
var reportMonthPattern = regexp.MustCompile(`^(\d{1,2})月份?$`)

func financialReportPeriod(title string) string {
	m := financialTitlePattern.FindStringSubmatch(compact(title))
	if m == nil {
		return ""
	}
	month := 0
	switch m[2] {
	case "", "全年":
		month = 12
	case "一季度", "第一季度":
		month = 3
	case "上半年":
		month = 6
	case "前三季度":
		month = 9
	default:
		if n := reportMonthPattern.FindStringSubmatch(m[2]); n != nil {
			month, _ = strconv.Atoi(n[1])
		}
	}
	if month < 1 || month > 12 {
		return ""
	}
	return fmt.Sprintf("%s%02d", m[1], month)
}

func (c *Client) loanReports(ctx context.Context, req LoanRequest) (map[string]loanReport, error) {
	begin, _ := time.Parse("200601", req.Begin)
	earliest := req.Begin
	if begin.Month() != time.January {
		earliest = begin.AddDate(0, -1, 0).Format("200601")
	}
	result := map[string]loanReport{}
	seen := map[string]bool{}
	address := c.origin + financialReportsPath
	for page := 0; page < 100; page++ {
		if seen[address] {
			return nil, fmt.Errorf("金融报告分页链接循环")
		}
		seen[address] = true
		doc, _, err := c.get(ctx, address)
		if err != nil {
			return nil, err
		}
		older := false
		var parseErr error
		doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			if parseErr != nil {
				return
			}
			period := financialReportPeriod(a.Text())
			if period == "" {
				return
			}
			if period < earliest {
				older = true
				return
			}
			if period > req.End {
				return
			}
			if _, ok := result[period]; ok {
				return
			}
			href, _ := a.Attr("href")
			source, err := c.resolve(address, href)
			if err != nil {
				parseErr = err
				return
			}
			reportDoc, fetched, err := c.get(ctx, source)
			if err != nil {
				parseErr = err
				return
			}
			report, err := parseLoanReport(reportDoc, period)
			if err != nil {
				parseErr = fmt.Errorf("%s 金融统计报告（%s）: %w", period, source, err)
				return
			}
			report.Source = source
			report.FetchedAt = fetched
			result[period] = report
		})
		if parseErr != nil {
			return nil, parseErr
		}
		if older {
			return result, nil
		}
		next := ""
		doc.Find("a").Each(func(_ int, a *goquery.Selection) {
			if compact(a.Text()) != "下一页" {
				return
			}
			href, _ := a.Attr("tagname")
			if !strings.HasPrefix(href, "/") {
				href, _ = a.Attr("href")
			}
			if href == "" {
				return
			}
			u, err := c.resolve(address, href)
			if err == nil {
				next = u
			}
		})
		if next == "" {
			return result, nil
		}
		address = next
	}
	return nil, fmt.Errorf("金融统计报告超过100页，请缩小查询范围")
}

var bankLoanBalancePattern = regexp.MustCompile(`人民币贷款余额(?:为)?([0-9.,]+)(万亿元|亿元)[，,]同比(增长|下降)([0-9.]+)%`)
var moneySupplyPattern = regexp.MustCompile(`(?:广义货币(?:供应量)?[（(]M2[）)]|M2)余额(?:为)?([0-9.,]+)(万亿元|亿元)[，,]同比(增长|下降)([0-9.]+)%`)
var narrowMoneyPattern = regexp.MustCompile(`(?:狭义货币(?:供应量)?[（(]M1[）)]|M1)余额(?:为)?([0-9.,]+)(万亿元|亿元)[，,]同比(增长|下降)([0-9.]+)%`)
var householdLoanPattern = regexp.MustCompile(`住户(?:部门)?贷款`)
var enterpriseLoanPattern = regexp.MustCompile(`(?:企[（(]事[）)]业单位|非金融企业及机关团体|非金融企业及其他部门)贷款`)
var monthlyPrefixPattern = regexp.MustCompile(`^(?:当月|本月|\d{1,2}月份?)[，,]?人民币贷款`)
var ytdPrefixPattern = regexp.MustCompile(`^(?:全年|上半年|一季度|第一季度|前三季度|前[一二三四五六七八九十两0-9]+个?月|1[—–\-]\d{1,2}月份?)[，,]?人民币贷款`)
var explicitMonthlyLoanPattern = regexp.MustCompile(`(?:^|[。；;])(?:当月|本月|(\d{1,2})月份?)[，,]?人民币贷款(增加|减少)([0-9.,]+)(万亿元|亿元)`)
var reportSectionPattern = regexp.MustCompile(`^[一二三四五六七八九十]+、`)

func amountInReport(text, prefix string) (*float64, error) {
	pattern := regexp.MustCompile(prefix + `(增加|减少)([0-9.,]+)(万亿元|亿元)`)
	m := pattern.FindStringSubmatch(text)
	if m == nil {
		return nil, nil
	}
	n, err := number(m[2])
	if err != nil {
		return nil, err
	}
	value := normalise(n, m[3])
	if m[1] == "减少" {
		v := -*value
		value = &v
	}
	return value, nil
}

func parseLoanReport(doc *goquery.Document, period string) (loanReport, error) {
	result := loanReport{Period: period, Monthly: map[string]*float64{}, YTD: map[string]*float64{}}
	result.PublishedDate = doc.Find(`meta[name="PubDate"]`).AttrOr("content", "")
	body := doc.Find("#zoom")
	if body.Length() == 0 {
		body = doc.Find("body")
	}
	fullText := compact(body.Text())
	result.M2Balance, result.M2YoY = parseMoneyPair(moneySupplyPattern, fullText)
	result.M1Balance, result.M1YoY = parseMoneyPair(narrowMoneyPattern, fullText)
	flowText := ""
	inLoanSection := false
	var parseErr error
	body.Find("p").Each(func(_ int, p *goquery.Selection) {
		text := compact(p.Text())
		if strings.Contains(text, "人民币贷款余额") && !strings.Contains(text, "对实体经济") {
			if m := bankLoanBalancePattern.FindStringSubmatch(text); m != nil {
				if result.Balance != nil {
					parseErr = fmt.Errorf("银行贷款余额段重复")
					return
				}
				n, err := number(m[1])
				if err != nil {
					parseErr = err
					return
				}
				result.Balance = normalise(n, m[2])
				result.YoY, err = number(m[4])
				if err != nil {
					parseErr = err
					return
				}
				if m[3] == "下降" {
					v := -*result.YoY
					result.YoY = &v
				}
				inLoanSection = true
				return
			}
		}
		if inLoanSection {
			if reportSectionPattern.MatchString(text) || (strings.Contains(text, "外币贷款余额") && !strings.Contains(text, "人民币贷款余额")) || strings.HasPrefix(text, "注") {
				inLoanSection = false
			} else {
				// 兼容总额、住户及企业分别成段的旧版报告，不要求“分部门看”字样。
				flowText += text
			}
		}
		if strings.HasPrefix(text, "注") && (strings.Contains(text, "金融统计范围") || strings.Contains(text, "企（事）业单位贷款") || strings.Contains(text, "新修订的狭义货币") || strings.Contains(text, "修订后的M1")) {
			result.Notes = append(result.Notes, text)
		}
	})
	if parseErr != nil {
		return result, parseErr
	}
	if result.Balance == nil || result.YoY == nil {
		return result, fmt.Errorf("缺少银行口径人民币贷款余额或同比")
	}
	if flowText == "" {
		return result, fmt.Errorf("缺少可识别的银行贷款新增额及分部门段落")
	}
	houseIndex := householdLoanPattern.FindStringIndex(flowText)
	enterpriseIndex := enterpriseLoanPattern.FindStringIndex(flowText)
	if houseIndex == nil || enterpriseIndex == nil || enterpriseIndex[0] <= houseIndex[0] {
		return result, fmt.Errorf("贷款分部门结构无法识别")
	}
	householdText := flowText[houseIndex[0]:enterpriseIndex[0]]
	enterpriseText := flowText[enterpriseIndex[0]:]
	if end := strings.Index(enterpriseText, "非银行业金融机构贷款"); end >= 0 {
		enterpriseText = enterpriseText[:end]
	}
	values := map[string]*float64{}
	for _, part := range []struct{ key, text, prefix string }{
		{"total", flowText[:houseIndex[0]], "人民币贷款"},
		{"households", householdText, `住户(?:部门)?贷款`},
		{"households-short", householdText, "短期贷款"}, {"households-long", householdText, "中长期贷款"},
		{"enterprises", enterpriseText, enterpriseLoanPattern.String()},
		{"enterprises-short", enterpriseText, "短期贷款"}, {"enterprises-long", enterpriseText, "中长期贷款"}, {"enterprise-bills", enterpriseText, "票据融资"},
	} {
		n, err := amountInReport(part.text, part.prefix)
		if err != nil {
			return result, err
		}
		values[part.key] = n
	}
	if values["total"] == nil {
		return result, fmt.Errorf("无法识别人民币贷款新增总额")
	}
	if monthlyPrefixPattern.MatchString(flowText) {
		result.Monthly = values
		if strings.HasSuffix(period, "01") {
			result.YTD = values
		}
	} else if ytdPrefixPattern.MatchString(flowText) {
		result.YTD = values
	} else {
		return result, fmt.Errorf("无法确认新增贷款所属期间，不将未知期间默认为累计")
	}
	// 季报/年报常在累计结构段落末尾另列本月新增总额，优先保留官方月度值。
	for _, m := range explicitMonthlyLoanPattern.FindAllStringSubmatch(flowText, -1) {
		if m[1] != "" {
			month, _ := strconv.Atoi(m[1])
			if fmt.Sprintf("%02d", month) != period[4:] {
				continue
			}
		}
		n, err := number(m[3])
		if err != nil {
			return result, err
		}
		value := normalise(n, m[4])
		if m[2] == "减少" {
			v := -*value
			value = &v
		}
		if old := result.Monthly["total"]; old != nil && *old != *value {
			return result, fmt.Errorf("官方月度贷款新增总额冲突")
		}
		result.Monthly["total"] = value
	}
	return result, nil
}

func parseMoneyPair(pattern *regexp.Regexp, text string) (*float64, *float64) {
	m := pattern.FindStringSubmatch(text)
	if m == nil {
		return nil, nil
	}
	balance, err := number(m[1])
	if err != nil {
		return nil, nil
	}
	yoy, err := number(m[4])
	if err != nil {
		return nil, nil
	}
	balance = normalise(balance, m[2])
	if m[3] == "下降" && yoy != nil {
		v := -*yoy
		yoy = &v
	}
	return balance, yoy
}

func monthlyLoanFlow(period, key string, current, previous loanReport) (*float64, string, bool) {
	if value := current.Monthly[key]; value != nil {
		return value, "published-monthly", false
	}
	if strings.HasSuffix(period, "01") {
		if value := current.YTD[key]; value != nil {
			return value, "january-published-ytd", false
		}
	}
	date, _ := time.Parse("200601", period)
	if previous.Period == date.AddDate(0, -1, 0).Format("200601") && current.YTD[key] != nil && previous.YTD[key] != nil && previous.Period[:4] == period[:4] {
		value := math.Round((*current.YTD[key]-*previous.YTD[key])*1e6) / 1e6
		return &value, "difference-of-rounded-ytd-releases", true
	}
	return nil, "unavailable", false
}
