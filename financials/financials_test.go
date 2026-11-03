package financials

import (
	"testing"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
)

// 贵州茅台 2025 年报的部分科目，单位元。
var moutai = map[string]any{
	"TOTAL_OPERATE_INCOME": 172054171890.91, "OPERATE_INCOME": 168838102514.79, "TOTAL_OPERATE_COST": 57370818034.33,
	"OPERATE_COST": 14892277570.91, "OPERATE_TAX_ADD": 27354227684.88, "SALE_EXPENSE": 7253499600.68, "MANAGE_EXPENSE": 8320061659.66,
	"RESEARCH_EXPENSE": 190112246.58, "FINANCE_EXPENSE": -815240284.72, "INVEST_INCOME": 561176.14, "FAIRVALUE_CHANGE_INCOME": 76162597.66,
	"OPERATE_PROFIT": 114808950164.24, "NONBUSINESS_INCOME": 74947039.7, "NONBUSINESS_EXPENSE": 128635598.86,
	"TOTAL_PROFIT": 114755261605.08, "INCOME_TAX": 29444936771.41, "NETPROFIT": 85310324833.67, "PARENT_NETPROFIT": 82320067101.68,
	"MINORITY_INTEREST": 2990257731.99, "DEDUCT_PARENT_NETPROFIT": 82293107655.25,
}

// 招商银行 2025 年报。
var cmb = map[string]any{
	"OPERATE_INCOME": 337532000000.0, "INTEREST_NI": 215593000000.0, "FEE_COMMISSION_NI": 75258000000.0, "INVEST_INCOME": 36837000000.0,
	"FAIRVALUE_CHANGE_INCOME": -8160000000.0, "EXCHANGE_INCOME": 3228000000.0, "OTHER_BUSINESS_INCOME": 14776000000.0,
	"INTEREST_EXPENSE": 135758000000.0, "OPERATE_TAX_ADD": 3097000000.0, "BUSINESS_MANAGE_EXPENSE": 107952000000.0,
	"CREDIT_IMPAIRMENT_LOSS": 39586000000.0, "OTHER_BUSINESS_COST": 7456000000.0, "OPERATE_PROFIT": 179252000000.0,
	"NONBUSINESS_INCOME": 105000000.0, "NONBUSINESS_EXPENSE": 364000000.0, "TOTAL_PROFIT": 178993000000.0, "INCOME_TAX": 27867000000.0,
	"NETPROFIT": 151126000000.0, "PARENT_NETPROFIT": 150181000000.0, "MINORITY_INTEREST": 945000000.0,
}

// requireTies 检查每一步的科目加上一个合计都等于这一步的合计。
func requireTies(t *testing.T, bridge *Bridge) {
	t.Helper()
	require.NotNil(t, bridge)
	current := 0.0
	if bridge.Start != nil {
		current = bridge.Start.Value
	}
	for _, step := range bridge.Steps {
		require.InDelta(t, step.Total.Value, current+sum(step.Items), 1, step.Total.Label)
		current = step.Total.Value
	}
}

func labels(step Step) []string {
	result := []string{}
	for _, item := range step.Items {
		result = append(result, item.Label)
	}
	return result
}

func TestGeneralIncome(t *testing.T) {
	bridge := generalIncome(moutai)
	requireTies(t, bridge)
	require.Equal(t, "营业总收入", bridge.Start.Label)
	totals := []string{}
	for _, step := range bridge.Steps {
		totals = append(totals, step.Total.Label)
	}
	require.Equal(t, []string{"毛利", "营业利润", "利润总额", "净利润", "归母净利润"}, totals)
	operating := labels(bridge.Steps[1])
	require.Contains(t, operating, "销售费用")
	require.NotContains(t, operating, "研发费用", "不到收入 0.5% 的科目合入其他")
	require.NotContains(t, operating, "财务净收益", "财务费用为负但金额很小，也合入其他")
	require.Equal(t, []string{"营业外收入", "营业外支出"}, labels(bridge.Steps[2]), "营业外收支再小也单列")
	require.Equal(t, []string{"少数股东损益"}, labels(bridge.Steps[4]))
}

func TestGeneralIncomeOppositeSigns(t *testing.T) {
	row := map[string]any{
		"TOTAL_OPERATE_INCOME": 100.0, "TOTAL_OPERATE_COST": 90.0, "OPERATE_COST": 80.0, "FINANCE_EXPENSE": -5.0, "SALE_EXPENSE": 15.0,
		"INVEST_INCOME": -10.0, "OPERATE_PROFIT": 0.0, "TOTAL_PROFIT": 0.0, "INCOME_TAX": -2.0, "NETPROFIT": 2.0, "PARENT_NETPROFIT": 2.0,
	}
	bridge := generalIncome(row)
	requireTies(t, bridge)
	require.Contains(t, bridge.Steps[1].Items, Item{"财务净收益", 5})
	require.Contains(t, bridge.Steps[1].Items, Item{"投资损失", -10})
	require.Contains(t, bridge.Steps[3].Items, Item{"所得税收益", 2})
}

func TestGeneralIncomeMissingTotals(t *testing.T) {
	require.Nil(t, generalIncome(map[string]any{"TOTAL_OPERATE_INCOME": 100.0}))
	require.Nil(t, generalIncome(map[string]any{"TOTAL_OPERATE_INCOME": 0.0, "TOTAL_OPERATE_COST": 1.0, "OPERATE_PROFIT": -1.0}), "没有收入时不拆")
	// 没有营业成本时跳过毛利这一步。
	row := map[string]any{"TOTAL_OPERATE_INCOME": 100.0, "TOTAL_OPERATE_COST": 60.0, "OPERATE_PROFIT": 40.0, "TOTAL_PROFIT": 40.0, "NETPROFIT": 30.0, "PARENT_NETPROFIT": 30.0}
	bridge := generalIncome(row)
	requireTies(t, bridge)
	require.Equal(t, "营业利润", bridge.Steps[0].Total.Label)
}

func TestFinancialIncome(t *testing.T) {
	bridge := financialIncome(eastmoney.CompanyBank, cmb)
	requireTies(t, bridge)
	require.Nil(t, bridge.Start, "银行的营业收入由各项收入构成，没有起点")
	require.Equal(t, "营业收入", bridge.Steps[0].Total.Label)
	require.Contains(t, bridge.Steps[0].Items, Item{"公允价值变动损失", -8160000000})
	require.NotContains(t, labels(bridge.Steps[1]), "利息支出", "银行的利息支出已经在利息净收入里扣掉")

	insurance := map[string]any{"OPERATE_INCOME": 100.0, "EARNED_PREMIUM": 60.0, "INTEREST_EXPENSE": 10.0, "OPERATE_PROFIT": 20.0, "TOTAL_PROFIT": 20.0, "NETPROFIT": 15.0, "PARENT_NETPROFIT": 15.0}
	bridge = financialIncome(eastmoney.CompanyInsurance, insurance)
	requireTies(t, bridge)
	require.Contains(t, labels(bridge.Steps[1]), "利息支出")
	require.Contains(t, labels(bridge.Steps[1]), "其他营业支出")
}

func TestCashBridge(t *testing.T) {
	row := map[string]any{"BEGIN_CCE": 100.0, "END_CCE": 125.0, "NETCASH_OPERATE": 60.0, "NETCASH_INVEST": -30.0, "NETCASH_FINANCE": -6.0, "RATE_CHANGE_EFFECT": 1.0}
	bridge := cashBridge(row)
	requireTies(t, bridge)
	require.Len(t, bridge.Steps[0].Items, 4, "加总已经对上，不需要差额")
	require.Nil(t, cashBridge(map[string]any{"BEGIN_CCE": 1.0}))
}

func TestAssets(t *testing.T) {
	row := map[string]any{"TOTAL_ASSETS": 100.0, "MONETARYFUNDS": 30.0, "ACCOUNTS_RECE": 10.0, "NOTE_RECE": 5.0, "INVENTORY": 20.0, "FIXED_ASSET": 15.0, "CIP": 5.0}
	require.Equal(t, []Item{{"货币资金", 30}, {"应收票据及账款", 15}, {"存货", 20}, {"固定资产和在建工程", 20}, {"其他资产", 15}}, generalAssets(row))
	row["NOTE_ACCOUNTS_RECE"] = 12.0
	require.Equal(t, Item{"应收票据及账款", 12}, generalAssets(row)[1], "有合计科目时不再加分项，避免重复")
	require.Nil(t, generalAssets(map[string]any{"TOTAL_ASSETS": 10.0, "MONETARYFUNDS": 30.0}), "拆出的科目超过总资产时不返回")
	require.Nil(t, generalAssets(nil))
}

func TestMetrics(t *testing.T) {
	balance := map[string]any{"TOTAL_ASSETS": 200.0, "TOTAL_LIABILITIES": 50.0}
	cash := map[string]any{"NETCASH_OPERATE": 61522204989.35}
	m := metrics(eastmoney.CompanyGeneral, moutai, balance, cash)
	require.InDelta(t, (168838102514.79-14892277570.91)/168838102514.79*100, *m.GrossMargin, 1e-9, "毛利率按营业收入")
	require.InDelta(t, 85310324833.67/172054171890.91*100, *m.NetMargin, 1e-9)
	require.InDelta(t, 25, *m.DebtRatio, 1e-9)
	require.Less(t, *m.FinanceRatio, 0.0)
	require.Nil(t, m.CostIncomeRatio)
	require.InDelta(t, 61522204989.35/85310324833.67, *m.CashToProfit, 1e-12)

	loss := metrics(eastmoney.CompanyGeneral, map[string]any{"TOTAL_OPERATE_INCOME": 100.0, "NETPROFIT": -5.0}, nil, map[string]any{"NETCASH_OPERATE": 3.0})
	require.Nil(t, loss.CashToProfit, "净利润为负时比值没有意义")
	require.Nil(t, loss.DebtRatio)

	bank := metrics(eastmoney.CompanyBank, cmb, nil, nil)
	require.Nil(t, bank.GrossMargin)
	require.InDelta(t, 107952000000.0/337532000000*100, *bank.CostIncomeRatio, 1e-9)
}

func TestCollectSkipsSmallItems(t *testing.T) {
	items := collect(map[string]any{"A": 100.0, "B": 0.4}, []spec{cost("A", "甲", ""), cost("B", "乙", "")}, 1)
	require.Equal(t, []Item{{"甲", -100}}, items)
}

func TestImpairmentCountedOnce(t *testing.T) {
	row := map[string]any{
		"TOTAL_OPERATE_INCOME": 100.0, "TOTAL_OPERATE_COST": 70.0, "OPERATE_COST": 60.0, "ASSET_IMPAIRMENT_LOSS": 10.0, "ASSET_IMPAIRMENT_INCOME": -10.0,
		"OPERATE_PROFIT": 20.0, "TOTAL_PROFIT": 20.0, "NETPROFIT": 20.0, "PARENT_NETPROFIT": 20.0,
	}
	bridge := generalIncome(row)
	requireTies(t, bridge)
	count := 0
	for _, item := range bridge.Steps[1].Items {
		if item.Label == "资产减值损失" {
			count++
		}
	}
	require.Equal(t, 1, count, "新旧两个科目都有值时只列一次")

	old := map[string]any{"TOTAL_OPERATE_INCOME": 100.0, "TOTAL_OPERATE_COST": 70.0, "OPERATE_COST": 60.0, "ASSET_IMPAIRMENT_LOSS": 10.0, "OPERATE_PROFIT": 30.0, "TOTAL_PROFIT": 30.0, "NETPROFIT": 30.0, "PARENT_NETPROFIT": 30.0}
	require.Contains(t, generalIncome(old).Steps[1].Items, Item{"资产减值损失", -10}, "旧报表只有旧科目")
}

func TestGeneralTypeWithFinancialFormat(t *testing.T) {
	row := map[string]any{"OPERATE_INCOME": 100.0, "BUSINESS_MANAGE_EXPENSE": 30.0, "OPERATE_PROFIT": 60.0, "TOTAL_PROFIT": 60.0, "NETPROFIT": 45.0, "PARENT_NETPROFIT": 45.0}
	period := Build("2025-12-31", eastmoney.CompanyGeneral, row, nil, nil)
	requireTies(t, period.Income)
	require.Equal(t, "营业收入", period.Income.Steps[0].Total.Label)
	require.Equal(t, 100.0, *period.Metrics.Revenue)
	require.Nil(t, Build("2025-12-31", eastmoney.CompanyGeneral, map[string]any{}, nil, nil).Income)
}
