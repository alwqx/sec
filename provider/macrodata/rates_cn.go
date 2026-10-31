package macrodata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/chinabond"
	"github.com/alwqx/sec/provider/pbc"
)

// pbcPublication 按源精度拆分公布时间：只有日期时不补时分秒。
func pbcPublication(value, precision string) (*string, *time.Time, error) {
	switch {
	case value == "":
		return nil, nil, nil
	case precision == "second":
		at, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, nil, fmt.Errorf("无效公布时间: %s", value)
		}
		date := at.Format("2006-01-02")
		return &date, &at, nil
	case precision == "day":
		return &value, nil, nil
	}
	return nil, nil, fmt.Errorf("未知公布时间精度: %s", precision)
}

func cnRateSeries(id, indicator, measure, unit, frequency, basis, definition, coverage string) Series {
	s := pbcSeries(id, indicator, measure, unit, basis, definition, coverage)
	s.Frequency, s.PriceBasis = frequency, "not_applicable"
	return s
}

// shortHash 生成稳定的事件标识片段，不依赖记录顺序。
func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:8]
}

// LPR 两个期限各自成序列；基点变化只比较相邻月份，不跨缺口，也不把报价展开成日度利率。
func LPR(items []pbc.LPRObservation, begin, end string) (*Dataset, error) {
	if items == nil {
		items = []pbc.LPRObservation{}
	}
	d, err := newDataset("CN", begin, end, items)
	if err != nil {
		return nil, err
	}
	coverage := "全国银行间同业拆借中心受权公布的贷款市场报价利率，2019年8月改革后口径，在下一次发布前有效"
	previous := map[string]pbc.LPRObservation{}
	for _, item := range items {
		if item.Unit != "%" || item.Definition != "lpr-reform-2019" {
			return nil, fmt.Errorf("LPR统计口径不符合统一导出契约: %s", item.Period)
		}
		released := item.ChangeStatus != "missing-release"
		if released && (item.Source == "" || item.FetchedAt == nil || item.QuoteDate == "") {
			return nil, fmt.Errorf("LPR公告缺少来源、抓取时间或报价日: %s", item.Period)
		}
		publishedDate, publishedAt, err := pbcPublication(item.PublishedAt, item.PublishedPrecision)
		if err != nil {
			return nil, err
		}
		month, err := time.Parse("200601", item.Period)
		if err != nil || month.Format("200601") != item.Period {
			return nil, fmt.Errorf("无效所属月份: %s", item.Period)
		}
		basePeriod := month.AddDate(0, -1, 0).Format("200601")
		for _, tenor := range []struct {
			id, name     string
			rate, change *float64
			prior        func(pbc.LPRObservation) *float64
		}{
			{"1y", "1年期LPR", item.OneYearRate, item.OneYearChangeBP, func(o pbc.LPRObservation) *float64 { return o.OneYearRate }},
			{"5y-plus", "5年期以上LPR", item.FiveYearPlusRate, item.FiveYearPlusChangeBP, func(o pbc.LPRObservation) *float64 { return o.FiveYearPlusRate }},
		} {
			base := func() Observation {
				o := Observation{Period: item.Period, Source: text(item.Source), PublishedDate: publishedDate, PublishedAt: publishedAt, DataVersion: "current_visible_revision"}
				if item.QuoteDate != "" {
					o.AsOfDate = text(item.QuoteDate)
				}
				if item.FetchedAt != nil {
					o.FetchedAt = timestamp(*item.FetchedAt)
				}
				return o
			}
			missing := "source_value_missing"
			if !released {
				missing = "report_not_found"
			}
			rate := base()
			rate.Value = tenor.rate
			if !released {
				rate.Note = item.Note
			}
			if err := d.appendNBSReason(cnRateSeries("cn.lpr."+tenor.id+".rate", "lpr."+tenor.id, "rate", "percent", "monthly", "month", tenor.name, coverage), rate, false, "", missing); err != nil {
				return nil, err
			}
			change := base()
			change.Value = tenor.change
			change.Base = &Base{Period: basePeriod}
			if prior, ok := previous[basePeriod]; ok {
				change.Base.Value = tenor.prior(prior)
			}
			if tenor.change != nil && change.Base.Value != nil && tenor.rate != nil && math.Abs(*tenor.change-(*tenor.rate-*change.Base.Value)*100) > 1e-6 {
				return nil, fmt.Errorf("LPR变化与相邻月份报价不一致: %s", item.Period)
			}
			if tenor.change != nil && tenor.rate == nil {
				return nil, fmt.Errorf("LPR变化缺少当月报价: %s", item.Period)
			}
			reason := "baseline_missing"
			switch {
			case !released:
				reason, change.Note = "report_not_found", item.Note
			case item.Period == pbc.LPRCoverageStart:
				reason, change.Note = "methodology_break", "2019年8月报价机制改革首月，不与旧口径比较"
			case tenor.change == nil:
				change.Note = item.Note
			case change.Base.Value == nil:
				change.Note = "上月报价在查询窗口外，基期水平未由provider返回；不从变化值反推"
			}
			s := cnRateSeries("cn.lpr."+tenor.id+".change", "lpr."+tenor.id, "previous_month_change", "basis_points", "monthly", "month", tenor.name+"相邻月变化", coverage)
			s.ValueOrigin, s.Formula, s.ChangeMethod = "derived", text("(current_rate - previous_month_rate) * 100"), "difference-of-adjacent-monthly-releases"
			if err := d.appendNBSReason(s, change, false, "", reason); err != nil {
				return nil, err
			}
		}
		previous[item.Period] = item
	}
	return d, nil
}

// ReverseRepo 只输出窗口内的官方调整公告；调整前利率来自公告原文，区间前最后一次公告保留在original。
func ReverseRepo(result *pbc.ReverseRepoResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("逆回购结果不能为空")
	}
	if result.Indicator != "pbc-7-day-reverse-repo-policy-rate" || result.TenorDays != 7 || result.Unit != "%" {
		return nil, fmt.Errorf("逆回购统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("CN", result.Begin, result.End, "event", result)
	if err != nil {
		return nil, err
	}
	coverage := "人民银行公开市场7天逆回购操作利率调整公告，2024年7月固定利率机制以来；每条观测是一次调整，不展开成日度利率"
	rate := cnRateSeries("cn.reverse-repo.7d.rate", "reverse-repo.7d", "rate", "percent", "event", "effective-date", "7天期逆回购操作利率", coverage)
	change := cnRateSeries("cn.reverse-repo.7d.change", "reverse-repo.7d", "adjustment_change", "basis_points", "event", "effective-date", "7天期逆回购操作利率调整幅度", coverage)
	change.ValueOrigin, change.Formula = "derived", text("(rate - previous_rate) * 100")
	d.Series = append(d.Series, rate, change)
	previous := result.PreviousEvent
	for _, event := range result.Events {
		if err := validatePeriod(event.EffectiveDate, "event"); err != nil {
			return nil, err
		}
		if event.Source == "" || event.FetchedAt.IsZero() || event.Rate < 0 || event.PreviousRate < 0 {
			return nil, fmt.Errorf("逆回购公告缺少来源或利率无效: %s", event.EffectiveDate)
		}
		if math.Abs(event.ChangeBasisPoints-(event.Rate-event.PreviousRate)*100) > 1e-6 {
			return nil, fmt.Errorf("逆回购调整幅度与前后利率不一致: %s", event.EffectiveDate)
		}
		if previous != nil && (previous.EffectiveDate >= event.EffectiveDate || math.Abs(previous.Rate-event.PreviousRate) > 1e-9) {
			return nil, fmt.Errorf("逆回购前次公告与调整前利率不一致: %s", event.EffectiveDate)
		}
		publishedDate, publishedAt, err := pbcPublication(event.PublishedAt, event.PublishedPrecision)
		if err != nil {
			return nil, err
		}
		observation := func(seriesID string, value float64) Observation {
			return Observation{SeriesID: seriesID, Period: event.EffectiveDate, Value: &value, Source: text(event.Source), FetchedAt: timestamp(event.FetchedAt), PublishedDate: publishedDate, PublishedAt: publishedAt, DataVersion: "current_visible_revision"}
		}
		o := observation(rate.ID, event.Rate)
		if err := d.append(o, "source_value_missing"); err != nil {
			return nil, err
		}
		o = observation(change.ID, event.ChangeBasisPoints)
		before := event.PreviousRate
		o.Base = &Base{Value: &before}
		if previous != nil {
			o.Base.Period = previous.EffectiveDate
		}
		if err := d.append(o, "calculation_unavailable"); err != nil {
			return nil, err
		}
		current := event
		previous = &current
	}
	return d, nil
}

// ReverseRepoHistory 保留固定利率机制前的逐次中标利率；所属期是公告公布日，同日可有多次操作。
func ReverseRepoHistory(result *pbc.ReverseRepoHistoryResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("逆回购历史结果不能为空")
	}
	if result.Indicator != "7-day-reverse-repo-awarded-rate-history" || result.Unit != "%" || result.Currency != "CNY" || result.DateBasis != "publication-date" {
		return nil, fmt.Errorf("逆回购历史统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("CN", result.Begin, result.End, "event", result)
	if err != nil {
		return nil, err
	}
	s := cnRateSeries("cn.reverse-repo-history.7d.awarded_rate", "reverse-repo-history.7d", "awarded_rate", "percent", "event", "publication-date", "7天期逆回购中标利率", "固定利率机制前逐次公开市场操作的中标利率；不是政策利率调整事件，未操作的日期没有观测")
	d.Series = append(d.Series, s)
	seen := map[string]bool{}
	for _, event := range result.Events {
		period := strings.ReplaceAll(event.PublicationDate, "-", "")
		if err := validatePeriod(period, "event"); err != nil {
			return nil, err
		}
		if event.SourceURL == "" || event.Term != "7天" || event.RatePercent < 0 {
			return nil, fmt.Errorf("逆回购操作缺少来源或期限不符: %s", event.PublicationDate)
		}
		id := period + "/" + shortHash(event.SourceURL, event.PublishedAt, event.AwardedRateRaw)
		if seen[id] {
			return nil, fmt.Errorf("逆回购操作重复: %s", id)
		}
		seen[id] = true
		value := event.RatePercent
		o := Observation{SeriesID: s.ID, EventID: id, Period: period, Value: &value, RawValue: text(event.AwardedRateRaw), Source: text(event.SourceURL), PublishedDate: text(event.PublicationDate), DataVersion: "current_visible_revision"}
		if event.OperationDate != "" {
			o.AsOfDate = text(event.OperationDate)
		}
		if err := d.append(o, "source_value_missing"); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// RRR 只导出官方调整幅度，不推算准备金率水平；同一生效日可有多个机构范围，用event_id区分。
func RRR(result *pbc.RRRResult) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("存款准备金率结果不能为空")
	}
	if result.Indicator != "rmb-reserve-requirement-ratio-adjustments" || result.Currency != "CNY" || result.Unit != "百分点" {
		return nil, fmt.Errorf("存款准备金率统计口径不符合统一导出契约")
	}
	d, err := newDatasetFrequency("CN", result.Begin, result.End, "event", result)
	if err != nil {
		return nil, err
	}
	coverage := "人民币法定存款准备金率调整幅度，负值为下调；区间幅度不取均值，不代表每家银行的具体幅度，也不是准备金率水平"
	lower := cnRateSeries("cn.rrr.change_lower", "rrr", "change_lower", "percentage_points", "event", "effective-date", "调整幅度下界", coverage)
	upper := cnRateSeries("cn.rrr.change_upper", "rrr", "change_upper", "percentage_points", "event", "effective-date", "调整幅度上界", coverage)
	d.Series = append(d.Series, lower, upper)
	seen := map[string]bool{}
	for _, event := range result.Events {
		if err := validatePeriod(event.EffectiveDate, "event"); err != nil {
			return nil, err
		}
		if event.Source == "" || event.FetchedAt.IsZero() || event.Scope == "" || event.ScopeType == "" || event.ChangePPLower > event.ChangePPUpper || event.TrancheIndex < 1 || event.TrancheIndex > event.TrancheCount {
			return nil, fmt.Errorf("存款准备金率事件不符合统一导出契约: %s", event.EffectiveDate)
		}
		id := event.EffectiveDate + "/" + event.ScopeType + "/" + shortHash(event.Scope, event.Exclusions, strconv.FormatBool(event.Additional), strconv.Itoa(event.TrancheIndex))
		if seen[id] {
			return nil, fmt.Errorf("存款准备金率事件重复: %s", id)
		}
		seen[id] = true
		publishedDate, publishedAt, err := pbcPublication(event.PublishedAt, event.PublishedPrecision)
		if err != nil {
			return nil, err
		}
		d.PolicyEvents = append(d.PolicyEvents, PolicyEvent{ID: id, EffectiveDate: event.EffectiveDate, EffectiveDateBasis: event.EffectiveDateBasis, ScopeType: event.ScopeType, Scope: event.Scope, Exclusions: event.Exclusions, Additional: event.Additional, TrancheIndex: event.TrancheIndex, TrancheCount: event.TrancheCount, Title: event.Title})
		for _, field := range []struct {
			id    string
			value float64
		}{{lower.ID, event.ChangePPLower}, {upper.ID, event.ChangePPUpper}} {
			value := field.value
			o := Observation{SeriesID: field.id, EventID: id, Period: event.EffectiveDate, Value: &value, Source: text(event.Source), FetchedAt: timestamp(event.FetchedAt), PublishedDate: publishedDate, PublishedAt: publishedAt, DataVersion: "current_visible_revision"}
			if err := d.append(o, "source_value_missing"); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// CNTreasury 保留全部关键期限的官方收益率原文；空值不补，接口不提供抓取和公布时间。
func CNTreasury(result *chinabond.GetChinaBondResp, source, begin, end string) (*Dataset, error) {
	if result == nil {
		return nil, fmt.Errorf("国债收益率结果不能为空")
	}
	if result.Flag != "0" || result.HeList == nil || source == "" {
		return nil, fmt.Errorf("国债收益率响应不符合统一导出契约")
	}
	d, err := newDatasetFrequency("CN", begin, end, "daily", result)
	if err != nil {
		return nil, err
	}
	tenors := []struct {
		id    string
		value func(*chinabond.ChinaBondItem) string
	}{
		{"3m", func(i *chinabond.ChinaBondItem) string { return i.ThreeMonth }},
		{"6m", func(i *chinabond.ChinaBondItem) string { return i.SixMonth }},
		{"1y", func(i *chinabond.ChinaBondItem) string { return i.OneYear }},
		{"2y", func(i *chinabond.ChinaBondItem) string { return i.TwoYear }},
		{"3y", func(i *chinabond.ChinaBondItem) string { return i.ThreeYear }},
		{"5y", func(i *chinabond.ChinaBondItem) string { return i.FiveYear }},
		{"7y", func(i *chinabond.ChinaBondItem) string { return i.SevenYear }},
		{"10y", func(i *chinabond.ChinaBondItem) string { return i.TenYear }},
		{"15y", func(i *chinabond.ChinaBondItem) string { return i.FifteenYear }},
		{"20y", func(i *chinabond.ChinaBondItem) string { return i.TwentyYear }},
		{"30y", func(i *chinabond.ChinaBondItem) string { return i.ThirtyYear }},
	}
	for _, tenor := range tenors {
		d.Series = append(d.Series, cnRateSeries("cn.treasury."+tenor.id+".yield", "treasury."+tenor.id, "yield_to_maturity", "percent", "daily", "valuation-date", "中债国债收益率曲线"+tenor.id+"到期收益率", "中债国债收益率曲线（qxmc=1）；无记录的日期不补"))
	}
	items := append([]*chinabond.ChinaBondItem{}, result.HeList...)
	for _, item := range items {
		if item == nil {
			return nil, fmt.Errorf("国债收益率记录不能为空")
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	seen := map[string]bool{}
	for _, item := range items {
		date, err := time.Parse("2006-01-02", item.Date)
		if err != nil || date.Format("2006-01-02") != item.Date {
			return nil, fmt.Errorf("无效国债收益率日期: %s", item.Date)
		}
		if seen[item.Date] {
			return nil, fmt.Errorf("国债收益率日期重复: %s", item.Date)
		}
		seen[item.Date] = true
		for _, tenor := range tenors {
			raw := tenor.value(item)
			o := Observation{SeriesID: "cn.treasury." + tenor.id + ".yield", Period: date.Format("20060102"), RawValue: text(raw), Source: text(source), DataVersion: "current_visible_revision"}
			if raw != "" {
				value, err := strconv.ParseFloat(raw, 64)
				if err != nil {
					return nil, fmt.Errorf("无效国债收益率数值: %s %s", item.Date, raw)
				}
				o.Value = &value
			}
			if err := d.append(o, "source_value_missing"); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
