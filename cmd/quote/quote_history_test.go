package quote

import (
	"github.com/stretchr/testify/require"
	"os"
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

	printQuoteHistory(os.Stdout, []*eastmoney.Quote{quote1, quote2})
}

func TestFallbackQuoteHidesMissingTurnover(t *testing.T) {
	quote := &eastmoney.Quote{NoTurnover: true}
	require.Equal(t, "-", turnoverText(quote))
	require.Equal(t, "-", velocityText(quote))
	quote = &eastmoney.Quote{TurnOver: 100000000, Velocity: 1.5}
	require.NotEqual(t, "-", turnoverText(quote))
	require.Equal(t, "1.5", velocityText(quote))

	quote = &eastmoney.Quote{Close: 11, NoChange: true}
	require.Equal(t, "11", closeText(quote))
	require.Equal(t, "-", amplitudeText(quote))
	quote = &eastmoney.Quote{Close: 11, Change: 1, ChangeRate: 10, Amplitude: 2}
	require.Equal(t, "11 1 10%", closeText(quote))
	require.Equal(t, "2", amplitudeText(quote))
}
