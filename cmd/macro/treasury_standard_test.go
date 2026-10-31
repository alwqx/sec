package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const fiscalDataHost = "https://api.fiscaldata.treasury.gov"

func mockDebtFixture(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../../provider/fiscaldata/testdata/debt-20260930.json")
	require.NoError(t, err)
	gock.New(fiscalDataHost).Get("/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions").Reply(200).BodyString(string(raw))
}

func mockAuctionsFixture(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../../provider/fiscaldata/testdata/auctions-202609.json")
	require.NoError(t, err)
	gock.New(fiscalDataHost).Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").Reply(200).BodyString(string(raw))
}

// mockDebtCashFixture 按类别拆分官方总额样本，与provider的精确类别查询对应。
func mockDebtCashFixture(t *testing.T) {
	t.Helper()
	raw, err := os.ReadFile("../../provider/fiscaldata/testdata/cash-totals-20260930.json")
	require.NoError(t, err)
	var totals map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &totals))
	var rows []map[string]string
	require.NoError(t, json.Unmarshal(totals["data"], &rows))
	for _, suffix := range []string{"IIIB", "III-B"} {
		for _, side := range []string{"Issues", "Redemp."} {
			category := "Public Debt Cash " + side + " (Table " + suffix + ")"
			selected := []map[string]string{}
			for _, row := range rows {
				if row["transaction_catg"] == category {
					selected = append(selected, row)
				}
			}
			var meta map[string]any
			require.NoError(t, json.Unmarshal(totals["meta"], &meta))
			meta["count"], meta["total-count"], meta["total-pages"] = len(selected), len(selected), min(len(selected), 1)
			gock.New(fiscalDataHost).Get("/services/api/fiscal_service/v1/accounting/dts/deposits_withdrawals_operating_cash").MatchParam("filter", regexp.QuoteMeta("transaction_catg:eq:"+category)).Reply(200).JSON(map[string]any{"data": selected, "meta": meta})
		}
	}
	adjustments, err := os.ReadFile("../../provider/fiscaldata/testdata/cash-adjustments-20260930.json")
	require.NoError(t, err)
	gock.New(fiscalDataHost).Get("/services/api/fiscal_service/v1/accounting/dts/adjustment_public_debt_transactions_cash_basis").Reply(200).BodyString(string(adjustments))
}

func runStandard(t *testing.T, args ...string) macrodata.Dataset {
	t.Helper()
	cmd := NewMacroCLI()
	cmd.SetArgs(append(append([]string{"us"}, args...), "--standard-json", "--no-cache"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	return result
}

func observationValue(t *testing.T, result macrodata.Dataset, seriesID string) *float64 {
	t.Helper()
	for _, o := range result.Observations {
		if o.SeriesID == seriesID {
			return o.Value
		}
	}
	require.FailNow(t, "未找到观测", seriesID)
	return nil
}

func TestUSDebtStandardOutput(t *testing.T) {
	defer gock.Off()
	mockDebtFixture(t)
	result := runStandard(t, "debt", "--begin", "20260930", "--end", "20260930")
	require.Len(t, result.Observations, 23*3+2*3)
	require.Equal(t, 57718.0, *observationValue(t, result, "us.treasury-debt.marketable.net_recorded_issuance"))
	require.Equal(t, -10.0, *observationValue(t, result, "us.treasury-debt.marketable.issues.bills-regular-series.daily"))
	require.Empty(t, result.Events)
	require.True(t, gock.IsDone())
}

func TestUSDebtCashStandardOutput(t *testing.T) {
	defer gock.Off()
	mockDebtCashFixture(t)
	result := runStandard(t, "debt-cash", "--begin", "20260930", "--end", "20260930")
	require.Len(t, result.Observations, 3*3+13*3)
	require.Equal(t, 232753.0, *observationValue(t, result, "us.treasury-debt-cash.issues.daily"))
	require.Equal(t, 175898.0, *observationValue(t, result, "us.treasury-debt-cash.redemptions.daily"))
	require.Equal(t, 56855.0, *observationValue(t, result, "us.treasury-debt-cash.net.daily"))
	require.Equal(t, -14664.0, *observationValue(t, result, "us.treasury-debt-cash.net.month_to_date"))
	require.Equal(t, -6.0, *observationValue(t, result, "us.treasury-debt-cash.adjustment.issues.inflation-protected-securities-increment-minus.daily"))
	require.True(t, gock.IsDone())
}

func TestUSAuctionsStandardOutput(t *testing.T) {
	defer gock.Off()
	mockAuctionsFixture(t)
	result := runStandard(t, "auctions", "--begin", "20260901", "--end", "20260930")
	require.Len(t, result.Series, 10)
	require.NotEmpty(t, result.Events)
	require.Len(t, result.Observations, len(result.Events)*10)
	first := result.Observations[0]
	require.Equal(t, result.Events[0].ID, first.EventID)
	require.Equal(t, "20260901", first.Period)
	require.Equal(t, 108522561100.0, *result.Observations[1].Value)
	require.Equal(t, "issue-date", result.Series[0].PeriodBasis)
	require.True(t, gock.IsDone())
}

func TestTreasuryStandardErrors(t *testing.T) {
	for _, test := range []struct {
		args []string
		path string
		mock func(*testing.T)
	}{
		{[]string{"debt", "--begin", "20260930", "--end", "20260930"}, "/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions", mockDebtFixture},
		{[]string{"debt-cash", "--begin", "20260930", "--end", "20260930"}, "/services/api/fiscal_service/v1/accounting/dts/deposits_withdrawals_operating_cash", mockDebtCashFixture},
		{[]string{"auctions", "--begin", "20260901", "--end", "20260930"}, "/services/api/fiscal_service/v1/accounting/od/auctions_query", mockAuctionsFixture},
	} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			t.Run(test.args[0]+"/"+mode, func(t *testing.T) {
				defer gock.Off()
				args := append(append([]string{"us"}, test.args...), "--standard-json", "--no-cache")
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "http":
					gock.New(fiscalDataHost).Get(test.path).Reply(503)
				case "writer":
					test.mock(t)
				}
				cmd := NewMacroCLI()
				cmd.SetArgs(args)
				var out bytes.Buffer
				cmd.SetOut(&out)
				if mode == "writer" {
					cmd.SetOut(failingMoneyWriter{})
				}
				cmd.SetErr(&bytes.Buffer{})
				cmd.SilenceUsage = true
				err := cmd.ExecuteContext(ctx)
				require.Error(t, err)
				switch mode {
				case "flags":
					require.ErrorContains(t, err, "none of the others can be")
				case "cancel":
					require.ErrorIs(t, err, context.Canceled)
				case "http":
					require.ErrorContains(t, err, "503")
				case "writer":
					require.ErrorContains(t, err, "测试写入失败")
				}
				if mode != "writer" {
					require.Empty(t, out.String())
				}
				require.True(t, gock.IsDone())
			})
		}
	}
}
