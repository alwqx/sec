package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/bond"
	"github.com/stretchr/testify/require"
)

func TestUSTreasury(t *testing.T) {
	fetched := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	result := &bond.QueryBondResp{Data: []*bond.BondYieldItem{
		{Date: "2026-01-05", BC3Month: 0, BC10Year: 4.3, MissingTenors: map[string]bool{"1m": true}},
		{Date: "2026-01-02", BC1Month: 3.7, BC10Year: 4.25},
	}}
	d, err := USTreasury(result, fetched, "20260101", "20260131")
	require.NoError(t, err)
	require.Equal(t, "US", d.Region)
	require.Len(t, d.Series, 11)
	require.Equal(t, "daily", d.Series[0].Frequency)
	require.Equal(t, "par_yield", d.Series[0].Measure)
	require.Equal(t, "official", d.Series[0].ValueOrigin)
	require.Len(t, d.Observations, 22)

	byID := map[string]Observation{}
	for _, o := range d.Observations {
		byID[o.SeriesID+"/"+o.Period] = o
	}
	require.Equal(t, "20260102", d.Observations[0].Period, "按日期升序输出")
	require.Equal(t, 3.7, *byID["us.treasury.1m.yield/20260102"].Value)
	missing := byID["us.treasury.1m.yield/20260105"]
	require.Nil(t, missing.Value)
	require.Equal(t, "missing", missing.Status)
	require.Equal(t, "source_value_missing", *missing.MissingReason)
	zero := byID["us.treasury.3m.yield/20260105"]
	require.Equal(t, "available", zero.Status)
	require.Equal(t, 0.0, *zero.Value, "真实0%不是缺失")
	require.Contains(t, *zero.Source, "field_tdr_date_value=2026", "来源是实际请求的年度地址")

	var original []map[string]any
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Len(t, original, 2)
	require.Nil(t, original[0]["1m"], "原结果里缺失的期限不是0")
	require.NotContains(t, original[0], "YClose")
}

func TestUSTreasuryEmptyWindowKeepsSeries(t *testing.T) {
	d, err := USTreasury(&bond.QueryBondResp{Data: []*bond.BondYieldItem{}}, time.Now(), "20260103", "20260104")
	require.NoError(t, err)
	require.Len(t, d.Series, 11)
	require.Empty(t, d.Observations)
}

func TestUSTreasuryRejectsBrokenContract(t *testing.T) {
	now := time.Now()
	item := func(date string) *bond.BondYieldItem { return &bond.BondYieldItem{Date: date} }
	for name, test := range map[string]struct {
		result  *bond.QueryBondResp
		fetched time.Time
		message string
	}{
		"nil result":   {nil, now, "结果不能为空"},
		"nil data":     {&bond.QueryBondResp{}, now, "不符合统一导出契约"},
		"no fetch":     {&bond.QueryBondResp{Data: []*bond.BondYieldItem{}}, time.Time{}, "不符合统一导出契约"},
		"nil item":     {&bond.QueryBondResp{Data: []*bond.BondYieldItem{nil}}, now, "记录不能为空"},
		"bad date":     {&bond.QueryBondResp{Data: []*bond.BondYieldItem{item("20260102")}}, now, "无效美国国债收益率日期"},
		"duplicate":    {&bond.QueryBondResp{Data: []*bond.BondYieldItem{item("2026-01-02"), item("2026-01-02")}}, now, "日期重复"},
		"out of range": {&bond.QueryBondResp{Data: []*bond.BondYieldItem{item("2025-12-31")}}, now, "超出查询范围"},
		"conflict":     {&bond.QueryBondResp{Data: []*bond.BondYieldItem{{Date: "2026-01-02", BC1Month: 3.7, MissingTenors: map[string]bool{"1m": true}}}}, now, "缺失标记与数值冲突"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := USTreasury(test.result, test.fetched, "20260101", "20260131")
			require.ErrorContains(t, err, test.message)
		})
	}
}
