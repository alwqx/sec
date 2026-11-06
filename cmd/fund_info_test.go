package cmd

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/fund"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
)

func TestPrintFundInfo(t *testing.T) {
	nav, change, week := 1.4164, 1.11, 3.9
	var out bytes.Buffer
	printFundInfo(&out, &fund.Info{ExCode: "OF005827", Name: "易方达蓝筹精选混合", NAV: &nav, DayChange: &change, NAVDate: "2026-11-05",
		NetAssetsNote: "189.33亿元（截止至：2026年09月30日）", Returns: &eastmoney.FundReturns{Week: &week}})
	text := out.String()
	require.Contains(t, text, "单位净值\t1.4164")
	require.Contains(t, text, "日涨幅\t+1.11%")
	require.Contains(t, text, "累计净值\t-", "没有的值显示 -")
	require.Contains(t, text, "阶段收益\t近1周 +3.90%")
	require.Contains(t, text, "净资产\t189.33亿元")

	yield10k, yield7d := 0.2211, 0.798
	out.Reset()
	printFundInfo(&out, &fund.Info{ExCode: "OF000198", MoneyMarket: true, Yield10K: &yield10k, Yield7D: &yield7d})
	text = out.String()
	require.Contains(t, text, "万份收益\t0.2211 元")
	require.Contains(t, text, "七日年化\t0.798%")
	require.NotContains(t, text, "单位净值")
	require.NotContains(t, text, "阶段收益", "没有收益率时不显示")
}
