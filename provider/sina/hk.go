package sina

import (
	"context"
	"strings"
)

// hkQuoteProfile 返回港股公司概况中新浪能提供的部分：名称、价格和市值。
// 公司名称、行业、上市日期、市盈率和市净率等由 hkstock 包从东方财富补上。
func hkQuoteProfile(ctx context.Context, exCode string) (*CorpProfile, error) {
	quote, part, err := Info(ctx, exCode)
	if err != nil {
		return nil, err
	}
	metrics := calculateCoreMetrics(exCode, quote, part)
	return &CorpProfile{
		ExCode:          strings.ToUpper(exCode),
		Name:            quote.Name,
		HistoryName:     quote.Name,
		Current:         metrics.Price,
		PriceIsYClose:   metrics.PriceIsYClose,
		MarketCap:       metrics.MarketCap,
		TradedMarketCap: metrics.TradedMarketCap,
		HKTrust:         part.HKTrust,
	}, nil
}
