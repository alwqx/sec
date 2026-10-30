package mofcom

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var cnyPartnerTitleRE = regexp.MustCompile(`^(\d{4})年(\d{1,2})月进出口商品主要国别（地区）总值表（人民币值）$`)

type cnyPartnerResult struct {
	Rows      []TradePartner
	Source    string
	FetchedAt time.Time
	Note      string
	Scope     string
}

func (c *Client) cnyPartners(ctx context.Context, period string) (cnyPartnerResult, error) {
	result := cnyPartnerResult{Rows: []TradePartner{}}
	err := c.walkCNYReports(ctx, period, period, cnyPartnerTitleRE, func(release articleRelease) error {
		address := articleURL + "?id=" + strconv.Itoa(release.ID)
		var parsed cnyPartnerResult
		_, fetched, err := c.request(ctx, http.MethodGet, address, "https://fdi.mofcom.gov.cn/come-datatongji-con.html?id="+strconv.Itoa(release.ID), nil, nil, func(raw []byte) error {
			var env articleEnvelope
			if err := json.Unmarshal(raw, &env); err != nil || env.Code != 0 || env.Data.ID != release.ID || env.Data.Name != release.Name || env.Data.IssueDate != release.IssueDate || env.Data.Source != "海关总署" {
				return fmt.Errorf("人民币伙伴报告元数据与目录不符")
			}
			var err error
			parsed, err = parseCNYPartners(env.Data.Content, period)
			return err
		})
		if err != nil {
			return err
		}
		parsed.Source, parsed.FetchedAt = address, fetched
		for i := range parsed.Rows {
			parsed.Rows[i].Source, parsed.Rows[i].FetchedAt, parsed.Rows[i].PublishedDate = address, fetched, release.IssueDate
		}
		result = parsed
		return nil
	})
	return result, err
}

func parseCNYPartners(rawHTML, period string) (cnyPartnerResult, error) {
	result := cnyPartnerResult{Rows: []TradePartner{}, Note: "人民币伙伴表含总值、区域组织及成员，范围交叠，不能加总。原始月值、组织成员与范围说明见 --json。"}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rawHTML))
	if err != nil {
		return result, err
	}
	table := doc.Find("table").First()
	if table.Length() == 0 || !strings.Contains(compactText(doc.Text()), "亿元人民币") {
		return result, fmt.Errorf("人民币伙伴报告缺少亿元人民币数据表")
	}
	month, _ := strconv.Atoi(period[4:])
	monthLabel, cumulative := fmt.Sprintf("%d月", month), fmt.Sprintf("1至%d月", month)
	header, subheader := false, false
	seen := map[string]bool{}
	var parseErr error
	table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
		if parseErr != nil {
			return
		}
		cells := []string{}
		tr.ChildrenFiltered("td,th").Each(func(_ int, cell *goquery.Selection) { cells = append(cells, compactText(cell.Text())) })
		if len(cells) == 0 {
			return
		}
		if len(cells) == 1 {
			if strings.HasPrefix(cells[0], "注") {
				result.Scope += cells[0]
			}
			return
		}
		if strings.Contains(cells[0], "进口原产国") {
			header = len(cells) == 5 && strings.Contains(cells[0], "出口最终目的国") && cells[1] == "进出口" && cells[2] == "出口" && cells[3] == "进口" && strings.Contains(cells[4], "累计比去年同期")
			if !header {
				parseErr = fmt.Errorf("人民币伙伴表主表头变化")
			}
			return
		}
		if !subheader {
			want := []string{monthLabel, cumulative, monthLabel, cumulative, monthLabel, cumulative, "进出口", "出口", "进口"}
			if !header || strings.Join(cells, "|") != strings.Join(want, "|") {
				parseErr = fmt.Errorf("人民币伙伴表月份或列顺序变化")
			} else {
				subheader = true
			}
			return
		}
		if len(cells) != 10 || cells[0] == "" || seen[cells[0]] {
			parseErr = fmt.Errorf("人民币伙伴表行缺列或名称重复")
			return
		}
		seen[cells[0]] = true
		item := TradePartner{Name: cells[0], Period: period, Currency: "CNY", Unit: "亿元人民币", RawValues: map[string]string{"name": cells[0]}}
		fields := []struct {
			key  string
			dest **float64
		}{{"total_monthly", &item.TotalMonthly}, {"total_ytd", &item.TotalYTD}, {"exports_monthly", &item.ExportsMonthly}, {"exports_ytd", &item.ExportsYTD}, {"imports_monthly", &item.ImportsMonthly}, {"imports_ytd", &item.ImportsYTD}, {"total_ytd_yoy_percent", &item.TotalYTDYoY}, {"exports_ytd_yoy_percent", &item.ExportsYTDYoY}, {"imports_ytd_yoy_percent", &item.ImportsYTDYoY}}
		for i, field := range fields {
			value, err := parseTextNumber(cells[i+1])
			if err != nil || i < 6 && value != nil && *value < 0 {
				parseErr = fmt.Errorf("人民币伙伴表%s数值无效", field.key)
				return
			}
			*field.dest = value
			item.RawValues[field.key] = cells[i+1]
		}
		result.Rows = append(result.Rows, item)
	})
	if parseErr != nil {
		return result, parseErr
	}
	if !header || !subheader || !seen["总值"] || len(result.Rows) < 2 {
		return result, fmt.Errorf("人民币伙伴表缺少总值或伙伴记录")
	}
	return result, nil
}
