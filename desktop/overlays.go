package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/indicator"
	"github.com/alwqx/sec/provider/eastmoney"
)

const (
	maxOverlayPeriod = 250
	maxMAOverlays    = 6
)

// overlaySpec 是一条指标请求：k 为 0 时是均线，否则是布林带。
type overlaySpec struct {
	period int
	k      float64
}

// overlay 是与 results 逐日对齐的指标序列，窗口不足的日期为 null。
type overlay struct {
	Label  string     `json:"label"`
	Kind   string     `json:"kind"`
	Values []*float64 `json:"values"`
}

func parsePeriod(text string) (int, error) {
	period, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || period <= 0 || period > maxOverlayPeriod {
		return 0, fmt.Errorf("指标周期须为 1 至 %d 的整数", maxOverlayPeriod)
	}
	return period, nil
}

// parseOverlays 解析 ma=5,20 和 boll=20,2。
func parseOverlays(ma, boll string) ([]overlaySpec, error) {
	specs := []overlaySpec{}
	if ma != "" {
		parts := strings.Split(ma, ",")
		if len(parts) > maxMAOverlays {
			return nil, fmt.Errorf("均线最多 %d 条", maxMAOverlays)
		}
		for _, part := range parts {
			period, err := parsePeriod(part)
			if err != nil {
				return nil, err
			}
			specs = append(specs, overlaySpec{period: period})
		}
	}
	if boll != "" {
		parts := strings.Split(boll, ",")
		if len(parts) != 2 {
			return nil, fmt.Errorf("布林带参数格式为 周期,倍数")
		}
		period, err := parsePeriod(parts[0])
		if err != nil {
			return nil, err
		}
		k, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err != nil || math.IsNaN(k) || k <= 0 || k > 10 {
			return nil, fmt.Errorf("布林带倍数须大于 0 且不超过 10")
		}
		specs = append(specs, overlaySpec{period: period, k: k})
	}
	return specs, nil
}

// warmupBegin 往前多取数据，使查询区间第一天的指标也有完整窗口。
// 交易日少于自然日，按最长周期的 2 倍加 15 根估算，再换算成自然日。
func warmupBegin(begin time.Time, specs []overlaySpec, freq eastmoney.Freq) time.Time {
	longest := 0
	for _, spec := range specs {
		longest = max(longest, spec.period)
	}
	if longest == 0 {
		return begin
	}
	return begin.AddDate(0, 0, -(longest*2+15)*freq.BarDays())
}

// nullBefore 返回下标 first 起的序列，start 之前窗口不足的位置为 null。
// 0 和负数可能是有效值，所以按下标判断。
func nullBefore(values []float64, start, first int) []*float64 {
	result := make([]*float64, 0, len(values)-first)
	for i := first; i < len(values); i++ {
		if i < start {
			result = append(result, nil)
			continue
		}
		value := values[i]
		result = append(result, &value)
	}
	return result
}

// computeOverlays 用含预热的完整数据计算指标，只返回 first 之后的部分。
func computeOverlays(quotes []*eastmoney.Quote, first int, specs []overlaySpec) []overlay {
	closes := make([]float64, len(quotes))
	for i, quote := range quotes {
		closes[i] = quote.Close
	}
	trim := func(values []float64, period int) []*float64 { return nullBefore(values, period-1, first) }
	overlays := []overlay{}
	for _, spec := range specs {
		if spec.k == 0 {
			overlays = append(overlays, overlay{fmt.Sprintf("MA%d", spec.period), "ma", trim(indicator.SMA(closes, spec.period), spec.period)})
			continue
		}
		mid, upper, lower := indicator.Bollinger(closes, spec.period, spec.k)
		overlays = append(overlays,
			overlay{"BOLL上轨", "boll", trim(upper, spec.period)},
			overlay{"BOLL中轨", "boll", trim(mid, spec.period)},
			overlay{"BOLL下轨", "boll", trim(lower, spec.period)},
		)
	}
	return overlays
}
