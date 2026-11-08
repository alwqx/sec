package aitool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
)

// get_macro_series 的输出预算：最近 12 期、最多 8 个序列，约 3 KB。
const (
	macroPeriods   = 12
	maxMacroSeries = 8
	// 口径说明和观测备注各自最多保留几条、每条多长。
	maxMacroNotes   = 5
	macroNoteLength = 100
	// 序列太多时，notes 里最多列出几组没有返回的序列。
	maxMacroGroups = 12
)

// MacroIndicator 是宏观目录里的一个指标。Period 是 begin、end 的格式：month、quarter 或 date。
// DefaultBegin 是事件型指标的默认开始日期 YYYYMMDD，命令的默认窗口经常没有观测时使用。
type MacroIndicator struct {
	Region   string // cn 或 us
	Command  string // sec macro <region> <command>
	Title    string
	Category string // 主题分类的中文名，如「价格」
	Period   string
	Options  []MacroOption
	// DefaultBegin 为空时用命令自己的默认窗口。
	DefaultBegin string
	// EarliestBegin 是命令接受的最早开始所属期，格式同 Period；默认开始期不早于它。为空时不限。
	EarliestBegin string
}

// MacroOption 是影响导出内容的命令参数；Values 为空时是开关，取值只能是 true。
type MacroOption struct {
	Flag   string
	Label  string
	Values []string
}

// MacroQuery 是一次统一导出查询，Begin、End 为空时用命令的默认值。
type MacroQuery struct {
	Region, Command string
	Begin, End      string
	Options         map[string]string
}

// MacroSource 提供宏观目录和统一导出（sec macro ... --standard-json）。
// 宏观命令在 cmd/macro 里，本包不能依赖 cmd/，由调用方注入；参数不合法时 Export 返回 InputError。
type MacroSource interface {
	Indicators() []MacroIndicator
	Export(ctx context.Context, query MacroQuery) ([]byte, error)
}

// MacroSeriesTool 返回 get_macro_series：一个宏观指标的口径和最近 12 期观测。
func MacroSeriesTool(source MacroSource) Tool {
	indicators := source.Indicators()
	return Tool{
		Name: "get_macro_series",
		Description: "查询一个中国或美国宏观指标最近 12 期的官方数据，以及每个序列的口径：单位、季调、价格基准、是官方值还是计算值（带公式）。" +
			"一个指标通常有几个序列，比如 CPI 有同比和环比；rows 从新到旧，每期的 values 和 series 一一对应。" +
			"可以用 begin、end 查更早的区间，仍然只返回区间内最近 12 期；序列很多的指标（如社会融资规模、美国国债发行）只返回前 8 个，用 series 筛选。",
		InputSchema: macroSchema(indicators),
		Call: func(ctx context.Context, input json.RawMessage) (*Result, error) {
			return callMacroSeries(ctx, source, indicators, input)
		},
	}
}

var macroPeriodLabels = map[string]string{"month": "月", "quarter": "季", "date": "日"}

// macroSchema 从目录生成参数 Schema：indicator 的枚举和按分类列出的说明。
func macroSchema(indicators []MacroIndicator) json.RawMessage {
	ids := make([]string, 0, len(indicators))
	var list, options strings.Builder
	category := ""
	for _, item := range indicators {
		id := item.Region + "." + item.Command
		ids = append(ids, id)
		if item.Category != category {
			if category != "" {
				list.WriteString("；")
			}
			category = item.Category
			list.WriteString(category + "：")
		} else {
			list.WriteString("、")
		}
		fmt.Fprintf(&list, "%s %s（%s）", id, item.Title, macroPeriodLabels[item.Period])
		for _, option := range item.Options {
			values := "true"
			if len(option.Values) > 0 {
				values = strings.Join(option.Values, "|")
			}
			if options.Len() > 0 {
				options.WriteString("；")
			}
			fmt.Fprintf(&options, "%s 的 %s（%s）=%s", id, option.Flag, option.Label, values)
		}
	}
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"indicator": map[string]any{
				"type": "string", "enum": ids,
				"description": "指标，cn 是中国、us 是美国，括号里是所属期的频率。" + list.String(),
			},
			"begin": map[string]any{
				"type":        "string",
				"description": "开始所属期，含当期：月度指标 YYYY-MM，季度指标 YYYYQn（如 2025Q1），日度和事件指标 YYYY-MM-DD。不传时取最近 12 期",
			},
			"end": map[string]any{
				"type":        "string",
				"description": "结束所属期，含当期，格式同 begin，默认最新一期",
			},
			"options": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": []string{"string", "boolean"}},
				"description":          "部分指标的分项选项，不传时用默认分项。" + options.String(),
			},
			"series": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "minLength": 1},
				"description": "只返回 id 包含其中任一字符串的序列，如 [\"total\"]、[\"yoy\"]；序列 id 见上次结果的 series 或 notes",
			},
		},
		"required":             []string{"indicator"},
		"additionalProperties": false,
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		// 只有字符串和切片，不会失败。
		panic(err)
	}
	return raw
}

type macroInput struct {
	Indicator string         `json:"indicator"`
	Begin     string         `json:"begin"`
	End       string         `json:"end"`
	Options   map[string]any `json:"options"` // 字符串，开关也可以用布尔值
	Series    []string       `json:"series"`
}

// macroBasis 是频率、所属期口径、季调和价格基准。所有序列相同时只写在 data 上，否则写在各序列上。
type macroBasis struct {
	Frequency          string `json:"frequency,omitempty"`
	PeriodBasis        string `json:"period_basis,omitempty"`
	SeasonalAdjustment string `json:"seasonal_adjustment,omitempty"`
	PriceBasis         string `json:"price_basis,omitempty"`
}

// macroSeries 是一个序列的口径。
type macroSeries struct {
	ID         string `json:"id"`
	Definition string `json:"definition"`
	Measure    string `json:"measure"`
	Unit       string `json:"unit"`
	macroBasis
	IndexBase   string  `json:"index_base,omitempty"`
	Annualized  bool    `json:"annualized,omitempty"`
	ValueOrigin string  `json:"value_origin"`      // official 是官方值，derived 是 sec 按 formula 计算
	Formula     *string `json:"formula,omitempty"` // 只有计算值
}

// macroRow 是一期观测，Values 和 series 一一对应，缺失是 null。
type macroRow struct {
	Period string `json:"period"`
	// Event 只有事件型指标（准备金率调整、国债拍卖等）：同一天可能有几次，用它区分。
	Event       string     `json:"event,omitempty"`
	Preliminary bool       `json:"preliminary,omitempty"`
	Values      []*float64 `json:"values"`
}

type macroData struct {
	Indicator string `json:"indicator"`
	Title     string `json:"title"`
	macroBasis
	Series []macroSeries `json:"series"`
	Rows   []macroRow    `json:"rows"` // 从新到旧
	// OmittedSeries 是超过 8 个、没有返回的序列数。
	OmittedSeries int `json:"omitted_series,omitempty"`
}

func callMacroSeries(ctx context.Context, source MacroSource, indicators []MacroIndicator, input json.RawMessage) (*Result, error) {
	var in macroInput
	if err := Decode(input, &in); err != nil {
		return nil, err
	}
	id := strings.ToLower(strings.TrimSpace(in.Indicator))
	if id == "" {
		return nil, Inputf("indicator 不能为空，如 cn.cpi、us.treasury")
	}
	index := slices.IndexFunc(indicators, func(item MacroIndicator) bool { return item.Region+"."+item.Command == id })
	if index < 0 {
		return nil, Inputf("没有指标 %s，请从 indicator 的枚举里选择，如 cn.cpi、us.payrolls", in.Indicator)
	}
	indicator := indicators[index]
	query := MacroQuery{Region: indicator.Region, Command: indicator.Command, Options: map[string]string{}}
	var err error
	if query.Begin, err = macroPeriod(indicator.Period, "begin", in.Begin); err != nil {
		return nil, err
	}
	if query.End, err = macroPeriod(indicator.Period, "end", in.End); err != nil {
		return nil, err
	}
	if query.Begin != "" && query.End != "" && query.Begin > query.End {
		return nil, Inputf("begin（%s）不能晚于 end（%s）", in.Begin, in.End)
	}
	if query.End != "" && query.End < indicator.EarliestBegin {
		return nil, Inputf("%s 的数据从 %s 开始，end 不能更早", id, macroPeriodText(indicator.EarliestBegin))
	}
	for flag, raw := range in.Options {
		var value string
		switch v := raw.(type) {
		case string:
			value = strings.ToLower(strings.TrimSpace(v))
		case bool:
			value = fmt.Sprint(v)
		default:
			return nil, Inputf("%s 的选项 %s 应为字符串或布尔值", id, flag)
		}
		i := slices.IndexFunc(indicator.Options, func(option MacroOption) bool { return option.Flag == flag })
		if i < 0 {
			return nil, Inputf("%s 没有选项 %s%s", id, flag, macroOptionHint(indicator))
		}
		allowed := indicator.Options[i].Values
		if len(allowed) == 0 {
			// 开关：false 等于不传。
			if value == "false" {
				continue
			}
			allowed = []string{"true"}
		}
		if !slices.Contains(allowed, value) {
			return nil, Inputf("%s 的选项 %s 只能是 %s", id, flag, strings.Join(allowed, "、"))
		}
		query.Options[flag] = value
	}
	filters := []string{}
	for _, filter := range in.Series {
		if filter = strings.ToLower(strings.TrimSpace(filter)); filter != "" {
			filters = append(filters, filter)
		}
	}
	if query.Begin == "" {
		query.Begin = defaultMacroBegin(indicator, query.End, utils.ShanghaiToday())
	}

	raw, err := source.Export(ctx, query)
	if err != nil {
		if IsInputError(err) {
			return nil, err
		}
		return nil, fmt.Errorf("%s 宏观数据查询失败：%w", id, err)
	}
	var dataset macrodata.Dataset
	if err := json.Unmarshal(raw, &dataset); err != nil {
		return nil, fmt.Errorf("%s 宏观数据格式无效：%w", id, err)
	}
	return macroResult(id, indicator, &dataset, filters)
}

// macroPeriod 把 YYYY-MM、YYYYQn、YYYY-MM-DD 换成命令的 YYYYMM、YYYYQn、YYYYMMDD，空字符串表示不限。
func macroPeriod(period, name, value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	switch period {
	case "quarter":
		if quarterPeriod.MatchString(value) {
			return value, nil
		}
		return "", Inputf("%s 的格式应为 YYYYQn，如 2025Q1", name)
	case "month":
		if t, err := time.Parse("2006-01", value); err == nil {
			return t.Format("200601"), nil
		}
		if t, err := time.Parse("200601", value); err == nil {
			return t.Format("200601"), nil
		}
		return "", Inputf("%s 的格式应为 YYYY-MM，如 2025-01", name)
	default:
		if t, err := time.Parse(time.DateOnly, value); err == nil {
			return t.Format("20060102"), nil
		}
		if t, err := time.Parse("20060102", value); err == nil {
			return t.Format("20060102"), nil
		}
		return "", Inputf("%s 的格式应为 YYYY-MM-DD，如 2025-01-02", name)
	}
}

var quarterPeriod = regexp.MustCompile(`^\d{4}Q[1-4]$`)

// defaultMacroBegin 是没传 begin 时的开始所属期：从 end（没有时从今天）往前多留几期，最新几期还没公布时仍有 12 期，
// 并且不早于指标的 EarliestBegin。日度指标用命令的默认窗口（约一个月），事件型指标用目录给的开始日期。
func defaultMacroBegin(indicator MacroIndicator, end string, today time.Time) string {
	begin := ""
	switch indicator.Period {
	case "month":
		ref := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
		if t, err := time.Parse("200601", end); err == nil {
			ref = t
		}
		begin = ref.AddDate(0, -(macroPeriods + 3), 0).Format("200601")
	case "quarter":
		year, quarter := today.Year(), (int(today.Month())-1)/3+1
		if quarterPeriod.MatchString(end) {
			year, _ = strconv.Atoi(end[:4])
			quarter = int(end[5] - '0')
		}
		begin = fmt.Sprintf("%dQ%d", year-4, quarter)
	default:
		// 给的 end 早于事件型指标的开始日期时，用命令自己的默认窗口。
		if end == "" || indicator.DefaultBegin <= end {
			begin = indicator.DefaultBegin
		}
	}
	if begin != "" && begin < indicator.EarliestBegin {
		begin = indicator.EarliestBegin
	}
	return begin
}

func macroOptionHint(indicator MacroIndicator) string {
	if len(indicator.Options) == 0 {
		return "，这个指标没有选项"
	}
	flags := []string{}
	for _, option := range indicator.Options {
		flags = append(flags, option.Flag)
	}
	return "，可选：" + strings.Join(flags, "、")
}

type macroKey struct{ period, event string }

func macroResult(id string, indicator MacroIndicator, dataset *macrodata.Dataset, filters []string) (*Result, error) {
	series := dataset.Series
	if len(filters) > 0 {
		series = slices.DeleteFunc(slices.Clone(series), func(s macrodata.Series) bool {
			return !slices.ContainsFunc(filters, func(filter string) bool { return strings.Contains(strings.ToLower(s.ID), filter) })
		})
		if len(series) == 0 {
			return nil, Inputf("%s 没有 id 包含 %s 的序列，可选：%s", id, strings.Join(filters, "、"), macroSeriesHint(dataset.Series))
		}
	}
	data := &macroData{Indicator: id, Title: indicator.Title, Series: []macroSeries{}, Rows: []macroRow{}}
	if len(series) > maxMacroSeries {
		data.OmittedSeries = len(series) - maxMacroSeries
	}
	all := series
	series = series[:min(len(series), maxMacroSeries)]
	column := map[string]int{}
	for i, s := range series {
		column[s.ID] = i
		item := macroSeries{
			ID: s.ID, Definition: s.Definition, Measure: s.Measure, Unit: s.Unit, IndexBase: s.IndexBase, Annualized: s.Annualized, ValueOrigin: s.ValueOrigin,
			macroBasis: macroBasis{Frequency: s.Frequency, PeriodBasis: s.PeriodBasis, SeasonalAdjustment: s.SeasonalAdjustment, PriceBasis: s.PriceBasis},
		}
		if s.ValueOrigin != "official" {
			item.Formula = s.Formula
		}
		data.Series = append(data.Series, item)
	}
	hoistBasis(data)

	// 按所属期和事件分组，事件型指标同一天可能有几行。
	rows := map[macroKey]*macroRow{}
	notes, sources := map[macroKey][]string{}, map[macroKey][]string{}
	keys := []macroKey{}
	for _, o := range dataset.Observations {
		i, ok := column[o.SeriesID]
		if !ok {
			continue
		}
		key := macroKey{o.Period, o.EventID}
		row := rows[key]
		if row == nil {
			row = &macroRow{Period: macroPeriodText(o.Period), Event: macroEventLabel(dataset, o.EventID), Values: make([]*float64, len(series))}
			rows[key] = row
			keys = append(keys, key)
		}
		row.Values[i] = rounded(o.Value, 4)
		if o.Preliminary != nil && *o.Preliminary {
			row.Preliminary = true
		}
		// 备注去掉链接后按文字去重。
		if note := stripURLs(o.Note); note != "" {
			notes[key] = append(notes[key], note)
		}
		if o.Source != nil {
			sources[key] = append(sources[key], *o.Source)
		}
	}
	// 所属期从新到旧；同一期的事件保持原顺序。
	slices.SortStableFunc(keys, func(a, b macroKey) int { return strings.Compare(b.period, a.period) })

	result := NewResult("get_macro_series", "", data)
	result.Notes = append(result.Notes, "rows 按所属期从新到旧，每期的 values 和 series 一一对应；null 表示这一期没有公布或无法计算，不是 0")
	// 最新几期所有序列都没有数据：多半是还没公布，不返回，as_of 是有数据的最新一期。
	unpublished := 0
	for unpublished < len(keys) && allNull(rows[keys[unpublished]].Values) {
		unpublished++
	}
	if unpublished > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("最近 %d 期（最新到 %s）所有序列都还没有数据，可能尚未公布，已省略", unpublished, rows[keys[0]].Period))
		keys = keys[unpublished:]
	}
	if len(keys) > macroPeriods {
		result.Notes = append(result.Notes, fmt.Sprintf("区间内共 %d 期，只列最近 %d 期；可以用 begin、end 查更早的区间", len(keys), macroPeriods))
		keys = keys[:macroPeriods]
	}
	// 观测备注按文字去重，记下涉及的所属期。
	observationNotes, notePeriods, sourceURLs := []string{}, map[string][]string{}, []string{}
	for _, key := range keys {
		row := *rows[key]
		data.Rows = append(data.Rows, row)
		sourceURLs = append(sourceURLs, sources[key]...)
		for _, note := range notes[key] {
			if _, ok := notePeriods[note]; !ok {
				observationNotes = append(observationNotes, note)
			}
			if !slices.Contains(notePeriods[note], row.Period) {
				notePeriods[note] = append(notePeriods[note], row.Period)
			}
		}
	}
	for i, note := range observationNotes {
		periods := notePeriods[note]
		label := periods[0]
		if len(periods) > 1 {
			label = fmt.Sprintf("%s 等 %d 期", periods[0], len(periods))
		}
		observationNotes[i] = label + "：" + note
	}
	// 来源只算返回的序列和所属期。
	result.Source = macroSource(sourceURLs)
	if len(data.Rows) > 0 {
		result.AsOf = data.Rows[0].Period
	} else {
		result.Notes = append(result.Notes, "区间内没有观测；可以放宽 begin、end")
	}

	if data.OmittedSeries > 0 {
		result.Notes = append(result.Notes, fmt.Sprintf("共 %d 个序列，只返回前 %d 个；用 series 筛选其他序列，如：%s", len(all), maxMacroSeries, macroSeriesHint(all[maxMacroSeries:])))
	}
	if slices.ContainsFunc(data.Series, func(s macroSeries) bool { return s.ValueOrigin != "official" }) {
		result.Notes = append(result.Notes, "value_origin 不是 official 的序列由 sec 按 formula 从官方数据计算，不是官方公布值")
	}
	if slices.ContainsFunc(data.Rows, func(row macroRow) bool { return row.Preliminary }) {
		result.Notes = append(result.Notes, "preliminary 为 true 的一期是初值，以后可能修订")
	}
	coverage := []string{}
	for _, s := range series {
		coverage = append(coverage, stripURLs(s.Coverage), stripURLs(s.Methodology), stripURLs(s.ChangeMethod))
	}
	result.Notes = append(result.Notes, compactNotes("口径：", coverage)...)
	result.Notes = append(result.Notes, compactNotes("说明：", observationNotes)...)
	return result, nil
}

// hoistBasis 把所有序列都相同的口径字段移到 data 上，减少重复。
func hoistBasis(data *macroData) {
	if len(data.Series) == 0 {
		return
	}
	fields := []func(b *macroBasis) *string{
		func(b *macroBasis) *string { return &b.Frequency },
		func(b *macroBasis) *string { return &b.PeriodBasis },
		func(b *macroBasis) *string { return &b.SeasonalAdjustment },
		func(b *macroBasis) *string { return &b.PriceBasis },
	}
	for _, field := range fields {
		value := *field(&data.Series[0].macroBasis)
		if slices.ContainsFunc(data.Series, func(s macroSeries) bool { return *field(&s.macroBasis) != value }) {
			continue
		}
		*field(&data.macroBasis) = value
		for i := range data.Series {
			*field(&data.Series[i].macroBasis) = ""
		}
	}
}

// macroSeriesHint 是 series 筛选的提示：序列不多时列出 id，否则列出 id 去掉末尾度量（如 .yoy_percent）后的分组。
func macroSeriesHint(series []macrodata.Series) string {
	names := []string{}
	for _, s := range series {
		name := s.ID
		if len(series) > maxMacroGroups {
			name = strings.TrimSuffix(s.ID, "."+s.Measure)
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	hint := strings.Join(names[:min(len(names), maxMacroGroups)], "、")
	if len(names) > maxMacroGroups {
		hint += fmt.Sprintf(" 等 %d 组", len(names))
	}
	return hint
}

func allNull(values []*float64) bool {
	return !slices.ContainsFunc(values, func(v *float64) bool { return v != nil })
}

// macroPeriodText 把 YYYYMM、YYYYMMDD 换成 YYYY-MM、YYYY-MM-DD，季度 YYYYQn 不变。
func macroPeriodText(period string) string {
	if t, err := time.Parse("200601", period); err == nil && len(period) == 6 {
		return t.Format("2006-01")
	}
	if t, err := time.Parse("20060102", period); err == nil && len(period) == 8 {
		return t.Format(time.DateOnly)
	}
	return period
}

// macroEventLabel 是事件型观测的简短说明：政策调整用适用范围（同一天可能有全面和定向两次），国债拍卖用期限、品种和 CUSIP。
func macroEventLabel(dataset *macrodata.Dataset, eventID string) string {
	if eventID == "" {
		return ""
	}
	for _, e := range dataset.PolicyEvents {
		if e.ID == eventID {
			label := e.Scope
			if e.Exclusions != "" {
				label += "（" + e.Exclusions + "）"
			}
			if e.TrancheCount > 1 {
				label += fmt.Sprintf("，第 %d/%d 批", e.TrancheIndex, e.TrancheCount)
			}
			return utils.TruncateRunes(label, 50)
		}
	}
	for _, e := range dataset.Events {
		if e.ID == eventID {
			return strings.TrimSpace(e.Term + " " + e.SecurityType + " " + e.CUSIP)
		}
	}
	return eventID
}

var urlPattern = regexp.MustCompile(`[；;，,]?\s*(上期报告[：:])?\s*https?://\S+`)

// stripURLs 去掉说明里的链接：同一句说明常常只有链接不同，去掉后才能去重。
func stripURLs(text string) string {
	return strings.TrimSpace(urlPattern.ReplaceAllString(text, ""))
}

// compactNotes 去重并截短，最多保留 maxMacroNotes 条。
func compactNotes(prefix string, texts []string) []string {
	seen := map[string]bool{}
	out := []string{}
	omitted := 0
	for _, text := range texts {
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		if len(out) == maxMacroNotes {
			omitted++
			continue
		}
		out = append(out, prefix+utils.TruncateRunes(strings.ReplaceAll(text, "\n", " "), macroNoteLength))
	}
	if omitted > 0 {
		out = append(out, fmt.Sprintf("%s另有 %d 条未列出", prefix, omitted))
	}
	return out
}

// macroHosts 把观测的来源链接换成机构名。
var macroHosts = map[string]string{
	"data.stats.gov.cn": "国家统计局", "www.stats.gov.cn": "国家统计局",
	"www.pbc.gov.cn": "中国人民银行", "www.chinamoney.com.cn": "中国货币网", "yield.chinabond.com.cn": "中债",
	"gks.mof.gov.cn": "财政部", "data.mofcom.gov.cn": "商务部", "fdi.mofcom.gov.cn": "商务部",
	"api.bls.gov": "美国劳工统计局（BLS）", "fred.stlouisfed.org": "圣路易斯联储 FRED",
	"www.federalreserve.gov": "美联储", "api.fiscaldata.treasury.gov": "美国财政部", "home.treasury.gov": "美国财政部", "markets.newyorkfed.org": "纽约联储",
}

// macroSource 把观测的来源链接汇总成机构名，按出现顺序。
func macroSource(sources []string) string {
	names := []string{}
	for _, source := range sources {
		u, err := url.Parse(source)
		if err != nil || u.Host == "" {
			continue
		}
		name := macroHosts[u.Host]
		if name == "" {
			name = u.Host
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return strings.Join(names, "、")
}
