package metal

import (
	"github.com/alwqx/sec/provider/metal"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// dailyRow 是 --json 输出。没有前一日收盘价时涨跌是 null。
type dailyRow struct {
	Date       string   `json:"date"`
	Open       float64  `json:"open"`
	Close      float64  `json:"close"`
	High       float64  `json:"high"`
	Low        float64  `json:"low"`
	YClose     *float64 `json:"yclose"`
	Change     *float64 `json:"change"`
	ChangeRate *float64 `json:"change_rate"` // 单位 %
}

func newDailyRow(item *metal.DailyHQItem) dailyRow {
	row := dailyRow{Date: item.Date, Open: item.Open, Close: item.Close, High: item.High, Low: item.Low}
	if item.YClose > 0 {
		// provider 里的涨跌幅是比例，输出统一为 %，和其他命令一致。
		rate := item.ChangeRate * 100
		row.YClose, row.Change, row.ChangeRate = &item.YClose, &item.Change, &rate
	}
	return row
}

func printDailyJSON(cmd *cobra.Command, inst metal.Instrument, items []*metal.DailyHQItem) error {
	rows := make([]dailyRow, 0, len(items))
	for _, item := range items {
		rows = append(rows, newDailyRow(item))
	}
	return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"inst": inst.ID, "name": inst.Name, "unit": inst.Unit, "results": rows})
}
