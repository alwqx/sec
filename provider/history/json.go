package history

import (
	"github.com/alwqx/sec/provider/eastmoney"
)

// Row 是一根 K 线的 --json 输出。数据源没有的字段是 null，不是 0。
type Row struct {
	Date         string   `json:"date"`
	Open         float64  `json:"open"`
	Close        float64  `json:"close"`
	High         float64  `json:"high"`
	Low          float64  `json:"low"`
	Volume       int64    `json:"volume"`        // 成交量，单位：股或份
	Amount       *float64 `json:"amount"`        // 成交额
	TurnoverRate *float64 `json:"turnover_rate"` // 换手率，单位 %
	Change       *float64 `json:"change"`
	ChangeRate   *float64 `json:"change_rate"` // 单位 %
	Amplitude    *float64 `json:"amplitude"`   // 单位 %
}

// Output 是 K 线类命令的 --json 输出。
type Output struct {
	ExCode     string `json:"ex_code"`
	Name       string `json:"name"`
	Freq       string `json:"freq"`
	FQ         string `json:"fq"`
	SourceNote string `json:"source_note"`
	Results    []Row  `json:"results"`
}

// NewOutput 组装 --json 输出。
func NewOutput(exCode, name string, req *eastmoney.GetQuoteHistoryReq, result *Result, quotes []*eastmoney.Quote) Output {
	output := Output{ExCode: exCode, Name: name, Freq: string(req.Freq), FQ: fqName(req.FQT), SourceNote: result.Note, Results: make([]Row, 0, len(quotes))}
	if output.Freq == "" {
		output.Freq = string(eastmoney.FreqDay)
	}
	for _, quote := range quotes {
		row := Row{Date: quote.Date.Format("2006-01-02"), Open: quote.Open, Close: quote.Close, High: quote.High, Low: quote.Low, Volume: quote.Volume}
		if !quote.NoTurnover {
			row.Amount, row.TurnoverRate = number(quote.TurnOver), number(quote.Velocity)
		}
		if !quote.NoChange {
			row.Change, row.ChangeRate, row.Amplitude = number(quote.Change), number(quote.ChangeRate), number(quote.Amplitude)
		}
		output.Results = append(output.Results, row)
	}
	return output
}

func number(value float64) *float64 { return &value }

func fqName(fqt eastmoney.FuQuanType) string {
	switch fqt {
	case eastmoney.QuoteFQTFront:
		return "qfq"
	case eastmoney.QuoteFQTPost:
		return "hfq"
	}
	return "bfq"
}
