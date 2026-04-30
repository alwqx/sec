package metal

import (
	"context"
	"encoding/json"
	"testing"

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
	resp, err := getAllDailyQuote(ctx)
	require.Nil(t, err)
	require.NotNil(t, resp)
	require.EqualValues(t, 6, len(resp.Time))
}

func TestQueryAu999(t *testing.T) {
	defer gock.Off()
	// https://www.sge.com.cn/graph/Dailyhq?instid=Au99.99
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").
		MatchParam("instid", "Au99.99").
		Reply(200).BodyString(defaultAu999DailyHQBody)
	ctx := context.TODO()

	// 1. 查询全部数据
	req := &QueryAu999Req{
		Start: "2016-12-19",
		End:   "2017-01-04",
	}
	resp, err := QueryAu999(ctx, req)
	require.Nil(t, err)
	require.NotNil(t, resp)
	require.EqualValues(t, 6, len(resp.Data))

	// 2. 查询全部数据
	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").
		MatchParam("instid", "Au99.99").
		Reply(200).BodyString(defaultAu999DailyHQBody)
	req = &QueryAu999Req{
		Start: "2017-01-03",
		End:   "2017-01-03",
	}
	resp, err = QueryAu999(ctx, req)
	require.Nil(t, err)
	require.NotNil(t, resp)
	require.EqualValues(t, 1, len(resp.Data))
}
