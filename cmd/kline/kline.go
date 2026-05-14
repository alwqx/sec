package kline

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/render"
	"github.com/alwqx/sec/types"
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
		RunE: KLineHandler,
	}
	rootCmd.Flags().StringP("begin", "b", "", "Begin date 20260101")
	rootCmd.Flags().StringP("end", "e", "", "End date 20260131")
	rootCmd.Flags().StringP("fq", "f", "", "FuQuan type: bfq none, qfq front, hfq post")
	rootCmd.Flags().IntP("height", "H", 20, "Chart height in rows")
	rootCmd.Flags().Bool("half-block", false, "Use half-block chars for 2x resolution")
	rootCmd.Flags().Bool("paging", false, "Fixed candle width instead of auto-scaling")
	rootCmd.Flags().Bool("no-volume", false, "Hide volume subgraph")

	return rootCmd
}

// KLineHandler is the handler for sec kline command.
func KLineHandler(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return errors.New("args of command should be one")
	}

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

	defaultEnd := time.Now()
	defaultBegin := defaultEnd.Add(-90 * 24 * time.Hour)

	beginStr, err := cmd.Flags().GetString("begin")
	if err != nil {
		return err
	}
	if beginStr != "" {
		defaultBegin, err = time.Parse(eastmoney.TimeYYMMDD, beginStr)
		if err != nil {
			return err
		}
	}
	req.Begin = defaultBegin.Format(eastmoney.TimeYYMMDD)

	endStr, err := cmd.Flags().GetString("end")
	if err != nil {
		return err
	}
	if endStr != "" {
		defaultEnd, err = time.Parse(eastmoney.TimeYYMMDD, endStr)
		if err != nil {
			return err
		}
	}
	req.End = defaultEnd.Format(eastmoney.TimeYYMMDD)

	if defaultEnd.Before(defaultBegin) {
		bs := defaultBegin.Format(eastmoney.TimeYYMMDD)
		es := defaultEnd.Format(eastmoney.TimeYYMMDD)
		slog.Error("invalid time range", "begin", bs, "end", es)
		return fmt.Errorf("invalid begin %s and end %s", bs, es)
	}

	quotes, err := eastmoney.GetQuoteHistory(cmd.Context(), req)
	if err != nil {
		slog.Error("failed GetQuoteHistory", "code", req.Code, "error", err)
		return err
	}

	if len(quotes) == 0 {
		slog.Info("no quote data", "code", req.Code)
		return nil
	}

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
