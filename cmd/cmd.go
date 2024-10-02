package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/version"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "sec",
		Short:         "Secutiry Information Client",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Run: func(cmd *cobra.Command, args []string) {
			if version, _ := cmd.Flags().GetBool("version"); version {
				versionHandler(cmd, args)
				return
			}

			cmd.Print(cmd.UsageString())
		},
	}

	rootCmd.Flags().BoolP("version", "v", false, "Show version information")

	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search code and name of a secutiry/stock",
		Args:  cobra.ExactArgs(1),
		RunE:  SearchHandler,
	}

	infoCmd := &cobra.Command{
		Use:   "info",
		Short: "Print basic information of a secutiry/stock",
		Args:  cobra.ExactArgs(1),
		RunE:  InfoHandler,
	}

	quoteCmd := &cobra.Command{
		Use:   "quote",
		Short: "Print quote information of a secutiry/stock",
		Args:  cobra.ExactArgs(1),
		RunE:  QuoteHandler,
	}

	rootCmd.AddCommand(searchCmd, infoCmd, quoteCmd)

	return rootCmd
}

func versionHandler(cmd *cobra.Command, _ []string) {
	fmt.Printf("sec version is %s\n", version.Version)
}

func SearchHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	secs := sina.Search(args[0])
	printSecs(secs)

	return nil
}

func InfoHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	// 1. search security
	secs := sina.Search(args[0])
	if len(secs) == 0 {
		slog.Warn(fmt.Sprintf("no result of %s", args[0]))
		return nil
	}

	// 2. choose the first item
	sec := secs[0]
	profile := sina.Profile(sec.ExCode)
	fmt.Printf("基本信息\n证券代码\t%s\n简称历史\t%s\n公司名称\t%s\n上市日期\t%s\n发行价格\t%.2f\n行业分类\t%s\n主营业务\t%s\n办公地址\t%s\n公司网址\t%s\n当前价格\t%.2f\n市净率PB\t%.2f\n市盈率TTM\t%.2f\n总市值  \t%s\n流通市值\t%s\n",
		sec.ExCode, profile.HistoryName, profile.Name, profile.ListingDate, profile.ListingPrice,
		profile.Category, profile.MainBusiness, profile.BusinessAddress, profile.WebSite,
		profile.Current, profile.PB, profile.PeTTM, humanNum(profile.MarketCap), humanNum(profile.TradedMarketCap))

	return nil
}

func humanNum(cap float64) (res string) {
	if cap <= 0.0 {
		res = " - "
	} else if cap > 100_000_000.0 {
		res = fmt.Sprintf("%-.2f亿", cap/100_000_000.0)
	} else {
		res = fmt.Sprintf("%-.2f万", cap/10_000.0)
	}
	return
}

func QuoteHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	// 1. search security
	secs := sina.Search(args[0])
	if len(secs) == 0 {
		slog.Warn(fmt.Sprintf("no result of %s", args[0]))
		return nil
	}

	// 2. choose the first item
	sec := secs[0]
	quote, err := sina.Quote(sec.ExCode)
	if err != nil {
		return err
	}
	if quote == nil {
		slog.Warn(fmt.Sprintf("no result of %s", args[0]))
		return nil
	}
	// TODO: 港股指数成交额 * 1000
	printQuote(quote)

	return nil
}

func printQuote(quote *types.SinaQuote) {
	if quote == nil {
		return
	}

	// 计算涨跌
	rate := (quote.Current/quote.YClose - 1.0) * 100.0
	curWithRate := fmt.Sprintf("%-.2f %-.2f%s", quote.Current, rate, "%")

	data := [][]string{
		{
			fmt.Sprintf("%s %s", quote.TradeDate, quote.Time),
			quote.Name,
			quote.Code,
			// strconv.FormatFloat(quote.Current, 'g', -1, 64),
			curWithRate,
			strconv.FormatFloat(quote.YClose, 'g', -1, 64),
			strconv.FormatFloat(quote.Open, 'g', -1, 64),
			strconv.FormatFloat(quote.High, 'g', -1, 64),
			strconv.FormatFloat(quote.Low, 'g', -1, 64),
			humanNum(float64(quote.TurnOver)),
			humanNum(quote.Volume),
		},
	}
	table := tablewriter.NewWriter(os.Stdout)
	headers := []string{"时间", "名称", "证券代码", "当前价格", "昨收", "今开", "最高", "最低", "成交量", "成交额"}
	table.SetHeader(headers)

	headerStyles := make([]tablewriter.Colors, 0, len(headers))
	for range headers {
		headerStyles = append(headerStyles, tablewriter.Colors{tablewriter.Bold})
	}

	columnsStyles := make([]tablewriter.Colors, 0, len(headers))
	for _, title := range headers {
		var item tablewriter.Colors = tablewriter.Colors{}
		if title == headers[3] {
			if rate > 0 {
				item = tablewriter.Colors{tablewriter.Bold, tablewriter.FgRedColor}
			} else if rate < 0 {
				item = tablewriter.Colors{tablewriter.Bold, tablewriter.FgGreenColor}
			}
		}
		columnsStyles = append(columnsStyles, item)
	}

	table.SetHeaderColor(headerStyles...)
	table.SetColumnColor(columnsStyles...)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")
	table.AppendBulk(data)
	table.Render()
}

func printSecs(secs []types.BasicSecurity) {
	num := len(secs)
	if num == 0 {
		return
	}

	data := make([][]string, 0, num)
	for _, sec := range secs {
		data = append(data, []string{sec.ExCode, sec.Name})
	}

	table := tablewriter.NewWriter(os.Stdout)
	headers := []string{"证券代码", "证券名称"}
	table.SetHeader(headers)
	headerStyles := make([]tablewriter.Colors, 0, len(headers))
	for range headers {
		headerStyles = append(headerStyles, tablewriter.Colors{tablewriter.Bold})
	}
	table.SetHeaderColor(headerStyles...)

	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetNoWhiteSpace(true)
	table.SetTablePadding("\t")
	table.AppendBulk(data)
	table.Render()
}
