package quote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alwqx/sec/cmd/internal/termtable"
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

	realTime, err := cmd.Flags().GetBool("realtime")
	if err != nil {
		return err
	}
	if !realTime {
		return quoteMultiSec(cmd.Context(), cmd.OutOrStdout(), dedupKeys)
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
	err = quoteMultiSecRealtime(ctx, cmd.OutOrStdout(), dedupKeys)
	return err
}

// keyProblem 是一个没有拿到行情的关键字或证券。retry 为 true 表示搜索失败，可以稍后重试。
type keyProblem struct {
	key, reason string
	retry       bool
}

// resolveKeys 并发搜索关键字，结果保持输入顺序，搜到同一只证券的关键字只保留一个。
// 找不到或搜索失败的关键字单独返回，不影响其他证券。
func resolveKeys(ctx context.Context, keys []string) ([]*sina.BasicSecurity, []keyProblem) {
	type found struct {
		secs []*sina.BasicSecurity
		err  error
	}
	results := make([]found, len(keys))
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			secs, err := sina.Resolve(ctx, key)
			results[i] = found{secs, err}
		}()
	}
	wg.Wait()
	secs := []*sina.BasicSecurity{}
	problems := []keyProblem{}
	if ctx.Err() != nil {
		return secs, problems
	}
	seen := map[string]bool{}
	for i, result := range results {
		switch {
		case result.err != nil:
			problems = append(problems, keyProblem{keys[i], fmt.Sprintf("搜索失败（%v）", result.err), true})
		case len(result.secs) == 0:
			problems = append(problems, keyProblem{keys[i], "未找到证券", false})
		case !seen[strings.ToUpper(result.secs[0].ExCode)]:
			seen[strings.ToUpper(result.secs[0].ExCode)] = true
			secs = append(secs, result.secs[0])
		}
	}
	return secs, problems
}

// missingQuotes 返回搜到了但行情里没有的证券，比如停牌或已退市。
func missingQuotes(secs []*sina.BasicSecurity, quotes []*sina.SecurityQuote) []keyProblem {
	got := map[string]bool{}
	for _, quote := range quotes {
		got[strings.ToUpper(quote.ExCode)] = true
	}
	problems := []keyProblem{}
	for _, sec := range secs {
		if !got[strings.ToUpper(sec.ExCode)] {
			problems = append(problems, keyProblem{sec.ExCode, "没有行情数据", false})
		}
	}
	return problems
}

func problemText(problems []keyProblem) string {
	parts := make([]string, 0, len(problems))
	for _, problem := range problems {
		parts = append(parts, problem.key+" "+problem.reason)
	}
	return "部分证券没有行情：" + strings.Join(parts, "；")
}

// limitKeys 只查询前 maxQuoteKeys 个关键字，多出来的列为问题，不静默丢掉。
func limitKeys(keys []string) ([]string, []keyProblem) {
	if len(keys) <= maxQuoteKeys {
		return keys, nil
	}
	problems := []keyProblem{}
	for _, key := range keys[maxQuoteKeys:] {
		problems = append(problems, keyProblem{key, fmt.Sprintf("超过 %d 只，未查询", maxQuoteKeys), false})
	}
	return keys[:maxQuoteKeys], problems
}

// quoteMultiSec 显示查到的证券；有关键字找不到、搜索失败或行情获取失败时，在表格后一并报错并以非 0 退出。
func quoteMultiSec(ctx context.Context, out io.Writer, keys []string) error {
	keys, skipped := limitKeys(keys)
	secs, problems := resolveKeys(ctx, keys)
	problems = append(problems, skipped...)
	// 按 Ctrl-C 取消不是失败。
	if ctx.Err() != nil {
		return nil
	}
	slog.DebugContext(ctx, "quoteMultiSec", "secs", secs)
	if len(secs) > 0 {
		quotes, err := queryQuotes(ctx, secs)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// 行情接口失败时，搜索阶段的问题也要报出来。
			if len(problems) > 0 {
				return fmt.Errorf("%s；行情获取失败: %w", problemText(problems), err)
			}
			return err
		}
		printQuote(out, quotes)
		problems = append(problems, missingQuotes(secs, quotes)...)
	}
	if len(problems) > 0 {
		return errors.New(problemText(problems))
	}
	return nil
}

// realtimeRetryEvery 是实时模式重试搜索失败的关键字的间隔，按刷新次数计，约 15 秒。
const realtimeRetryEvery = 5

// refreshInterval 和 clearScreen 是实时模式的刷新间隔和清屏，测试里替换成很短的间隔和空操作。
var (
	refreshInterval = 3 * time.Second
	clearScreen     = clearTerm
)

// quoteMultiSecRealtime 每 3 秒刷新一次。没拿到行情的证券和行情接口的错误列在表格下方，清屏后也看得到；
// 搜索失败的关键字定期重试，行情接口失败时保留上一次的行情，下一轮再试。
func quoteMultiSecRealtime(ctx context.Context, out io.Writer, keys []string) error {
	keys, skipped := limitKeys(keys)
	secs, problems := resolveKeys(ctx, keys)
	slog.DebugContext(ctx, "quoteMultiSecRealtime", "secs", secs)
	var quotes []*sina.SecurityQuote
	for round := 0; ; round++ {
		if ctx.Err() != nil {
			return nil
		}
		if round > 0 && round%realtimeRetryEvery == 0 {
			secs, problems = retryProblems(ctx, secs, problems)
			// 重试时按了 Ctrl-C，搜索结果不完整，不能当成没有可显示的证券。
			if ctx.Err() != nil {
				return nil
			}
			problems = inKeyOrder(problems, keys)
		}
		retrying := false
		for _, problem := range problems {
			retrying = retrying || problem.retry
		}
		if len(secs) == 0 && !retrying {
			return errors.New(problemText(append(problems, skipped...)))
		}
		var quoteErr error
		if len(secs) > 0 {
			latest, err := queryQuotes(ctx, secs)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				quoteErr = err
			} else {
				quotes = latest
			}
		}
		clearScreen()
		printQuote(out, quotes)
		shown := append(append([]keyProblem{}, problems...), skipped...)
		if quoteErr == nil {
			shown = append(shown, missingQuotes(secs, quotes)...)
		}
		if len(shown) > 0 {
			fmt.Fprintf(out, "\n%s\n", problemText(shown))
		}
		if quoteErr != nil && len(quotes) > 0 {
			fmt.Fprintf(out, "\n行情获取失败，显示的是上一次的行情，3 秒后重试：%v\n", quoteErr)
		} else if quoteErr != nil {
			fmt.Fprintf(out, "\n行情获取失败，3 秒后重试：%v\n", quoteErr)
		}
		if retrying {
			fmt.Fprintln(out, "搜索失败的证券约每 15 秒重试一次")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(refreshInterval):
		}
	}
}

// retryProblems 重新搜索失败的关键字，搜到的加入证券列表。找不到的关键字不再重试。
func retryProblems(ctx context.Context, secs []*sina.BasicSecurity, problems []keyProblem) ([]*sina.BasicSecurity, []keyProblem) {
	retry := []string{}
	kept := []keyProblem{}
	for _, problem := range problems {
		if problem.retry {
			retry = append(retry, problem.key)
		} else {
			kept = append(kept, problem)
		}
	}
	if len(retry) == 0 {
		return secs, problems
	}
	found, again := resolveKeys(ctx, retry)
	have := map[string]bool{}
	for _, sec := range secs {
		have[strings.ToUpper(sec.ExCode)] = true
	}
	for _, sec := range found {
		if !have[strings.ToUpper(sec.ExCode)] {
			have[strings.ToUpper(sec.ExCode)] = true
			secs = append(secs, sec)
		}
	}
	return secs, append(kept, again...)
}

// inKeyOrder 按关键字的输入顺序排列问题，重试后列表顺序不变，刷新时不跳动。
func inKeyOrder(problems []keyProblem, keys []string) []keyProblem {
	position := map[string]int{}
	for i, key := range keys {
		position[key] = i
	}
	sort.SliceStable(problems, func(i, j int) bool { return position[problems[i].key] < position[problems[j].key] })
	return problems
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
		found, err := sina.Resolve(ctx, key)
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
func printQuote(out io.Writer, quotes []*sina.SecurityQuote) {
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
				item = termtable.Change(rate, true)
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
