package quote

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func NewQuoteCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "quote",
		Short:         "Secutiry quote root Command",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Print(cmd.UsageString())
		},
		RunE: QuoteHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().BoolP("realtime", "r", false, "Realtime updaet quote info")

	return rootCmd
}

func QuoteHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

	// 查询参数由逗号分隔
	keys := strings.Split(args[0], ",")
	dedupKeys := stringSliceDedup(keys)
	slog.Debug("QuoteHandler", "dedupKeys", dedupKeys)
	if len(dedupKeys) > 5 {
		slog.Warn("QuoteHandler support 5 secs at most, will choose top 5 keys")
		dedupKeys = dedupKeys[:5]
	}

	realTime, err := cmd.Flags().GetBool("realtime")
	if err != nil {
		return err
	}
	if !realTime {
		return quoteMultiSec(dedupKeys)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for {
			s := <-c
			slog.InfoContext(ctx, "QuoteHandler", "get a signal", s.String())
			switch s {
			case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
				cancel()
				time.Sleep(time.Second)
				return
			case syscall.SIGHUP:
			default:
				return
			}
		}
	}()

	slog.DebugContext(ctx, "QuoteHandler", "realTime", realTime)
	err = quoteMultiSecRealtime(ctx, dedupKeys)
	return err
}

func quoteMultiSec(keys []string) error {
	// keys 长度不能超过5
	if len(keys) > 5 {
		slog.Warn("quoteMultiSec support 5 secs at most, will choose top 5 keys")
		keys = keys[:5]
	}
	// 1. search security
	secs := sina.MultiSearch(keys)
	if len(secs) == 0 {
		slog.Warn(fmt.Sprintf("no result of %v", keys))
		return nil
	}

	slog.Debug("quoteMultiSec", "secs", secs)

	codes := make([]string, 0, len(secs))
	secMap := make(map[string]sina.BasicSecurity, len(secs))
	for i, sec := range secs {
		codes = append(codes, sec.ExCode)
		secMap[sec.Name] = secs[i]
	}

	res, err := sina.QuoteWs(codes)
	if err != nil {
		return err
	}

	// 填充证券代码
	for _, quote := range res {
		if sec, ok := secMap[quote.Name]; ok {
			quote.ExCode = sec.ExCode
			quote.Code = sec.Code
		}
	}

	printQuote(res)

	return nil
}

func quoteMultiSecRealtime(ctx context.Context, keys []string) error {
	// keys 长度不能超过5
	if len(keys) > 5 {
		slog.WarnContext(ctx, "quoteMultiSecRealtime support 5 secs at most, will choose top 5 keys")
		keys = keys[:5]
	}
	// 1. search security
	secs := sina.MultiSearch(keys)
	if len(secs) == 0 {
		slog.Warn(fmt.Sprintf("no result of %v", keys))
		return nil
	}

	slog.Debug("quoteMultiSecRealtime", "secs", secs)

	codes := make([]string, 0, len(secs))
	secMap := make(map[string]sina.BasicSecurity, len(secs))
	for i, sec := range secs {
		codes = append(codes, sec.ExCode)
		secMap[sec.Name] = secs[i]
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			res, err := sina.QuoteWs(codes)
			if err != nil {
				return err
			}

			// 填充证券代码
			for _, quote := range res {
				if sec, ok := secMap[quote.Name]; ok {
					quote.ExCode = sec.ExCode
					quote.Code = sec.Code
				}
			}
			clearTerm()
			printQuote(res)

			time.Sleep(3 * time.Second)
		}
	}
}

// printQuote 打印 quote 信息
// TODO: 修复列偏移
func printQuote(quotes []*sina.SecurityQuote) {
	// types.JSONify(quotes)

	if len(quotes) == 0 {
		return
	}

	headers := []string{"时间", "名称", "当前价格", "昨收", "今开", "最高", "最低", "成交量", "成交额", "证券代码"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, len(quotes))
	for _, quote := range quotes {
		// 计算涨跌
		rate := (quote.Current/quote.YClose - 1.0) * 100.0
		curWithRate := fmt.Sprintf("%-.5g %-.2g%s", quote.Current, rate, "%")

		row := []string{
			fmt.Sprintf("%s %s", quote.TradeDate, quote.Time),
			quote.Name,
			curWithRate,
			strconv.FormatFloat(quote.YClose, 'g', -1, 64),
			strconv.FormatFloat(quote.Open, 'g', -1, 64),
			strconv.FormatFloat(quote.High, 'g', -1, 64),
			strconv.FormatFloat(quote.Low, 'g', -1, 64),
			types.HumanNum(float64(quote.TurnOver)),
			types.HumanNum(quote.Volume),
			quote.ExCode,
		}

		data = append(data, row)

		styles := make([]tablewriter.Colors, 0, len(headers))
		for _, title := range headers {
			var item tablewriter.Colors = tablewriter.Colors{}
			if title == headers[2] {
				if rate > 0 {
					item = tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgRedColor}
				} else if rate < 0 {
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

// stringSliceDedup 字符串数组去重
func stringSliceDedup(strs []string) []string {
	num := len(strs)
	if num == 0 {
		return strs
	}

	res := make([]string, 0, num)
	vis := make(map[string]struct{}, num)
	for _, str := range strs {
		if _, ok := vis[str]; !ok {
			res = append(res, str)
			vis[str] = struct{}{}
		}
	}

	return res
}

// clearTerm 总端清屏
func clearTerm() {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "cls")
	default:
		cmd = exec.Command("clear")
	}

	cmd.Stdout = os.Stdout
	cmd.Run()
}
