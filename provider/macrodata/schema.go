package macrodata

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// Dataset 使用长表观测，每个数值通过SeriesID关联明确的统计口径。
// Original保留适配前的provider结果，不等同于上游HTTP原始响应。
type Dataset struct {
	frequency     string
	SchemaVersion string          `json:"schema_version"`
	Region        string          `json:"region"`
	Begin         string          `json:"begin"`
	End           string          `json:"end"`
	Series        []Series        `json:"series"`
	Events        []Event         `json:"events,omitempty"`
	PolicyEvents  []PolicyEvent   `json:"policy_events,omitempty"`
	Observations  []Observation   `json:"observations"`
	Original      json.RawMessage `json:"original"`
}

// Event 保存同一日期可能有多个的事件元数据，观测通过EventID关联。
type Event struct {
	ID                            string  `json:"id"`
	CUSIP                         string  `json:"cusip"`
	SecurityType                  string  `json:"security_type"`
	Term                          string  `json:"security_term"`
	AnnouncementDate              *string `json:"announcement_date"`
	AuctionDate                   string  `json:"auction_date"`
	IssueDate                     string  `json:"issue_date"`
	MaturityDate                  string  `json:"maturity_date"`
	OriginalIssueDate             *string `json:"original_issue_date"`
	Reopening                     *bool   `json:"reopening"`
	FloatingRate                  *bool   `json:"floating_rate"`
	InflationIndexed              *bool   `json:"inflation_indexed"`
	SOMAInsideOffering            *bool   `json:"soma_inside_offering_amount"`
	FIMACompetitiveInsideOffering *bool   `json:"fima_competitive_inside_offering_amount"`
}

// PolicyEvent 保存同一生效日可能有多个的政策调整范围，观测通过EventID关联。
type PolicyEvent struct {
	ID                 string `json:"id"`
	EffectiveDate      string `json:"effective_date"`
	EffectiveDateBasis string `json:"effective_date_basis,omitempty"`
	ScopeType          string `json:"scope_type"`
	Scope              string `json:"scope"`
	Exclusions         string `json:"exclusions,omitempty"`
	Additional         bool   `json:"additional"`
	TrancheIndex       int    `json:"tranche_index"`
	TrancheCount       int    `json:"tranche_count"`
	Title              string `json:"title,omitempty"`
}

type Series struct {
	Methodology        string  `json:"methodology,omitempty"`
	ChangeMethod       string  `json:"change_method,omitempty"`
	TaxBasis           string  `json:"tax_basis,omitempty"`
	Survey             string  `json:"survey,omitempty"`
	Numerator          string  `json:"numerator,omitempty"`
	Denominator        string  `json:"denominator,omitempty"`
	Coverage           string  `json:"coverage,omitempty"`
	ComponentsAdditive *bool   `json:"components_additive,omitempty"`
	IndexBase          string  `json:"index_base,omitempty"`
	ReferenceYear      *int    `json:"reference_year,omitempty"`
	ID                 string  `json:"id"`
	Indicator          string  `json:"indicator"`
	Measure            string  `json:"measure"`
	Unit               string  `json:"unit"`
	Frequency          string  `json:"frequency"`
	PeriodBasis        string  `json:"period_basis"`
	SeasonalAdjustment string  `json:"seasonal_adjustment"`
	PriceBasis         string  `json:"price_basis"`
	Annualized         bool    `json:"annualized"`
	ValueOrigin        string  `json:"value_origin"`
	Definition         string  `json:"definition"`
	Formula            *string `json:"formula"`
}

type Base struct {
	Period   string   `json:"period"`
	Value    *float64 `json:"value"`
	RawValue *string  `json:"raw_value"`
}

type Observation struct {
	EventID           string     `json:"event_id,omitempty"`
	AsOfDate          *string    `json:"as_of_date,omitempty"`
	FiscalYear        int        `json:"fiscal_year,omitempty"`
	Preliminary       *bool      `json:"preliminary,omitempty"`
	Inputs            []Base     `json:"inputs,omitempty"`
	RevisionIndicator string     `json:"revision_indicator,omitempty"`
	SeriesID          string     `json:"series_id"`
	Period            string     `json:"period"`
	PeriodBegin       string     `json:"period_begin,omitempty"`
	PeriodEnd         string     `json:"period_end,omitempty"`
	SourceSeriesID    string     `json:"source_series_id,omitempty"`
	Value             *float64   `json:"value"`
	Status            string     `json:"status"`
	MissingReason     *string    `json:"missing_reason"`
	RawValue          *string    `json:"raw_value"`
	Source            *string    `json:"source"`
	FetchedAt         *time.Time `json:"fetched_at"`
	PublishedDate     *string    `json:"published_date"`
	PublishedAt       *time.Time `json:"published_at"`
	RevisionID        *string    `json:"revision_id"`
	DataVersion       string     `json:"data_version"`
	Base              *Base      `json:"base"`
	Note              string     `json:"note"`
}

func text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func timestamp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func newDataset(region, begin, end string, original any) (*Dataset, error) {
	return newDatasetFrequency(region, begin, end, "monthly", original)
}

func validatePeriod(period, frequency string) error {
	if frequency == "quarterly" {
		if len(period) == 6 && period[4] == 'Q' && period[5] >= '1' && period[5] <= '4' {
			year, err := strconv.Atoi(period[:4])
			if err == nil && year > 0 && fmt.Sprintf("%04d", year) == period[:4] {
				return nil
			}
		}
	} else {
		layout := "200601"
		if frequency == "daily" || frequency == "weekly" || frequency == "event" {
			layout = "20060102"
		} else if frequency != "monthly" {
			return fmt.Errorf("不支持的频率: %s", frequency)
		}
		date, err := time.Parse(layout, period)
		if err == nil && date.Format(layout) == period && date.Year() > 0 {
			return nil
		}
	}
	return fmt.Errorf("无效所属期 %q（%s）", period, frequency)
}

func newDatasetFrequency(region, begin, end, frequency string, original any) (*Dataset, error) {
	for _, period := range []string{begin, end} {
		if err := validatePeriod(period, frequency); err != nil {
			return nil, err
		}
	}
	if begin > end {
		return nil, fmt.Errorf("开始所属期不能晚于截至所属期")
	}
	raw, err := json.Marshal(original)
	if err != nil {
		return nil, fmt.Errorf("保存原始provider结果失败: %w", err)
	}
	return &Dataset{frequency: frequency, SchemaVersion: "1", Region: region, Begin: begin, End: end, Series: []Series{}, Observations: []Observation{}, Original: raw}, nil
}

// appendDiagnostic 用于备注只说明逐行缺失原因的provider：有值的观测不带备注，
// 避免可用观测带上其他序列的基期说明。
func (d *Dataset) appendDiagnostic(o Observation, reason string) error {
	if o.Value != nil {
		o.Note = ""
	}
	return d.append(o, reason)
}

func (d *Dataset) append(o Observation, reason string) error {
	if err := validatePeriod(o.Period, d.frequency); err != nil {
		return err
	}
	if o.Period < d.Begin || o.Period > d.End {
		return fmt.Errorf("观测所属期超出查询范围: %s", o.Period)
	}
	if o.Value != nil && (math.IsNaN(*o.Value) || math.IsInf(*o.Value, 0)) {
		return fmt.Errorf("观测值不是有限数值")
	}
	o.Status = "available"
	if o.Value == nil {
		o.Status, o.MissingReason = "missing", text(reason)
	}
	d.Observations = append(d.Observations, o)
	return nil
}
