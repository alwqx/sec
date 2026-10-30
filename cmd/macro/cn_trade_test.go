package macro

import (
	"bytes"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/mofcom"
	"github.com/stretchr/testify/require"
)

func TestQueryCNTradeMonths(t *testing.T) {
	command := newCNTradeCLI()
	begin, end, err := queryCNTradeMonths(command, time.Date(2026, 1, 1, 2, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "202501", begin)
	require.Equal(t, "202512", end)
}

func TestPrintCNTradeCompact(t *testing.T) {
	var out bytes.Buffer
	value := 100.0
	result := &mofcom.TradeResult{PartnerPeriod: "202608", Observations: []mofcom.TradeObservation{{Period: "202608", CNY: &mofcom.TradeCurrencyValues{Exports: mofcom.TradeAmount{Monthly: &value}}}}, Partners: []mofcom.TradePartner{{Name: "日本", TotalYTD: &value}}}
	require.NoError(t, printCNTrade(&out, result))
	require.Contains(t, out.String(), "2026-08")
	require.Contains(t, out.String(), "人民币出口")
	require.Contains(t, out.String(), "主要伙伴")
	require.Contains(t, out.String(), "服务贸易未纳入")
}

func TestPrintCNTradeMissingPartners(t *testing.T) {
	var out bytes.Buffer
	value := 100.0
	result := &mofcom.TradeResult{
		PartnerPeriod: "201512", PartnerNote: "商务部未提供201512月主要伙伴统计；保留可用的月度贸易数据",
		Observations: []mofcom.TradeObservation{{Period: "201512", USD: &mofcom.TradeCurrencyValues{Exports: mofcom.TradeAmount{Monthly: &value}}}},
	}
	require.NoError(t, printCNTrade(&out, result))
	require.Contains(t, out.String(), "2015-12")
	require.Contains(t, out.String(), "100.00")
	require.Contains(t, out.String(), result.PartnerNote)
	require.NotContains(t, out.String(), "出口累计")
}

func TestPrintCNTradeCNYPartnerUnitsAndScope(t *testing.T) {
	var out bytes.Buffer
	value := 100.0
	result := &mofcom.TradeResult{PartnerPeriod: "202608", PartnerCurrency: "CNY", PartnerNote: "范围交叠，不能加总，详见 --json", PartnerScope: "不应将全部组织成员范围长文输出到终端", Partners: []mofcom.TradePartner{{Name: "欧洲联盟", Currency: "CNY", TotalYTD: &value}}}
	require.NoError(t, printCNTrade(&out, result))
	require.Contains(t, out.String(), "累计值（亿元人民币）")
	require.Contains(t, out.String(), "不能加总")
	require.NotContains(t, out.String(), result.PartnerScope)
	require.NotContains(t, out.String(), "累计值（亿美元）")
}
