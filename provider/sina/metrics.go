package sina

import (
	"context"
	"strings"
)

// CoreMetrics 公司核心指标，计算口径与 CLI info 共用。
type CoreMetrics struct {
	Category        string
	PB              float64
	PeTTM           float64
	MarketCap       float64
	TradedMarketCap float64
	TradeDate       string
	Time            string
}

func calculateCoreMetrics(exCode string, quote *SecurityQuote, profile *sinaPartProfile) *CoreMetrics {
	factor := 10000.0 // A 股股本单位为万股
	if strings.HasPrefix(strings.ToUpper(exCode), "HK") {
		factor = 1
	}
	metrics := &CoreMetrics{
		Category:        profile.Category,
		MarketCap:       quote.Current * profile.Cap * factor,
		TradedMarketCap: quote.Current * profile.TradeCap * factor,
		TradeDate:       quote.TradeDate, Time: quote.Time,
	}
	if profile.VPS != 0 {
		metrics.PB = quote.Current / profile.VPS
	}
	if profile.Profit > 0 {
		metrics.PeTTM = quote.Current * profile.Cap / profile.Profit / 10000.0
	}
	return metrics
}

// QueryCoreMetrics 只抓取行情及扩展指标，不依赖公司资料 HTML 页面。
func QueryCoreMetrics(ctx context.Context, exCode string) (*CoreMetrics, error) {
	quote, profile, err := Info(ctx, exCode)
	if err != nil {
		return nil, err
	}
	return calculateCoreMetrics(exCode, quote, profile), nil
}
