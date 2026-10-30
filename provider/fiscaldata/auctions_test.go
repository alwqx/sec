package fiscaldata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func mockAuction(t *testing.T, env envelope, page int) {
	t.Helper()
	gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").MatchParam("page[number]", fmt.Sprint(page)).MatchParam("page[size]", "1000").Reply(200).BodyString(encode(t, env))
}
func auctionRequest() *AuctionsRequest { return &AuctionsRequest{Begin: "20260901", End: "20260930"} }

func TestAuctionsOfficialEventsAndAmounts(t *testing.T) {
	defer gock.Off()
	mockAuction(t, fixture(t, "auctions-202609.json"), 1)
	result, e := NewClient("").Auctions(context.Background(), auctionRequest())
	require.NoError(t, e)
	require.Len(t, result.Observations, 37)
	require.Equal(t, "issue_date", result.DateBasis)
	require.Equal(t, "par value", result.AmountBasis)
	require.Equal(t, "USD", result.AmountUnit)
	first := result.Observations[0]
	require.Equal(t, "912797VE4", first.CUSIP)
	require.Equal(t, "2026-08-27", first.AuctionDate)
	require.Equal(t, "2026-09-01", first.IssueDate)
	require.Equal(t, "2026-06-02", *first.OriginalIssueDate)
	require.True(t, *first.Reopening)
	require.Equal(t, 100000000000.0, *first.Amounts.Offering)
	require.Equal(t, 108522561100.0, *first.Amounts.TotalAccepted)
	require.Equal(t, 8521960400.0, *first.Amounts.SOMA)
	require.False(t, *first.SOMAIncluded)
	require.True(t, *first.FIMAIncluded)
	require.Equal(t, 2000000000.0, *first.Amounts.FIMA)
	require.Equal(t, 99.716111, *first.PricePer100)
	require.Equal(t, "108522561100", first.Raw["total_accepted"])
	require.NotEmpty(t, first.Source)
	require.False(t, first.FetchedAt.IsZero())
	require.Nil(t, result.PublishedAt)
	require.Nil(t, result.RevisionID)
	counts := map[string]int{}
	frn, tips := 0, 0
	for _, r := range result.Observations {
		counts[r.CUSIP]++
		if r.FloatingRate != nil && *r.FloatingRate {
			frn++
		}
		if r.InflationIndexed != nil && *r.InflationIndexed {
			tips++
		}
	}
	require.Equal(t, 2, counts["912797VN4"], "同CUSIP再发行不能去重丢失")
	require.Positive(t, frn)
	require.Positive(t, tips)
	require.True(t, gock.IsDone())
}

func TestAuctionsDateBasisHistoricalAndMissingFields(t *testing.T) {
	defer gock.Off()
	env := fixture(t, "auctions-202609.json")
	env.Data = env.Data[:1]
	env.Meta.Total, env.Meta.Count = 1, 1
	gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").MatchParam("filter", "auction_date:gte:2026-08-27,auction_date:lte:2026-08-27").MatchParam("sort", "auction_date,cusip,issue_date").Reply(200).BodyString(encode(t, env))
	result, e := NewClient("").Auctions(context.Background(), &AuctionsRequest{Begin: "20260827", End: "20260827", DateBasis: "auction"})
	require.NoError(t, e)
	require.Equal(t, "auction_date", result.DateBasis)
	require.Equal(t, "2026-08-27", result.FirstDate)
	require.Equal(t, "2026-09-01", result.Observations[0].IssueDate)
	mockAuction(t, fixture(t, "auctions-1998.json"), 1)
	result, e = NewClient("").Auctions(context.Background(), &AuctionsRequest{Begin: "19980727", End: "19980831"})
	require.NoError(t, e)
	require.Len(t, result.Observations, 17)
	require.Nil(t, result.Observations[0].Amounts.SOMA)
	require.Nil(t, result.Observations[0].SOMAIncluded)
	env.Data[0]["soma_accepted"] = "0"
	env.Data[0]["soma_included"] = "null"
	mockAuction(t, env, 1)
	result, e = NewClient("").Auctions(context.Background(), auctionRequest())
	require.NoError(t, e)
	require.Equal(t, 0.0, *result.Observations[0].Amounts.SOMA)
	require.Nil(t, result.Observations[0].SOMAIncluded)
	require.True(t, gock.IsDone())
}

func TestAuctionsRejectsInvalidResponses(t *testing.T) {
	for name, change := range map[string]func(*envelope){
		"cusip":            func(e *envelope) { e.Data[0]["cusip"] = "bad" },
		"date basis range": func(e *envelope) { e.Data[0]["issue_date"] = "2026-08-29" },
		"date order":       func(e *envelope) { e.Data[0]["auction_date"] = "2026-09-02" },
		"maturity":         func(e *envelope) { e.Data[0]["maturity_date"] = "2026-08-29" },
		"bad date":         func(e *envelope) { e.Data[0]["issue_date"] = "null" },
		"announcement":     func(e *envelope) { e.Data[0]["announcemt_date"] = "2026-09-02" },
		"original issue":   func(e *envelope) { e.Data[0]["original_issue_date"] = "2026-09-02" },
		"flag":             func(e *envelope) { e.Data[0]["soma_included"] = "unknown" },
		"type":             func(e *envelope) { e.Data[0]["security_type"] = "Other" },
		"negative amount":  func(e *envelope) { e.Data[0]["total_accepted"] = "-1" },
		"nonfinite":        func(e *envelope) { e.Data[0]["total_accepted"] = "NaN" },
		"missing amount":   func(e *envelope) { delete(e.Data[0], "total_accepted") },
		"zero price":       func(e *envelope) { e.Data[0]["price_per100"] = "0" },
		"duplicate":        func(e *envelope) { e.Data[1] = e.Data[0] },
		"unsorted":         func(e *envelope) { e.Data[0], e.Data[1] = e.Data[1], e.Data[0] },
		"truncated":        func(e *envelope) { e.Meta.Total++ },
		"pages":            func(e *envelope) { e.Meta.Pages++ },
		"count":            func(e *envelope) { e.Meta.Count++ },
	} {
		t.Run(name, func(t *testing.T) {
			defer gock.Off()
			env := fixture(t, "auctions-202609.json")
			change(&env)
			mockAuction(t, env, 1)
			_, e := NewClient("").Auctions(context.Background(), auctionRequest())
			require.Error(t, e)
			require.True(t, gock.IsDone())
		})
	}
}

func TestAuctionsMultiplePages(t *testing.T) {
	for _, mode := range []string{"valid", "overlap", "changed total", "http"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			env := fixture(t, "auctions-202609.json")
			row := env.Data[0]
			env.Data = nil
			for i := 1; i <= 1000; i++ {
				r := map[string]string{}
				for k, v := range row {
					r[k] = v
				}
				r["cusip"] = fmt.Sprintf("%09d", i)
				env.Data = append(env.Data, r)
			}
			env.Meta.Total, env.Meta.Count, env.Meta.Pages = 1001, 1000, 2
			mockAuction(t, env, 1)
			second := fixture(t, "auctions-202609.json")
			second.Data = second.Data[:1]
			second.Data[0]["cusip"] = "000001001"
			second.Meta.Total, second.Meta.Count, second.Meta.Pages = 1001, 1, 2
			if mode == "overlap" {
				second.Data[0]["cusip"] = "000001000"
			}
			if mode == "changed total" {
				second.Data = append(second.Data, second.Data[0])
				second.Meta.Total, second.Meta.Count = 1002, 2
			}
			if mode == "http" {
				gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").Reply(503)
			} else {
				mockAuction(t, second, 2)
			}
			r, e := NewClient("").Auctions(context.Background(), auctionRequest())
			if mode == "valid" {
				require.NoError(t, e)
				require.Len(t, r.Observations, 1001)
			} else {
				require.Error(t, e)
				require.Nil(t, r)
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestAuctionsAuctionDatePagination(t *testing.T) {
	for _, mode := range []string{"valid", "overlap", "out of order"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			env := fixture(t, "auctions-202609.json")
			original := env.Data[0]
			env.Data = nil
			for i := 1; i <= pageSize; i++ {
				row := make(map[string]string, len(original))
				for key, value := range original {
					row[key] = value
				}
				row["cusip"] = fmt.Sprintf("%09d", i)
				row["issue_date"] = "2026-09-05"
				env.Data = append(env.Data, row)
			}
			env.Meta.Total, env.Meta.Count, env.Meta.Pages = pageSize+1, pageSize, 2
			second := fixture(t, "auctions-202609.json")
			second.Data = second.Data[:1]
			// 第二页的发行日和CUSIP均小于前页末行，仍须按拍卖日正确排序。
			second.Data[0]["auction_date"] = "2026-08-28"
			second.Data[0]["issue_date"] = "2026-08-31"
			second.Data[0]["cusip"] = "000000001"
			second.Meta.Total, second.Meta.Count, second.Meta.Pages = pageSize+1, 1, 2
			if mode == "overlap" {
				second.Data[0] = env.Data[pageSize-1]
			}
			if mode == "out of order" {
				second.Data[0]["auction_date"] = "2026-08-27"
				second.Data[0]["cusip"] = "000000000"
			}
			for i, page := range []envelope{env, second} {
				gock.New("https://api.fiscaldata.treasury.gov").
					Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").
					MatchParam("filter", "auction_date:gte:2026-08-27,auction_date:lte:2026-08-28").
					MatchParam("sort", "auction_date,cusip,issue_date").
					MatchParam("page[number]", fmt.Sprint(i+1)).MatchParam("page[size]", "1000").
					Reply(200).BodyString(encode(t, page))
			}
			result, err := NewClient("").Auctions(context.Background(), &AuctionsRequest{Begin: "20260827", End: "20260828", DateBasis: "auction"})
			if mode == "valid" {
				require.NoError(t, err)
				require.Len(t, result.Observations, pageSize+1)
				require.Equal(t, "2026-08-27", result.FirstDate)
				require.Equal(t, "2026-08-28", result.LastDate)
				require.Equal(t, "2026-08-31", result.Observations[pageSize].IssueDate)
				require.Equal(t, result.Observations[0].CUSIP, result.Observations[pageSize].CUSIP, "同CUSIP不同拍卖日的事件须全部保留")
			} else {
				require.ErrorContains(t, err, "拍卖事件重复或排序变化")
				require.Nil(t, result, "跨页失败不能返回部分事件")
			}
			require.True(t, gock.IsDone())
		})
	}
}

func TestAuctionsFutureIssueDateBoundary(t *testing.T) {
	defer gock.Off()
	today := time.Now().UTC().Truncate(24 * time.Hour)
	issueDate := today.AddDate(0, 0, 7).Format("2006-01-02")
	env := fixture(t, "auctions-202609.json")
	env.Data = env.Data[:1]
	env.Meta.Total, env.Meta.Count = 1, 1
	row := env.Data[0]
	row["auction_date"] = today.Format("2006-01-02")
	row["issue_date"] = issueDate
	row["maturity_date"] = today.AddDate(0, 0, 35).Format("2006-01-02")
	row["announcemt_date"] = today.AddDate(0, 0, -2).Format("2006-01-02")
	row["original_issue_date"] = "null"
	filter := "auction_date:gte:" + row["auction_date"] + ",auction_date:lte:" + row["auction_date"]
	gock.New("https://api.fiscaldata.treasury.gov").
		Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").
		MatchParam("filter", filter).MatchParam("sort", "auction_date,cusip,issue_date").
		Reply(200).BodyString(encode(t, env))
	period := today.Format("20060102")
	result, err := NewClient("").Auctions(context.Background(), &AuctionsRequest{Begin: period, End: period, DateBasis: "auction"})
	require.NoError(t, err, "已拍卖但尚未发行的记录仍属于今日拍卖结果")
	require.Len(t, result.Observations, 1)
	require.Equal(t, issueDate, result.Observations[0].IssueDate)
	require.Equal(t, issueDate, result.Observations[0].Raw["issue_date"])
	require.Equal(t, row["auction_date"], result.FirstDate)
	require.Equal(t, row["auction_date"], result.LastDate)
	require.Nil(t, result.PublishedAt)
	// 同一记录不属于今日发行口径，不能因拍卖日为今日而混入。
	_, _, err = parseAuctions([]byte(encode(t, env)), &AuctionsRequest{Begin: period, End: period, DateBasis: "issue"}, 1)
	require.ErrorContains(t, err, "超出查询口径区间")
	row["auction_date"] = today.AddDate(0, 0, 1).Format("2006-01-02")
	_, _, err = parseAuctions([]byte(encode(t, env)), &AuctionsRequest{Begin: period, End: period, DateBasis: "auction"}, 1)
	require.ErrorContains(t, err, "超出查询口径区间", "未来发行可以保留，但未来拍卖不能混入今日拍卖结果")
	for _, basis := range []string{"issue", "auction"} {
		err := (&AuctionsRequest{Begin: period, End: today.AddDate(0, 0, 1).Format("20060102"), DateBasis: basis}).Validate()
		require.Error(t, err, "两种口径均不能主动查询未来日期范围")
	}
	require.True(t, gock.IsDone())
}

func TestAuctionsCacheRefreshEmptyAndCancellation(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := auctionRequest()
	mockAuction(t, fixture(t, "auctions-202609.json"), 1)
	first, e := client.Auctions(context.Background(), req)
	require.NoError(t, e)
	again, e := client.Auctions(context.Background(), req)
	require.NoError(t, e)
	require.Equal(t, first, again)
	paths, e := filepath.Glob(filepath.Join(client.CacheDir, "*.json"))
	require.NoError(t, e)
	require.Len(t, paths, 1)
	before, e := os.ReadFile(paths[0])
	require.NoError(t, e)
	client.Refresh = true
	gock.New("https://api.fiscaldata.treasury.gov").Get("/services/api/fiscal_service/v1/accounting/od/auctions_query").Reply(200).BodyString(`<html>error</html>`)
	_, e = client.Auctions(context.Background(), req)
	require.Error(t, e)
	after, e := os.ReadFile(paths[0])
	require.NoError(t, e)
	require.Equal(t, before, after)
	client.Refresh = false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = client.Auctions(ctx, req)
	require.ErrorIs(t, e, context.Canceled)
	empty := fixture(t, "auctions-202609.json")
	empty.Data = []map[string]string{}
	empty.Meta.Total, empty.Meta.Count, empty.Meta.Pages = 0, 0, 0
	mockAuction(t, empty, 1)
	r, e := NewClient("").Auctions(context.Background(), req)
	require.NoError(t, e)
	require.Empty(t, r.Observations)
	require.Empty(t, r.FirstDate)
	require.True(t, gock.IsDone())
	for _, r := range []*AuctionsRequest{nil, {Begin: "19980726", End: "19980727"}, {Begin: "20260931", End: "20260931"}, {Begin: "20260930", End: "20260929"}, {Begin: "20260901", End: "20260930", DateBasis: "announcement"}} {
		require.Error(t, r.Validate())
	}
	raw, e := json.Marshal(empty)
	require.NoError(t, e)
	_, _, e = parseAuctions(raw, req, 1)
	require.NoError(t, e)
}
