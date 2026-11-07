package quote

import (
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"

	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/history"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewQuoteHistoryCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "quote-history",
		Aliases:       []string{"qh"},
		Short:         "Print quote history of specific security",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Args: cobra.ExactArgs(1),
		RunE: QuoteHistoryHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().BoolP("realtime", "r", false, "Realtime update quote info")
	rootCmd.Flags().Bool("json", false, "Output JSON")
	rootCmd.Flags().BoolP("desc", "d", false, "Order by date in descending order")
	rootCmd.Flags().StringP("begin", "b", "", "Begin date 20250101")
	rootCmd.Flags().StringP("end", "e", "", "End date 20250131")
	rootCmd.Flags().String("freq", "day", "K-line frequency: day, week, month")
	rootCmd.Flags().StringP("fq", "f", "", "FuQuan type choice: bfq none, qfq front, hfq post")

	return rootCmd
}

// QuoteHistoryHandler 查询行情历史
func QuoteHistoryHandler(cmd *cobra.Command, args []string) error {

	// 查询参数由逗号分隔
	key := args[0]
	secs, err := sina.Resolve(cmd.Context(), key)
	if err != nil {
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	num := len(secs)
	if num == 0 {
		if asJSON {
			return fmt.Errorf("未找到证券: %s", key)
		}
		slog.Info("search no sec", "code", key)
		return nil
	}

	// 默认选择第一个查询结果
	sec := secs[0]
	slog.Debug("QuoteHistoryHandler", "num", num, "excode", sec.ExCode, "code", sec.Code, "exchange", sec.ExChange)
	market, err := history.Market(sec)
	if err != nil {
		return err
	}
	req := &eastmoney.GetQuoteHistoryReq{Code: sec.Code, MarketCode: market}

	// 复权类型
	fqt, err := cmd.Flags().GetString("fq")
	if err != nil {
		return err
	}
	switch fqt {
	case "bfq":
		req.FQT = eastmoney.QuoteFQTDefault
	case "qfq":
		req.FQT = eastmoney.QuoteFQTFront
	case "hfq":
		req.FQT = eastmoney.QuoteFQTPost
	default:
		slog.Debug("QuoteHistoryHandler use default fqt", "fqt", fqt)
		req.FQT = eastmoney.QuoteFQTDefault
	}

	beginStr, _ := cmd.Flags().GetString("begin")
	endStr, _ := cmd.Flags().GetString("end")
	freqText, _ := cmd.Flags().GetString("freq")
	req.Freq, err = eastmoney.ParseFreq(freqText)
	if err != nil {
		return err
	}
	// 默认区间按周期放大，根数大致相同。
	req.Begin, req.End, err = utils.ParseBeginEnd(beginStr, endStr, 30*req.Freq.BarDays(), eastmoney.TimeYYMMDD, eastmoney.TimeYYMMDD)
	if err != nil {
		return err
	}

	result, err := history.Get(cmd.Context(), req)
	if err != nil {
		slog.Error("failed QuoteHistoryHandler", "code", req.Code, "error", err)
		return err
	}
	quotes := result.Quotes
	if result.Note != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), result.Note)
		for _, quote := range quotes {
			quote.Name = sec.Name
		}
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

	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), history.NewOutput(sec.ExCode, sec.Name, req, result, quotes))
	}
	printQuoteHistory(cmd.OutOrStdout(), quotes)

	return nil
}

// 没有前一日收盘价时涨跌和振幅未知，不能显示成 0。
func closeText(quote *eastmoney.Quote) string {
	if quote.NoChange {
		return fmt.Sprintf("%-.5g", quote.Close)
	}
	return fmt.Sprintf("%-.5g %-.5g %-.2g%s", quote.Close, quote.Change, quote.ChangeRate, "%")
}

func amplitudeText(quote *eastmoney.Quote) string {
	if quote.NoChange {
		return "-"
	}
	return strconv.FormatFloat(quote.Amplitude, 'g', -1, 64)
}

// 备用数据没有成交额和换手率，不能显示成 0。
func turnoverText(quote *eastmoney.Quote) string {
	if quote.NoTurnover {
		return "-"
	}
	return utils.HumanNum(float64(quote.TurnOver))
}

func velocityText(quote *eastmoney.Quote) string {
	if quote.NoTurnover {
		return "-"
	}
	return strconv.FormatFloat(quote.Velocity, 'g', -1, 64)
}

// printQuote 打印 quote 信息
func printQuoteHistory(out io.Writer, quotes []*eastmoney.Quote) {
	if len(quotes) == 0 {
		return
	}

	headers := []string{"日期", "名称", "收盘", "开盘", "最高", "最低", "成交额", "成交量", "振幅", "换手率", "证券代码"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, len(quotes))
	for _, quote := range quotes {
		combineClose := closeText(quote)
		row := []string{
			utils.TimeYYMMDDString(quote.Date),
			quote.Name,
			combineClose,
			strconv.FormatFloat(quote.Open, 'g', -1, 64),
			strconv.FormatFloat(quote.High, 'g', -1, 64),
			strconv.FormatFloat(quote.Low, 'g', -1, 64),
			turnoverText(quote),
			utils.HumanNum(float64(quote.Volume)),
			amplitudeText(quote),
			velocityText(quote),
			quote.Market.String() + quote.Code,
		}

		data = append(data, row)

		styles := make([]tablewriter.Colors, 0, len(headers))
		for _, title := range headers {
			var item tablewriter.Colors = tablewriter.Colors{}
			// 收盘
			if title == headers[2] {
				v := quote.ChangeRate
				item = termtable.Change(v, true)
			}
			styles = append(styles, item)
		}
		columnsStyles = append(columnsStyles, styles)
	}

	table := termtable.New(out, headers, termtable.LeftHeader)

	for i, row := range data {
		table.Rich(row, columnsStyles[i])
	}

	table.Render()
}
