package cmd

import (
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/alwqx/sec/cmd/announcements"
	"github.com/alwqx/sec/cmd/balancesheet"
	"github.com/alwqx/sec/cmd/bond"
	"github.com/alwqx/sec/cmd/commodity"
	secconfig "github.com/alwqx/sec/cmd/config"
	"github.com/alwqx/sec/cmd/filings"
	"github.com/alwqx/sec/cmd/fx"
	"github.com/alwqx/sec/cmd/insider"
	"github.com/alwqx/sec/cmd/internal/termtable"
	"github.com/alwqx/sec/cmd/ipo"
	"github.com/alwqx/sec/cmd/kline"
	"github.com/alwqx/sec/cmd/macro"
	"github.com/alwqx/sec/cmd/metal"
	"github.com/alwqx/sec/cmd/quote"
	"github.com/alwqx/sec/cmd/strategy"
	"github.com/alwqx/sec/cmd/upgrade"
	"github.com/alwqx/sec/cmd/valuation"
	"github.com/alwqx/sec/cmd/watch"
	"github.com/alwqx/sec/hkstock"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/version"
	"github.com/spf13/cobra"
)

func NewCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "sec",
		Short:         "Security Information Client",
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
		Short:   "Search code and name of a security/stock",
		Args:    cobra.ExactArgs(1),
		RunE:    SearchHandler,
	}
	searchCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	searchCmd.Flags().Bool("json", false, "Output JSON")

	infoCmd := &cobra.Command{
		Use:     "info",
		Aliases: []string{"i"},
		Short:   "Print basic information of a security/stock",
		Args:    cobra.ExactArgs(1),
		RunE:    InfoHandler,
	}
	infoCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	infoCmd.Flags().BoolP("dividends", "d", false, "show dividend info")
	infoCmd.Flags().Bool("json", false, "Output JSON")

	rootCmd.AddCommand(
		searchCmd, infoCmd,
		balancesheet.NewBalanceSheetCLI(),
		balancesheet.NewBalanceSheetDownloadCLI(),
		bond.NewBondCLI(), bond.NewBondHistoryCLI(),
		kline.NewKLineCLI(),
		macro.NewMacroCLI(),
		fx.NewFXCLI(),
		quote.NewQuoteCLI(), quote.NewQuoteHistoryCLI(),
		metal.NewMetalCLI(),
		commodity.NewCommodityCLI(),
		filings.NewFilingsCLI(),
		secconfig.NewConfigCLI(),
		upgrade.NewUpgradeCLI(),
		valuation.NewValuationCLI(),
		strategy.NewStrategyCLI(),
		watch.NewWatchCLI(),
		announcements.NewAnnouncementsCLI(),
		insider.NewInsiderCLI(),
		ipo.NewIPOCLI(),
	)

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
	secs, err := sina.Search(cmd.Context(), args[0])
	if err != nil {
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		if secs == nil {
			secs = []*sina.BasicSecurity{}
		}
		return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"results": secs})
	}
	printSecs(cmd.OutOrStdout(), secs)

	return nil
}

func InfoHandler(cmd *cobra.Command, args []string) error {
	opts := new(types.InfoOptions)

	dividend, err := cmd.Flags().GetBool("dividends")
	if err != nil {
		return err
	}
	opts.Dividend = dividend

	// 1. search security
	secs, err := sina.Resolve(cmd.Context(), args[0])
	if err != nil {
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if len(secs) == 0 {
		if asJSON {
			return fmt.Errorf("未找到证券: %s", args[0])
		}
		slog.Warn("no result", "code", args[0])
		return nil
	}

	// 2. choose the first item
	sec := secs[0]
	// 美股没有新浪的公司资料页，公司资料改用 SEC EDGAR。
	if strings.HasPrefix(sec.ExCode, "$") {
		return usInfoHandler(cmd, sec.ExCode, asJSON, opts.Dividend)
	}
	// 基金没有公司资料，改用天天基金的基金信息。
	if sina.IsFundCode(sec.ExCode) {
		return fundInfoHandler(cmd, sec.ExCode, asJSON, opts.Dividend)
	}
	opts.Code = sec.Code
	opts.ExCode = sec.ExCode
	var profile *sina.CorpProfile
	// 新浪的公司资料页只有 A 股，港股的公司资料改用东方财富。
	if types.IsHCode(sec.ExCode) {
		profile, err = hkstock.Profile(cmd.Context(), sec.ExCode)
	} else {
		profile, err = sina.Profile(cmd.Context(), opts)
	}
	if err != nil {
		return err
	}

	if asJSON {
		return printInfoJSON(cmd, sec.ExCode, profile, opts)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "证券代码\t%s\n简称历史\t%s\n公司名称\t%s\n上市日期\t%s\n发行价格\t%s\n行业分类\t%s\n主营业务\t%s\n办公地址\t%s\n公司网址\t%s\n当前价格\t%s\n市净率PB\t%s\n市盈率TTM\t%s\n总市值  \t%s\n流通市值\t%s\n",
		sec.ExCode, profile.HistoryName, profile.Name, profile.ListingDate, utils.HumanRatio(profile.ListingPrice),
		profile.Category, profile.MainBusiness, profile.BusinessAddress, profile.WebSite,
		profile.PriceText(), utils.HumanRatio(profile.PB), utils.HumanRatio(profile.PeTTM), utils.HumanNum(profile.MarketCap), utils.HumanNum(profile.TradedMarketCap))
	if profile.Description != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "公司简介\t%s\n", profile.Description)
	}

	if opts.Dividend {
		dids, err := sina.QueryDividends(cmd.Context(), opts.Code)
		if err != nil {
			slog.Error("failed query dividends", "code", opts.Code, "error", err)
		} else {
			fmt.Fprintln(cmd.OutOrStdout())
			printDividends(cmd.OutOrStdout(), dids)
		}
	}

	return nil
}

// infoRow 是 sec info 的 --json 输出，不可用的指标是 null。
type infoRow struct {
	ExCode          string          `json:"ex_code"`
	Name            string          `json:"name"`
	HistoryName     string          `json:"history_name"`
	ListingDate     string          `json:"listing_date"`
	ListingPrice    *float64        `json:"listing_price"`
	Category        string          `json:"category"`
	MainBusiness    string          `json:"main_business"`
	Description     string          `json:"description"` // 公司简介，目前只有港股有，其他是空字符串
	BusinessAddress string          `json:"business_address"`
	RegisterAddress string          `json:"register_address"`
	WebSite         string          `json:"website"`
	Price           *float64        `json:"price"`
	PriceIsYClose   bool            `json:"price_is_yclose"`
	PB              *float64        `json:"pb"`
	PETTM           *float64        `json:"pe_ttm"`
	MarketCap       *float64        `json:"market_cap"`
	TradedMarketCap *float64        `json:"traded_market_cap"`
	Dividends       []sina.Dividend `json:"dividends,omitzero"` // 只有 -d 时输出，没有分红时是空数组
}

func nonZero(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

func printInfoJSON(cmd *cobra.Command, exCode string, profile *sina.CorpProfile, opts *types.InfoOptions) error {
	row := infoRow{
		ExCode: exCode, Name: profile.Name, HistoryName: profile.HistoryName, ListingDate: profile.ListingDate, ListingPrice: nonZero(profile.ListingPrice),
		Category: profile.Category, MainBusiness: profile.MainBusiness, Description: profile.Description, BusinessAddress: profile.BusinessAddress, RegisterAddress: profile.RegisterAddress, WebSite: profile.WebSite,
		Price: nonZero(profile.Current), PriceIsYClose: profile.PriceIsYClose, PB: nonZero(profile.PB), PETTM: nonZero(profile.PeTTM),
		MarketCap: nonZero(profile.MarketCap), TradedMarketCap: nonZero(profile.TradedMarketCap),
	}
	if opts.Dividend {
		dividends, err := sina.QueryDividends(cmd.Context(), opts.Code)
		if err != nil {
			return fmt.Errorf("查询分红失败: %w", err)
		}
		row.Dividends = append([]sina.Dividend{}, dividends...)
	}
	return utils.PrintJSON(cmd.OutOrStdout(), row)
}

func printSecs(out io.Writer, secs []*sina.BasicSecurity) {
	num := len(secs)
	if num == 0 {
		return
	}

	data := make([][]string, 0, num)
	for _, sec := range secs {
		data = append(data, []string{sec.ExCode, sec.Name, string(sec.SecurityType), sec.ExChange})
	}

	headers := []string{"证券代码", "证券名称", "证券类型", "交易所"}
	table := termtable.New(out, headers, termtable.LeftHeader, termtable.Compact)

	table.AppendBulk(data)
	table.Render()
}

func printDividends(out io.Writer, dids []sina.Dividend) {
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

	headers := []string{"公告日期", "分红送配", "除权除息日", "股权登记日"}
	table := termtable.New(out, headers, termtable.LeftHeader, termtable.Compact)

	table.AppendBulk(data)
	table.Render()
}
