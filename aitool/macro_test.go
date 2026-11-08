package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/stretchr/testify/require"
)

// fakeMacro 记录查询，按 export 返回结果。
type fakeMacro struct {
	queries []MacroQuery
	export  func(query MacroQuery) (*macrodata.Dataset, error)
}

func (f *fakeMacro) Indicators() []MacroIndicator {
	return []MacroIndicator{
		{Region: "cn", Command: "cpi", Title: "CPI", Category: "价格", Period: "month"},
		{Region: "cn", Command: "investment", Title: "固定资产投资", Category: "投资与房地产", Period: "month",
			Options: []MacroOption{{Flag: "sector", Label: "分项", Values: []string{"total", "manufacturing"}}}},
		{Region: "cn", Command: "unemployment", Title: "城镇调查失业率", Category: "就业", Period: "month", Options: []MacroOption{{Flag: "age-groups", Label: "年龄分组"}}},
		{Region: "cn", Command: "rrr", Title: "存款准备金率调整", Category: "利率与收益率", Period: "date", DefaultBegin: "20180101", EarliestBegin: "20180101"},
		{Region: "us", Command: "gdp", Title: "实际GDP", Category: "增长与产出", Period: "quarter"},
	}
}

func (f *fakeMacro) Export(ctx context.Context, query MacroQuery) ([]byte, error) {
	f.queries = append(f.queries, query)
	dataset, err := f.export(query)
	if err != nil {
		return nil, err
	}
	return json.Marshal(dataset)
}

func callMacroJSON(t *testing.T, source *fakeMacro, input string) (*Result, *macroData, error) {
	t.Helper()
	result, err := MacroSeriesTool(source).Call(context.Background(), json.RawMessage(input))
	if err != nil {
		return nil, nil, err
	}
	data, ok := result.Data.(*macroData)
	require.True(t, ok)
	return result, data, nil
}

func cpiSeries(id, measure, origin string) macrodata.Series {
	s := macrodata.Series{
		ID: id, Indicator: "cpi", Measure: measure, Unit: "index", Frequency: "monthly", PeriodBasis: "month",
		SeasonalAdjustment: "not-seasonally-adjusted", PriceBasis: "price-index", ValueOrigin: origin, Definition: "居民消费价格指数",
		Coverage: "全国月度相对指数，未季调",
	}
	if origin == "derived" {
		s.Unit, s.Formula = "percent", ptrText("official_index - 100")
	}
	return s
}

func ptrText(s string) *string { return &s }

// cpiDataset 是 2025-01 到 2026-04 共 16 个月的 CPI：同比指数和计算的同比，最后一个月还没公布。
func cpiDataset() *macrodata.Dataset {
	d := &macrodata.Dataset{Region: "CN", Series: []macrodata.Series{
		cpiSeries("cn.cpi.yoy_index", "yoy_index", "official"),
		cpiSeries("cn.cpi.yoy_percent", "yoy_percent", "derived"),
	}}
	source := "https://data.stats.gov.cn/esData"
	month := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 16 {
		period := month.AddDate(0, i, 0).Format("200601")
		index, change := 100+float64(i)/10, float64(i)/10
		var indexValue, changeValue *float64
		if i < 15 {
			indexValue, changeValue = &index, ptrTo(change+1e-9)
		}
		note := ""
		if i >= 12 && i < 15 {
			note = "数据为初步核算；上期报告：https://www.stats.gov.cn/a.html"
		}
		d.Observations = append(d.Observations,
			macrodata.Observation{SeriesID: "cn.cpi.yoy_index", Period: period, Value: indexValue, Source: &source, Note: note},
			macrodata.Observation{SeriesID: "cn.cpi.yoy_percent", Period: period, Value: changeValue, Source: &source},
		)
	}
	return d
}

func TestMacroSeries(t *testing.T) {
	source := &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) { return cpiDataset(), nil }}
	result, data, err := callMacroJSON(t, source, `{"indicator":" CN.CPI "}`)
	require.NoError(t, err)
	require.Len(t, source.queries, 1)
	query := source.queries[0]
	require.Equal(t, "cn", query.Region)
	require.Equal(t, "cpi", query.Command)
	require.Equal(t, defaultMacroBegin(source.Indicators()[0], "", utils.ShanghaiToday()), query.Begin, "多取几期，最新几期没公布时仍有 12 期")
	require.Empty(t, query.End)

	require.Equal(t, "get_macro_series", result.Tool)
	require.Equal(t, "国家统计局", result.Source)
	require.Equal(t, "2026-03", result.AsOf, "最后一个月还没公布，不算")
	require.Equal(t, "cn.cpi", data.Indicator)
	require.Equal(t, "CPI", data.Title)
	require.Equal(t, "monthly", data.Frequency, "所有序列相同的口径写在 data 上")
	require.Equal(t, "price-index", data.PriceBasis)
	require.Empty(t, data.Series[0].Frequency)
	require.Nil(t, data.Series[0].Formula, "官方值没有公式")
	require.Equal(t, "official_index - 100", *data.Series[1].Formula)

	require.Len(t, data.Rows, 12)
	require.Equal(t, macroRow{Period: "2026-03", Values: []*float64{ptrTo(101.4), ptrTo(1.4)}}, data.Rows[0], "从新到旧，四舍五入")
	require.Equal(t, "2025-04", data.Rows[11].Period)

	notes := strings.Join(result.Notes, "\n")
	for _, want := range []string{"最近 1 期（最新到 2026-04）", "区间内共 15 期，只列最近 12 期", "不是官方公布值", "口径：全国月度相对指数，未季调", "说明：2026-03 等 3 期：数据为初步核算"} {
		require.Contains(t, notes, want)
	}
	require.NotContains(t, notes, "https://", "去掉备注里的链接")
	require.Equal(t, 1, strings.Count(notes, "口径："), "相同的口径只写一次")

	text, err := json.Marshal(result)
	require.NoError(t, err)
	require.Less(t, len(text), 4096)
}

func TestMacroSeriesQuery(t *testing.T) {
	source := &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) { return cpiDataset(), nil }}
	_, data, err := callMacroJSON(t, source, `{"indicator":"cn.investment","begin":"2025-01","end":"2026-04","options":{"sector":" Manufacturing "}}`)
	require.NoError(t, err)
	require.Equal(t, MacroQuery{Region: "cn", Command: "investment", Begin: "202501", End: "202604", Options: map[string]string{"sector": "manufacturing"}}, source.queries[0])
	require.Equal(t, "2026-03", data.Rows[0].Period, "给了 end 也省略最新的全空期，as_of 有数据")

	// 开关也可以用布尔值，false 等于不传。
	_, _, err = callMacroJSON(t, source, `{"indicator":"cn.unemployment","options":{"age-groups":true}}`)
	require.NoError(t, err)
	require.Equal(t, "true", source.queries[1].Options["age-groups"])
	_, _, err = callMacroJSON(t, source, `{"indicator":"cn.unemployment","options":{"age-groups":false}}`)
	require.NoError(t, err)
	require.Empty(t, source.queries[2].Options)
	source.queries = slices.Delete(source.queries, 2, 3)

	_, _, err = callMacroJSON(t, source, `{"indicator":"us.gdp","begin":"2024q1","end":"2025Q4"}`)
	require.NoError(t, err)
	require.Equal(t, "2024Q1", source.queries[2].Begin)

	_, _, err = callMacroJSON(t, source, `{"indicator":"cn.rrr","begin":"2025-05-15"}`)
	require.NoError(t, err)
	require.Equal(t, "20250515", source.queries[3].Begin)
	_, _, err = callMacroJSON(t, source, `{"indicator":"cn.rrr"}`)
	require.NoError(t, err)
	require.Equal(t, "20180101", source.queries[4].Begin, "事件型指标用目录给的开始日期")

	// 只筛出计算值。
	_, data, err = callMacroJSON(t, source, `{"indicator":"cn.cpi","series":["PERCENT",""]}`)
	require.NoError(t, err)
	require.Len(t, data.Series, 1)
	require.Equal(t, "cn.cpi.yoy_percent", data.Series[0].ID)
	require.Len(t, data.Rows[0].Values, 1)

	// 只传 end：默认开始期从 end 往前推，不从今天往前推。
	source.queries = nil
	_, _, err = callMacroJSON(t, source, `{"indicator":"cn.cpi","end":"2020-01"}`)
	require.NoError(t, err)
	require.Equal(t, "201810", source.queries[0].Begin)
	require.Equal(t, "202001", source.queries[0].End)
}

func TestMacroSeriesSource(t *testing.T) {
	// 被筛掉的序列来自另一个机构，不算进来源。
	source := &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) {
		d := cpiDataset()
		d.Series = append(d.Series, cpiSeries("cn.cpi.other", "other", "official"))
		d.Observations = append(d.Observations, macrodata.Observation{SeriesID: "cn.cpi.other", Period: "202603", Value: ptrTo(1), Source: ptrText("https://www.pbc.gov.cn/x")})
		return d, nil
	}}
	result, _, err := callMacroJSON(t, source, `{"indicator":"cn.cpi"}`)
	require.NoError(t, err)
	require.Equal(t, "国家统计局、中国人民银行", result.Source)
	result, _, err = callMacroJSON(t, source, `{"indicator":"cn.cpi","series":["yoy"]}`)
	require.NoError(t, err)
	require.Equal(t, "国家统计局", result.Source)
}

func TestDefaultMacroBegin(t *testing.T) {
	today := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	month, quarter := MacroIndicator{Period: "month"}, MacroIndicator{Period: "quarter"}
	require.Equal(t, "202508", defaultMacroBegin(month, "", today))
	require.Equal(t, "202502", defaultMacroBegin(month, "", time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)), "月底不会多进一个月")
	require.Equal(t, "201810", defaultMacroBegin(month, "202001", today), "从 end 往前推")
	require.Equal(t, "2022Q4", defaultMacroBegin(quarter, "", today))
	require.Equal(t, "2016Q2", defaultMacroBegin(quarter, "2020Q2", today))
	require.Empty(t, defaultMacroBegin(MacroIndicator{Period: "date"}, "", today), "日度指标用命令的默认窗口")

	event := MacroIndicator{Period: "date", DefaultBegin: "20081216"}
	require.Equal(t, "20081216", defaultMacroBegin(event, "", today))
	require.Empty(t, defaultMacroBegin(event, "20080101", today), "end 早于开始日期时用命令的默认窗口")

	// 不早于覆盖起点。
	limited := MacroIndicator{Period: "month", EarliestBegin: "202510"}
	require.Equal(t, "202510", defaultMacroBegin(limited, "", today))
	require.Equal(t, "202510", defaultMacroBegin(limited, "202604", today))
}

func TestMacroSeriesEvents(t *testing.T) {
	source := &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) {
		d := &macrodata.Dataset{
			Series: []macrodata.Series{{ID: "cn.rrr.change_lower", Measure: "change_lower", Frequency: "event", ValueOrigin: "official"}},
			PolicyEvents: []macrodata.PolicyEvent{
				{ID: "a", Scope: "金融机构", Exclusions: "不含已执行5%存款准备金率的金融机构"},
				{ID: "b", Scope: "汽车金融公司和金融租赁公司"},
			},
		}
		for _, o := range []struct {
			period, event string
			value         float64
		}{{"20240927", "a", -0.5}, {"20250515", "a", -0.5}, {"20250515", "b", -5}} {
			d.Observations = append(d.Observations, macrodata.Observation{SeriesID: "cn.rrr.change_lower", Period: o.period, EventID: o.event, Value: ptrTo(o.value)})
		}
		return d, nil
	}}
	_, data, err := callMacroJSON(t, source, `{"indicator":"cn.rrr"}`)
	require.NoError(t, err)
	require.Len(t, data.Rows, 3, "同一天的两次调整分成两行")
	require.Equal(t, "2025-05-15", data.Rows[0].Period)
	require.Equal(t, "金融机构（不含已执行5%存款准备金率的金融机构）", data.Rows[0].Event)
	require.Equal(t, "汽车金融公司和金融租赁公司", data.Rows[1].Event)
	require.Equal(t, -5.0, *data.Rows[1].Values[0])
	require.Equal(t, "2024-09-27", data.Rows[2].Period)
}

func TestMacroSeriesManySeries(t *testing.T) {
	source := &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) {
		d := &macrodata.Dataset{}
		for i := range 20 {
			for _, measure := range []string{"level", "yoy_percent"} {
				id := fmt.Sprintf("cn.financing.item%02d.%s", i, measure)
				d.Series = append(d.Series, macrodata.Series{ID: id, Measure: measure, Frequency: "monthly", ValueOrigin: "official"})
				d.Observations = append(d.Observations, macrodata.Observation{SeriesID: id, Period: "202609", Value: ptrTo(1)})
			}
		}
		return d, nil
	}}
	result, data, err := callMacroJSON(t, source, `{"indicator":"cn.cpi"}`)
	require.NoError(t, err)
	require.Len(t, data.Series, maxMacroSeries)
	require.Equal(t, 32, data.OmittedSeries)
	require.Len(t, data.Rows[0].Values, maxMacroSeries)
	notes := strings.Join(result.Notes, "\n")
	require.Contains(t, notes, "共 40 个序列，只返回前 8 个")
	require.Contains(t, notes, "cn.financing.item04、cn.financing.item05", "序列很多时按分组提示")
	require.Contains(t, notes, "等 16 组")

	_, data, err = callMacroJSON(t, source, `{"indicator":"cn.cpi","series":["item19"]}`)
	require.NoError(t, err)
	require.Zero(t, data.OmittedSeries)
	require.Equal(t, "cn.financing.item19.level", data.Series[0].ID)

	_, _, err = callMacroJSON(t, source, `{"indicator":"cn.cpi","series":["nosuch"]}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "没有 id 包含 nosuch 的序列")
}

func TestMacroSeriesErrors(t *testing.T) {
	source := &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) { return cpiDataset(), nil }}
	for input, want := range map[string]string{
		`{}`:                                     "indicator 不能为空",
		`{"indicator":"cn.nosuch"}`:              "没有指标 cn.nosuch",
		`{"indicator":"cn.cpi","begin":"2026"}`:  "YYYY-MM",
		`{"indicator":"us.gdp","end":"2026-03"}`: "YYYYQn",
		`{"indicator":"cn.rrr","begin":"x"}`:     "YYYY-MM-DD",
		`{"indicator":"cn.cpi","begin":"2026-05","end":"2026-01"}`:       "不能晚于",
		`{"indicator":"cn.cpi","options":{"sector":"total"}}`:            "这个指标没有选项",
		`{"indicator":"cn.investment","options":{"x":"total"}}`:          "可选：sector",
		`{"indicator":"cn.investment","options":{"sector":"real"}}`:      "只能是 total、manufacturing",
		`{"indicator":"cn.unemployment","options":{"age-groups":"yes"}}`: "只能是 true",
		`{"indicator":"cn.cpi","unknown":1}`:                             "参数格式不正确",
		`{"indicator":"cn.unemployment","options":{"age-groups":1}}`:     "应为字符串或布尔值",
		`{"indicator":"cn.rrr","end":"2017-12-31"}`:                      "cn.rrr 的数据从 2018-01-01 开始",
	} {
		_, _, err := callMacroJSON(t, source, input)
		require.True(t, IsInputError(err), input)
		require.ErrorContains(t, err, want, input)
	}
	require.Empty(t, source.queries, "参数错误不查询")

	// 上游失败：只查一次，原样说明。
	source = &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) { return nil, errors.New("HTTP 503") }}
	_, _, err := callMacroJSON(t, source, `{"indicator":"cn.cpi"}`)
	require.False(t, IsInputError(err))
	require.EqualError(t, err, "cn.cpi 宏观数据查询失败：HTTP 503")
	require.Len(t, source.queries, 1, "不重试")

	source = &fakeMacro{export: func(MacroQuery) (*macrodata.Dataset, error) { return nil, Inputf("无效的宏观指标查询") }}
	_, _, err = callMacroJSON(t, source, `{"indicator":"cn.cpi"}`)
	require.True(t, IsInputError(err), "数据源返回的参数错误不包装")
	require.True(t, IsInputError(err))
}

func TestMacroSchema(t *testing.T) {
	tool := MacroSeriesTool(&fakeMacro{})
	var schema struct {
		Properties map[string]struct {
			Enum        []string `json:"enum"`
			Description string   `json:"description"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(tool.InputSchema, &schema))
	indicator := schema.Properties["indicator"]
	require.Equal(t, []string{"cn.cpi", "cn.investment", "cn.unemployment", "cn.rrr", "us.gdp"}, indicator.Enum)
	require.Contains(t, indicator.Description, "价格：cn.cpi CPI（月）；投资与房地产：cn.investment 固定资产投资（月）")
	require.Contains(t, indicator.Description, "us.gdp 实际GDP（季）")
	options := schema.Properties["options"].Description
	require.Contains(t, options, "cn.investment 的 sector（分项）=total|manufacturing")
	require.Contains(t, options, "cn.unemployment 的 age-groups（年龄分组）=true")

	_, err := NewRegistry(tool)
	require.NoError(t, err)
}

func TestMacroSource(t *testing.T) {
	require.Equal(t, "中国人民银行、unknown.example.com", macroSource([]string{"https://www.pbc.gov.cn/a", "https://unknown.example.com/b", "https://www.pbc.gov.cn/c", "://bad"}))
}
