package quote

import (
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
)

func TestPrintQuoteHistory(t *testing.T) {
	now := time.Now()
	// 2025-06-03 真实数据
	quote1 := &eastmoney.Quote{
		Date:       now.Add(-24 * time.Hour),
		Code:       "600036",
		Name:       "招商银行",
		Market:     1,
		Open:       30.19,
		Close:      30.32,
		High:       30.46,
		Low:        30.12,
		Volume:     1789000000,
		TurnOver:   590000,
		Amplitude:  1.12,
		ChangeRate: -0.26,
		Change:     -0.08,
		Velocity:   0.82,
	}

	// 2025-06-04 真实数据
	quote2 := &eastmoney.Quote{
		Date:       now.Add(-48 * time.Hour),
		Code:       "600036",
		Name:       "招商银行",
		Market:     1,
		Open:       30.38,
		Close:      30.8,
		High:       30.98,
		Low:        30.3,
		Volume:     2579000000,
		TurnOver:   839200,
		Amplitude:  2.24,
		ChangeRate: 1.58,
		Change:     0.48,
		Velocity:   1.16,
	}

	printQuoteHistory([]*eastmoney.Quote{quote1, quote2})
}
