package metal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/alwqx/sec/utils"
)

var (
	sgeAu999DailyQuoteUrl = "https://www.sge.com.cn/graph/Dailyhq?instid=Au99.99"
)

// DailyHQItem 日行情
type DailyHQItem struct {
	Date     string
	DateTime time.Time // 对 Date 解析后的 time 实例，用于比较时间等操作
	Open     float64
	Close    float64
	Low      float64
	High     float64
	// 昨日收盘价，接口中默认没有这个值，如果有多个记录，可以按照时间排序后
	// 人工计算填充 YClose/Change/ChangeRate 这个值
	YClose     float64
	Change     float64
	ChangeRate float64
}

// dailyHQResp 日行情
type dailyHQResp struct {
	Time []*DailyHQItem
}

// innerDailyHQResp 内部返回数据结构体
type innerDailyHQResp struct {
	Time [][]interface{} `json:"time"`
}

// getAllDailyQuote 获取 https://www.sge.com.cn/sjzx/mrhq 中的全部日行情数据
func getAllDailyQuote(ctx context.Context) (*dailyHQResp, error) {
	resp, err := utils.MakeRequest(ctx, http.MethodGet, sgeAu999DailyQuoteUrl, nil, nil, 0)
	if err != nil {
		slog.ErrorContext(ctx, "failed request", "url", sgeAu999DailyQuoteUrl)
		return nil, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var tmp *innerDailyHQResp
	err = json.Unmarshal(body, &tmp)
	if err != nil {
		return nil, err
	}

	items, err := parseDailyHQ(tmp)
	if err != nil {
		return nil, err
	}

	return &dailyHQResp{Time: items}, nil
}

// parseDailyHQ 解析内部返回的 body 到实际结构体
func parseDailyHQ(resp *innerDailyHQResp) ([]*DailyHQItem, error) {
	if resp == nil {
		return nil, errors.New("resp is nil")
	}

	res := make([]*DailyHQItem, 0, len(resp.Time))
	var (
		ok  bool
		err error
	)
	for i := range resp.Time {
		hq := resp.Time[i]
		if len(hq) != 5 {
			slog.Error("parseDailyHQ: invalid resp", "resp", *resp)
			return nil, errors.New("invalid resp")
		}

		item := new(DailyHQItem)
		item.Date, ok = hq[0].(string)
		if !ok {
			slog.Error("parseDailyHQ: invalid date", "data", hq)
			return nil, errors.New("invalid data")
		}

		item.DateTime, err = time.Parse(utils.LayoutYYMMDD, item.Date)
		if err != nil {
			return nil, err
		}

		item.Open, ok = hq[1].(float64)
		if !ok {
			slog.Error("parseDailyHQ: invalid open", "data", hq)
			return nil, errors.New("invalid open")
		}
		item.Close, ok = hq[2].(float64)
		if !ok {
			slog.Error("parseDailyHQ: invalid close", "data", hq)
			return nil, errors.New("invalid close")
		}

		item.Low, ok = hq[3].(float64)
		if !ok {
			slog.Error("parseDailyHQ: invalid low", "data", hq)
			return nil, errors.New("invalid low")
		}
		item.High, ok = hq[4].(float64)
		if !ok {
			highInt, ok := hq[4].(int)
			if !ok {
				slog.Error("parseDailyHQ: invalid high", "data", hq)
				return nil, errors.New("invalid high")
			} else {
				item.High = float64(highInt)
			}
		}

		res = append(res, item)
	}

	// 对数据按照时间进行升序排列，这样即可顺序填充 YClose
	sort.Slice(res, func(i, j int) bool {
		return res[i].DateTime.Before(res[j].DateTime)
	})
	// 填充 YClose
	num := len(res)
	for i := range num {
		if i == 0 {
			// i=0时第一条记录无法填充 YClose，默认-1
			res[i].YClose = -1
			res[i].ChangeRate = 0
		} else {
			res[i].YClose = res[i-1].Close
			res[i].Change = res[i].Close - res[i].YClose
			res[i].ChangeRate = res[i].Change / res[i].YClose
		}
	}

	return res, nil
}

// QueryAu999Req 请求结构体
type QueryAu999Req struct {
	Start, End string
}

// QueryAu999Resp 返回数据结构体
type QueryAu999Resp struct {
	Data []*DailyHQItem
}

// QueryAu999 根据时间范围查询数据
func QueryAu999(ctx context.Context, req *QueryAu999Req) (*QueryAu999Resp, error) {
	if req == nil {
		return nil, errors.New("req is new")
	}
	// 校验时间
	var (
		start, end time.Time
		err        error
	)
	start, err = time.Parse(utils.LayoutYYMMDD, req.Start)
	if err != nil {
		return nil, err
	}
	end, err = time.Parse(utils.LayoutYYMMDD, req.End)
	if err != nil {
		return nil, err
	}

	res, err := getAllDailyQuote(ctx)
	if err != nil {
		return nil, err
	}
	data := make([]*DailyHQItem, 0)
	for i := range res.Time {
		item := res.Time[i]
		if start.After(item.DateTime) || end.Before(item.DateTime) {
			continue
		}
		data = append(data, item)
	}

	resp := &QueryAu999Resp{
		Data: data,
	}

	return resp, nil
}
