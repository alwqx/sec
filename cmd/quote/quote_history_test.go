package quote

import (
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
)

func TestPrintQuoteHistory(t *testing.T) {
	now := time.Now()
	quote1 := &eastmoney.Quote{
		Date:       now.Add(-24 * time.Hour),
		Code:       "600036",
		Name:       "招商银行",
		Market:     1,
		Open:       38.9,
		Close:      39.05,
		High:       40.01,
		Low:        38.87,
		Volume:     123456,
		TurnOver:   7891011,
		Amplitude:  0.22,
		ChangeRate: 0.12,
		Change:     0.56,
		Velocity:   12.4,
	}

	quote2 := &eastmoney.Quote{
		Date:       now.Add(-48 * time.Hour),
		Code:       "600036",
		Name:       "招商银行",
		Market:     1,
		Open:       34.22,
		Close:      35.53,
		High:       40.01,
		Low:        38.87,
		Volume:     123456,
		TurnOver:   7891011,
		Amplitude:  -0.22,
		ChangeRate: 0.12,
		Change:     -0.56,
		Velocity:   12.4,
	}

	printQuoteHistory([]*eastmoney.Quote{quote1, quote2})
}
