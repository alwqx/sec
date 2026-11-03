package eastmoney

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestCompanyTypeOfBoard(t *testing.T) {
	require.Equal(t, CompanyBank, companyTypeOfBoard("银行Ⅱ"))
	require.Equal(t, CompanySecurities, companyTypeOfBoard("证券Ⅱ"))
	require.Equal(t, CompanyInsurance, companyTypeOfBoard("保险Ⅱ"))
	require.Equal(t, CompanyGeneral, companyTypeOfBoard("白酒Ⅱ"))
	require.Equal(t, CompanyGeneral, companyTypeOfBoard(""))
}

func TestGetStatementsSplitsDates(t *testing.T) {
	defer gock.Off()
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbAjaxNew").
		MatchParam("dates", "^2025-12-31,2024-12-31,2023-12-31,2022-12-31,2021-12-31$").MatchParam("reportType", "^2$").Reply(200).
		BodyString(`{"data":[{"REPORT_DATE":"2025-12-31 00:00:00","TOTAL_OPERATE_INCOME":10}]}`)
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbAjaxNew").
		MatchParam("dates", "^2020-12-31$").Reply(200).BodyString(`{"data":[{"REPORT_DATE":"2020-12-31 00:00:00","TOTAL_OPERATE_INCOME":5}]}`)
	rows, err := GetStatements(context.Background(), "sh600519", CompanyGeneral, StatementIncome, true,
		[]string{"2025-12-31", "2024-12-31", "2023-12-31", "2022-12-31", "2021-12-31", "2020-12-31"})
	require.NoError(t, err)
	require.Len(t, rows, 2, "接口一次最多 5 个报告期")
	require.Equal(t, 5.0, rows["2020-12-31"]["TOTAL_OPERATE_INCOME"])
	require.True(t, gock.IsDone())
}

func TestGetStatementsBalanceIsAlwaysPointInTime(t *testing.T) {
	defer gock.Off()
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/zcfzbAjaxNew").MatchParam("reportType", "^1$").Reply(200).BodyString(`{"data":[]}`)
	_, err := GetStatements(context.Background(), "SH600519", CompanyGeneral, StatementBalance, true, []string{"2026-09-30"})
	require.NoError(t, err)
	require.True(t, gock.IsDone(), "资产负债表没有单季值")
}

func TestF10RetriesOnceAndTreatsErrorObjectAsEmpty(t *testing.T) {
	defer gock.Off()
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbDateAjaxNew").Reply(503)
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbDateAjaxNew").Reply(200).
		BodyString(`{"data":[{"REPORT_DATE":"2025-12-31 00:00:00"},{"REPORT_DATE":"2024-12-31 00:00:00"}]}`)
	dates, err := StatementDates(context.Background(), "SH600519", CompanyGeneral, true)
	require.NoError(t, err)
	require.Equal(t, []string{"2025-12-31", "2024-12-31"}, dates)

	// 公司类型不对时接口返回不含 data 的对象。
	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbDateAjaxNew").Reply(200).BodyString(`{"$types":{},"$type":"x"}`)
	dates, err = StatementDates(context.Background(), "SH600519", CompanyBank, true)
	require.NoError(t, err)
	require.Empty(t, dates)

	gock.New("https://emweb.securities.eastmoney.com").Get("/PC_HSF10/NewFinanceAnalysis/lrbDateAjaxNew").Times(2).Reply(500)
	_, err = StatementDates(context.Background(), "SH600519", CompanyGeneral, true)
	require.ErrorContains(t, err, "HTTP 500")
}

func TestCompanyTypeOfUnsupportedCode(t *testing.T) {
	_, err := CompanyTypeOf(context.Background(), "HK00700")
	require.ErrorIs(t, err, ErrUnsupportedCode)
}
