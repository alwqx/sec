package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alwqx/sec/financials"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func callFinancialsJSON(t *testing.T, input string) (*Result, *financialsData, error) {
	t.Helper()
	result, err := FinancialsTool().Call(context.Background(), json.RawMessage(input))
	if err != nil {
		return nil, nil, err
	}
	data, ok := result.Data.(*financialsData)
	require.True(t, ok)
	return result, data, nil
}

func period(date string, revenue, profit, margin float64) financials.Period {
	return financials.Period{Date: date, Metrics: financials.Metrics{Revenue: &revenue, ParentNetProfit: &profit, GrossMargin: &margin}}
}

func TestFinancialPeriods(t *testing.T) {
	periods := []financials.Period{
		period("2021-12-31", 80e8, -1e8, 30),
		period("2022-12-31", 100e8, 10e8, 40),
		period("2023-12-31", 120e8, 9e8, 41.5),
		// 2024 年缺失：2025 年没有上年同期。
		period("2025-12-31", 150e8, 12e8, 42),
	}

	got, suspect := financialPeriods(periods, 3)
	require.Empty(t, suspect)
	require.Len(t, got, 3, "只留最近 3 期")
	require.Equal(t, "2025-12-31", got[0].Date, "从新到旧")
	require.Equal(t, 150.0, *got[0].Revenue, "换成亿")
	require.Nil(t, got[0].RevenueYoY, "上年同期缺失")
	require.Nil(t, got[0].GrossMarginChange)

	require.Equal(t, 20.0, *got[1].RevenueYoY, "120 / 100 - 1")
	require.Equal(t, -10.0, *got[1].NetProfitYoY, "9 / 10 - 1")
	require.Equal(t, 1.5, *got[1].GrossMarginChange, "41.5 - 40，单位是百分点")
	require.Nil(t, got[1].DeductNetProfitYoY, "没有扣非净利润")

	require.Equal(t, 25.0, *got[2].RevenueYoY)
	require.Nil(t, got[2].NetProfitYoY, "上年亏损，同比没有意义")
	require.Equal(t, 10.0, *got[2].GrossMarginChange)

	// 美股财年末不在月底，相隔 364 天也是上年同期。
	us, _ := financialPeriods([]financials.Period{period("2024-09-28", 100e8, 1e8, 40), period("2025-09-27", 110e8, 1e8, 40)}, 5)
	require.Equal(t, 10.0, *us[0].RevenueYoY)
}

func TestFinancialPeriodsBadRevenue(t *testing.T) {
	// 蔚来的情况：FY2023 营收取错科目，毛利率远超 100%。
	bad := period("2023-12-31", 1.67e8, -211e8, 1825)
	bad.Metrics.NetMargin = ptrTo(-12650)
	got, suspect := financialPeriods([]financials.Period{
		period("2022-12-31", 1.23e8, -145e8, 4180), bad, period("2024-12-31", 657e8, -226e8, 9.88),
	}, 2)
	require.Equal(t, []string{"2023-12-31", "2022-12-31"}, suspect, "返回的一期和作为基数的上一期，从新到旧")

	require.Equal(t, 657.0, *got[0].Revenue, "本期数据正常")
	require.Nil(t, got[0].RevenueYoY, "基数有误，不算同比")
	require.Nil(t, got[0].GrossMarginChange)
	require.NotNil(t, got[0].GrossMargin)
	require.Nil(t, got[0].NetProfitYoY, "上年亏损")

	require.Nil(t, got[1].Revenue)
	require.Nil(t, got[1].GrossMargin)
	require.Nil(t, got[1].NetMargin)
	require.Equal(t, -211.0, *got[1].NetProfit, "利润不依赖营收，照常返回")
}

// 两个财年的美股：营收 900 亿到 1000 亿美元，净利润 200 亿到 250 亿美元。
const financialsSECFacts = `{"entityName":"Test Inc.","facts":{"us-gaap":{
"Revenues":{"units":{"USD":[
 {"start":"2024-01-01","end":"2024-12-31","val":90000000000,"form":"10-K","filed":"2025-02-01"},
 {"start":"2025-01-01","end":"2025-12-31","val":100000000000,"form":"10-K","filed":"2026-02-01"}]}},
"CostOfRevenue":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":40000000000,"form":"10-K","filed":"2026-02-01"}]}},
"NetIncomeLoss":{"units":{"USD":[
 {"start":"2024-01-01","end":"2024-12-31","val":20000000000,"form":"10-K","filed":"2025-02-01"},
 {"start":"2025-01-01","end":"2025-12-31","val":25000000000,"form":"10-K","filed":"2026-02-01"}]}},
"Assets":{"units":{"USD":[{"end":"2025-12-31","val":200000000000,"form":"10-K","filed":"2026-02-01"}]}},
"Liabilities":{"units":{"USD":[{"end":"2025-12-31","val":120000000000,"form":"10-K","filed":"2026-02-01"}]}},
"NetCashProvidedByUsedInOperatingActivities":{"units":{"USD":[{"start":"2025-01-01","end":"2025-12-31","val":30000000000,"form":"10-K","filed":"2026-02-01"}]}}
}}}`

// stubSECFacts 让美股取数返回 body 或 err，测试结束后恢复。
func stubSECFacts(t *testing.T, body string, err error) {
	t.Helper()
	original := loadSECFacts
	t.Cleanup(func() { loadSECFacts = original })
	loadSECFacts = func(ctx context.Context, ticker string) (edgar.Company, *edgar.Facts, error) {
		require.Equal(t, "TEST", ticker, "去掉 $ 前缀")
		if err != nil {
			return edgar.Company{}, nil, err
		}
		facts, parseErr := edgar.ParseFacts(1, []byte(body))
		require.NoError(t, parseErr)
		return edgar.Company{CIK: 1, Ticker: ticker}, facts, nil
	}
}

const testSuggest = `var suggestvalue="测试,41,test,test,测试公司,,测试公司,99,1,,,,";`

func TestFinancialsUS(t *testing.T) {
	defer gock.Off()
	suggestKey(`\$TEST`).Reply(200).BodyString(testSuggest)
	stubSECFacts(t, financialsSECFacts, nil)

	result, data, err := callFinancialsJSON(t, `{"code":"$TEST"}`)
	require.NoError(t, err)
	require.Equal(t, "get_financials", result.Tool)
	require.Equal(t, "SEC EDGAR XBRL", result.Source)
	require.Equal(t, "2025-12-31", result.AsOf)
	require.Equal(t, "$TEST", data.ExCode)
	require.Equal(t, "测试公司", data.Name, "名称和其他工具一样用新浪的")
	require.Equal(t, "annual", data.Period)
	require.Equal(t, "USD", data.Currency)
	require.Equal(t, "US GAAP", data.Standard)
	require.Equal(t, "亿", data.Unit)
	require.Len(t, data.Periods, 2)

	latest := data.Periods[0]
	require.Equal(t, "2025", latest.Label)
	require.Equal(t, 1000.0, *latest.Revenue)
	require.Equal(t, 11.11, *latest.RevenueYoY)
	require.Equal(t, 250.0, *latest.NetProfit)
	require.Equal(t, 25.0, *latest.NetProfitYoY)
	require.Equal(t, 60.0, *latest.GrossMargin)
	require.Equal(t, 25.0, *latest.NetMargin)
	require.Equal(t, 300.0, *latest.OperatingCash)
	require.Equal(t, 1.2, *latest.CashToProfit)
	require.Equal(t, 60.0, *latest.DebtRatio)
	require.Nil(t, latest.DeductNetProfit, "SEC 没有扣非净利润")
	require.Nil(t, data.Periods[1].RevenueYoY, "最早一期没有上年同期")

	notes := strings.Join(result.Notes, "\n")
	require.Contains(t, notes, "金额单位是亿美元")
	require.Contains(t, notes, "财年")
	require.Contains(t, notes, "只有 2 期数据")
	require.NotContains(t, notes, "毛利率超过 100%")
	require.NotContains(t, notes, "单季")
}

func TestFinancialsErrors(t *testing.T) {
	for input, want := range map[string]string{
		`{}`:                          "code 不能为空",
		`{"code":"x","period":"ttm"}`: "period 只能是",
	} {
		_, _, err := callFinancialsJSON(t, input)
		require.True(t, IsInputError(err), input)
		require.ErrorContains(t, err, want, input)
	}

	defer gock.Off()
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	_, _, err := callFinancialsJSON(t, `{"code":"nosuch"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "没有找到证券 nosuch")

	// 港股、基金和指数不支持。
	for key, body := range map[string]string{
		"00700":  `var suggestvalue="腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG,,,";`,
		"005827": `var suggestvalue="易方达蓝筹,21,005827,of005827,易方达蓝筹精选混合,,易方达蓝筹精选混合,99,1,,,,";`,
		"000300": `var suggestvalue="sh000300,11,000300,sh000300,沪深300,,沪深300,99,1,,,,";`,
	} {
		suggestKey(key).Reply(200).BodyString(body)
		_, _, err := callFinancialsJSON(t, `{"code":"`+key+`"}`)
		require.True(t, IsInputError(err), key)
		require.ErrorContains(t, err, "暂不支持财报", key)
	}

	// SEC 没有这家公司是参数问题，其他失败是上游问题。
	suggestKey(`\$TEST`).Persist().Reply(200).BodyString(testSuggest)
	stubSECFacts(t, "", edgar.ErrNotFound)
	_, _, err = callFinancialsJSON(t, `{"code":"$TEST"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "SEC EDGAR 没有 $TEST")

	stubSECFacts(t, "", errors.New("HTTP 503"))
	_, _, err = callFinancialsJSON(t, `{"code":"$TEST"}`)
	require.False(t, IsInputError(err))
	require.ErrorContains(t, err, "SEC 财报获取失败")

	// IFRS 公司没有季度数据。
	stubSECFacts(t, `{"facts":{"ifrs-full":{"Revenue":{"units":{"TWD":[{"start":"2024-01-01","end":"2024-12-31","val":1,"form":"20-F","filed":"2025-04-17"}]}}}}}`, nil)
	_, _, err = callFinancialsJSON(t, `{"code":"$TEST","period":"quarter"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "IFRS")
}

func TestFinancialsSearchDown(t *testing.T) {
	defer gock.Off()
	// 新浪搜索失败时按代码返回，没有名称：用 SEC 的公司名。
	suggestKey(`\$TEST`).Persist().Reply(500)
	stubSECFacts(t, financialsSECFacts, nil)
	_, data, err := callFinancialsJSON(t, `{"code":"$TEST"}`)
	require.NoError(t, err)
	require.Equal(t, "Test Inc.", data.Name)

	stubSECFacts(t, "", edgar.ErrNotFound)
	_, _, err = callFinancialsJSON(t, `{"code":"$TEST"}`)
	require.EqualError(t, err, "SEC EDGAR 没有 $TEST 的财报数据，可能不向 SEC 申报", "没有名称时不写空括号")
}
