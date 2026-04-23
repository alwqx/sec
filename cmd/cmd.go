package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/cmd/quote"
	"github.com/alwqx/sec/provider/bond"
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
		PersistentPreRun: debugHandler,
		Run: func(cmd *cobra.Command, args []string) {
			if version, _ := cmd.Flags().GetBool("version"); version {
				versionHandler(cmd, args)
				return
			}

			cmd.Print(cmd.UsageString())
		},
	}

	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().BoolP("version", "v", false, "Show version information")

	searchCmd := &cobra.Command{
		Use:     "search",
		Aliases: []string{"s"},
		Short:   "Search code and name of a secutiry/stock",
		Args:    cobra.ExactArgs(1),
		RunE:    SearchHandler,
	}
	searchCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")

	infoCmd := &cobra.Command{
		Use:     "info",
		Aliases: []string{"i"},
		Short:   "Print basic information of a secutiry/stock",
		Args:    cobra.ExactArgs(1),
		RunE:    InfoHandler,
	}
	infoCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	infoCmd.Flags().BoolP("dividends", "d", false, "show dividend info")

	bondCmd := &cobra.Command{
		Use:     "bond",
		Aliases: []string{"b"},
		Short:   "Bond info",
		RunE:    BondHandler,
	}
	bondCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")

	bondHistoryCmd := &cobra.Command{
		Use:     "bond-history",
		Aliases: []string{"bh"},
		Short:   "Bond history info",
		RunE:    BondHistoryHandler,
	}
	bondHistoryCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	bondHistoryCmd.Flags().StringP("begin", "b", "", "Begin date 20250101")
	bondHistoryCmd.Flags().StringP("end", "e", "", "End date 20250131")

	rootCmd.AddCommand(searchCmd, infoCmd, bondCmd, bondHistoryCmd, quote.NewQuoteCLI(), quote.NewQuoteHistoryCLI())

	return rootCmd
}

// versionHandler print version
func versionHandler(cmd *cobra.Command, _ []string) {
	fmt.Println("SEC:")
	fmt.Printf("  version: %s\n", version.Version)
	fmt.Printf("  build time: %s\n", version.BuildTime)
	fmt.Printf("  git commit: %s\n", version.GitCommit)
}

// debugHandler set debug mode
func debugHandler(cmd *cobra.Command, args []string) {
	if debug, _ := cmd.Flags().GetBool("debug"); debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}
}

func SearchHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}
	secs := sina.Search(cmd.Context(), args[0])
	printSecs(secs)

	return nil
}

func InfoHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}
	opts := new(types.InfoOptions)

	dividend, err := cmd.Flags().GetBool("dividends")
	if err != nil {
		return err
	}
	opts.Dividend = dividend

	// 1. search security
	secs := sina.Search(cmd.Context(), args[0])
	if len(secs) == 0 {
		slog.Warn("no result", "code", args[0])
		return nil
	}

	// 2. choose the first item
	sec := secs[0]
	opts.Code = sec.Code
	opts.ExCode = sec.ExCode
	profile, err := sina.Profile(cmd.Context(), opts)
	if err != nil {
		return err
	}

	profile.ExCode = sec.ExChange
	fmt.Printf("证券代码\t%s\n简称历史\t%s\n公司名称\t%s\n上市日期\t%s\n发行价格\t%.2f\n行业分类\t%s\n主营业务\t%s\n办公地址\t%s\n公司网址\t%s\n当前价格\t%.2f\n市净率PB\t%.2f\n市盈率TTM\t%.2f\n总市值  \t%s\n流通市值\t%s\n",
		sec.ExCode, profile.HistoryName, profile.Name, profile.ListingDate, profile.ListingPrice,
		profile.Category, profile.MainBusiness, profile.BusinessAddress, profile.WebSite,
		profile.Current, profile.PB, profile.PeTTM, types.HumanNum(profile.MarketCap), types.HumanNum(profile.TradedMarketCap))

	if opts.Dividend {
		dids, err := sina.QueryDividends(cmd.Context(), opts.Code)
		if err != nil {
			slog.Error("failed query dividends", "code", opts.Code, "error", err)
		} else {
			fmt.Println()
			printDividends(dids)
		}
	}

	return nil
}

func printSecs(secs []*sina.BasicSecurity) {
	num := len(secs)
	if num == 0 {
		return
	}

	data := make([][]string, 0, num)
	for _, sec := range secs {
		data = append(data, []string{sec.ExCode, sec.Name, string(sec.SecurityType), sec.ExChange})
	}

	table := tablewriter.NewWriter(os.Stdout)
	headers := []string{"证券代码", "证券名称", "证券类型", "交易所"}
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

func printDividends(dids []sina.Dividend) {
	num := len(dids)
	if num == 0 {
		return
	}

	data := make([][]string, 0, num)
	for _, did := range dids {
		var sb strings.Builder
		sb.WriteString("10")
		if did.Shares > 0 {
			sb.WriteString(fmt.Sprintf("送%-.2f股", did.Shares))
		}
		if did.AddShares > 0 {
			sb.WriteString(fmt.Sprintf("转%-.2f股", did.AddShares))
		}
		if did.Bonus > 0 {
			sb.WriteString(fmt.Sprintf("派%-.2f元", did.Bonus))
		}

		bonus := sb.String()
		if sb.Len() < 3 {
			bonus = "不分配"
		}
		data = append(data, []string{did.PublicDate, bonus, did.DividendedDate, did.RecordDate})
	}

	table := tablewriter.NewWriter(os.Stdout)
	headers := []string{"公告日期", "分红送配", "除权除息日", "股权登记日"}
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

// BondHandler 国债相关命令
func BondHandler(cmd *cobra.Command, args []string) error {
	// 默认请求最近10天(考虑中秋、春节等假期)的数据，取最新一条
	defaultEnd := time.Now()
	defaultBegin := defaultEnd.Add(-10 * 24 * time.Hour)

	req := &bond.GetChinaBondReq{
		Start: defaultBegin.Format("2006-01-02"),
		End:   defaultEnd.Format("2006-01-02"),
	}

	slog.Debug("bond req info", "begin", req.Start, "end", req.End)

	resp, err := bond.GetChinaBond(cmd.Context(), req)
	if err != nil {
		return err
	}
	if len(resp.HeList) > 0 {
		printChinaBonds(resp.HeList[:1])
	} else {
		fmt.Printf("no data of range %s - %s", req.Start, req.End)
	}

	return nil
}

func printChinaBonds(bonds []*bond.ChinaBondItem) {
	num := len(bonds)
	if num == 0 {
		return
	}

	data := make([][]string, 0, num)
	for _, bond := range bonds {
		data = append(data, []string{bond.Date, bond.ThreeMonth, bond.SixMonth, bond.OneYear, bond.TwoYear, bond.ThreeYear, bond.FiveYear, bond.TenYear, bond.ThirtyYear})
	}
	sort.Slice(data, func(i, j int) bool {
		return data[i][0] < data[j][0]
	})

	table := tablewriter.NewWriter(os.Stdout)
	headers := []string{"日期", "3个月", "6个月", "1年", "2年", "3年", "5年", "10年", "30年"}
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
	table.SetNoWhiteSpace(false)
	table.SetTablePadding("\t")
	table.AppendBulk(data)
	table.Render()
}

// BondHistoryHandler 国债相关命令
func BondHistoryHandler(cmd *cobra.Command, args []string) error {
	defaultEnd := time.Now()
	defaultBegin := defaultEnd.Add(-30 * 24 * time.Hour)
	beginStr, err := cmd.Flags().GetString("begin")
	if err != nil {
		return err
	}

	req := &bond.GetChinaBondReq{}
	// 校验 begin
	if beginStr != "" {
		defaultBegin, err = time.Parse("20060102", beginStr)
		if err != nil {
			return err
		}
	}
	req.Start = defaultBegin.Format("2006-01-02")

	// 校验 end
	endStr, err := cmd.Flags().GetString("end")
	if err != nil {
		return err
	}
	if endStr != "" {
		defaultEnd, err = time.Parse("20060102", endStr)
		if err != nil {
			return err
		}
	}
	req.End = defaultEnd.Format("2006-01-02")
	slog.Debug("bond-history req info", "begin_str", beginStr, "end_str", endStr, "begin", req.Start, "end", req.End)

	resp, err := bond.GetChinaBond(cmd.Context(), req)
	if err != nil {
		return err
	}
	printChinaBonds(resp.HeList)

	return nil
}
