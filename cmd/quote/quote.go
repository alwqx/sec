package quote

import (
	"context"
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
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// maxQuoteKeys 是一次查询的证券数量上限。
const maxQuoteKeys = 5

func NewQuoteCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "quote",
		Aliases:       []string{"q"},
		Short:         "Security quote root Command",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Args: cobra.ExactArgs(1),
		RunE: QuoteHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().BoolP("realtime", "r", false, "Realtime update quote info")
	rootCmd.Flags().Bool("json", false, "Output JSON")
	rootCmd.MarkFlagsMutuallyExclusive("realtime", "json")

	return rootCmd
}

func QuoteHandler(cmd *cobra.Command, args []string) error {

	// 查询参数由逗号分隔
	keys := strings.Split(args[0], ",")
	dedupKeys := stringSliceDedup(keys)
	slog.Debug("QuoteHandler", "dedupKeys", dedupKeys)
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return quoteJSON(cmd, dedupKeys)
	}
	if len(dedupKeys) > maxQuoteKeys {
		slog.Warn("QuoteHandler support 5 secs at most, will choose top 5 keys")
		dedupKeys = dedupKeys[:maxQuoteKeys]
	}

	realTime, err := cmd.Flags().GetBool("realtime")
	if err != nil {
		return err
	}
	if !realTime {
		return quoteMultiSec(cmd.Context(), dedupKeys)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		for {
			s := <-c
			switch s {
			case syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT:
				slog.DebugContext(ctx, "QuoteHandler", "get a signal", s.String())
				cancel()
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

func quoteMultiSec(ctx context.Context, keys []string) error {
	// keys 长度不能超过5
	if len(keys) > 5 {
		slog.WarnContext(ctx, "quoteMultiSec support 5 secs at most, will choose top 5 keys")
		keys = keys[:5]
	}
	// 1. search security
	secs, err := sina.MultiSearch(ctx, keys)
	if err != nil {
		// 按 Ctrl-C 取消不是失败。
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	if len(secs) == 0 {
		slog.WarnContext(ctx, "no result", "keys", keys)
		return nil
	}

	slog.DebugContext(ctx, "quoteMultiSec", "secs", secs)

	res, err := queryQuotes(ctx, secs)
	if err != nil {
		return err
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
	secs, err := sina.MultiSearch(ctx, keys)
	if err != nil {
		// 按 Ctrl-C 取消不是失败。
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("搜索证券失败: %w", err)
	}
	if len(secs) == 0 {
		slog.WarnContext(ctx, "no result", "keys", keys)
		return nil
	}

	slog.DebugContext(ctx, "quoteMultiSecRealtime", "secs", secs)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			res, err := queryQuotes(ctx, secs)
			if err != nil {
				return err
			}
			clearTerm()
			printQuote(res)

			time.Sleep(3 * time.Second)
		}
	}
}

// queryQuotes 查询行情并填充证券代码。A 股和 H 股可能同名，只能按交易所代码匹配。
func queryQuotes(ctx context.Context, secs []*sina.BasicSecurity) ([]*sina.SecurityQuote, error) {
	codes := make([]string, 0, len(secs))
	secMap := make(map[string]*sina.BasicSecurity, len(secs))
	for _, sec := range secs {
		codes = append(codes, sec.ExCode)
		secMap[strings.ToUpper(sec.ExCode)] = sec
	}
	quotes, err := sina.QueryQuoteList(ctx, codes)
	if err != nil {
		return nil, err
	}
	for _, quote := range quotes {
		if sec, ok := secMap[strings.ToUpper(quote.ExCode)]; ok {
			quote.ExCode, quote.Code = sec.ExCode, sec.Code
		}
	}
	return quotes, nil
}

// quoteRow 是 sec quote 的 --json 输出，不可用的值是 null。
type quoteRow struct {
	ExCode        string   `json:"ex_code"`
	Code          string   `json:"code"`
	Name          string   `json:"name"`
	TradeDate     string   `json:"trade_date"`
	Time          string   `json:"time"`
	Price         *float64 `json:"price"`
	PriceIsYClose bool     `json:"price_is_yclose"`
	YClose        *float64 `json:"yclose"`
	Open          *float64 `json:"open"`
	High          *float64 `json:"high"`
	Low           *float64 `json:"low"`
	Change        *float64 `json:"change"`
	ChangePct     *float64 `json:"change_pct"`
	Volume        int64    `json:"volume"` // 成交量，单位：股或份
	Amount        float64  `json:"amount"` // 成交额
}

func positive(value float64) *float64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func newQuoteRow(quote *sina.SecurityQuote) quoteRow {
	price, isYClose := quote.EffectivePrice()
	row := quoteRow{
		ExCode: quote.ExCode, Code: quote.Code, Name: quote.Name, TradeDate: quote.TradeDate, Time: quote.Time,
		Price: positive(price), PriceIsYClose: isYClose, YClose: positive(quote.YClose), Open: positive(quote.Open),
		High: positive(quote.High), Low: positive(quote.Low), Volume: quote.TurnOver, Amount: quote.Volume,
	}
	if quote.Current > 0 && quote.YClose > 0 {
		change := quote.Current - quote.YClose
		pct := change / quote.YClose * 100
		row.Change, row.ChangePct = &change, &pct
	}
	return row
}

// quoteJSON 要么输出全部证券，要么失败，脚本不会拿到少了几只却看起来成功的结果。
func quoteJSON(cmd *cobra.Command, keys []string) error {
	if len(keys) > maxQuoteKeys {
		return fmt.Errorf("一次最多查询 %d 只证券，当前 %d 只", maxQuoteKeys, len(keys))
	}
	ctx := cmd.Context()
	secs := make([]*sina.BasicSecurity, 0, len(keys))
	for _, key := range keys {
		found, err := sina.Search(ctx, key)
		if err != nil {
			return fmt.Errorf("搜索证券失败: %w", err)
		}
		if len(found) == 0 {
			return fmt.Errorf("未找到证券: %s", key)
		}
		secs = append(secs, found[0])
	}
	quotes, err := queryQuotes(ctx, secs)
	if err != nil {
		return err
	}
	byCode := make(map[string]*sina.SecurityQuote, len(quotes))
	for _, quote := range quotes {
		byCode[quote.ExCode] = quote
	}
	rows := make([]quoteRow, 0, len(secs))
	for _, sec := range secs {
		quote, ok := byCode[sec.ExCode]
		if !ok {
			return fmt.Errorf("%s 没有行情数据", sec.ExCode)
		}
		rows = append(rows, newQuoteRow(quote))
	}
	return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"results": rows})
}

// currentText 返回当前价格和涨跌的文字及涨跌幅。没有成交价时显示昨收，不计算涨跌。
func currentText(quote *sina.SecurityQuote) (string, float64) {
	price, isYClose := quote.EffectivePrice()
	if price == 0 {
		return "—", 0
	}
	if isYClose {
		return fmt.Sprintf("%-.5g（昨收，当前无成交价）", price), 0
	}
	if quote.YClose <= 0 {
		return fmt.Sprintf("%-.5g", price), 0
	}
	diffPrice := price - quote.YClose
	rate := diffPrice / quote.YClose * 100.0
	return fmt.Sprintf("%-.5g %-.5g %-.2g%s", price, diffPrice, rate, "%"), rate
}

// printQuote 打印 quote 信息
func printQuote(quotes []*sina.SecurityQuote) {
	if len(quotes) == 0 {
		return
	}

	headers := []string{"时间", "名称", "当前价格", "昨收", "今开", "最高", "最低", "成交量", "成交额", "证券代码"}
	columnsStyles := make([][]tablewriter.Colors, 0, len(headers))

	data := make([][]string, 0, len(quotes))
	for _, quote := range quotes {
		curWithRate, rate := currentText(quote)

		row := []string{
			fmt.Sprintf("%s %s", quote.TradeDate, quote.Time),
			quote.Name,
			curWithRate,
			strconv.FormatFloat(quote.YClose, 'g', -1, 64),
			strconv.FormatFloat(quote.Open, 'g', -1, 64),
			strconv.FormatFloat(quote.High, 'g', -1, 64),
			strconv.FormatFloat(quote.Low, 'g', -1, 64),
			utils.HumanNum(float64(quote.TurnOver)),
			utils.HumanNum(quote.Volume),
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

// clearTerm 终端清屏
func clearTerm() {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "cls")
	default:
		cmd = exec.Command("clear")
	}

	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		slog.Error("clearTerm", "cmd error", err)
	}
}
