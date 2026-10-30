package forex

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type QuotePolicy struct {
	Now             time.Time
	MaxSkew, MaxAge time.Duration
}
type Alignment struct {
	Status      string   `json:"status"`
	SkewSeconds *float64 `json:"skew_seconds,omitempty"`
	TimeStart   string   `json:"time_start,omitempty"`
	TimeEnd     string   `json:"time_end,omitempty"`
	Note        string   `json:"note,omitempty"`
}
type Derivation struct {
	InputPriceType string  `json:"input_price_type"`
	Pivot          string  `json:"pivot,omitempty"`
	AsOf           string  `json:"as_of,omitempty"`
	MaxSkewSeconds float64 `json:"max_skew_seconds"`
	MaxAgeSeconds  float64 `json:"max_age_seconds"`
}

func (p QuotePolicy) Validate() error {
	if p.Now.IsZero() || p.MaxSkew < 0 || p.MaxSkew > time.Minute || p.MaxAge <= 0 || p.MaxAge > 24*time.Hour {
		return fmt.Errorf("报价策略要求当前时间有效、最大时间差0至60秒、最大报价年龄大于0且不超过24小时")
	}
	return nil
}

func quoteAlignment(inputs []Observation, p QuotePolicy) (Alignment, bool) {
	var first, last time.Time
	for _, item := range inputs {
		stamp, err := time.Parse(time.RFC3339, item.ObservedAt)
		if err != nil || stamp.In(Beijing).Format("2006-01-02") != item.Date {
			return Alignment{Status: "missing-timestamp", Note: "输入缺少有效报价时间"}, false
		}
		if first.IsZero() || stamp.Before(first) {
			first = stamp
		}
		if last.IsZero() || stamp.After(last) {
			last = stamp
		}
	}
	seconds := last.Sub(first).Seconds()
	a := Alignment{SkewSeconds: &seconds, TimeStart: first.Format(time.RFC3339), TimeEnd: last.Format(time.RFC3339)}
	if last.After(p.Now.Add(time.Minute)) {
		a.Status, a.Note = "future-timestamp", "输入报价时间超过本机时间一分钟，未计算"
		return a, false
	}
	if p.Now.Sub(first) > p.MaxAge {
		a.Status, a.Note = "stale-input", "输入报价超过允许年龄，未计算"
		return a, false
	}
	if last.Sub(first) > p.MaxSkew {
		a.Status, a.Note = "timestamp-mismatch", "输入报价时点不一致且超过允许时间差，未计算"
		return a, false
	}
	a.Status = "synchronized"
	if !first.Equal(last) {
		a.Status, a.Note = "within-tolerance", "按显式允许的时间差近似计算，输入并非同一时刻报价"
	}
	return a, true
}

type crossSpec struct {
	pivot    string
	pairs    map[string]string
	inverse  bool
	snapshot bool
}

func sourceSpec(input *Result) (crossSpec, error) {
	if input == nil {
		return crossSpec{}, fmt.Errorf("汇率输入不能为空")
	}
	switch {
	case input.Provider == "Federal Reserve via FRED" && input.PriceType == "noon-buying-rate" && input.Frequency == "daily" && input.Timezone == "America/New_York" && input.ObservationTime == "12:00" && input.DateBasis == "observation-date":
		return crossSpec{pivot: "USD", pairs: map[string]string{"CNY": "USD/CNY", "HKD": "USD/HKD"}}, nil
	case input.Provider == "CFETS" && input.PriceType == "onshore-reference" && input.Frequency == "daily" && input.Timezone == "Asia/Shanghai" && input.DateBasis == "observation-date":
		switch input.ObservationTime {
		case "10:00", "11:00", "14:00", "15:00", "16:00":
			return crossSpec{pivot: "CNY", pairs: map[string]string{"USD": "USD/CNY", "HKD": "HKD/CNY"}, inverse: true}, nil
		}
	case input.Provider == "Sina" && input.PriceType == "indicative-quote" && input.Frequency == "snapshot" && input.Timezone == "Asia/Shanghai" && input.DateBasis == "provider-quote-time":
		return crossSpec{pivot: "USD", pairs: map[string]string{"CNY": "USD/CNY", "CNH": "USD/CNH", "HKD": "USD/HKD"}, snapshot: true}, nil
	}
	return crossSpec{}, fmt.Errorf("交叉汇率仅接受FRED纽约中午价、CFETS固定时点参考汇率或新浪带时间戳报价；不能从未知收盘时间的日线或中间价推算即期交叉价")
}

// CrossPairs 按已核验来源的可用币种提供有向货币对，不把CNY与CNH当成同一个市场。
func CrossPairs(source string) []string {
	currencies := []string{"USD", "CNY", "HKD"}
	if source == "sina" {
		currencies = append(currencies, "CNH")
	}
	out := []string{}
	for _, base := range currencies {
		for _, quote := range currencies {
			if base != quote {
				out = append(out, base+"/"+quote)
			}
		}
	}
	return out
}

func validateInput(item Observation) error {
	parts := strings.Split(item.Pair, "/")
	if len(parts) != 2 || item.BaseCurrency != parts[0] || item.QuoteCurrency != parts[1] || item.IsDerived {
		return fmt.Errorf("输入币种方向无效或已是推导值：%s", item.Pair)
	}
	if _, err := time.Parse("2006-01-02", item.Date); err != nil {
		return fmt.Errorf("输入汇率日期无效")
	}
	if item.Value != nil && (*item.Value <= 0 || math.IsNaN(*item.Value) || math.IsInf(*item.Value, 0)) {
		return fmt.Errorf("输入汇率必须为正有限数值")
	}
	return nil
}

func fixedAlignment(input *Result, items []Observation) (Alignment, bool) {
	for _, item := range items {
		if item.ObservedAt != "" {
			zone := Beijing
			if input.Timezone != "Asia/Shanghai" {
				var err error
				zone, err = time.LoadLocation(input.Timezone)
				if err != nil {
					return Alignment{Status: "unknown-timezone", Note: "无法解析输入时区"}, false
				}
			}
			stamp, err := time.Parse(time.RFC3339, item.ObservedAt)
			if err != nil || stamp.In(zone).Format("2006-01-02 15:04:05") != item.Date+" "+input.ObservationTime+":00" {
				return Alignment{Status: "timestamp-mismatch", Note: "输入时间与已声明的固定观测时点不符"}, false
			}
		}
	}
	zero := 0.0
	return Alignment{Status: "same-fixed-time", SkewSeconds: &zero}, true
}

// CrossRates 使用同一Result中的同口径输入计算，以币种名匹配而不依赖排列顺序。
func CrossRates(input *Result, targets []string, policy QuotePolicy) (*Result, error) {
	spec, err := sourceSpec(input)
	if err != nil {
		return nil, err
	}
	source := "daily"
	if spec.snapshot {
		source = "sina"
		if err := policy.Validate(); err != nil {
			return nil, err
		}
	}
	if err := ValidatePairs(targets, CrossPairs(source)); err != nil {
		return nil, err
	}
	result := &Result{Provider: input.Provider, PriceType: "derived-cross-rate", Frequency: input.Frequency, Timezone: input.Timezone, ObservationTime: input.ObservationTime, DateBasis: input.DateBasis, Begin: input.Begin, End: input.End, DataVersion: input.DataVersion, Observations: []Observation{}, Notes: []string{"交叉汇率为推导值，逐条保留公式和原始输入；不代表可实际成交或银行兑换的价格。", "只在同源同口径、所需输入齐全且时间校验通过时计算；缺失或不对齐时保留空值及原因。"}, Derivation: &Derivation{InputPriceType: input.PriceType, Pivot: spec.pivot}}
	result.Notes = append(result.Notes, input.Notes...)
	if spec.snapshot {
		result.Derivation.AsOf = policy.Now.Format(time.RFC3339)
		result.Derivation.MaxAgeSeconds = policy.MaxAge.Seconds()
		result.Derivation.MaxSkewSeconds = policy.MaxSkew.Seconds()
	}
	groups := map[string]map[string]Observation{}
	allowed := map[string]bool{}
	for _, pair := range spec.pairs {
		allowed[pair] = true
	}
	for _, item := range input.Observations {
		if err := validateInput(item); err != nil {
			return nil, err
		}
		if !allowed[item.Pair] {
			return nil, fmt.Errorf("交叉汇率输入包含非基准货币对：%s", item.Pair)
		}
		key := item.Date
		if spec.snapshot {
			key = "snapshot"
		}
		if groups[key] == nil {
			groups[key] = map[string]Observation{}
		}
		if _, exists := groups[key][item.Pair]; exists {
			return nil, fmt.Errorf("输入存在重复日期或重复报价：%s", item.Pair)
		}
		groups[key][item.Pair] = item
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, target := range targets {
			parts := strings.Split(target, "/")
			base, quote := parts[0], parts[1]
			inputs := []Observation{}
			missing := false
			date := key
			if spec.snapshot {
				date = ""
			}
			term := func(currency string) string {
				if currency == spec.pivot {
					return "1"
				}
				if spec.inverse {
					return "(1 / " + spec.pairs[currency] + ")"
				}
				return spec.pairs[currency]
			}
			for _, currency := range []string{base, quote} {
				if currency == spec.pivot {
					continue
				}
				item, exists := groups[key][spec.pairs[currency]]
				if !exists {
					missing = true
					continue
				}
				inputs = append(inputs, item)
				if spec.snapshot && (date == "snapshot" || date < item.Date) {
					date = item.Date
				}
			}
			if spec.snapshot && date == "" {
				for _, candidate := range groups[key] {
					if candidate.Date > date {
						date = candidate.Date
					}
				}
			}
			item := NewObservation(target, date, "", time.Time{})
			item.IsDerived = true
			item.Formula = term(quote) + " / " + term(base)
			item.Inputs = inputs
			for _, leg := range inputs {
				if item.Source == "" {
					item.Source = leg.Source
				}
				if leg.FetchedAt.After(item.FetchedAt) {
					item.FetchedAt = leg.FetchedAt
				}
			}
			a := Alignment{Status: "missing-input", Note: "缺少所需货币对的同日输入"}
			valid := !missing
			if valid {
				for _, leg := range inputs {
					if leg.Value == nil {
						a = Alignment{Status: "missing-value", Note: "所需输入汇率为空"}
						valid = false
						break
					}
				}
			}
			if valid {
				if spec.snapshot {
					a, valid = quoteAlignment(inputs, policy)
				} else {
					a, valid = fixedAlignment(input, inputs)
				}
			}
			item.Alignment = &a
			item.Note = a.Note
			if valid {
				units := func(currency string) float64 {
					if currency == spec.pivot {
						return 1
					}
					v := *groups[key][spec.pairs[currency]].Value
					if spec.inverse {
						return 1 / v
					}
					return v
				}
				value := units(quote) / units(base)
				if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
					return nil, fmt.Errorf("交叉汇率计算结果溢出或无效")
				}
				item.Value = &value
				if spec.snapshot && a.Status == "synchronized" {
					item.ObservedAt = a.TimeEnd
				}
			}
			result.Observations = append(result.Observations, item)
		}
	}
	return result, nil
}

type SpreadResult struct {
	Provider        string        `json:"provider"`
	AsOf            string        `json:"as_of"`
	MaxSkewSeconds  float64       `json:"max_skew_seconds"`
	MaxAgeSeconds   float64       `json:"max_age_seconds"`
	IsDerived       bool          `json:"is_derived"`
	Formula         string        `json:"formula"`
	Inputs          []Observation `json:"inputs"`
	Alignment       Alignment     `json:"alignment"`
	Difference      *float64      `json:"difference_rmb_per_usd"`
	Pips            *float64      `json:"pips"`
	RelativePercent *float64      `json:"relative_percent"`
	RelativeBPS     *float64      `json:"relative_bps"`
	ImpliedCNHCNY   *float64      `json:"implied_cnh_cny"`
	Notes           []string      `json:"notes"`
}

// QuoteSpread 的正值表示同一美元需要更多离岸人民币，即CNH相对CNY更弱。
func QuoteSpread(input *Result, policy QuotePolicy) (*SpreadResult, error) {
	spec, err := sourceSpec(input)
	if err != nil {
		return nil, err
	}
	if !spec.snapshot {
		return nil, fmt.Errorf("CNY/CNH价差当前仅接受新浪带原始时间戳的报价")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	byPair := map[string]Observation{}
	for _, item := range input.Observations {
		if err := validateInput(item); err != nil {
			return nil, err
		}
		if item.Pair != "USD/CNY" && item.Pair != "USD/CNH" {
			return nil, fmt.Errorf("价差输入只允许USD/CNY和USD/CNH")
		}
		if _, ok := byPair[item.Pair]; ok {
			return nil, fmt.Errorf("价差输入存在重复货币对")
		}
		byPair[item.Pair] = item
	}
	result := &SpreadResult{Provider: input.Provider, AsOf: policy.Now.Format(time.RFC3339), MaxSkewSeconds: policy.MaxSkew.Seconds(), MaxAgeSeconds: policy.MaxAge.Seconds(), IsDerived: true, Formula: "USD/CNH - USD/CNY", Inputs: []Observation{}, Alignment: Alignment{Status: "missing-input", Note: "缺少在岸或离岸报价"}, Notes: []string{"正值表示CNH相对CNY更弱，负值表示CNH相对更强；价差单位为每美元对应的人民币差额，1点(pip)=0.0001。", "相对百分比和基点以USD/CNY为基数；隐含CNH/CNY=USD/CNY÷USD/CNH，单位为每1离岸人民币对应的在岸人民币，仅为推导值。"}}
	for _, pair := range []string{"USD/CNY", "USD/CNH"} {
		if item, exists := byPair[pair]; exists {
			result.Inputs = append(result.Inputs, item)
		}
	}
	if len(result.Inputs) != 2 {
		return result, nil
	}
	for _, item := range result.Inputs {
		if item.Value == nil {
			result.Alignment = Alignment{Status: "missing-value", Note: "输入报价为空"}
			return result, nil
		}
	}
	a, valid := quoteAlignment(result.Inputs, policy)
	result.Alignment = a
	if !valid {
		return result, nil
	}
	cny, cnh := *byPair["USD/CNY"].Value, *byPair["USD/CNH"].Value
	diff := cnh - cny
	pips := diff * 10000
	percent := diff / cny * 100
	bps := diff / cny * 10000
	ratio := cny / cnh
	if ratio <= 0 {
		return nil, fmt.Errorf("隐含CNH/CNY计算结果下溢")
	}
	for _, v := range []float64{diff, pips, percent, bps, ratio} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("价差计算结果溢出")
		}
	}
	result.Difference, result.Pips, result.RelativePercent, result.RelativeBPS, result.ImpliedCNHCNY = &diff, &pips, &percent, &bps, &ratio
	return result, nil
}
