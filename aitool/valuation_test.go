package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/valuation"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func callValuationJSON(t *testing.T, input string) (*Result, *valuationData, error) {
	t.Helper()
	result, err := ValuationTool().Call(context.Background(), json.RawMessage(input))
	if err != nil {
		return nil, nil, err
	}
	data, ok := result.Data.(*valuationData)
	require.True(t, ok)
	return result, data, nil
}

// sampleValuation 用 300 个交易日的估值计算一份完整结果：PE 从 10 涨到 39.9，最后一天是当前值。
func sampleValuation() *valuation.Result {
	day := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	points := make([]*eastmoney.ValuationPoint, 0, 300)
	for i := range 300 {
		pe, pb, ps := 10+float64(i)/10, 2.0, 3.0
		points = append(points, &eastmoney.ValuationPoint{
			Date: day.AddDate(0, 0, i).Format(time.DateOnly), Name: "招商银行", Board: "银行", Close: 40, MarketCap: 1e12, Shares: 25e9, PE: &pe, PB: &pb, PS: &ps,
		})
	}
	r := valuation.Compute(points, nil, valuation.DefaultDCFParams())
	r.Code = "SH600036"
	return r
}

// stubValuation 替换估值计算，测试结束后恢复。
func stubValuation(t *testing.T, a, us func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error)) {
	t.Helper()
	originalA, originalUS := analyzeA, analyzeUS
	t.Cleanup(func() { analyzeA, analyzeUS = originalA, originalUS })
	if a != nil {
		analyzeA = a
	}
	if us != nil {
		analyzeUS = us
	}
}

func TestValuation(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	var got valuation.DCFParams
	stubValuation(t, func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error) {
		require.Equal(t, "SH600036", exCode)
		got = params
		return sampleValuation(), nil
	}, nil)

	result, data, err := callValuationJSON(t, `{"code":"600036","wacc":10,"growth":5}`)
	require.NoError(t, err)
	require.Equal(t, 10.0, got.WACC, "参数传给估值计算")
	require.Equal(t, 5.0, *got.Growth)
	require.Equal(t, 3.0, got.TerminalGrowth, "没传的用默认值")

	require.Equal(t, "get_valuation", result.Tool)
	require.Equal(t, "东方财富估值与业绩报表", result.Source)
	require.Equal(t, "2025-10-27", result.AsOf)
	require.Equal(t, "估值结果由规则和假设机械计算，不构成投资建议。", result.Disclaimer)
	require.Equal(t, "招商银行", data.Name)
	require.Equal(t, 10000.0, data.MarketCap, "换成亿")

	require.Equal(t, 39.9, *data.PE.Current)
	require.Equal(t, 300, data.PE.Samples)
	require.Equal(t, 10.0, *data.PE.Min)
	require.Equal(t, 39.9, *data.PE.Max)
	require.Equal(t, 100.0, *data.PE.Percentile, "当前是历史最高")
	require.NotEmpty(t, data.Signals)
	require.NotEmpty(t, data.Assessment)

	text, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(text), `"series"`, "不返回逐日序列")
	require.NotContains(t, string(text), `"scenarios"`, "不返回历史情景价格")
	require.Less(t, len(text), 4096)
	require.Contains(t, strings.Join(result.Notes, "\n"), "金额单位是亿元")
}

func TestValuationResultNotes(t *testing.T) {
	r := sampleValuation()
	r.SharesNote, r.PeersNote = "股数按类别折算", "美股暂无同行业对比，规则汇总未计入行业规则"
	r.PB.History.Short, r.PB.History.Samples = true, 120
	r.DCF.Applicable, r.DCF.Warning = true, "终值占比过高"
	r.Dividends = &valuation.Dividends{Mismatches: []valuation.DividendMismatch{{ExDate: "2025-07-01", Sina: 1}}}
	r.Currency, r.Name = "USD", "Test Inc."

	result := ValuationResult(&sina.BasicSecurity{ExCode: "$TEST"}, r)
	data := result.Data.(*valuationData)
	require.Equal(t, "Test Inc.", data.Name, "新浪没有名称时用估值数据里的")
	require.Equal(t, "SEC EDGAR XBRL 与新浪美股日线", result.Source)
	notes := strings.Join(result.Notes, "\n")
	for _, want := range []string{"亿美元", "股数按类别折算", "美股暂无同行业对比", "终值占比过高", "PB 历史样本只有 120 个交易日，不计算分位和历史区间", "1 次分红和新浪的记录不一致", "不是目标价"} {
		require.Contains(t, notes, want)
	}
	require.NotContains(t, notes, "未能和新浪的分红记录核对", "美股不和新浪核对")
	require.Nil(t, data.PB.Min, "样本不足时不给区间")
	require.Nil(t, data.PB.Median)
	require.Equal(t, 120, data.PB.Samples)
	require.NotNil(t, data.PE.Min)

	// A 股没和新浪核对时说明股息率可能偏低。
	r.Currency = "CNY"
	notes = strings.Join(ValuationResult(&sina.BasicSecurity{ExCode: "SH600036"}, r).Notes, "\n")
	require.Contains(t, notes, "未能和新浪的分红记录核对")
	r.Dividends.MismatchChecked = true
	notes = strings.Join(ValuationResult(&sina.BasicSecurity{ExCode: "SH600036"}, r).Notes, "\n")
	require.NotContains(t, notes, "未能和新浪的分红记录核对")
	require.NotNil(t, data.Dividends.Pending, "空数组，不是 null")
	require.Nil(t, data.Peers)
}

func TestValuationCandidates(t *testing.T) {
	defer gock.Off()
	// 000001 是上证指数也是平安银行：指数没有估值时继续试股票。
	suggestKey("000001").Reply(200).BodyString(`var suggestvalue="sh000001,11,000001,sh000001,上证指数,,上证指数,99,1,,,,;sz000001,11,000001,sz000001,平安银行,,平安银行,99,1,ESG,,,";`)
	tried := []string{}
	stubValuation(t, func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error) {
		tried = append(tried, exCode)
		if exCode == "SH000001" {
			return nil, valuation.ErrNoData
		}
		return sampleValuation(), nil
	}, nil)
	_, data, err := callValuationJSON(t, `{"code":"000001"}`)
	require.NoError(t, err)
	require.Equal(t, []string{"SH000001", "SZ000001"}, tried)
	require.Equal(t, "SZ000001", data.ExCode)
	require.Equal(t, "平安银行", data.Name)
}

func TestValuationErrors(t *testing.T) {
	for input, want := range map[string]string{
		`{}`:                                 "code 不能为空",
		`{"code":"x","wacc":3}`:              "DCF 参数不合法",
		`{"code":"x","margin_of_safety":95}`: "安全边际",
	} {
		_, _, err := callValuationJSON(t, input)
		require.True(t, IsInputError(err), input)
		require.ErrorContains(t, err, want, input)
	}

	defer gock.Off()
	suggestKey("00700").Reply(200).BodyString(`var suggestvalue="腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG,,,";`)
	stubValuation(t, func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error) {
		return nil, valuation.ErrNoData
	}, nil)
	_, _, err := callValuationJSON(t, `{"code":"00700"}`)
	require.True(t, IsInputError(err))
	require.EqualError(t, err, "HK00700（腾讯控股）："+valuation.ErrNoData.Error(), "不重复说没有数据")

	suggestKey(`\$TEST`).Persist().Reply(200).BodyString(testSuggest)
	stubValuation(t, nil, func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error) {
		return nil, fmt.Errorf("这家公司按 IFRS 申报：%w", valuation.ErrForeign)
	})
	_, _, err = callValuationJSON(t, `{"code":"$TEST"}`)
	require.True(t, IsInputError(err), "外国公司是参数问题")
	require.ErrorContains(t, err, "美股估值暂只支持美国本土公司")

	// 读取申报封面超时：错误同时包着 ErrNoData 和超时，按超时报上游错误。
	stubValuation(t, nil, func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error) {
		return nil, fmt.Errorf("%w（读取申报封面股数：%w）", valuation.ErrStaleShares, context.DeadlineExceeded)
	})
	_, _, err = callValuationJSON(t, `{"code":"$TEST"}`)
	require.False(t, IsInputError(err), "超时不是参数问题")
	require.ErrorContains(t, err, "估值计算超时或被取消")

	stubValuation(t, nil, func(ctx context.Context, exCode string, params valuation.DCFParams) (*valuation.Result, error) {
		return nil, errors.New("HTTP 503")
	})
	_, _, err = callValuationJSON(t, `{"code":"$TEST"}`)
	require.False(t, IsInputError(err))
	require.ErrorContains(t, err, "估值计算失败")
}

func TestSummarizePeers(t *testing.T) {
	rank, median := 2, 12.0
	got := summarizePeers(&valuation.Peers{
		Board: "白酒", Date: "2026-11-06",
		PE: valuation.PeerMetric{Samples: 3, Excluded: 4, Short: true},
		PB: valuation.PeerMetric{Samples: 19, Median: &median, Rank: &rank},
	})
	require.True(t, got.PE.Short, "样本不足，中位数和排名是 null 的原因")
	require.Equal(t, 4, got.PE.Excluded)
	require.Nil(t, got.PE.Median)
	require.False(t, got.PB.Short)
	require.Equal(t, 12.0, *got.PB.Median)
	require.Equal(t, 2, *got.PB.Rank)
}
