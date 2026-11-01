// Package indicator 提供 CLI 与 Desktop 共用的技术指标计算。
package indicator

import "math"

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
