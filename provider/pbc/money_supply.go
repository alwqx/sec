package pbc

import (
	"context"
	"time"
)

type MoneySupplyRequest struct{ Begin, End string }

func (r MoneySupplyRequest) Validate() error {
	return (FinancingRequest{Begin: r.Begin, End: r.End}).Validate()
}

// MoneySupplyObservation 保存人民银行金融统计报告直接公布的 M1、M2 余额和同比。
type MoneySupplyObservation struct {
	Region           string    `json:"region"`
	Period           string    `json:"period"`
	M1Balance        *float64  `json:"m1_balance"`
	M1YoY            *float64  `json:"m1_yoy_percent"`
	M1Definition     string    `json:"m1_definition"`
	M2Balance        *float64  `json:"m2_balance"`
	M2YoY            *float64  `json:"m2_yoy_percent"`
	Unit             string    `json:"unit"`
	Source           string    `json:"source"`
	PublishedDate    string    `json:"published_date,omitempty"`
	FetchedAt        time.Time `json:"fetched_at"`
	StatisticalNotes []string  `json:"statistical_notes"`
	Note             string    `json:"note,omitempty"`
}

// MoneySupply 读取金融统计数据报告的 M1、M2 期末余额及官方同比。
func (c *Client) MoneySupply(ctx context.Context, req MoneySupplyRequest) ([]MoneySupplyObservation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	reports, err := c.loanReports(ctx, LoanRequest{Begin: req.Begin, End: req.End})
	if err != nil {
		return nil, err
	}
	start, _ := time.Parse("200601", req.Begin)
	end, _ := time.Parse("200601", req.End)
	items := make([]MoneySupplyObservation, 0, int(end.Sub(start).Hours()/(24*28))+1)
	for date := start; !date.After(end); date = date.AddDate(0, 1, 0) {
		period := date.Format("200601")
		report, found := reports[period]
		item := MoneySupplyObservation{
			Region: "CN", Period: period, Unit: "亿元",
		}
		if period >= "202501" {
			item.M1Definition = "revised-2025"
		} else {
			item.M1Definition = "original"
		}
		if found {
			item.M1Balance, item.M1YoY = report.M1Balance, report.M1YoY
			item.M2Balance, item.M2YoY = report.M2Balance, report.M2YoY
			item.Source, item.PublishedDate, item.FetchedAt = report.Source, report.PublishedDate, report.FetchedAt
			item.StatisticalNotes = report.Notes
		} else {
			item.Note = "当前金融统计报告目录未检索到该月份；保留缺失值"
		}
		if found {
			if item.M1Balance == nil || item.M1YoY == nil {
				item.Note = "报告未提供可解析的 M1 余额或同比"
			}
			if item.M2Balance == nil || item.M2YoY == nil {
				if item.Note != "" {
					item.Note += "；"
				}
				item.Note += "报告未提供可解析的 M2 余额或同比"
			}
		}
		items = append(items, item)
	}
	return items, nil
}
