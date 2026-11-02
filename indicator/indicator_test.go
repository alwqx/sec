package indicator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSMA(t *testing.T) {
	require.Equal(t, []float64{0, 0, 2, 3, 4}, SMA([]float64{1, 2, 3, 4, 5}, 3))
	require.Equal(t, []float64{1, 2}, SMA([]float64{1, 2}, 1))
	require.Equal(t, []float64{0, 0}, SMA([]float64{1, 2}, 3), "数据不足")
	require.Equal(t, []float64{0, 0}, SMA([]float64{1, 2}, 0), "周期不合法")
	require.Empty(t, SMA(nil, 3))
}

func TestBollinger(t *testing.T) {
	// 2、4、4、4、5、5、7、9 的总体标准差是 2，均值是 5。
	values := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	mid, upper, lower := Bollinger(values, 8, 2)
	require.Equal(t, 5.0, mid[7])
	require.Equal(t, 9.0, upper[7])
	require.Equal(t, 1.0, lower[7])
	require.Zero(t, mid[6], "窗口不足")
	require.Zero(t, upper[6])
	require.Zero(t, lower[6])

	mid, upper, lower = Bollinger([]float64{1}, 3, 2)
	require.Equal(t, []float64{0}, mid)
	require.Equal(t, []float64{0}, upper)
	require.Equal(t, []float64{0}, lower)
}

func TestEMA(t *testing.T) {
	result := EMA([]float64{1, 2, 3, 4}, 3)
	require.Equal(t, []float64{0, 0, 2, 3}, result)
	require.Equal(t, []float64{0, 0}, EMA([]float64{1, 2}, 3))
	require.Equal(t, []float64{0}, EMA([]float64{1}, 0))
}

func TestMACD(t *testing.T) {
	values := make([]float64, 60)
	for i := range values {
		values[i] = 10 + float64(i)
	}
	line, signal, histogram := MACD(values, 12, 26, 9)
	require.Zero(t, line[24], "慢线窗口不足")
	require.Greater(t, line[59], 0.0, "持续上涨时快线高于慢线")
	require.InDelta(t, (line[59]-signal[59])*2, histogram[59], 1e-9)
	require.Equal(t, 33, MACDStart(12, 26, 9))
	require.Zero(t, signal[32], "信号线窗口不足")
	require.Zero(t, histogram[32])
	require.NotZero(t, signal[33])

	// 信号线以 MACD 线前 signal 个有效值的平均为起点，窗口不足的 0 不参与。
	line, signal, _ = MACD(values, 2, 4, 3)
	require.InDelta(t, (line[3]+line[4]+line[5])/3, signal[5], 1e-9)
	require.Equal(t, 5, MACDStart(2, 4, 3))

	line, signal, histogram = MACD(values[:3], 2, 4, 3)
	require.Equal(t, []float64{0, 0, 0}, line, "数据不足")
	require.Equal(t, []float64{0, 0, 0}, signal)
	require.Equal(t, []float64{0, 0, 0}, histogram)
	line, _, _ = MACD(values[:3], 0, 2, 1)
	require.Equal(t, []float64{0, 0, 0}, line, "周期不合法")
}

// 负价格是有效数据，不能被当成窗口不足。
func TestMACDNegativePrices(t *testing.T) {
	values := []float64{-5, -4, -3, -2, -1, -2, -3, -4, -5, -6}
	line, signal, _ := MACD(values, 2, 3, 2)
	require.Less(t, line[9], 0.0)
	require.NotEmpty(t, MACDSignals(line, signal, MACDStart(2, 3, 2)))
}

func TestRSI(t *testing.T) {
	require.Equal(t, []float64{0, 0, 100, 100}, RSI([]float64{1, 2, 3, 4}, 2), "只涨不跌")
	require.Equal(t, []float64{0, 0, 0, 0}, RSI([]float64{4, 3, 2, 1}, 2), "只跌不涨时是0")
	require.Equal(t, []float64{0, 0}, RSI([]float64{1, 2}, 2), "数据不足")
	result := RSI([]float64{1, 2, 1, 2, 1}, 2)
	require.InDelta(t, 50, result[2], 1e-9)
	require.Equal(t, []float64{0, 0, 50, 50}, RSI([]float64{3, 3, 3, 3}, 2), "完全走平是中性值")
	require.Equal(t, []float64{0, 50, 100, 50}, RSI([]float64{3, 3, 4, 4}, 1), "周期为1时当天不变是中性值")
}

func TestMASignals(t *testing.T) {
	fast := []float64{0, 1, 3, 3, 1}
	slow := []float64{0, 2, 2, 2, 2}
	require.Equal(t, []Signal{{2, Buy, "金叉"}, {4, Sell, "死叉"}}, MASignals(fast, slow, 1))
	require.Equal(t, []Signal{{4, Sell, "死叉"}}, MASignals(fast, slow, 2), "start 当天没有前一个有效值")

	// 前复权价格为负时均线也是负数，仍然产生信号。
	require.Equal(t, []Signal{{2, Buy, "金叉"}}, MASignals([]float64{0, -3, -1}, []float64{0, -2, -2}, 1))
}

func TestMACDSignals(t *testing.T) {
	line := []float64{0, -1, 1, 1, -1}
	signal := []float64{0, 0, 0, 0, 0}
	require.Equal(t, []Signal{{2, Buy, "MACD金叉"}, {4, Sell, "MACD死叉"}}, MACDSignals(line, signal, 1))
	require.Equal(t, []Signal{{4, Sell, "MACD死叉"}}, MACDSignals(line, signal, 2), "信号线有效之前不出信号")
}

func TestRSISignals(t *testing.T) {
	rsi := []float64{0, 20, 35, 80, 60}
	require.Equal(t, []Signal{{2, Buy, "RSI 35回升"}, {4, Sell, "RSI 60回落"}}, RSISignals(rsi, 1, 70, 30))

	// 第 2 天起有值，只跌不涨时 RSI 是有效的 0，反弹应产生买入信号。
	closes := []float64{10, 9, 8, 7, 12}
	got := RSISignals(RSI(closes, 2), 2, 70, 30)
	require.Len(t, got, 1)
	require.Equal(t, 4, got[0].Index)
	require.Equal(t, Buy, got[0].Type)
	require.Empty(t, RSISignals([]float64{0, 0, 40}, 2, 70, 30), "窗口不足的 0 不算超卖")
}

func TestBollingerSignals(t *testing.T) {
	values := []float64{5, 9, 11, 14, 11}
	upper := []float64{0, 12, 12, 12, 12}
	lower := []float64{0, 8, 8, 8, 8}
	got := BollingerSignals(values, upper, lower, 1)
	require.Equal(t, []Signal{{4, Sell, "触及上轨回落"}}, got, "窗口刚满的第一天不和为0的上一天轨道比较")
	values[1] = 7
	require.Equal(t, []Signal{{2, Buy, "触及下轨回升"}, {4, Sell, "触及上轨回落"}}, BollingerSignals(values, upper, lower, 1))

	// 下轨可以小于等于 0，这些天的上轨信号不能被跳过。
	require.Equal(t, []Signal{{2, Sell, "触及上轨回落"}}, BollingerSignals([]float64{5, 60, 40}, []float64{0, 50, 50}, []float64{0, -5, -5}, 1))
}
