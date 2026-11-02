// Package indicator 提供 CLI 与 Desktop 共用的技术指标计算。
package indicator

import (
	"fmt"
	"math"
)

// SMA 返回简单移动平均，长度与 values 相同。
// 前 period-1 个位置窗口不足，值为 0；period 不合法或数据不足时全部为 0。
func SMA(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 || len(values) < period {
		return result
	}
	sum := 0.0
	for i := 0; i < period-1; i++ {
		sum += values[i]
	}
	for i := period - 1; i < len(values); i++ {
		sum += values[i]
		result[i] = sum / float64(period)
		sum -= values[i-period+1]
	}
	return result
}

// Bollinger 返回布林带中轨、上轨和下轨，标准差取总体标准差。窗口不足的位置值为 0。
func Bollinger(values []float64, period int, k float64) (mid, upper, lower []float64) {
	mid = SMA(values, period)
	upper = make([]float64, len(values))
	lower = make([]float64, len(values))
	if period <= 0 || len(values) < period {
		return
	}
	for i := period - 1; i < len(values); i++ {
		sum := 0.0
		for j := i - period + 1; j <= i; j++ {
			diff := values[j] - mid[i]
			sum += diff * diff
		}
		stddev := math.Sqrt(sum / float64(period))
		upper[i] = mid[i] + k*stddev
		lower[i] = mid[i] - k*stddev
	}
	return
}

// EMA 返回指数移动平均，以前 period 个值的简单平均为起点。窗口不足的位置值为 0。
func EMA(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 || len(values) < period {
		return result
	}
	k := 2.0 / float64(period+1)
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	result[period-1] = sum / float64(period)
	for i := period; i < len(values); i++ {
		result[i] = values[i]*k + result[i-1]*(1-k)
	}
	return result
}

// MACD 返回 MACD 线、信号线和柱（差值的 2 倍）。MACD 线从下标 max(fast,slow)-1 开始有值，
// 信号线和柱再晚 signal-1 个位置，有值的下标见 MACDStart；窗口不足的位置值为 0。
func MACD(values []float64, fast, slow, signal int) (line, signalLine, histogram []float64) {
	line = make([]float64, len(values))
	signalLine = make([]float64, len(values))
	histogram = make([]float64, len(values))
	lineStart := max(fast, slow) - 1
	if fast <= 0 || slow <= 0 || signal <= 0 || len(values) <= lineStart {
		return
	}
	emaFast := EMA(values, fast)
	emaSlow := EMA(values, slow)
	for i := lineStart; i < len(values); i++ {
		line[i] = emaFast[i] - emaSlow[i]
	}
	// 信号线从 MACD 线第一个有效值开始算，窗口不足的 0 不参与。
	copy(signalLine[lineStart:], EMA(line[lineStart:], signal))
	for i := MACDStart(fast, slow, signal); i < len(values); i++ {
		histogram[i] = (line[i] - signalLine[i]) * 2
	}
	return
}

// MACDStart 返回信号线和柱第一个有效值的下标。
func MACDStart(fast, slow, signal int) int {
	return max(fast, slow) + signal - 2
}

// RSI 返回使用 Wilder 平滑的相对强弱指标，从下标 period 开始有值。数据不足时全部为 0。
func RSI(values []float64, period int) []float64 {
	result := make([]float64, len(values))
	if period <= 0 || len(values) < period+1 {
		return result
	}
	gains := make([]float64, len(values))
	losses := make([]float64, len(values))
	for i := 1; i < len(values); i++ {
		diff := values[i] - values[i-1]
		if diff > 0 {
			gains[i] = diff
		} else {
			losses[i] = -diff
		}
	}
	avgGain, avgLoss := 0.0, 0.0
	for i := 1; i <= period; i++ {
		avgGain += gains[i]
		avgLoss += losses[i]
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)
	value := func() float64 {
		if avgLoss == 0 {
			// 窗口内完全走平时没有强弱之分，取中性值。
			if avgGain == 0 {
				return 50
			}
			return 100
		}
		return 100 - 100/(1+avgGain/avgLoss)
	}
	result[period] = value()
	for i := period + 1; i < len(values); i++ {
		avgGain = (avgGain*float64(period-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + losses[i]) / float64(period)
		result[i] = value()
	}
	return result
}

// Signal 是规则在某个位置触发的买入或卖出信号。
type Signal struct {
	Index  int
	Type   string // buy 或 sell
	Reason string
}

const (
	Buy  = "buy"
	Sell = "sell"
)

// 信号函数的 start 是指标第一个有效值的下标。窗口不足的位置值为 0，但 0 和负数也可能是
// 真实值（RSI 可以是 0，前复权价格可以为负），所以按下标判断有效性，不看值的大小。

// MASignals 在快线上穿慢线时买入、下穿时卖出。
func MASignals(fast, slow []float64, start int) []Signal {
	signals := []Signal{}
	for i := max(start+1, 1); i < len(fast); i++ {
		if fast[i-1] <= slow[i-1] && fast[i] > slow[i] {
			signals = append(signals, Signal{i, Buy, "金叉"})
		} else if fast[i-1] >= slow[i-1] && fast[i] < slow[i] {
			signals = append(signals, Signal{i, Sell, "死叉"})
		}
	}
	return signals
}

// MACDSignals 在 MACD 线上穿信号线时买入、下穿时卖出。
func MACDSignals(line, signalLine []float64, start int) []Signal {
	signals := []Signal{}
	for i := max(start+1, 1); i < len(line); i++ {
		if line[i-1] <= signalLine[i-1] && line[i] > signalLine[i] {
			signals = append(signals, Signal{i, Buy, "MACD金叉"})
		} else if line[i-1] >= signalLine[i-1] && line[i] < signalLine[i] {
			signals = append(signals, Signal{i, Sell, "MACD死叉"})
		}
	}
	return signals
}

// BollingerSignals 在收盘价从下轨外回升时买入、从上轨外回落时卖出。
func BollingerSignals(values, upper, lower []float64, start int) []Signal {
	signals := []Signal{}
	for i := max(start+1, 1); i < len(values); i++ {
		if values[i-1] <= lower[i-1] && values[i] > lower[i] {
			signals = append(signals, Signal{i, Buy, "触及下轨回升"})
		} else if values[i-1] >= upper[i-1] && values[i] < upper[i] {
			signals = append(signals, Signal{i, Sell, "触及上轨回落"})
		}
	}
	return signals
}

// RSISignals 在 RSI 从超卖区回升时买入、从超买区回落时卖出。
func RSISignals(rsi []float64, start int, overbought, oversold float64) []Signal {
	signals := []Signal{}
	for i := max(start+1, 1); i < len(rsi); i++ {
		if rsi[i-1] < oversold && rsi[i] >= oversold {
			signals = append(signals, Signal{i, Buy, fmt.Sprintf("RSI %.0f回升", rsi[i])})
		} else if rsi[i-1] > overbought && rsi[i] <= overbought {
			signals = append(signals, Signal{i, Sell, fmt.Sprintf("RSI %.0f回落", rsi[i])})
		}
	}
	return signals
}
