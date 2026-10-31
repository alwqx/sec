package macrodata

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

var nbsFetched = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func pmiItem(survey, indicator string, index *float64) nbs.PMIObservation {
	return nbs.PMIObservation{Survey: survey, Indicator: indicator, Period: "202609", Index: index, Unit: "%", SeasonallyAdjusted: true, SeriesID: "id-" + indicator, SeriesName: indicator + "(%)", Source: "url", FetchedAt: nbsFetched}
}

func TestPMIKeepsDiffusionIndexAndSurveys(t *testing.T) {
	d, err := PMI([]nbs.PMIObservation{pmiItem("manufacturing", "new-orders", value(49.2)), pmiItem("manufacturing", "employment", nil)}, "manufacturing", "202609", "202609")
	require.NoError(t, err)
	require.Equal(t, "CN", d.Region)
	require.Equal(t, "cn.pmi.new-orders.diffusion_index", d.Series[0].ID)
	require.Equal(t, "pmi.new-orders", d.Series[0].Indicator)
	require.Equal(t, "index", d.Series[0].Unit, "扩散指数不是百分比变化")
	require.Equal(t, "seasonally-adjusted", d.Series[0].SeasonalAdjustment)
	require.Equal(t, "manufacturing", d.Series[0].Survey)
	require.NotContains(t, d.Series[0].Coverage, "价格上升")
	require.Equal(t, 49.2, *d.Observations[0].Value, "不减100")
	require.Equal(t, "id-new-orders", d.Observations[0].SourceSeriesID)
	require.Equal(t, "source_value_missing", *d.Observations[1].MissingReason)
	require.Nil(t, d.Observations[0].PublishedAt)

	other, err := PMI([]nbs.PMIObservation{pmiItem("nonmanufacturing", "new-orders", value(0))}, "nonmanufacturing", "202609", "202609")
	require.NoError(t, err)
	require.NotEqual(t, d.Series[0].ID, other.Series[0].ID, "两个调查的同名分项不共用序列")
	require.NotEqual(t, d.Series[0].Indicator, other.Series[0].Indicator)
	require.Equal(t, "cn.nonmanufacturing-pmi.new-orders.diffusion_index", other.Series[0].ID)
	require.Equal(t, 0.0, *other.Observations[0].Value, "零值不是缺失")

	readings, err := PMI([]nbs.PMIObservation{pmiItem("nonmanufacturing", "supplier-delivery-times", value(51)), pmiItem("nonmanufacturing", "input-prices", value(52)), pmiItem("nonmanufacturing", "selling-prices", value(53))}, "nonmanufacturing", "202609", "202609")
	require.NoError(t, err)
	require.Contains(t, readings.Series[0].Coverage, "交货加快")
	require.Contains(t, readings.Series[1].Coverage, "价格上升")
	require.Contains(t, readings.Series[2].Coverage, "价格上升")

	empty, err := PMI(nil, "manufacturing", "202609", "202609")
	require.NoError(t, err)
	require.Empty(t, empty.Observations)
	require.JSONEq(t, "[]", string(empty.Original))

	for _, mutate := range []func(*nbs.PMIObservation){
		func(i *nbs.PMIObservation) { i.Survey = "nonmanufacturing" },
		func(i *nbs.PMIObservation) { i.Indicator = "" },
		func(i *nbs.PMIObservation) { i.Unit = "点" },
		func(i *nbs.PMIObservation) { i.SeasonallyAdjusted = false },
		func(i *nbs.PMIObservation) { i.SeriesID = "" },
		func(i *nbs.PMIObservation) { i.SeriesName = "" },
		func(i *nbs.PMIObservation) { i.Source = "" },
		func(i *nbs.PMIObservation) { i.Period = "2026-09" },
		func(i *nbs.PMIObservation) { i.Period = "202610" },
	} {
		item := pmiItem("manufacturing", "new-orders", value(50))
		mutate(&item)
		d, err := PMI([]nbs.PMIObservation{item}, "manufacturing", "202609", "202609")
		require.Error(t, err)
		require.Nil(t, d)
	}
	item := pmiItem("manufacturing", "new-orders", value(50))
	_, err = PMI([]nbs.PMIObservation{item, item}, "manufacturing", "202609", "202609")
	require.Error(t, err)
	_, err = PMI(nil, "composite", "202609", "202609")
	require.Error(t, err)
}

func industrialItem(period string) nbs.IndustrialObservation {
	published := time.Date(2026, 10, 19, 2, 6, 0, 0, time.UTC)
	fetched := nbsFetched.Add(time.Hour)
	return nbs.IndustrialObservation{Period: period, YTDPeriod: period[:4] + "01-" + period, YoY: value(5.5), YTD: value(5.3), MoM: value(0.61), Unit: "%", PriceBasis: "real", MoMSeasonallyAdjusted: true, YoYSeriesID: "yoy", YTDSeriesID: "ytd", Source: "library", FetchedAt: nbsFetched, MoMSource: "release", MoMPublishedAt: &published, MoMFetchedAt: &fetched}
}

func TestIndustrialSeparatesMeasures(t *testing.T) {
	item := industrialItem("202609")
	d, err := Industrial([]nbs.IndustrialObservation{item}, "202609", "202609")
	require.NoError(t, err)
	require.Len(t, d.Series, 3)
	require.Equal(t, "cn.industrial.yoy_percent", d.Series[0].ID)
	require.Equal(t, "not-seasonally-adjusted", d.Series[0].SeasonalAdjustment, "同比不标季调")
	require.Equal(t, "real", d.Series[0].PriceBasis)
	require.Equal(t, "year-to-date", d.Series[1].PeriodBasis)
	require.Equal(t, "seasonally-adjusted", d.Series[2].SeasonalAdjustment)
	require.Equal(t, "202601", d.Observations[1].PeriodBegin)
	require.Equal(t, "ytd", d.Observations[1].SourceSeriesID)
	require.Nil(t, d.Observations[0].PublishedAt, "发布稿时间不套用到发布库数据")
	require.Equal(t, "library", *d.Observations[0].Source)
	mom := d.Observations[2]
	require.Equal(t, 0.61, *mom.Value)
	require.Equal(t, "release", *mom.Source)
	require.NotNil(t, mom.PublishedAt)
	require.Equal(t, item.MoMFetchedAt.Unix(), mom.FetchedAt.Unix(), "环比用发布稿自己的抓取时间")

	february := industrialItem("202602")
	february.YoY, february.YTD, february.MoM, february.MoMSource, february.MoMPublishedAt, february.MoMFetchedAt = nil, value(6.3), nil, "", nil, nil
	february.Note = "1—2月合并累计，非2月单月同比；" + nbs.IndustrialMoMMissingNote
	d, err = Industrial([]nbs.IndustrialObservation{february}, "202602", "202602")
	require.NoError(t, err)
	require.Equal(t, "source_value_missing", *d.Observations[0].MissingReason, "不把累计值复制成单月同比")
	require.Equal(t, "1—2月合并累计，非2月单月同比", d.Observations[0].Note)
	require.Equal(t, 6.3, *d.Observations[1].Value)
	require.Equal(t, "202602", d.Observations[1].PeriodEnd)
	require.Contains(t, d.Observations[1].Note, "1—2月")
	require.Equal(t, nbs.IndustrialMoMMissingNote, d.Observations[2].Note, "备注按指标分开")
	require.Nil(t, d.Observations[2].Source)

	january := industrialItem("202601")
	january.YoY, january.YTD = nil, nil
	d, err = Industrial([]nbs.IndustrialObservation{january}, "202601", "202601")
	require.NoError(t, err)
	require.Contains(t, d.Observations[0].Note, "1—2月")
	require.Contains(t, d.Observations[1].Note, "1—2月")
	require.Empty(t, d.Observations[2].Note, "环比有值，不带1月说明")
	january = industrialItem("202601")
	d, err = Industrial([]nbs.IndustrialObservation{january}, "202601", "202601")
	require.NoError(t, err)
	require.Empty(t, d.Observations[0].Note, "1月有值时不说明")
	march := industrialItem("202603")
	march.YoY = nil
	d, err = Industrial([]nbs.IndustrialObservation{march}, "202603", "202603")
	require.NoError(t, err)
	require.Empty(t, d.Observations[0].Note, "其他月份缺失不归因于1—2月合并")

	for _, mutate := range []func(*nbs.IndustrialObservation){
		func(i *nbs.IndustrialObservation) { i.Unit = "亿元" },
		func(i *nbs.IndustrialObservation) { i.PriceBasis = "nominal" },
		func(i *nbs.IndustrialObservation) { i.MoMSeasonallyAdjusted = false },
		func(i *nbs.IndustrialObservation) { i.YoYSeriesID = "" },
		func(i *nbs.IndustrialObservation) { i.YTDSeriesID = "" },
		func(i *nbs.IndustrialObservation) { i.Source = "" },
		func(i *nbs.IndustrialObservation) { i.MoMSource = "" },
		func(i *nbs.IndustrialObservation) { i.MoMFetchedAt = nil },
		func(i *nbs.IndustrialObservation) { i.YTDPeriod = "202601-202608" },
	} {
		item := industrialItem("202609")
		mutate(&item)
		d, err := Industrial([]nbs.IndustrialObservation{item}, "202609", "202609")
		require.Error(t, err)
		require.Nil(t, d)
	}
	var original []nbs.IndustrialObservation
	empty, err := Industrial(nil, "202609", "202609")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(empty.Original, &original))
	require.Empty(t, original)
}

func unemploymentItem(indicator, version, scope, period string, rate *float64) nbs.UnemploymentObservation {
	return nbs.UnemploymentObservation{Period: period, Indicator: indicator, Rate: rate, Unit: "%", DefinitionVersion: version, SeriesKey: indicator + ":" + version, StudentScope: scope, SeriesID: "id", SeriesName: indicator + "(%)", Source: "url", FetchedAt: nbsFetched, Note: "口径说明"}
}

func TestUnemploymentSplitsDefinitionVersions(t *testing.T) {
	d, err := Unemployment([]nbs.UnemploymentObservation{
		unemploymentItem("age-16-24", "legacy-including-students", "included-if-in-labour-force", "202306", value(21.3)),
		unemploymentItem("age-16-24", "publication-paused-202307-202311", "not-applicable", "202307", nil),
		unemploymentItem("age-16-24", "excluding-students-since-202312", "excluded", "202312", value(14.9)),
		unemploymentItem("age-25-29", "excluding-students-since-202312", "excluded", "202306", nil),
		unemploymentItem("urban-surveyed", "national-surveyed", "included-if-in-labour-force", "202306", value(0)),
		unemploymentItem("age-25-59", "legacy-including-students", "included-if-in-labour-force", "202307", nil),
		unemploymentItem("age-25-59", "legacy-including-students", "included-if-in-labour-force", "202312", nil),
		unemploymentItem("age-25-29", "excluding-students-since-202312", "excluded", "202307", nil),
	}, "202306", "202312")
	require.NoError(t, err)
	require.Len(t, d.Series, 6)
	require.Equal(t, "publication_paused", *d.Observations[5].MissingReason, "旧分组同样受暂停影响")
	require.Equal(t, "source_value_missing", *d.Observations[6].MissingReason, "暂停期后旧分组的缺失不算暂停")
	require.Equal(t, "source_value_missing", *d.Observations[7].MissingReason, "新分组尚未开始发布")
	require.Equal(t, "cn.unemployment.age-16-24.legacy-including-students.rate", d.Series[0].ID)
	require.NotEqual(t, d.Series[0].ID, d.Series[2].ID, "断点前后不共用序列")
	require.Equal(t, d.Series[0].Indicator, d.Series[2].Indicator)
	require.Equal(t, "percent", d.Series[0].Unit)
	require.Equal(t, "not-seasonally-adjusted", d.Series[0].SeasonalAdjustment)
	require.Contains(t, d.Series[2].Methodology, "excluded")
	require.Equal(t, 21.3, *d.Observations[0].Value)
	require.Equal(t, "口径说明", d.Observations[0].Note)
	require.Equal(t, "publication_paused", *d.Observations[1].MissingReason)
	require.Equal(t, "source_value_missing", *d.Observations[3].MissingReason, "新分组历史缺口不算暂停")
	require.Equal(t, 0.0, *d.Observations[4].Value)
	require.Equal(t, "available", d.Observations[4].Status)

	for _, mutate := range []func(*nbs.UnemploymentObservation){
		func(i *nbs.UnemploymentObservation) { i.Indicator = "" },
		func(i *nbs.UnemploymentObservation) { i.DefinitionVersion = "" },
		func(i *nbs.UnemploymentObservation) { i.SeriesKey = "age-16-24:other" },
		func(i *nbs.UnemploymentObservation) { i.StudentScope = "unknown" },
		func(i *nbs.UnemploymentObservation) { i.Unit = "人" },
		func(i *nbs.UnemploymentObservation) { i.SeasonallyAdjusted = true },
		func(i *nbs.UnemploymentObservation) { i.SeriesID = "" },
		func(i *nbs.UnemploymentObservation) { i.SeriesName = "" },
		func(i *nbs.UnemploymentObservation) { i.Source = "" },
		func(i *nbs.UnemploymentObservation) { i.Period = "202401" },
	} {
		item := unemploymentItem("age-16-24", "legacy-including-students", "included-if-in-labour-force", "202306", value(21.3))
		mutate(&item)
		d, err := Unemployment([]nbs.UnemploymentObservation{item}, "202306", "202312")
		require.Error(t, err)
		require.Nil(t, d)
	}
	paused := unemploymentItem("age-16-24", "publication-paused-202307-202311", "not-applicable", "202307", value(1))
	_, err = Unemployment([]nbs.UnemploymentObservation{paused}, "202307", "202307")
	require.Error(t, err, "暂停发布期不应有数值")
	empty, err := Unemployment(nil, "202306", "202306")
	require.NoError(t, err)
	require.Empty(t, empty.Series)
}
