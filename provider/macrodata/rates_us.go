package macrodata

import (
	"fmt"
	"sort"
	"time"

	"github.com/alwqx/sec/provider/bond"
)

// USTreasury 把财政部收益率曲线转为统一序列，每个期限各成一条日度序列。
func USTreasury(result *bond.QueryBondResp, fetchedAt time.Time, begin, end string) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("美国国债收益率结果不能为空")
	}
	if result.Data == nil || fetchedAt.IsZero() {
		return nil, fmt.Errorf("美国国债收益率响应不符合统一导出契约")
	}
	items := append([]*bond.BondYieldItem{}, result.Data...)
	for _, item := range items {
		if item == nil {
			return nil, fmt.Errorf("美国国债收益率记录不能为空")
		}
	}
	d, err := newDatasetFrequency("US", begin, end, "daily", bond.CurveDays(items))
	if err != nil {
		return nil, err
	}
	tenors := []struct {
		id    string
		value func(*bond.BondYieldItem) float64
	}{
		{"1m", func(i *bond.BondYieldItem) float64 { return i.BC1Month }},
		{"3m", func(i *bond.BondYieldItem) float64 { return i.BC3Month }},
		{"6m", func(i *bond.BondYieldItem) float64 { return i.BC6Month }},
		{"1y", func(i *bond.BondYieldItem) float64 { return i.BC1Year }},
		{"2y", func(i *bond.BondYieldItem) float64 { return i.BC2Year }},
		{"3y", func(i *bond.BondYieldItem) float64 { return i.BC3Year }},
		{"5y", func(i *bond.BondYieldItem) float64 { return i.BC5Year }},
		{"7y", func(i *bond.BondYieldItem) float64 { return i.BC7Year }},
		{"10y", func(i *bond.BondYieldItem) float64 { return i.BC10Year }},
		{"20y", func(i *bond.BondYieldItem) float64 { return i.BC20Year }},
		{"30y", func(i *bond.BondYieldItem) float64 { return i.BC30Year }},
	}
	for _, tenor := range tenors {
		s := pbcSeries("us.treasury."+tenor.id+".yield", "treasury."+tenor.id, "par_yield", "percent", "record-date", "美国财政部国债平价收益率曲线"+tenor.id+"收益率", "财政部每日国债平价收益率曲线；无记录的日期不补")
		s.Frequency, s.PriceBasis, s.SeasonalAdjustment = "daily", "not_applicable", "not_applicable"
		d.Series = append(d.Series, s)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	seen := map[string]bool{}
	for _, item := range items {
		date, err := time.Parse("2006-01-02", item.Date)
		if err != nil || date.Format("2006-01-02") != item.Date {
			return nil, fmt.Errorf("无效美国国债收益率日期: %s", item.Date)
		}
		if seen[item.Date] {
			return nil, fmt.Errorf("美国国债收益率日期重复: %s", item.Date)
		}
		seen[item.Date] = true
		for _, tenor := range tenors {
			o := Observation{SeriesID: "us.treasury." + tenor.id + ".yield", Period: date.Format("20060102"), Source: text(bond.YieldCurveYearURL(date.Year())), FetchedAt: timestamp(fetchedAt), DataVersion: "current_visible_revision"}
			value := tenor.value(item)
			if item.MissingTenors[tenor.id] {
				if value != 0 {
					return nil, fmt.Errorf("美国国债收益率缺失标记与数值冲突: %s %s", item.Date, tenor.id)
				}
			} else {
				o.Value = &value
			}
			if err := d.append(o, "source_value_missing"); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
