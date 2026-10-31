package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/alwqx/sec/provider/fiscaldata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSDebtCashJSON(t *testing.T) {
	defer gock.Off()
	raw, e := os.ReadFile("../../provider/fiscaldata/testdata/cash-totals-20260930.json")
	require.NoError(t, e)
	var totals map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &totals))
	var rows []map[string]string
	require.NoError(t, json.Unmarshal(totals["data"], &rows))
	for _, suffix := range []string{"IIIB", "III-B"} {
		for _, side := range []string{"Issues", "Redemp."} {
			category := "Public Debt Cash " + side + " (Table " + suffix + ")"
			selected := []map[string]string{}
			for _, r := range rows {
				if r["transaction_catg"] == category {
					selected = append(selected, r)
				}
			}
			var meta map[string]any
			require.NoError(t, json.Unmarshal(totals["meta"], &meta))
			meta["count"], meta["total-count"] = len(selected), len(selected)
			meta["total-pages"] = 0
			if len(selected) > 0 {
				meta["total-pages"] = 1
			}
			body := map[string]any{"data": selected, "meta": meta}
			gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/dts/deposits_withdrawals_operating_cash").MatchParam("filter", regexp.QuoteMeta("record_date:gte:2026-09-30,record_date:lte:2026-09-30,transaction_catg:eq:"+category)).Reply(200).JSON(body)
		}
	}
	raw, e = os.ReadFile("../../provider/fiscaldata/testdata/cash-adjustments-20260930.json")
	require.NoError(t, e)
	gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/dts/adjustment_public_debt_transactions_cash_basis").MatchParam("filter", "record_date:gte:2026-09-30,record_date:lte:2026-09-30").Reply(200).BodyString(string(raw))
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"us", "debt-cash", "--begin", "20260930", "--end", "20260930", "--json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result fiscaldata.CashResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, "treasury_debt_cash", result.Indicator)
	require.Equal(t, "百万美元", result.Unit)
	require.Len(t, result.Observations, 1)
	row := result.Observations[0]
	require.Equal(t, 232753.0, *row.Issues.Today)
	require.Equal(t, 175898.0, *row.Redemptions.Today)
	require.Equal(t, 56855.0, *row.Net.Today)
	require.Len(t, row.Adjustments, 13)
	require.Nil(t, result.PublishedAt)
	require.True(t, gock.IsDone())
}

func TestUSDebtCashTerminalMissingAndZero(t *testing.T) {
	zero, negative := 0.0, -10.0
	result := &fiscaldata.CashResult{Observations: []fiscaldata.CashObservation{{Date: "2026-09-30", Issues: &fiscaldata.CashRow{CashAmounts: fiscaldata.CashAmounts{Today: &zero}}, Net: fiscaldata.CashAmounts{Today: &negative}, Note: "官方现金总额缺失"}}}
	var out bytes.Buffer
	require.NoError(t, printUSDebtCash(&out, result))
	require.Contains(t, out.String(), "2026-09-30")
	require.Contains(t, out.String(), "—")
	require.Contains(t, out.String(), "-10")
	require.Contains(t, out.String(), "官方现金发行")
	require.Contains(t, out.String(), "百万美元")
	require.Contains(t, out.String(), result.Observations[0].Note)
	out.Reset()
	require.NoError(t, printUSDebtCash(&out, &fiscaldata.CashResult{}))
	require.Contains(t, out.String(), "不代表现金发行或赎回为零")
	for _, args := range [][]string{{"us", "debt-cash", "extra"}, {"us", "debt-cash", "--end", "bad"}, {"us", "debt-cash", "--begin", "20051002", "--end", "20051003"}, {"us", "debt-cash", "--begin", "20260930", "--end", "20260929"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
