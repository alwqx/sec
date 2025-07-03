package quote

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewQuoteHistoryCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "quote-history",
		Aliases:       []string{"qh"},
		Short:         "Print quote history of sec",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Print(cmd.UsageString())
		},
		RunE: QuoteHistoryHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().BoolP("realtime", "r", false, "Realtime updaet quote info")
	rootCmd.Flags().BoolP("desc", "d", false, "Order by date in descending order")
	rootCmd.Flags().StringP("begin", "b", "", "Begin date 20250101")
	rootCmd.Flags().StringP("end", "e", "", "End date 20250131")

	return rootCmd
}

// QuoteHistoryHandler 查询行情历史
func QuoteHistoryHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	// 查询参数由逗号分隔
	key := args[0]
	secs := sina.Search(key)
	num := len(secs)
	if num == 0 {
		slog.Info("QuoteHistoryHandler", "no sec fund for %s", key)
		return nil
	}

	// 默认选择第一个查询结果
	sec := secs[0]
	slog.Debug("QuoteHistoryHandler", "num", num, "excode", sec.ExCode, "code", sec.Code, "exchange", sec.ExChange)
	now := time.Now()
	req := &eastmoney.GetQuoteHistoryReq{
		Code: sec.Code,
		End:  now.Format(eastmoney.TimeYYMMDD),
	}

	switch sec.ExChange {
	case "sh":
		req.MarketCode = 1
	case "sz":
		req.MarketCode = 0
	}

	defaultBegin := now.Add(-30 * 24 * time.Hour)
	defaultEnd := now

	beginStr, err := cmd.Flags().GetString("begin")
	if err != nil {
		return err
	}
	// 校验
	if beginStr != "" {
		defaultBegin, err = time.Parse(eastmoney.TimeYYMMDD, beginStr)
		if err != nil {
			return err
		}
		req.Begin = beginStr
	} else {
		req.Begin = now.Add(-30 * 24 * time.Hour).Format(eastmoney.TimeYYMMDD)
	}

	endStr, err := cmd.Flags().GetString("end")
	if err != nil {
		return err
	}
	if endStr != "" {
		defaultEnd, err = time.Parse(eastmoney.TimeYYMMDD, endStr)
		if err != nil {
			return err
		}
		req.End = endStr
	} else {
		req.End = now.Format(eastmoney.TimeYYMMDD)
	}

	if defaultEnd.Before(defaultBegin) {
		bs := defaultBegin.Format(eastmoney.TimeYYMMDD)
		es := defaultEnd.Format(eastmoney.TimeYYMMDD)
		slog.Error("invalid begin and end time", "begin", bs, "end", es)
		return fmt.Errorf("invalid begin %s and end %s", bs, es)
	}

	quotes, err := eastmoney.GetQuoteHistory(req)
	if err != nil {
		slog.Error("QuoteHistoryHandler", "eastmoney.GetQuoteHistory %d", req.Code, "error", err)
		return err
	}

	// 判断是否重新排序
	desc, err := cmd.Flags().GetBool("desc")
	if err != nil {
		return err
	}
	if desc {
		sort.Slice(quotes, func(i, j int) bool {
			return quotes[i].Date.After(quotes[j].Date)
		})
	}

	printQuoteHistory(quotes)

	return nil
}

// printQuote 打印 quote 信息
func printQuoteHistory(quotes []*eastmoney.Quote) {
	if len(quotes) == 0 {
		return
	}

	headers := []string{"日期", "名称", "收盘", "开盘", "最高", "最低", "成交额", "成交量", "振幅", "换手率", "证券代码"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, len(quotes))
	for _, quote := range quotes {
		combineClose := fmt.Sprintf("%-.5g %-.5g %-.2g%s", quote.Close, quote.Change, quote.ChangeRate, "%")
		row := []string{
			utils.TimeYYMMDDString(quote.Date),
			quote.Name,
			combineClose,
			strconv.FormatFloat(quote.Open, 'g', -1, 64),
			strconv.FormatFloat(quote.High, 'g', -1, 64),
			strconv.FormatFloat(quote.Low, 'g', -1, 64),
			types.HumanNum(float64(quote.TurnOver)),
			types.HumanNum(float64(quote.Volume)),
			strconv.FormatFloat(quote.Amplitude, 'g', -1, 64),
			strconv.FormatFloat(quote.Velocity, 'g', -1, 64),
			quote.Market.String() + quote.Code,
		}

		data = append(data, row)

		styles := make([]tablewriter.Colors, 0, len(headers))
		for _, title := range headers {
			var item tablewriter.Colors = tablewriter.Colors{}
			// 收盘
			if title == headers[2] {
				v := quote.ChangeRate
				if v > 0 {
					item = tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgRedColor}
				} else if v < 0 {
					item = tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgGreenColor}
				}
			}
			styles = append(styles, item)
		}
		columnsStyles = append(columnsStyles, styles)
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader(headers)

	for i, row := range data {
		table.Rich(row, columnsStyles[i])
	}

	headerStyles := make([]tablewriter.Colors, 0, len(headers))
	for range headers {
		headerStyles = append(headerStyles, tablewriter.Colors{tablewriter.Bold})
	}

	table.SetHeaderColor(headerStyles...)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetNoWhiteSpace(false)
	table.SetTablePadding("\t")
	table.Render()
}
