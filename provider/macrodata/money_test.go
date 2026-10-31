package macrodata

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
)

func value(n float64) *float64 { return &n }

func usSample() *fred.MoneyResult {
	return &fred.MoneyResult{Region: "US", Indicator: "money", SeriesID: "M2SL", Frequency: "monthly", PeriodBasis: "monthly-average", Unit: "十亿美元", SeasonallyAdjusted: true, Begin: "202609", End: "202609", Source: "https://fred.stlouisfed.org/graph/fredgraph.csv?id=M2SL", FetchedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC), DataVersion: "latest-available", Observations: []fred.MoneyObservation{{MoneyValue: fred.MoneyValue{Period: "202609", Level: value(110), RawValue: "110.0"}, YoY: value(10), MoM: value(0), YoYBase: fred.MoneyValue{Period: "202509", Level: value(100), RawValue: "100.0"}, MoMBase: fred.MoneyValue{Period: "202608", Level: value(110), RawValue: "110.0"}}}}
}

func TestCNMoneyDefinitionsAndOfficialGrowth(t *testing.T) {
	fetched := time.Date(2025, 2, 14, 12, 0, 0, 0, time.UTC)
	items := []pbc.MoneySupplyObservation{
		{Region: "CN", Period: "202412", M1Definition: "original", M1Balance: value(100), M1YoY: value(-2), M2Balance: value(300), M2YoY: value(0), Source: "https://www.pbc.gov.cn/old.html", FetchedAt: fetched, PublishedDate: "2025-01-14"},
		{Region: "CN", Period: "202501", M1Definition: "revised-2025", M1Balance: value(200), M1YoY: value(1), M2Balance: value(0), Source: "https://www.pbc.gov.cn/new.html", FetchedAt: fetched, StatisticalNotes: []string{"M1口径变化"}},
	}
	d, err := CNMoney(items, "202412", "202501")
	require.NoError(t, err)
	require.Equal(t, "1", d.SchemaVersion)
	require.Equal(t, "CN", d.Region)
	require.Len(t, d.Series, 6)
	require.Len(t, d.Observations, 8)
	require.Equal(t, "cn.m1.original.level", d.Observations[0].SeriesID)
	require.Equal(t, "cn.m1.revised-2025.level", d.Observations[4].SeriesID)
	require.Equal(t, 1.0, *d.Observations[5].Value, "官方同比不能用跨口径余额推算")
	require.Nil(t, d.Observations[5].Base)
	for _, s := range d.Series {
		require.Equal(t, "official", s.ValueOrigin)
		require.Equal(t, "month-end", s.PeriodBasis)
		require.Equal(t, "not-seasonally-adjusted", s.SeasonalAdjustment)
		require.Equal(t, "monthly", s.Frequency)
		require.Equal(t, "nominal", s.PriceBasis)
		require.False(t, s.Annualized)
		require.Nil(t, s.Formula)
	}
	require.Equal(t, "CNY_100_million", d.Series[0].Unit)
	require.Equal(t, "percent", d.Series[1].Unit)
	require.Equal(t, "available", d.Observations[6].Status)
	require.Equal(t, 0.0, *d.Observations[6].Value)
	require.Nil(t, d.Observations[6].MissingReason)
	require.Equal(t, "missing", d.Observations[7].Status)
	require.Equal(t, "source_value_missing", *d.Observations[7].MissingReason)
	require.Equal(t, fetched, *d.Observations[0].FetchedAt)
	require.Equal(t, "2025-01-14", *d.Observations[0].PublishedDate)
	require.Nil(t, d.Observations[0].PublishedAt, "日期不能伪造为精确发布时间")
	require.Nil(t, d.Observations[0].RawValue, "PBC适配结果没有原始数字字符串，不应伪造")
	require.Nil(t, d.Observations[0].RevisionID)
	var restored []pbc.MoneySupplyObservation
	require.NoError(t, json.Unmarshal(d.Original, &restored))
	require.Equal(t, items, restored)
}

func TestCNMoneyMissingReportAndEmpty(t *testing.T) {
	d, err := CNMoney([]pbc.MoneySupplyObservation{{Period: "202609", M1Definition: "revised-2025", Note: "未检索到报告"}}, "202609", "202609")
	require.NoError(t, err)
	for _, o := range d.Observations {
		require.Equal(t, "missing", o.Status)
		require.Equal(t, "report_not_found", *o.MissingReason)
		require.Nil(t, o.Value)
		require.Nil(t, o.Source)
		require.Nil(t, o.FetchedAt)
		require.Equal(t, "未检索到报告", o.Note)
	}
	d, err = CNMoney(nil, "202609", "202609")
	require.NoError(t, err)
	require.NotNil(t, d.Series)
	require.NotNil(t, d.Observations)
	require.JSONEq(t, "[]", string(d.Original))
}

func TestUSMoneyMetadataAndOriginal(t *testing.T) {
	r := usSample()
	d, err := USMoney(r)
	require.NoError(t, err)
	require.Len(t, d.Series, 3)
	require.Len(t, d.Observations, 3)
	require.Equal(t, "USD_billion", d.Series[0].Unit)
	require.Equal(t, "monthly-average", d.Series[0].PeriodBasis)
	require.Equal(t, "seasonally-adjusted", d.Series[0].SeasonalAdjustment)
	require.Equal(t, "official", d.Series[0].ValueOrigin)
	require.Equal(t, "derived", d.Series[1].ValueOrigin)
	require.Equal(t, "percent", d.Series[1].Unit)
	require.False(t, d.Series[1].Annualized)
	require.Equal(t, "(current_level / base_level - 1) * 100", *d.Series[1].Formula)
	require.Equal(t, "110.0", *d.Observations[0].RawValue)
	require.Nil(t, d.Observations[1].RawValue)
	require.Equal(t, "202509", d.Observations[1].Base.Period)
	require.Equal(t, "100.0", *d.Observations[1].Base.RawValue)
	require.Equal(t, "202608", d.Observations[2].Base.Period)
	require.Equal(t, 0.0, *d.Observations[2].Value)
	for _, o := range d.Observations {
		require.Equal(t, "available", o.Status)
		require.Nil(t, o.MissingReason)
		require.Equal(t, r.Source, *o.Source)
		require.Equal(t, r.FetchedAt, *o.FetchedAt)
		require.Nil(t, o.PublishedDate)
		require.Nil(t, o.PublishedAt)
		require.Nil(t, o.RevisionID)
		require.Equal(t, "current_visible_revision", o.DataVersion)
	}
	var restored fred.MoneyResult
	require.NoError(t, json.Unmarshal(d.Original, &restored))
	require.Equal(t, *r, restored)
}

func TestUSMoneyMissingReasons(t *testing.T) {
	for _, mode := range []string{"current", "base", "zero", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			r := usSample()
			r.Observations[0].YoY = nil
			reason := "calculation_unavailable"
			switch mode {
			case "current":
				r.Observations[0].Level = nil
				r.Observations[0].RawValue = "."
				reason = "current_value_missing"
			case "base":
				r.Observations[0].YoYBase.Level = nil
				reason = "baseline_missing"
			case "zero":
				r.Observations[0].YoYBase.Level = value(0)
				reason = "zero_baseline"
			}
			d, err := USMoney(r)
			require.NoError(t, err)
			require.Equal(t, "missing", d.Observations[1].Status)
			require.Equal(t, reason, *d.Observations[1].MissingReason)
			require.Nil(t, d.Observations[1].Value)
			if mode == "current" {
				require.Equal(t, ".", *d.Observations[0].RawValue)
			}
		})
	}
}

func TestMoneyAdapterRejectsInvalidData(t *testing.T) {
	_, err := USMoney(nil)
	require.Error(t, err)
	for _, mutate := range []func(*fred.MoneyResult){
		func(r *fred.MoneyResult) { r.Begin = "202613" },
		func(r *fred.MoneyResult) { r.Begin = "202610" },
		func(r *fred.MoneyResult) { r.Observations[0].Period = "202608" },
		func(r *fred.MoneyResult) { r.Observations[0].Period = "202613" },
		func(r *fred.MoneyResult) { r.Observations = append(r.Observations, r.Observations[0]) },
		func(r *fred.MoneyResult) { r.Observations[0].Level = value(math.Inf(1)) },
		func(r *fred.MoneyResult) { r.Observations[0].YoY = value(math.NaN()) },
	} {
		r := usSample()
		mutate(r)
		d, err := USMoney(r)
		require.Error(t, err)
		require.Nil(t, d)
	}
	for _, items := range [][]pbc.MoneySupplyObservation{
		{{Period: "202501", M1Definition: "original"}},
		{{Period: "202412", M1Definition: "revised-2025"}},
		{{Period: "202501", M1Definition: "revised-2025", M2Balance: value(math.NaN())}},
		{{Period: "202501", M1Definition: "revised-2025"}, {Period: "202501", M1Definition: "revised-2025"}},
	} {
		d, err := CNMoney(items, "202412", "202501")
		require.Error(t, err)
		require.Nil(t, d)
	}
}
