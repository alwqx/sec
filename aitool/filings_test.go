package aitool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func callFilingsJSON(t *testing.T, input string) (*Result, *filingsData, error) {
	t.Helper()
	result, err := FilingsTool().Call(context.Background(), json.RawMessage(input))
	if err != nil {
		return nil, nil, err
	}
	data, ok := result.Data.(*filingsData)
	require.True(t, ok)
	return result, data, nil
}

// stubFilings 替换上游查询，测试结束后恢复。
func stubFilings(t *testing.T, ann func(ctx context.Context, code string, req *cninfo.QueryRequest) (*cninfo.QueryResponse, error),
	sec func(ctx context.Context, ticker string, query edgar.FilingQuery) (*edgar.FilingsResult, error)) {
	t.Helper()
	originalAnn, originalSEC := queryAnnouncements, queryFilings
	t.Cleanup(func() { queryAnnouncements, queryFilings = originalAnn, originalSEC })
	if ann != nil {
		queryAnnouncements = ann
	}
	if sec != nil {
		queryFilings = sec
	}
}

// announcements 生成 n 条公告，从 2026-11-03 起每天往前一条；第二条已失效，不应返回。
func announcements(n int) []*cninfo.Announcement {
	out := make([]*cninfo.Announcement, 0, n)
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, cninfo.Beijing)
	for i := range n {
		a := &cninfo.Announcement{
			SecCode: "600036", Title: fmt.Sprintf("招商银行<em>公告</em>%d", i), Time: day.AddDate(0, 0, -i).UnixMilli(),
			AdjunctURL: fmt.Sprintf("finalpage/2026-11-03/%d.PDF", i),
		}
		if i == 1 {
			a.InvalidationFlag = 1
		}
		out = append(out, a)
	}
	return out
}

func TestFilingsAShare(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Persist().Reply(200).BodyString(cmbSuggest)
	var got *cninfo.QueryRequest
	stubFilings(t, func(ctx context.Context, code string, req *cninfo.QueryRequest) (*cninfo.QueryResponse, error) {
		require.Equal(t, "600036", code)
		got = req
		return &cninfo.QueryResponse{Total: 120, Data: announcements(30)}, nil
	}, nil)

	result, data, err := callFilingsJSON(t, `{"code":"600036","category":"annual","begin":"2026-01-01","limit":3}`)
	require.NoError(t, err)
	require.Equal(t, cninfo.CategoryAnnual, got.Category)
	require.Equal(t, "2026-01-01", got.StartDate)
	require.Equal(t, utils.ShanghaiToday().Format(time.DateOnly), got.EndDate, "只给开始日期时结束日期是今天")

	require.Equal(t, "list_filings", result.Tool)
	require.Equal(t, "巨潮资讯", result.Source)
	require.Empty(t, result.AsOf, "列表没有统一的截止日期")
	require.Equal(t, "SH600036", data.ExCode)
	require.Equal(t, 120, *data.Total)
	require.Len(t, data.Results, 3)
	require.Equal(t, filingItem{Date: "2026-11-03", Title: "招商银行公告0", URL: "https://static.cninfo.com.cn/finalpage/2026-11-03/0.PDF"}, data.Results[0], "去掉高亮标签")
	require.Equal(t, "2026-11-01", data.Results[1].Date, "跳过已失效的公告")
	require.Contains(t, strings.Join(result.Notes, "\n"), "共 120 条，只列最近 3 条")

	// A 股传空白的 forms 当作没传。
	_, _, err = callFilingsJSON(t, `{"code":"600036","forms":[" "]}`)
	require.NoError(t, err)
}

func TestFilingsCandidates(t *testing.T) {
	defer gock.Off()
	// 000001 是上证指数也是平安银行：跳过指数。
	suggestKey("000001").Reply(200).BodyString(`var suggestvalue="sh000001,11,000001,sh000001,上证指数,,上证指数,99,1,,,,;sz000001,11,000001,sz000001,平安银行,,平安银行,99,1,ESG,,,";`)
	stubFilings(t, func(ctx context.Context, code string, req *cninfo.QueryRequest) (*cninfo.QueryResponse, error) {
		return &cninfo.QueryResponse{}, nil
	}, nil)
	_, data, err := callFilingsJSON(t, `{"code":"000001"}`)
	require.NoError(t, err)
	require.Equal(t, "SZ000001", data.ExCode)
	require.Equal(t, "平安银行", data.Name)

	// 只有指数时报不支持。
	suggestKey("000300").Reply(200).BodyString(`var suggestvalue="sh000300,11,000300,sh000300,沪深300,,沪深300,99,1,,,,";`)
	_, _, err = callFilingsJSON(t, `{"code":"000300"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "SH000300（沪深300） 暂不支持")
}

func TestFilingsUS(t *testing.T) {
	defer gock.Off()
	suggestKey(`\$TEST`).Persist().Reply(200).BodyString(testSuggest)
	var got edgar.FilingQuery
	filings := []edgar.Filing{
		{Form: "10-K", FilingDate: "2026-11-03", ReportDate: "2026-09-26", Description: "年报", URL: "https://www.sec.gov/a.htm"},
		{Form: "SC 13G", FilingDate: "2026-10-01", URL: "https://www.sec.gov/b.htm"},
	}
	stubFilings(t, nil, func(ctx context.Context, ticker string, query edgar.FilingQuery) (*edgar.FilingsResult, error) {
		require.Equal(t, "TEST", ticker)
		got = query
		return &edgar.FilingsResult{Profile: &edgar.Profile{Name: "Test Inc."}, Filings: filings, Truncated: true}, nil
	})

	result, data, err := callFilingsJSON(t, `{"code":"$TEST"}`)
	require.NoError(t, err)
	require.Equal(t, edgar.DefaultForms, got.Forms, "默认申报类型")
	require.Equal(t, defaultFilings, got.Want)
	require.Equal(t, "SEC EDGAR", result.Source)
	require.Nil(t, data.Total)
	require.Equal(t, filingItem{Date: "2026-11-03", Title: "年报", Form: "10-K", ReportDate: "2026-09-26", URL: "https://www.sec.gov/a.htm"}, data.Results[0])
	require.Equal(t, "SC 13G", data.Results[1].Title, "没有中文说明时用申报类型")
	notes := strings.Join(result.Notes, "\n")
	require.Contains(t, notes, "不含 Form 4")
	require.Contains(t, notes, "结果可能不全")

	_, _, err = callFilingsJSON(t, `{"code":"$TEST","forms":[" 8-K ",""],"end":"2026-06-30"}`)
	require.NoError(t, err)
	require.Equal(t, []string{"8-K"}, got.Forms)
	require.Equal(t, "", got.Begin)
	require.Equal(t, "2026-06-30", got.End)
}

func TestFilingsErrors(t *testing.T) {
	for input, want := range map[string]string{
		`{}`:                              "code 不能为空",
		`{"code":"x","limit":0}`:          "limit 须在 1 到 20",
		`{"code":"x","limit":21}`:         "limit 须在 1 到 20",
		`{"code":"x","begin":"20260101"}`: "YYYY-MM-DD",
		`{"code":"x","begin":"2026-12-01","end":"2026-01-01"}`: "不能晚于",
		`{"code":"x","category":"ttm"}`:                        "category 只能是",
	} {
		_, _, err := callFilingsJSON(t, input)
		require.True(t, IsInputError(err), input)
		require.ErrorContains(t, err, want, input)
	}

	defer gock.Off()
	suggestKey("00700").Reply(200).BodyString(`var suggestvalue="腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG,,,";`)
	_, _, err := callFilingsJSON(t, `{"code":"00700"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "暂不支持公告和申报")

	// 参数和市场不匹配。
	suggestKey("600036").Persist().Reply(200).BodyString(cmbSuggest)
	suggestKey(`\$TEST`).Persist().Reply(200).BodyString(testSuggest)
	_, _, err = callFilingsJSON(t, `{"code":"600036","forms":["10-K"]}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "forms 只用于美股")
	_, _, err = callFilingsJSON(t, `{"code":"$TEST","category":"annual"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "category 只用于 A 股")

	// 上游：找不到是参数问题，其他失败是上游问题。
	stubFilings(t, func(ctx context.Context, code string, req *cninfo.QueryRequest) (*cninfo.QueryResponse, error) {
		return nil, errCNINFONotFound
	}, func(ctx context.Context, ticker string, query edgar.FilingQuery) (*edgar.FilingsResult, error) {
		return nil, edgar.ErrNotFound
	})
	_, _, err = callFilingsJSON(t, `{"code":"600036"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "巨潮资讯的证券列表里没有 SH600036")
	require.ErrorContains(t, err, "缓存一天")
	_, _, err = callFilingsJSON(t, `{"code":"$TEST"}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "SEC EDGAR 没有 $TEST")

	stubFilings(t, func(ctx context.Context, code string, req *cninfo.QueryRequest) (*cninfo.QueryResponse, error) {
		return nil, errors.New("HTTP 502")
	}, func(ctx context.Context, ticker string, query edgar.FilingQuery) (*edgar.FilingsResult, error) {
		return nil, errors.New("HTTP 503")
	})
	_, _, err = callFilingsJSON(t, `{"code":"600036"}`)
	require.False(t, IsInputError(err))
	require.ErrorContains(t, err, "巨潮资讯公告查询失败")
	_, _, err = callFilingsJSON(t, `{"code":"$TEST"}`)
	require.False(t, IsInputError(err))
	require.ErrorContains(t, err, "SEC 申报查询失败")
}
