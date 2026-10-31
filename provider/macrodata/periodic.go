package macrodata

import (
	"fmt"
	"strconv"
	"time"
)

type monthlyProvenance struct {
	source      string
	fetchedAt   time.Time
	publishedAt *time.Time
	revisionID  *string
}

type periodicValue struct {
	period string
	value  *float64
	raw    string
}

type periodicObservation struct {
	current, yearAgo, previous periodicValue
	yoy, change                *float64
	note                       string
	asOfDate                   *string
}

// periodicRule 说明变化值的单位和基期要求；零值表示百分比增速且只排除零基期。
type periodicRule struct {
	method       string
	points       bool
	positiveBase bool
}

// appendPeriodicChanges 适配严格月／季基期的水平及两种变化值，不重新计算provider数值。
func appendPeriodicChanges(d *Dataset, level Series, provenance monthlyProvenance, items []periodicObservation, rule periodicRule) error {
	prefix := level.ID
	level.ID += "." + level.Measure
	d.Series = append(d.Series, level)
	previousPrefix := "mom"
	if level.Frequency == "quarterly" {
		previousPrefix = "qoq"
	}
	measures := []string{"yoy_percent", previousPrefix + "_percent"}
	if rule.points {
		measures = []string{"yoy_percentage_points", previousPrefix + "_percentage_points"}
	}
	for _, measure := range measures {
		s := level
		s.ID, s.Measure, s.Unit, s.Annualized, s.ComponentsAdditive = prefix+"."+measure, measure, "percent", false, nil
		s.ValueOrigin, s.Formula, s.ChangeMethod = "derived", text("(current_level / base_level - 1) * 100"), rule.method
		if rule.points {
			s.Unit, s.Formula = "percentage_points", text("current_level - base_level")
		}
		d.Series = append(d.Series, s)
	}
	seen := map[string]bool{}
	for _, item := range items {
		if err := validatePeriod(item.current.period, level.Frequency); err != nil {
			return err
		}
		if seen[item.current.period] {
			return fmt.Errorf("指标所属期重复: %s", item.current.period)
		}
		seen[item.current.period] = true
		previous, yearAgo := periodicBasePeriods(item.current.period, level.Frequency)
		for i, field := range []struct {
			v        *float64
			base     *periodicValue
			expected string
		}{{item.current.value, nil, ""}, {item.yoy, &item.yearAgo, yearAgo}, {item.change, &item.previous, previous}} {
			o := Observation{SeriesID: d.Series[i].ID, Period: item.current.period, Value: field.v, AsOfDate: item.asOfDate, SourceSeriesID: level.Definition, Source: text(provenance.source), FetchedAt: timestamp(provenance.fetchedAt), PublishedAt: provenance.publishedAt, RevisionID: provenance.revisionID, DataVersion: "current_visible_revision", Note: item.note}
			reason := "source_value_missing"
			if i == 0 {
				o.RawValue = text(item.current.raw)
			} else {
				if field.base.period != field.expected {
					return fmt.Errorf("指标计算基期不匹配: %s，预期%s", field.base.period, field.expected)
				}
				if field.v != nil && (item.current.value == nil || field.base.value == nil || rule.rejectsBase(*field.base.value)) {
					return fmt.Errorf("指标计算值缺少有效当期或基期: %s，基期%s", item.current.period, field.base.period)
				}
				o.Base = &Base{Period: field.base.period, Value: field.base.value, RawValue: text(field.base.raw)}
				reason = "calculation_unavailable"
				if item.current.value == nil {
					reason = "current_value_missing"
				} else if field.base.value == nil {
					reason = "baseline_missing"
				} else if rule.positiveBase && *field.base.value <= 0 {
					reason = "nonpositive_baseline"
				} else if !rule.points && *field.base.value == 0 {
					reason = "zero_baseline"
				}
			}
			if err := d.appendDiagnostic(o, reason); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r periodicRule) rejectsBase(base float64) bool {
	if r.positiveBase {
		return base <= 0
	}
	return !r.points && base == 0
}

func periodicBasePeriods(period, frequency string) (string, string) {
	if frequency == "quarterly" {
		year, _ := strconv.Atoi(period[:4])
		q := year*4 + int(period[5]-'1')
		format := func(n int) string { return fmt.Sprintf("%04dQ%d", n/4, n%4+1) }
		return format(q - 1), format(q - 4)
	}
	month, _ := time.Parse("200601", period)
	return month.AddDate(0, -1, 0).Format("200601"), month.AddDate(-1, 0, 0).Format("200601")
}
