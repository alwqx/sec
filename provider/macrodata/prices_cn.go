package macrodata

import (
	"fmt"
	"math"
	"strings"

	"github.com/alwqx/sec/provider/nbs"
)

// CNPrices 分开官方相对指数和“指数减100”的涨跌幅；相对指数不是固定基期指数，目录缺少核心CPI时保留缺口。
func CNPrices(items []nbs.Observation, indicator, begin, end string) (*Dataset, error) {
	name := map[string]string{"cpi": "居民消费价格指数", "core-cpi": "不包括食品和能源居民消费价格指数", "ppi": "工业生产者出厂价格指数"}[indicator]
	if name == "" {
		return nil, fmt.Errorf("未知价格指标: %s", indicator)
	}
	if items == nil {
		items = []nbs.Observation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	coverage := "全国月度相对指数，未季调；不是固定基期指数，不能连乘代替长期累计涨幅"
	for _, item := range items {
		prefix := map[string]string{"上年同月=100": "yoy", "上月=100": "mom"}[item.Basis]
		definition := name + "(" + item.Basis + ")"
		if prefix == "" || item.Indicator != indicator || item.Region != "CN" || strings.Join(strings.Fields(item.SeriesName), "") != definition || item.Source == "" || item.FetchedAt.IsZero() {
			return nil, fmt.Errorf("价格指数统计口径不符合统一导出契约: %s %s", item.Period, item.Basis)
		}
		// 相对指数不可能小于等于0，这样的值说明接口异常，不当作官方值导出。
		if item.Index != nil && *item.Index <= 0 {
			return nil, fmt.Errorf("价格指数数值无效: %s %s %g", item.Period, item.Basis, *item.Index)
		}
		if (item.Change == nil) != (item.Index == nil) || (item.Index != nil && math.Abs(*item.Change-(*item.Index-100)) > 1e-6) {
			return nil, fmt.Errorf("价格涨跌幅与官方指数不一致: %s %s", item.Period, item.Basis)
		}
		indexReason, changeReason := "source_value_missing", "current_value_missing"
		if item.SeriesID == "" {
			if indicator != "core-cpi" || item.Index != nil || item.Note == "" {
				return nil, fmt.Errorf("价格指数缺少来源序列: %s %s", item.Period, item.Basis)
			}
			indexReason, changeReason = "source_series_unavailable", "source_series_unavailable"
		}
		base := Series{Indicator: indicator, Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "price-index", ValueOrigin: "official", Definition: definition, Coverage: coverage}
		observation := func() Observation {
			return Observation{Period: item.Period, SourceSeriesID: item.SeriesID, Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), DataVersion: "current_visible_revision", Note: item.Note}
		}
		index := base
		index.ID, index.Measure, index.Unit, index.IndexBase = "cn."+indicator+"."+prefix+"_index", prefix+"_index", "index", item.Basis
		o := observation()
		o.Value = item.Index
		if err := d.appendNBSReason(index, o, false, "", indexReason); err != nil {
			return nil, err
		}
		change := base
		change.ID, change.Measure, change.Unit, change.ValueOrigin, change.Formula = "cn."+indicator+"."+prefix+"_percent", prefix+"_percent", "percent", "derived", text("official_index - 100")
		o = observation()
		o.Value, o.Inputs = item.Change, []Base{{Period: item.Period, Value: item.Index}}
		if err := d.appendNBSReason(change, o, false, "", changeReason); err != nil {
			return nil, err
		}
	}
	return d, nil
}
