package kline

import (
	"fmt"
	"log/slog"
	"sync"

	"math"
	"strconv"
	"strings"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/render"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

func NewKLineCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "kline",
		Aliases:       []string{"kl"},
		Short:         "Print candlestick chart of specific security",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		Args: cobra.ExactArgs(1),
		RunE: KLineHandler,
	}
	rootCmd.Flags().StringP("begin", "b", "", "Begin date 20260101")
	rootCmd.Flags().StringP("end", "e", "", "End date 20260131")
	rootCmd.Flags().StringP("fq", "f", "", "FuQuan type: bfq none, qfq front, hfq post")
	rootCmd.Flags().IntP("height", "H", 20, "Chart height in rows")
	rootCmd.Flags().Bool("half-block", false, "Use half-block chars for 2x resolution")
	rootCmd.Flags().Bool("paging", false, "Fixed candle width instead of auto-scaling")
	rootCmd.Flags().Bool("no-volume", false, "Hide volume subgraph")
	// Indicator overlays
	rootCmd.Flags().String("ma", "", "MA periods, comma-separated (e.g. 5,20,60)")
	rootCmd.Flags().String("boll", "", "Bollinger Bands: period,k (e.g. 20,2.0)")

	return rootCmd
}

// KLineHandler is the handler for sec kline command.
func KLineHandler(cmd *cobra.Command, args []string) error {
	key := args[0]
	secs := sina.Search(cmd.Context(), key)
	if len(secs) == 0 {
		slog.Info("search no sec", "code", key)
		return nil
	}

	sec := secs[0]
	slog.Debug("KLineHandler", "excode", sec.ExCode, "code", sec.Code, "exchange", sec.ExChange)
	req := &eastmoney.GetQuoteHistoryReq{
		Code: sec.Code,
	}
	switch sec.ExChange {
	case "sh":
		req.MarketCode = 1
	case "sz":
		req.MarketCode = 0
	case types.ExChangeHKex:
		req.MarketCode = 116
	case types.ExChangeNasdaq:
		req.MarketCode = 105
	default:
		return fmt.Errorf("unsupported exchange: %s", sec.ExChange)
	}

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
		req.FQT = eastmoney.QuoteFQTDefault
	}

	beginStr, _ := cmd.Flags().GetString("begin")
	endStr, _ := cmd.Flags().GetString("end")
	req.Begin, req.End, err = utils.ParseBeginEnd(beginStr, endStr, 90, eastmoney.TimeYYMMDD, eastmoney.TimeYYMMDD)
	if err != nil {
		return err
	}

	var (
		quotes     []*eastmoney.Quote
		profile    *sina.CorpProfile
		err1, err2 error
		wg         sync.WaitGroup
	)
	wg.Add(2)

	opts := new(types.InfoOptions)
	opts.Code = sec.Code
	opts.ExCode = sec.ExCode
	go func() {
		defer wg.Done()
		profile, err1 = sina.Profile(cmd.Context(), opts)
	}()
	go func() {
		defer wg.Done()
		quotes, err2 = eastmoney.GetQuoteHistory(cmd.Context(), req)
	}()
	wg.Wait()

	if err1 != nil {
		slog.Error("failed sina.Profile", "code", sec.Code, "error", err1)
		return err1
	}

	if err2 != nil {
		slog.Error("failed GetQuoteHistory", "code", req.Code, "error", err2)
		return err2
	}
	if len(quotes) == 0 {
		slog.Info("no quote data", "code", req.Code)
		return nil
	}

	// 打印基本信息
	fmt.Fprintf(cmd.OutOrStdout(), "证券代码\t%s\n公司名称\t%s\n主营业务\t%s\n发行价格\t%.2f\n当前价格\t%.2f\n市净率PB\t%.2f\n市盈率TTM\t%.2f\n总市值  \t%s\n流通市值\t%s\n",
		sec.ExCode, profile.Name, profile.MainBusiness,
		profile.ListingPrice, profile.Current, profile.PB, profile.PeTTM,
		utils.HumanNum(profile.MarketCap), utils.HumanNum(profile.TradedMarketCap))

	// 渲染蜡烛图
	noVolume, _ := cmd.Flags().GetBool("no-volume")
	paging, _ := cmd.Flags().GetBool("paging")
	halfBlock, _ := cmd.Flags().GetBool("half-block")
	height, _ := cmd.Flags().GetInt("height")
	if height <= 0 {
		return fmt.Errorf("invalid height %d: must be > 0", height)
	}

	cfg := render.CandlestickConfig{
		Height:    height,
		Volume:    !noVolume,
		Paging:    paging,
		HalfBlock: halfBlock,
	}

	// Compute indicator overlays
	if maStr, _ := cmd.Flags().GetString("ma"); maStr != "" {
		for _, s := range strings.Split(maStr, ",") {
			token := strings.TrimSpace(s)
			period, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil || period <= 0 {
				return fmt.Errorf("invalid --ma value %q: expected positive integer", token)
			}
			ma := computeMAOverlay(quotes, period)
			cfg.Overlays = append(cfg.Overlays, render.OverlayLine{
				Values: ma,
				Color:  maColor(period),
				Label:  fmt.Sprintf("MA%d", period),
			})
		}
	}

	if bollStr, _ := cmd.Flags().GetString("boll"); bollStr != "" {
		parts := strings.Split(bollStr, ",")
		if len(parts) < 2 {
			return fmt.Errorf("invalid --boll value %q: expected period,k", bollStr)
		}

		periodStr := strings.TrimSpace(parts[0])
		kStr := strings.TrimSpace(parts[1])
		period, err := strconv.Atoi(strings.TrimSpace(periodStr))
		if err != nil {
			return err
		}
		if period <= 0 {
			return fmt.Errorf("invalid --boll period %q: expected positive integer", periodStr)
		}

		k, err := strconv.ParseFloat(strings.TrimSpace(kStr), 64)
		if err != nil {
			return err
		}
		if k <= 0 {
			return fmt.Errorf("invalid --boll k %q: expected positive number", kStr)
		}

		mid, upper, lower := computeBollOverlay(quotes, period, k)
		cfg.Overlays = append(cfg.Overlays,
			render.OverlayLine{Values: mid, Color: render.AnsiYellow, Label: fmt.Sprintf("MID%d", period), Style: '─'},
			render.OverlayLine{Values: upper, Color: render.AnsiCyan, Label: fmt.Sprintf("UP%.1f", k), Style: '·'},
			render.OverlayLine{Values: lower, Color: render.AnsiCyan, Label: fmt.Sprintf("LO%.1f", k), Style: '·'},
		)
	}

	candles := toCandles(quotes)
	return render.Render(cmd.OutOrStdout(), candles, cfg)
}

// toCandles converts eastmoney Quote slice to render Candle slice.
func toCandles(quotes []*eastmoney.Quote) []render.Candle {
	candles := make([]render.Candle, 0, len(quotes))
	for _, q := range quotes {
		candles = append(candles, render.Candle{
			Date:   q.Date,
			Open:   q.Open,
			Close:  q.Close,
			High:   q.High,
			Low:    q.Low,
			Volume: q.Volume,
		})
	}
	return candles
}

// computeMAOverlay returns the SMA values for the given period, aligned with quotes.
// Values before the period is complete are set to 0 (not drawn).
func computeMAOverlay(quotes []*eastmoney.Quote, period int) []float64 {
	result := make([]float64, len(quotes))
	if len(quotes) < period {
		return result
	}
	sum := 0.0
	for i := 0; i < period-1; i++ {
		sum += quotes[i].Close
	}
	for i := period - 1; i < len(quotes); i++ {
		sum += quotes[i].Close
		result[i] = sum / float64(period)
		sum -= quotes[i-period+1].Close
	}
	return result
}

// computeBollOverlay returns mid, upper, lower Bollinger Band values.
func computeBollOverlay(quotes []*eastmoney.Quote, period int, k float64) (mid, upper, lower []float64) {
	n := len(quotes)
	mid = make([]float64, n)
	upper = make([]float64, n)
	lower = make([]float64, n)
	if n < period {
		return
	}

	// Compute mid line (SMA)
	sum := 0.0
	for i := 0; i < period-1; i++ {
		sum += quotes[i].Close
	}
	for i := period - 1; i < n; i++ {
		sum += quotes[i].Close
		mid[i] = sum / float64(period)
		sum -= quotes[i-period+1].Close

		// Stddev
		sqSum := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := quotes[j].Close - mid[i]
			sqSum += diff * diff
		}
		stddev := math.Sqrt(sqSum / float64(period))
		upper[i] = mid[i] + k*stddev
		lower[i] = mid[i] - k*stddev
	}
	return
}

// maColor returns a color for the MA line based on period.
func maColor(period int) string {
	switch {
	case period <= 5:
		return render.AnsiWhite
	case period <= 10:
		return render.AnsiYellow
	case period <= 30:
		return render.AnsiCyan
	default:
		return render.AnsiBlue
	}
}
