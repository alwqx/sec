package macrodata

import (
	"fmt"
	"strings"

	"github.com/alwqx/sec/provider/nbs"
)

// pmiReading 补充读法与总体扩张或收缩不同的分项，这些分项不能统一解释为景气加快。
var pmiReading = map[string]string{
	"input-prices":            "；高于50表示投入品价格上升，不代表景气改善",
	"selling-prices":          "；高于50表示销售价格上升，不代表景气改善",
	"supplier-delivery-times": "；高于50表示交货加快，不代表景气改善",
}

// PMI 保留季调扩散指数水平，不减100，也不解释为增速；制造业和非制造业的同名分项不共用序列。
func PMI(items []nbs.PMIObservation, survey, begin, end string) (*Dataset, error) {
	root := map[string]string{"manufacturing": "pmi", "nonmanufacturing": "nonmanufacturing-pmi"}[survey]
	if root == "" {
		return nil, fmt.Errorf("未知PMI调查: %s", survey)
	}
	if items == nil {
		items = []nbs.PMIObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Survey != survey || item.Indicator == "" || item.Unit != "%" || !item.SeasonallyAdjusted || item.SeriesID == "" || item.SeriesName == "" || item.Source == "" {
			return nil, fmt.Errorf("PMI统计口径不符合统一导出契约: %s", item.Period)
		}
		// 官方以%发布，但这是扩散指数水平，单位不用percent，避免被当作增速或比率换算；原始单位保留在original。
		s := Series{ID: "cn." + root + "." + item.Indicator + ".diffusion_index", Indicator: root + "." + item.Indicator, Measure: "diffusion_index", Unit: "index", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "seasonally-adjusted", PriceBasis: "not_applicable", ValueOrigin: "official", Definition: strings.TrimSpace(item.SeriesName), Coverage: "全国采购经理调查扩散指数，50为临界点；不是产出增速" + pmiReading[item.Indicator], Survey: item.Survey}
		o := Observation{Period: item.Period, Value: item.Index, SourceSeriesID: item.SeriesID, Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), DataVersion: "current_visible_revision"}
		if err := d.appendNBS(s, o, false, ""); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// industrialJanuaryNote 只说明接口未返回；官方通常合并发布1—2月，但不据此断言每年的原因。
const industrialJanuaryNote = "接口未返回1月数值；官方通常合并发布1—2月数据，不拆分或补值"

// unemploymentPauseBegin、unemploymentPauseEnd 是分年龄组暂停发布的所属期。
const (
	unemploymentPauseBegin = "202307"
	unemploymentPauseEnd   = "202311"
)

// Industrial 区分单月同比、年初累计同比和季调环比；不从累计增速推算单月，也不拆分1—2月。
func Industrial(items []nbs.IndustrialObservation, begin, end string) (*Dataset, error) {
	if items == nil {
		items = []nbs.IndustrialObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Unit != "%" || item.PriceBasis != "real" || !item.MoMSeasonallyAdjusted || item.YoYSeriesID == "" || item.YTDSeriesID == "" || item.Source == "" {
			return nil, fmt.Errorf("工业增加值统计口径不符合统一导出契约: %s", item.Period)
		}
		if item.MoM != nil && (item.MoMSource == "" || item.MoMFetchedAt == nil) {
			return nil, fmt.Errorf("工业增加值环比缺少来源或抓取时间: %s", item.Period)
		}
		// provider的整行备注可能同时说明1—2月和环比缺失，这里按指标分开。
		monthNote, momNote := []string{}, []string{}
		for _, part := range strings.Split(item.Note, "；") {
			if part == nbs.IndustrialMoMMissingNote {
				momNote = append(momNote, part)
			} else if part != "" {
				monthNote = append(monthNote, part)
			}
		}
		base := Series{Indicator: "industrial", Unit: "percent", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "real", ValueOrigin: "official", Definition: "national-above-scale-value-added", Coverage: "全国规模以上工业增加值实际增速，扣除价格因素；不是产值水平"}
		for _, spec := range []struct {
			measure, sourceID string
			value             *float64
			cumulative        bool
		}{{"yoy_percent", item.YoYSeriesID, item.YoY, false}, {"ytd_yoy_percent", item.YTDSeriesID, item.YTD, true}} {
			s := base
			s.ID, s.Measure = "cn.industrial."+spec.measure, spec.measure
			o := Observation{Period: item.Period, Value: spec.value, SourceSeriesID: spec.sourceID, Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), DataVersion: "current_visible_revision", Note: strings.Join(monthNote, "；")}
			if spec.value == nil && o.Note == "" && strings.HasSuffix(item.Period, "01") {
				o.Note = industrialJanuaryNote
			}
			if err := d.appendNBS(s, o, spec.cumulative, item.YTDPeriod); err != nil {
				return nil, err
			}
		}
		s := base
		s.ID, s.Measure, s.SeasonalAdjustment = "cn.industrial.mom_percent", "mom_percent", "seasonally-adjusted"
		o := Observation{Period: item.Period, Value: item.MoM, Source: text(item.MoMSource), PublishedAt: item.MoMPublishedAt, DataVersion: "current_visible_revision", Note: strings.Join(momNote, "；")}
		if item.MoMFetchedAt != nil {
			o.FetchedAt = timestamp(*item.MoMFetchedAt)
		}
		if err := d.appendNBS(s, o, false, ""); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Unemployment 按指标和口径版本分开序列，年龄分组不跨2023年断点连接。
func Unemployment(items []nbs.UnemploymentObservation, begin, end string) (*Dataset, error) {
	if items == nil {
		items = []nbs.UnemploymentObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	scopes := map[string]bool{"included-if-in-labour-force": true, "excluded": true, "not-applicable": true}
	for _, item := range items {
		paused := strings.HasPrefix(item.DefinitionVersion, "publication-paused")
		if item.Indicator == "" || item.DefinitionVersion == "" || item.SeriesKey != item.Indicator+":"+item.DefinitionVersion || !scopes[item.StudentScope] || item.Unit != "%" || item.SeasonallyAdjusted || item.SeriesID == "" || item.SeriesName == "" || item.Source == "" {
			return nil, fmt.Errorf("失业率统计口径不符合统一导出契约: %s", item.Period)
		}
		if paused && item.Rate != nil {
			return nil, fmt.Errorf("暂停发布期不应有失业率数值: %s", item.Period)
		}
		s := Series{ID: "cn.unemployment." + item.Indicator + "." + item.DefinitionVersion + ".rate", Indicator: "unemployment." + item.Indicator, Measure: "rate", Unit: "percent", Frequency: "monthly", PeriodBasis: "month", SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "not_applicable", ValueOrigin: "official", Definition: strings.TrimSpace(item.SeriesName), Coverage: "全国城镇调查失业率水平，不是登记失业率或增速", Methodology: item.DefinitionVersion + "; students: " + item.StudentScope}
		o := Observation{Period: item.Period, Value: item.Rate, SourceSeriesID: item.SeriesID, Source: text(item.Source), FetchedAt: timestamp(item.FetchedAt), DataVersion: "current_visible_revision", Note: item.Note}
		reason := "source_value_missing"
		// 旧25—59岁分组保留原口径版本，但同样受暂停影响；新分组此时尚未开始发布，仍是历史缺口。
		if paused || (item.Indicator == "age-25-59" && item.Rate == nil && item.Period >= unemploymentPauseBegin && item.Period <= unemploymentPauseEnd) {
			reason = "publication_paused"
		}
		if err := d.appendNBSReason(s, o, false, "", reason); err != nil {
			return nil, err
		}
	}
	return d, nil
}
