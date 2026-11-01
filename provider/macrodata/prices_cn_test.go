package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func priceItem(indicator, name, period, basis, seriesID string, index float64) nbs.Observation {
	change := index - 100
	return nbs.Observation{Region: "CN", Indicator: indicator, Period: period, Basis: basis, Index: &index, Change: &change, SeriesID: seriesID, SeriesName: name + " (" + basis + ") ", Source: "url", FetchedAt: rateFetched}
}

func TestCNPricesSeparatesIndexAndChange(t *testing.T) {
	d, err := CNPrices([]nbs.Observation{
		priceItem("cpi", "居民消费价格指数", "202609", "上年同月=100", "yoy-id", 100.9),
		priceItem("cpi", "居民消费价格指数", "202609", "上月=100", "mom-id", 100),
	}, "cpi", "202609", "202609")
	require.NoError(t, err)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 4)
	series := seriesByID(d)
	index := series["cn.cpi.yoy_index"]
	require.Equal(t, "official", index.ValueOrigin)
	require.Equal(t, "index", index.Unit)
	require.Equal(t, "上年同月=100", index.IndexBase)
	require.Equal(t, "price-index", index.PriceBasis)
	require.Equal(t, "not-seasonally-adjusted", index.SeasonalAdjustment)
	require.Equal(t, "居民消费价格指数(上年同月=100)", index.Definition)
	change := series["cn.cpi.yoy_percent"]
	require.Equal(t, "derived", change.ValueOrigin)
	require.Equal(t, "percent", change.Unit)
	require.Equal(t, "official_index - 100", *change.Formula)
	require.Empty(t, change.IndexBase)
	require.Equal(t, "上月=100", series["cn.cpi.mom_index"].IndexBase)

	require.Equal(t, 100.9, *observationOf(t, d, "cn.cpi.yoy_index").Value)
	percent := observationOf(t, d, "cn.cpi.yoy_percent")
	require.InDelta(t, 0.9, *percent.Value, 1e-9)
	require.Equal(t, 100.9, *percent.Inputs[0].Value)
	require.Equal(t, "202609", percent.Inputs[0].Period)
	require.Equal(t, "yoy-id", percent.SourceSeriesID)
	require.Equal(t, "mom-id", observationOf(t, d, "cn.cpi.mom_index").SourceSeriesID)
	flat := observationOf(t, d, "cn.cpi.mom_percent")
	require.Equal(t, "available", flat.Status)
	require.Equal(t, 0.0, *flat.Value, "持平是有效的零")
	require.Nil(t, flat.PublishedAt)
	require.Nil(t, flat.RawValue)
	require.Equal(t, rateFetched, *flat.FetchedAt)

	var original []nbs.Observation
	require.NoError(t, json.Unmarshal(d.Original, &original))
	require.Len(t, original, 2)
}

func TestCNPricesKeepsGaps(t *testing.T) {
	blank := priceItem("ppi", "工业生产者出厂价格指数", "201001", "上月=100", "mom-id", 100)
	blank.Index, blank.Change = nil, nil
	d, err := CNPrices([]nbs.Observation{blank}, "ppi", "201001", "201001")
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *observationOf(t, d, "cn.ppi.mom_index").MissingReason)
	require.Equal(t, "current_value_missing", *observationOf(t, d, "cn.ppi.mom_percent").MissingReason)

	gap := priceItem("core-cpi", "不包括食品和能源居民消费价格指数", "202001", "上年同月=100", "", 100)
	gap.Index, gap.Change, gap.Note = nil, nil, "该历史目录未提供核心 CPI 指标"
	d, err = CNPrices([]nbs.Observation{gap}, "core-cpi", "202001", "202001")
	require.NoError(t, err)
	for _, id := range []string{"cn.core-cpi.yoy_index", "cn.core-cpi.yoy_percent"} {
		o := observationOf(t, d, id)
		require.Equal(t, "source_series_unavailable", *o.MissingReason, "目录缺少指标不等于该月数值为空")
		require.Contains(t, o.Note, "未提供核心")
		require.Empty(t, o.SourceSeriesID)
		require.NotNil(t, o.Source)
	}

	d, err = CNPrices(nil, "cpi", "202609", "202609")
	require.NoError(t, err)
	require.Empty(t, d.Series)
	require.Empty(t, d.Observations)
}

func TestCNPricesRejectsContractViolations(t *testing.T) {
	for _, mutate := range []func(*nbs.Observation){
		func(i *nbs.Observation) { i.Region = "US" },
		func(i *nbs.Observation) { i.Indicator = "ppi" },
		func(i *nbs.Observation) { i.Basis = "上年同期=100" },
		func(i *nbs.Observation) { i.SeriesName = "非食品居民消费价格指数(上年同月=100)" },
		func(i *nbs.Observation) { i.SeriesID = "" },
		func(i *nbs.Observation) { i.Source = "" },
		func(i *nbs.Observation) { i.FetchedAt = time.Time{} },
		func(i *nbs.Observation) { i.Change = nil },
		func(i *nbs.Observation) { i.Index = nil },
		func(i *nbs.Observation) { i.Change = value(2) },
		func(i *nbs.Observation) { i.Index, i.Change = value(0), value(-100) },
		func(i *nbs.Observation) { i.Index, i.Change = value(-1), value(-101) },
		func(i *nbs.Observation) { i.Period = "2026-09" },
		func(i *nbs.Observation) { i.Period = "202610" },
	} {
		item := priceItem("cpi", "居民消费价格指数", "202609", "上年同月=100", "id", 100.9)
		mutate(&item)
		d, err := CNPrices([]nbs.Observation{item}, "cpi", "202609", "202609")
		require.Error(t, err)
		require.Nil(t, d)
	}
	item := priceItem("cpi", "居民消费价格指数", "202609", "上年同月=100", "id", 100.9)
	_, err := CNPrices([]nbs.Observation{item, item}, "cpi", "202609", "202609")
	require.Error(t, err, "同月同口径重复")
	_, err = CNPrices(nil, "gold", "202609", "202609")
	require.Error(t, err)
	gap := priceItem("cpi", "居民消费价格指数", "202609", "上年同月=100", "", 100)
	gap.Index, gap.Change = nil, nil
	_, err = CNPrices([]nbs.Observation{gap}, "cpi", "202609", "202609")
	require.Error(t, err, "只有核心CPI允许目录缺口")
	gap = priceItem("core-cpi", "不包括食品和能源居民消费价格指数", "202001", "上年同月=100", "", 100)
	gap.Index, gap.Change = nil, nil
	_, err = CNPrices([]nbs.Observation{gap}, "core-cpi", "202001", "202001")
	require.Error(t, err, "目录缺口必须说明原因")
}
