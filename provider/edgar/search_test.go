package edgar

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// 全文检索只传基础类型（结果已含修订版），主文件来自 _id 的文件名。
func TestSearchFilings(t *testing.T) {
	defer gock.Off()
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("forms", "^10-K$").MatchParam("ciks", "0000019617").MatchParam("from", "^0$").
		Reply(200).BodyString(`{"hits":{"total":{"value":2},"hits":[
{"_id":"0001628280-26-008131:jpm-20251231.htm","_source":{"adsh":"0001628280-26-008131","form":"10-K","file_date":"2026-02-13","period_ending":"2025-12-31"}},
{"_id":"0000950123-06-009854:y23756e10vkza.htm","_source":{"adsh":"0000950123-06-009854","form":"10-K/A","file_date":"2006-08-03","period_ending":"2005-12-31"}}]}}`)
	result, err := client(t).SearchFilings(context.Background(), 19617, FilingQuery{Forms: []string{"10-K", "10-K/A"}})
	require.NoError(t, err)
	require.Len(t, result.Filings, 2)
	require.False(t, result.Truncated)
	require.Equal(t, "https://www.sec.gov/Archives/edgar/data/19617/000162828026008131/jpm-20251231.htm", result.Filings[0].URL)
	require.Equal(t, "年报（修订）", result.Filings[1].Description)
	require.True(t, gock.IsDone())
}

func mockTruncatedSubmissions() {
	files := make([]string, maxOlderPages+1)
	for i := range files {
		files[i] = fmt.Sprintf(`{"name":"older-%d.json","filingFrom":"2000-01-01"}`, i)
	}
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000019617.json").Reply(200).
		BodyString(`{"name":"JPM","filings":{"recent":{"accessionNumber":["a-1"],"filingDate":["2026-02-13"],"form":["10-K"]},"files":[` + strings.Join(files, ",") + `]}}`)
	gock.New("https://data.sec.gov").Get("/submissions/older-").Times(maxOlderPages).Reply(200).
		BodyString(`{"accessionNumber":[],"filingDate":[],"form":[]}`)
}

func searchPage(offset, count, total int, form string) string {
	var body strings.Builder
	fmt.Fprintf(&body, `{"hits":{"total":{"value":%d,"relation":"eq"},"hits":[`, total)
	for i := 0; i < count; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"_id":"search-%d:x.htm","_source":{"adsh":"search-%d","form":%q,"file_date":"2025-02-14","file_description":"PRICING SUPPLEMENT"}}`, offset+i, offset+i, form)
	}
	body.WriteString(`]}}`)
	return body.String()
}

func TestFilingsSearchPageCap(t *testing.T) {
	defer gock.Off()
	mockTruncatedSubmissions()
	for page := 0; page < maxSearchPages; page++ {
		gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("from", fmt.Sprintf("^%d$", page*searchPageSize)).
			Reply(200).BodyString(searchPage(page*searchPageSize, searchPageSize, 2100, "10-K"))
	}
	result, err := client(t).Filings(context.Background(), 19617, FilingQuery{Forms: []string{"10-K"}, Want: 3000})
	require.NoError(t, err)
	require.True(t, result.Truncated, "达到 20 页上限不能标记为已查全")
	require.Len(t, result.Filings, 2001)
	require.True(t, gock.IsDone())
}

func TestFilingsSearchCancellationKeepsResults(t *testing.T) {
	defer gock.Off()
	mockTruncatedSubmissions()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 已取到一页全文检索结果后，请求在读取下一页时取消。
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("from", "^0$").
		Reply(200).BodyString(searchPage(0, searchPageSize, 500, "10-K"))
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("from", "^100$").
		AddMatcher(func(_ *http.Request, _ *gock.Request) (bool, error) {
			cancel()
			return true, nil
		}).ReplyError(context.Canceled)
	result, err := client(t).Filings(ctx, 19617, FilingQuery{Forms: []string{"10-K"}, Want: 500})
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Len(t, result.Filings, 101, "保留 submissions 和成功取得的全文检索页")
	require.Equal(t, "a-1", result.Filings[0].Accession)
	require.True(t, gock.IsDone())
}

func TestFilingsSearchFailureKeepsResults(t *testing.T) {
	defer gock.Off()
	mockTruncatedSubmissions()
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").Reply(502)
	result, err := client(t).Filings(context.Background(), 19617, FilingQuery{Forms: []string{"10-K"}, Want: 500})
	require.NoError(t, err)
	require.True(t, result.Truncated)
	require.Len(t, result.Filings, 1)
	require.True(t, gock.IsDone())
}

func TestSearchFilingsRangeAndWant(t *testing.T) {
	defer gock.Off()
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("sort", "^desc$").
		MatchParam("dateRange", "custom").MatchParam("startdt", "2025-01-01").MatchParam("enddt", "2025-12-31").
		Reply(200).BodyString(searchPage(0, searchPageSize, 500, "N-CSR"))
	result, err := client(t).SearchFilings(context.Background(), 19617, FilingQuery{Forms: []string{"N-CSR"}, Begin: "2025-01-01", End: "2025-12-31", Want: 5})
	require.NoError(t, err)
	require.False(t, result.Truncated, "已满足所需条数，不再翻下一页")
	require.GreaterOrEqual(t, len(result.Filings), 5)
	require.Equal(t, "PRICING SUPPLEMENT", result.Filings[0].Description)
	require.True(t, gock.IsDone())
}

func TestSearchFilingsFiltersAndDeduplicates(t *testing.T) {
	defer gock.Off()
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").Reply(200).BodyString(`{"hits":{"total":{"value":5},"hits":[
{"_source":{"adsh":"a","form":"10-K","file_date":"2025-02-14"}},
{"_source":{"adsh":"a","form":"10-K","file_date":"2025-02-14"}},
{"_source":{"adsh":"b","form":"10-K","file_date":"2024-02-14"}},
{"_source":{"adsh":"c","form":"8-K","file_date":"2025-02-14"}},
{"_source":{"adsh":"d","form":"10-K/A","file_date":"2025-03-14"}}]}}`)
	result, err := client(t).SearchFilings(context.Background(), 19617, FilingQuery{Forms: []string{"10-K"}, Begin: "2025-01-01", End: "2025-12-31", Want: 3})
	require.NoError(t, err)
	require.False(t, result.Truncated)
	require.Len(t, result.Filings, 2)
	require.Equal(t, "d", result.Filings[0].Accession, "结果按日期倒序")
	require.True(t, gock.IsDone())
}

func TestSearchFilingsLowerBoundTotal(t *testing.T) {
	defer gock.Off()
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("from", "^0$").
		Reply(200).BodyString(strings.Replace(searchPage(0, searchPageSize, 100, "10-K"), `"eq"`, `"gte"`, 1))
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("from", "^100$").
		Reply(200).BodyString(searchPage(100, 1, 101, "10-K"))
	result, err := client(t).SearchFilings(context.Background(), 19617, FilingQuery{Forms: []string{"10-K"}})
	require.NoError(t, err)
	require.False(t, result.Truncated)
	require.Len(t, result.Filings, 101, "总数只是下限时不能提前停止")
	require.True(t, gock.IsDone())
}

func TestSearchFilingsKeepsAmendmentOnlyFilter(t *testing.T) {
	defer gock.Off()
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").MatchParam("forms", "^10-K/A,8-K$").
		Reply(200).BodyString(searchPage(0, 1, 1, "10-K/A"))
	result, err := client(t).SearchFilings(context.Background(), 19617, FilingQuery{Forms: []string{"10-k/a", "8-k"}})
	require.NoError(t, err)
	require.Len(t, result.Filings, 1, "未指定基础类型时不能丢掉仅修订版的筛选条件")
	require.True(t, gock.IsDone())
}

// 读到分页上限还不够时，用全文检索补全，不再标记 Truncated。
func TestFilingsTruncatedUsesSearch(t *testing.T) {
	defer gock.Off()
	files := `"files":[`
	for i := 1; i <= maxOlderPages+1; i++ {
		if i > 1 {
			files += ","
		}
		files += `{"name":"CIK0000019617-submissions-0` + string(rune('0'+i/10)) + string(rune('0'+i%10)) + `.json","filingFrom":"2000-01-01"}`
	}
	files += "]"
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000019617.json").Reply(200).BodyString(`{"name":"JPM","filings":{"recent":{"accessionNumber":["a-1"],"filingDate":["2026-02-13"],"form":["10-K"]},` + files + `}}`)
	gock.New("https://data.sec.gov").Get("/submissions/CIK0000019617-submissions-").Times(maxOlderPages).Reply(200).BodyString(`{"accessionNumber":[],"filingDate":[],"form":[]}`)
	gock.New("https://efts.sec.gov").Get("/LATEST/search-index").Reply(200).BodyString(`{"hits":{"total":{"value":2},"hits":[
{"_id":"a-1:x.htm","_source":{"adsh":"a-1","form":"10-K","file_date":"2026-02-13"}},
{"_id":"b-2:y.htm","_source":{"adsh":"b-2","form":"10-K","file_date":"2025-02-14"}}]}}`)
	result, err := client(t).Filings(context.Background(), 19617, FilingQuery{Forms: []string{"10-K"}, Want: 20})
	require.NoError(t, err)
	require.False(t, result.Truncated)
	require.Len(t, result.Filings, 2, "按申报编号去重")
	require.Equal(t, "2025-02-14", result.Filings[1].FilingDate)
	require.True(t, gock.IsDone())
}
