package cmd

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/alwqx/sec/provider"
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
	fmt.Printf("sec version is v0.0.1\n")
}

func SearchHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	res := provider.Search(args[0])
	num := len(res)
	for i := range num {
		item := res[i]
		fmt.Printf("%-8s\t%s", item.ExCode, item.Name)
		if i != num-1 {
			fmt.Println()
		}
	}

	return nil
}

func InfoHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	// 1. search security
	secs := provider.Search(args[0])
	if len(secs) == 0 {
		slog.Warn("no result of %s", args[0])
		return nil
	}

	// 2. choose the first item
	sec := secs[0]
	fmt.Println(sec)
	// for i := range num {
	// 	item := res[i]
	// 	fmt.Printf("%-8s\t%s", item.ExCode, item.Name)
	// 	if i != num-1 {
	// 		fmt.Println()
	// 	}
	// }

	return nil
}
