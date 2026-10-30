package chinamoney

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/forex"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".json")
	require.NoError(t, err)
	return raw
}

func TestParseFixingOfficialFields(t *testing.T) {
	raw := fixture(t, "fixing")
	_, items, err := parseFixing(raw, forex.OnshorePairs, "2026-10-19", "2026-10-23", 1)
	require.NoError(t, err)
	require.Len(t, items, 10)
	require.Equal(t, "USD/CNY", items[0].Pair)
	require.Equal(t, "USD", items[0].BaseCurrency)
	require.Equal(t, "CNY", items[0].QuoteCurrency)
	require.Equal(t, 6.718, *items[0].Value)
	require.Equal(t, 0.85696, *items[1].Value)
	for _, tc := range []struct {
		name   string
		change func(*fixingEnvelope)
	}{
		{"wrong code", func(e *fixingEnvelope) { e.Head.Code = "500" }},
		{"unexpected pair", func(e *fixingEnvelope) { e.Data.Pairs = []string{"USD/CNY", "USD/CNH"} }},
		{"duplicate pair", func(e *fixingEnvelope) { e.Data.Pairs = []string{"USD/CNY", "USD/CNY"} }},
		{"missing pair", func(e *fixingEnvelope) { e.Data.Pairs = []string{"USD/CNY"} }},
		{"empty pair name", func(e *fixingEnvelope) { e.Data.Pairs = []string{"USD/CNY", ""} }},
		{"wrong range", func(e *fixingEnvelope) { e.Data.Begin = "2026-10-01" }},
		{"wrong page", func(e *fixingEnvelope) { e.Data.Page = 2 }},
		{"missing records", func(e *fixingEnvelope) { e.Records = nil }},
		{"wrong columns", func(e *fixingEnvelope) { e.Records[0].Values = e.Records[0].Values[:1] }},
		{"invalid rate", func(e *fixingEnvelope) { e.Records[0].Values[0] = "0" }},
		{"duplicate day", func(e *fixingEnvelope) { e.Records[1].Date = e.Records[0].Date }},
		{"out of range", func(e *fixingEnvelope) { e.Records[0].Date = "2026-10-24" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env fixingEnvelope
			require.NoError(t, json.Unmarshal(raw, &env))
			tc.change(&env)
			bad, err := json.Marshal(env)
			require.NoError(t, err)
			_, _, err = parseFixing(bad, forex.OnshorePairs, "2026-10-19", "2026-10-23", 1)
			require.Error(t, err)
		})
	}
	var env fixingEnvelope
	require.NoError(t, json.Unmarshal(raw, &env))
	env.Records[0].Values[0] = "---"
	missing, err := json.Marshal(env)
	require.NoError(t, err)
	_, items, err = parseFixing(missing, forex.OnshorePairs, "2026-10-19", "2026-10-23", 1)
	require.NoError(t, err)
	require.Nil(t, items[0].Value)
	require.NotEmpty(t, items[0].Note)
}

func TestFixingMapsResponseColumnsIndependentlyOfRequestOrder(t *testing.T) {
	orders := [][]string{{"USD/CNY", "HKD/CNY"}, {"HKD/CNY", "USD/CNY"}}
	for _, requested := range orders {
		for _, returned := range orders {
			t.Run(fmt.Sprintf("request_%s_response_%s", requested[0], returned[0]), func(t *testing.T) {
				var env fixingEnvelope
				require.NoError(t, json.Unmarshal(fixture(t, "fixing"), &env))
				if returned[0] != env.Data.Pairs[0] {
					env.Data.Pairs = returned
					for i := range env.Records {
						values := env.Records[i].Values
						env.Records[i].Values = []string{values[1], values[0]}
					}
				}
				raw, err := json.Marshal(env)
				require.NoError(t, err)
				_, items, err := parseFixing(raw, requested, "2026-10-19", "2026-10-23", 1)
				require.NoError(t, err)
				require.Len(t, items, 10)
				for _, item := range items[:2] {
					if item.Pair == "USD/CNY" {
						require.Equal(t, 6.718, *item.Value)
						require.Equal(t, "6.7180", item.RawValues["rate"])
					} else {
						require.Equal(t, "HKD/CNY", item.Pair)
						require.Equal(t, 0.85696, *item.Value)
						require.Equal(t, "0.85696", item.RawValues["rate"])
					}
				}
			})
		}
	}
}

func TestFixingReversedRequestOrderAndCache(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.chinamoney.com.cn").Get("/ags/ms/cm-u-bk-ccpr/CcprHisNew").MatchParam("currency", "HKD/CNY,USD/CNY").Reply(200).BodyString(string(fixture(t, "fixing")))
	client := NewClient(t.TempDir())
	req := &forex.Request{Pairs: []string{"HKD/CNY", "USD/CNY"}, Begin: "20261019", End: "20261023"}
	result, err := client.Fixing(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 10)
	last := result.Observations[8:]
	require.Equal(t, "HKD/CNY", last[0].Pair)
	require.Equal(t, 0.85696, *last[0].Value)
	require.Equal(t, "USD/CNY", last[1].Pair)
	require.Equal(t, 6.718, *last[1].Value)
	cached, err := client.Fixing(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, cached)
	require.True(t, gock.IsDone())
}

func TestFixingPaginationAndCache(t *testing.T) {
	for _, mode := range []string{"valid", "duplicate", "incomplete", "changed total"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			var base fixingEnvelope
			require.NoError(t, json.Unmarshal(fixture(t, "fixing"), &base))
			base.Data.Begin, base.Data.End = "2026-10-01", "2026-10-11"
			base.Data.Total, base.Data.Pages = 11, 2
			row := base.Records[0]
			base.Records = nil
			for day := 1; day <= 10; day++ {
				entry := row
				entry.Date = fmt.Sprintf("2026-10-%02d", day)
				base.Records = append(base.Records, entry)
			}
			page1 := base
			page2 := base
			page2.Data.Page = 2
			row.Date = "2026-10-11"
			page2.Records = append(page2.Records[:0:0], row)
			switch mode {
			case "duplicate":
				page2.Records[0].Date = "2026-10-01"
			case "incomplete":
				page1.Data.Total = 12
				page2.Data.Total = 12
			case "changed total":
				page2.Data.Total = 10
			}
			gock.New("https://www.chinamoney.com.cn").Get("/ags/ms/cm-u-bk-ccpr/CcprHisNew").MatchParam("pageNum", "1").MatchParam("pageSize", "10").Reply(200).JSON(page1)
			gock.New("https://www.chinamoney.com.cn").Get("/ags/ms/cm-u-bk-ccpr/CcprHisNew").MatchParam("pageNum", "2").Reply(200).JSON(page2)
			client := NewClient(t.TempDir())
			req := &forex.Request{Pairs: forex.OnshorePairs, Begin: "20261001", End: "20261011"}
			result, err := client.Fixing(context.Background(), req)
			if mode != "valid" {
				require.Error(t, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.Len(t, result.Observations, 22)
				require.Equal(t, "2026-10-01", result.Observations[0].Date)
				for _, item := range result.Observations {
					require.NotEmpty(t, item.Source)
					require.False(t, item.FetchedAt.IsZero())
				}
				again, err := client.Fixing(context.Background(), req)
				require.NoError(t, err)
				require.Equal(t, result, again)
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestReferenceTimeAndMissingValues(t *testing.T) {
	raw := fixture(t, "reference")
	items, err := parseReference(raw, "USD/CNY", "23 Oct 2026", "23 Oct 2026", "16:00")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, 6.671, *items[0].Value)
	require.Equal(t, "2026-10-23T16:00:00+08:00", items[0].ObservedAt)
	for _, tc := range []struct {
		name   string
		change func(*referenceEnvelope)
		valid  bool
	}{
		{"missing value", func(e *referenceEnvelope) { e.Records[0]["rateOf16hour"] = "---" }, true},
		{"missing hour", func(e *referenceEnvelope) { delete(e.Records[0], "rateOf16hour") }, false},
		{"reset range", func(e *referenceEnvelope) { e.Data.Begin = "01 Oct 2026" }, false},
		{"wrong pair", func(e *referenceEnvelope) { e.Records[0]["ccyPair"] = "HKD/CNY" }, false},
		{"wrong day", func(e *referenceEnvelope) { e.Records[0]["dealDate"] = "2026-10-22" }, false},
		{"server error", func(e *referenceEnvelope) { e.Data.Message = "失败" }, false},
		{"no records field", func(e *referenceEnvelope) { e.Records = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var env referenceEnvelope
			require.NoError(t, json.Unmarshal(raw, &env))
			tc.change(&env)
			changed, err := json.Marshal(env)
			require.NoError(t, err)
			got, err := parseReference(changed, "USD/CNY", "23 Oct 2026", "23 Oct 2026", "16:00")
			if tc.valid {
				require.NoError(t, err)
				require.Nil(t, got[0].Value)
				require.Empty(t, got[0].ObservedAt)
				require.NotEmpty(t, got[0].Note)
			} else {
				require.Error(t, err)
			}
		})
	}
	require.Error(t, ValidateReference(&forex.Request{Pairs: []string{"USD/CNY"}, Begin: "20261023", End: "20261023", At: "16:30"}))
}

func TestReferenceMonthlyWindowsAndMetadata(t *testing.T) {
	defer gock.Off()
	req := &forex.Request{Pairs: []string{"USD/CNY"}, Begin: "20260930", End: "20261001", At: "16:00"}
	for _, window := range []struct{ begin, end, date string }{{"30 Sep 2026", "30 Sep 2026", "2026-09-30"}, {"01 Oct 2026", "01 Oct 2026", "2026-10-01"}} {
		var env referenceEnvelope
		require.NoError(t, json.Unmarshal(fixture(t, "reference"), &env))
		env.Data.Begin, env.Data.End = window.begin, window.end
		env.Records[0]["dealDate"] = window.date
		gock.New("https://www.chinamoney.com.cn").Get("/ags/ms/cm-u-bk-fx/RefRateHis").MatchParam("startDateTool", window.begin).MatchParam("endDateTool", window.end).MatchParam("currencyCode", "USD.CNY").Reply(200).JSON(env)
	}
	result, err := NewClient("").Reference(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
	require.Equal(t, "16:00", result.ObservationTime)
	require.Equal(t, "onshore-reference", result.PriceType)
	require.True(t, gock.IsDone())
	require.Equal(t, [][2]string{{"2024-02-28", "2024-02-29"}, {"2024-03-01", "2024-03-01"}}, monthWindows("20240228", "20240301"))
}
