package metal

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// defaultAu999DailyHQBody 日行情默认 body，用于测试，不要轻易改动
var defaultAu999DailyHQBody = `{
    "time": [
        [
            "2016-12-19",
            262.45,
            262.76,
            262.02,
            263.5
        ],
        [
            "2016-12-20",
            262.88,
            262.06,
            261.42,
            263.7
        ],
        [
            "2016-12-21",
            262.4,
            260.97,
            258.6,
            262.65
        ],
        [
            "2016-12-30",
            262.84,
            263.9,
            261,
            265.8
        ],
        [
            "2017-01-03",
            264.04,
            263.65,
            258,
            265.2
        ],
        [
            "2017-01-04",
            263.99,
            264.87,
            263.2,
            265
        ]
    ]
}`

func TestParseDailyHQ(t *testing.T) {
	// 1. nil
	res, err := parseDailyHQ(nil)
	require.NotNil(t, err)
	require.Nil(t, res)

	// 2. empty
	res, err = parseDailyHQ(&innerDailyHQResp{})
	require.Nil(t, err)
	require.NotNil(t, res)

	// 3. 1 record
	resp3 := &innerDailyHQResp{
		Time: [][]interface{}{
			{
				"2017-01-04",
				263.99,
				264.87,
				263.2,
				265,
			},
		},
	}
	res, err = parseDailyHQ(resp3)
	require.Nil(t, err)
	require.NotNil(t, res)
	require.EqualValues(t, 1, len(res))
	require.EqualValues(t, "2017-01-04", res[0].Date)
	require.EqualValues(t, 265, res[0].High)
	require.EqualValues(t, -1, res[0].YClose)

	// 4. multi record
	var resp4 *innerDailyHQResp
	err = json.Unmarshal([]byte(defaultAu999DailyHQBody), &resp4)
	require.Nil(t, err)
	res, err = parseDailyHQ(resp4)
	require.Nil(t, err)
	require.NotNil(t, res)
	require.EqualValues(t, 6, len(res))
	require.EqualValues(t, "2016-12-19", res[0].Date)
	require.EqualValues(t, 263.5, res[0].High)
	require.EqualValues(t, -1, res[0].YClose)

	require.EqualValues(t, "2017-01-04", res[5].Date)
	require.EqualValues(t, 265, res[5].High)
	require.EqualValues(t, 263.65, res[5].YClose)
	require.EqualValues(t, 263.9, res[4].YClose)

	parseDailyHQ(nil)
}

func TestGetAllDailyQuote(t *testing.T) {
	defer gock.Off()
	// https://www.sge.com.cn/graph/Dailyhq?instid=Au99.99
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").
		MatchParam("instid", "Au99.99").
		Reply(200).BodyString(defaultAu999DailyHQBody)
	ctx := context.TODO()
	resp, err := getAllDailyQuote(ctx, "Au99.99", false)
	require.Nil(t, err)
	require.NotNil(t, resp)
	require.EqualValues(t, 6, len(resp.Time))
}

func TestQuery(t *testing.T) {
	defer gock.Off()
	// https://www.sge.com.cn/graph/Dailyhq?instid=Au99.99
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").
		MatchParam("instid", "Au99.99").
		Reply(200).BodyString(defaultAu999DailyHQBody)
	ctx := context.TODO()

	// 1. 查询全部数据
	req := &QueryReq{
		Start: "2016-12-19",
		End:   "2017-01-04",
	}
	resp, err := Query(ctx, req)
	require.Nil(t, err)
	require.NotNil(t, resp)
	require.EqualValues(t, 6, len(resp.Data))

	// 2. 查询全部数据
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").
		MatchParam("instid", "Au99.99").
		Reply(200).BodyString(defaultAu999DailyHQBody)
	req = &QueryReq{
		Start: "2017-01-03",
		End:   "2017-01-03",
	}
	resp, err = Query(ctx, req)
	require.Nil(t, err)
	require.NotNil(t, resp)
	require.EqualValues(t, 1, len(resp.Data))
}

func TestDailyQuoteUpstreamStatus(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").Reply(502).BodyString("<html>")
	_, err := getAllDailyQuote(context.Background(), "Au99.99", false)
	require.ErrorContains(t, err, "HTTP 502")
}

// 返回的品种带名称和单位。
func TestQueryInstrument(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").
		MatchParam("instid", regexp.QuoteMeta("Ag99.99")).
		Reply(200).BodyString(`{"time":[["2026-11-02",11800,11900,11700,12000],["2026-11-03",11900,11655,11600,12010]]}`)
	resp, err := Query(context.Background(), &QueryReq{Inst: "ag999", Start: "2026-11-01", End: "2026-11-03"})
	require.NoError(t, err)
	require.Equal(t, "Ag99.99", resp.Instrument.ID)
	require.Equal(t, "元/千克", resp.Instrument.Unit)
	require.Len(t, resp.Data, 2)
	require.InDelta(t, -245, resp.Data[1].Change, 1e-9)
	require.True(t, gock.IsDone())
}

func TestFindInstrument(t *testing.T) {
	for id, want := range map[string]string{"": "Au99.99", "AU99.99": "Au99.99", "au999": "Au99.99", "ag99.99": "Ag99.99", " ag999 ": "Ag99.99"} {
		inst, err := FindInstrument(id)
		require.NoError(t, err, id)
		require.Equal(t, want, inst.ID, id)
	}
	_, err := FindInstrument("cu")
	require.ErrorContains(t, err, "不支持的品种 cu")
	require.ErrorContains(t, err, "Ag99.99（ag999）")
}

func TestBlockedAndCache(t *testing.T) {
	defer gock.Off()
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").Reply(403).BodyString("<title>您的访问请求可能对网站造成安全威胁，请求已被阻断。</title>")
	_, err := Query(context.Background(), &QueryReq{Start: "2016-12-01", End: "2017-01-31"})
	require.ErrorIs(t, err, ErrBlocked)

	CacheTTL = time.Minute
	t.Cleanup(func() { CacheTTL = 0; resetCache() })
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").MatchParam("instid", "Ag99.99").Times(2).Reply(200).BodyString(defaultAu999DailyHQBody)
	for range 3 {
		resp, err := Query(context.Background(), &QueryReq{Inst: "Ag99.99", Start: "2016-12-01", End: "2017-01-31"})
		require.NoError(t, err)
		require.Len(t, resp.Data, 6)
	}
	require.False(t, gock.IsDone(), "缓存期内只请求一次")
	_, err = Query(context.Background(), &QueryReq{Inst: "Ag99.99", Start: "2016-12-01", End: "2017-01-31", Fresh: true})
	require.NoError(t, err)
	require.True(t, gock.IsDone(), "Fresh 时重新请求")
}

// 拦截页的状态码不一定是 403；解析失败的正文不缓存，下一次重新请求。
func TestBlockedPageAndBadBodyNotCached(t *testing.T) {
	defer gock.Off()
	CacheTTL = time.Minute
	t.Cleanup(func() { CacheTTL = 0; resetCache() })
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").Reply(200).BodyString("<title>您的访问请求可能对网站造成安全威胁，请求已被阻断。</title>")
	_, err := Query(context.Background(), &QueryReq{Start: "2016-12-01", End: "2017-01-31"})
	require.ErrorIs(t, err, ErrBlocked)

	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").Reply(200).BodyString(`{"time":[["2016-12-19",1`)
	_, err = Query(context.Background(), &QueryReq{Start: "2016-12-01", End: "2017-01-31"})
	require.Error(t, err)
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").Reply(200).BodyString(defaultAu999DailyHQBody)
	resp, err := Query(context.Background(), &QueryReq{Start: "2016-12-01", End: "2017-01-31"})
	require.NoError(t, err)
	require.Len(t, resp.Data, 6)
}
