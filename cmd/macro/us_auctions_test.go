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

func TestUSAuctionsJSONAndDateFilters(t *testing.T) {
	for _, basis := range []string{"issue", "auction"} {
		t.Run(basis, func(t *testing.T) {
			defer gock.Off()
			raw, e := os.ReadFile("../../provider/fiscaldata/testdata/auctions-202609.json")
			require.NoError(t, e)
			begin, end := "20260901", "20260930"
			filter := "issue_date:gte:2026-09-01,issue_date:lte:2026-09-30"
			sort := "issue_date,cusip,auction_date"
			if basis == "auction" {
				begin = "20260827"
				filter = "auction_date:gte:2026-08-27,auction_date:lte:2026-09-30"
				sort = "auction_date,cusip,issue_date"
				// 只使用单个真实事件，避免将发行日期排序冒充拍卖日期排序。
				var d map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &d))
				var rows []map[string]string
				require.NoError(t, json.Unmarshal(d["data"], &rows))
				d["data"], e = json.Marshal(rows[:1])
				require.NoError(t, e)
				var meta map[string]any
				require.NoError(t, json.Unmarshal(d["meta"], &meta))
				meta["count"], meta["total-count"], meta["total-pages"] = 1, 1, 1
				d["meta"], e = json.Marshal(meta)
				require.NoError(t, e)
				raw, e = json.Marshal(d)
				require.NoError(t, e)
			}
			gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").MatchParam("filter", filter).MatchParam("sort", sort).Reply(200).BodyString(string(raw))
			cmd := NewMacroCLI()
			cmd.SetArgs([]string{"us", "auctions", "--begin", begin, "--end", end, "--date-basis", basis, "--json", "--no-cache"})
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			var result fiscaldata.AuctionsResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Equal(t, basis+"_date", result.DateBasis)
			require.Equal(t, 108522561100.0, *result.Observations[0].Amounts.TotalAccepted)
			require.Nil(t, result.PublishedAt)
			require.True(t, gock.IsDone())
		})
	}
}

func TestUSAuctionsTerminalFormattingAndInvalidArguments(t *testing.T) {
	var out bytes.Buffer
	value, zero, yes := 100000000000.0, 0.0, true
	result := &fiscaldata.AuctionsResult{DateBasis: "issue_date", Observations: []fiscaldata.AuctionObservation{{CUSIP: "912797VE4", SecurityType: "Bill", Term: "4-Week", IssueDate: "2026-09-01", AuctionDate: "2026-08-27", Reopening: &yes, Amounts: fiscaldata.AuctionAmounts{TotalAccepted: &value, SOMA: &zero}}, {CUSIP: "912797VN4", SecurityType: "Note", FloatingRate: &yes}}}
	require.NoError(t, printUSAuctions(&out, result))
	require.Contains(t, out.String(), "官方发行日")
	require.Contains(t, out.String(), "十亿美元面值")
	require.Contains(t, out.String(), "100.000")
	require.Contains(t, out.String(), "0.000")
	require.Contains(t, out.String(), "—")
	require.Contains(t, out.String(), "浮息国债")
	out.Reset()
	require.NoError(t, printUSAuctions(&out, &fiscaldata.AuctionsResult{DateBasis: "auction_date"}))
	require.Contains(t, out.String(), "按拍卖日筛选")
	require.Contains(t, out.String(), "不代表发行金额为零")
	for _, args := range [][]string{{"us", "auctions", "extra"}, {"us", "auctions", "--end", "bad"}, {"us", "auctions", "--begin", "19980726", "--end", "19980727"}, {"us", "auctions", "--date-basis", "announcement"}} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
