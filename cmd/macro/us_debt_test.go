package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/fiscaldata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUSDebtCommandJSON(t *testing.T) {
	defer gock.Off()
	raw, e := os.ReadFile("../../provider/fiscaldata/testdata/debt-20260930.json")
	require.NoError(t, e)
	gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/dts/public_debt_transactions").MatchParam("filter", "record_date:gte:2026-09-30,record_date:lte:2026-09-30").MatchParam("sort", "record_date,src_line_nbr").Reply(200).BodyString(string(raw))
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"us", "debt", "--begin", "20260930", "--end", "20260930", "--no-cache", "--json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result fiscaldata.DebtResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Rows, 23)
	require.Equal(t, 57718.0, *result.Summaries[0].Net)
	require.True(t, result.Summaries[0].IsDerived)
	require.Nil(t, result.PublishedAt)
	require.True(t, gock.IsDone())
}
func TestUSDebtTerminalAndValidation(t *testing.T) {
	var out bytes.Buffer
	negative, zero := -10.0, 0.0
	result := &fiscaldata.DebtResult{Summaries: []fiscaldata.DebtSummary{{Date: "2026-09-30", Market: "Marketable", Issues: &negative, Redemptions: &zero}}}
	require.NoError(t, printUSDebt(&out, result))
	require.Contains(t, out.String(), "可流通")
	require.Contains(t, out.String(), "-10")
	require.Contains(t, out.String(), "—")
	require.Contains(t, out.String(), "百万美元")
	require.Contains(t, out.String(), "不等于拍卖募集额")
	out.Reset()
	require.NoError(t, printUSDebt(&out, &fiscaldata.DebtResult{}))
	require.Contains(t, out.String(), "不代表发行或赎回为零")
	for _, args := range [][]string{{"us", "debt", "extra"}, {"us", "debt", "--begin", "20051002", "--end", "20051003"}, {"us", "debt", "--end", "bad"}, {"us", "debt", "--begin", "20260930", "--end", "20260929"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
