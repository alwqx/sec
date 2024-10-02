package cmd

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/version"
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
		Use:   "search SECURITY",
		Short: "Search code and name of a secutiry/stock",
		Args:  cobra.ExactArgs(1),
		RunE:  SearchHandler,
	}

	infoCmd := &cobra.Command{
		Use:   "info infomation of SECURITY",
		Short: "Print basic information of a secutiry/stock",
		Args:  cobra.ExactArgs(1),
		RunE:  InfoHandler,
	}

	rootCmd.AddCommand(searchCmd, infoCmd)

	return rootCmd
}

func versionHandler(cmd *cobra.Command, _ []string) {
	fmt.Printf("sec version is %s\n", version.Version)
}

func SearchHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	res := sina.Search(args[0])
	for _, item := range res {
		fmt.Printf("%-8s\t%s\n", item.ExCode, item.Name)
	}

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
	fmt.Printf("基本信息\n证券代码\t%s\n简称历史\t%s\n公司名称\t%s\n上市日期\t%s\n发行价格\t%.2f\n行业分类\t%s\n主营业务\t%s\n办公地址\t%s\n公司网址\t%s\n当前价格\t%.2f\n市净率PB\t%.2f\n市盈率TTM\t%.2f\n总市值  \t%.2f\n流通市值\t%.2f\n",
		sec.ExCode, profile.HistoryName, profile.Name, profile.ListingDate, profile.ListingPrice,
		profile.Category, profile.MainBusiness, profile.BusinessAddress, profile.WebSite,
		profile.Current, profile.PB, profile.PeTTM, profile.MarketCap, profile.TradedMarketCap)

	return nil
}
