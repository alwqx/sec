package usstock

import (
	"math"

	"github.com/alwqx/sec/provider/sina"
)

// Split 是一次拆股（Factor 大于 1，比如苹果 2020 年 1 拆 4）或合股（小于 1）。Date 是不复权日线跳变的那个交易日。
type Split struct {
	Date   string  `json:"date"`
	Factor float64 `json:"factor"`
}

// SharePoint 是用来核对拆股的一个股数，Since 是从哪天起可以用这个股数（封面股数的统计日、加权股数的申报日）。
type SharePoint struct {
	Since  string
	Shares float64
}

// splitFactors 是常见的拆股比例，合股用倒数。
var splitFactors = []float64{2, 3, 4, 5, 6, 7, 8, 10, 15, 20, 1.5}

// DetectSplits 从不复权日线的跳变识别拆股：相邻两天收盘价之比接近常见拆股比例（误差 4% 以内）且变化超过 40%。
// 还要用前后的股数核对：同一来源（封面或加权股数）在拆股前最后一次和之后第一次的股数（之后还没有申报时用新浪的当前股数 current）
// 之比与拆股比例相差不超过 25% 才算拆股；没有可核对的股数时不算，避免把真的暴跌一半当成 1 拆 2。
// 只看股数的跳变会把换股并购、封面列出的股票类别变化当成拆股，价格跳变排除了这些情况。估值和分红表共用。
func DetectSplits(bars []*sina.Bar, sources [][]SharePoint, current float64) []Split {
	splits := []Split{}
	for i := 1; i < len(bars); i++ {
		if bars[i].Close <= 0 || bars[i-1].Close <= 0 {
			continue
		}
		ratio := bars[i-1].Close / bars[i].Close
		if ratio < 1.4 && ratio > 1/1.4 {
			continue
		}
		factor := 0.0
		for _, k := range splitFactors {
			if math.Abs(ratio/k-1) < 0.04 {
				factor = k
			} else if math.Abs(ratio*k-1) < 0.04 {
				factor = 1 / k
			}
		}
		if factor == 0 {
			continue
		}
		date := bars[i].Date
		for _, points := range sources {
			var before, after *SharePoint
			for j := range points {
				if points[j].Since < date {
					if before == nil || points[j].Since >= before.Since {
						before = &points[j]
					}
				} else if after == nil || points[j].Since < after.Since {
					after = &points[j]
				}
			}
			later := current
			if after != nil {
				later = after.Shares
			}
			if before != nil && later > 0 && math.Abs(later/before.Shares/factor-1) <= 0.25 {
				splits = append(splits, Split{date, factor})
				break
			}
		}
	}
	return splits
}
