package chinabond

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestGetChinaBondOfficialSamples(t *testing.T) {
	defer gock.Off()
	for _, tc := range []struct {
		name, start, end string
		count            int
	}{
		{"treasury-20060301-20060307", "2006-03-01", "2006-03-07", 5},
		{"treasury-20261019-20261026", "2026-10-19", "2026-10-26", 6},
	} {
		raw, err := os.ReadFile("testdata/" + tc.name + ".json")
		require.NoError(t, err)
		gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").MatchParam("startDate", tc.start).MatchParam("endDate", tc.end).Reply(200).BodyString(string(raw))
		result, err := GetChinaBond(context.Background(), &GetChinaBondReq{tc.start, tc.end})
		require.NoError(t, err)
		require.Len(t, result.HeList, tc.count)
		require.Equal(t, tc.end, result.HeList[0].Date)
		require.Equal(t, tc.start, result.HeList[tc.count-1].Date)
	}
	require.True(t, gock.IsDone())
}

func TestGetChinaBondInvalidResponses(t *testing.T) {
	defer gock.Off()
	valid := `{"flag":"0","heList":[{"workTime":"2023-06-01","oneYear":"0","twoYear":null,"fiveYear":"","tenYear":"2.70"}]}`
	for _, body := range []string{"<html>busy</html>", "null", "{}", `{"flag":"1","heList":[]}`, `{"flag":"0","heList":[null]}`,
		strings.Replace(valid, "2023-06-01", "2023-02-30", 1), strings.Replace(valid, "2023-06-01", "2023-06-03", 1),
		strings.Replace(valid, `"oneYear":"0"`, `"oneYear":"NaN"`, 1), strings.Replace(valid, `"oneYear":"0"`, `"oneYear":"Inf"`, 1),
		strings.Replace(valid, `"oneYear":"0"`, `"oneYear":"invalid"`, 1),
		`{"flag":"0","heList":[{"workTime":"2023-06-01"},{"workTime":"2023-06-01"}]}`,
	} {
		gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").Reply(200).BodyString(body)
		result, err := GetChinaBond(context.Background(), &GetChinaBondReq{"2023-06-01", "2023-06-02"})
		require.Error(t, err)
		require.Nil(t, result)
	}
	for _, body := range []string{valid, `{"flag":"0","heList":[]}`} {
		gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").Reply(200).BodyString(body)
		_, err := GetChinaBond(context.Background(), &GetChinaBondReq{"2023-06-01", "2023-06-02"})
		require.NoError(t, err)
	}
	gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").Reply(503)
	_, err := GetChinaBond(context.Background(), &GetChinaBondReq{"2023-06-01", "2023-06-02"})
	require.ErrorContains(t, err, "503")
	require.True(t, gock.IsDone())
}

func TestGetChinaBond(t *testing.T) {
	defer gock.Off()
	response, err := GetChinaBond(context.Background(), nil)
	require.Error(t, err)
	require.Nil(t, response)
	for _, req := range []*GetChinaBondReq{{"invalid", "2023-06-02"}, {"2023-06-01", "invalid"}, {"2023-06-02", "2023-06-01"}, {"2023-01-01", "2024-01-01"}} {
		_, err := GetChinaBond(context.Background(), req)
		require.Error(t, err)
	}
	// 恢复PR #66原测试的请求日期、参数和字符串收益率。
	body := `{"heList":[{"workTime":"2023-06-02","oneYear":"1.97","twoYear":"2.19","fiveYear":"2.45","tenYear":"2.70","fifteenYear":"","twentyYear":null,"qxmc":"中债国债收益率曲线"}],"flag":"0"}`
	gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").MatchParams(map[string]string{"startDate": "2023-06-01", "endDate": "2023-06-02", "gjqx": "0", "locale": "cn_ZH", "qxmc": "1"}).MatchHeader("Accept", `application/json, text/javascript, \*/\*; q=0.01`).Reply(200).BodyString(body)
	response, err = GetChinaBond(context.Background(), &GetChinaBondReq{"2023-06-01", "2023-06-02"})
	require.NoError(t, err)
	require.Equal(t, "0", response.Flag)
	require.Len(t, response.HeList, 1)
	require.Equal(t, "1.97", response.HeList[0].OneYear)
	require.Empty(t, response.HeList[0].FifteenYear)
	require.Empty(t, response.HeList[0].TwentyYear)
	require.True(t, gock.IsDone())
}
