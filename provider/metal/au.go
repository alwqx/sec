package metal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const sgeDailyQuoteURL = "https://www.sge.com.cn/graph/Dailyhq"

// Instrument 是上海黄金交易所的一个交易品种。白银按千克报价，黄金和铂金按克报价。
type Instrument struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Unit string `json:"unit"`
	// Alias 是不带点的简写。
	Alias string `json:"alias"`
}

// Instruments 是可以查询日行情的品种，第一个是默认品种。日行情从 2016 年 12 月起。
// 上海黄金交易所还有 Au(T+D)、Ag(T+D)、Pt99.95 等品种，接口相同，需要时加在这里即可。
var Instruments = []Instrument{
	{"Au99.99", "黄金现货", "元/克", "au999"},
	{"Ag99.99", "白银现货", "元/千克", "ag999"},
}

// FindInstrument 按代码查找品种，不区分大小写；也接受不带点的写法，比如 ag999。
func FindInstrument(id string) (Instrument, error) {
	key := strings.ToLower(strings.TrimSpace(id))
	if key == "" {
		return Instruments[0], nil
	}
	for _, inst := range Instruments {
		if key == strings.ToLower(inst.ID) || key == inst.Alias {
			return inst, nil
		}
	}
	return Instrument{}, fmt.Errorf("不支持的品种 %s，可选：%s", id, InstrumentList())
}

// InstrumentList 列出全部品种和别名，用于报错和命令行帮助。
func InstrumentList() string {
	names := make([]string, 0, len(Instruments))
	for _, inst := range Instruments {
		names = append(names, inst.ID+"（"+inst.Alias+"）")
	}
	return strings.Join(names, "、")
}

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

// getAllDailyQuote 获取 https://www.sge.com.cn/sjzx/mrhq 中一个品种的全部日行情数据
func getAllDailyQuote(ctx context.Context, instID string, fresh bool) (*dailyHQResp, error) {
	link := sgeDailyQuoteURL + "?instid=" + url.QueryEscape(instID)
	var items []*DailyHQItem
	_, err := fetch(ctx, link, fresh, func(body []byte) error {
		var tmp *innerDailyHQResp
		if err := json.Unmarshal(body, &tmp); err != nil {
			return err
		}
		var err error
		items, err = parseDailyHQ(tmp)
		return err
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed request", "url", link, "error", err)
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

// QueryReq 请求结构体，Inst 为空时查询 Au99.99。Fresh 为 true 时不用缓存。
type QueryReq struct {
	Inst       string
	Start, End string
	Fresh      bool
}

// QueryResp 返回数据结构体
type QueryResp struct {
	Instrument Instrument
	Data       []*DailyHQItem
}

// Query 根据时间范围查询一个品种的日行情。昨收在区间筛选前计算，区间第一天也有涨跌。
func Query(ctx context.Context, req *QueryReq) (*QueryResp, error) {
	if req == nil {
		return nil, errors.New("req is new")
	}
	// 校验时间
	var (
		start, end time.Time
		err        error
	)
	inst, err := FindInstrument(req.Inst)
	if err != nil {
		return nil, err
	}
	start, err = time.Parse(utils.LayoutYYMMDD, req.Start)
	if err != nil {
		return nil, err
	}
	end, err = time.Parse(utils.LayoutYYMMDD, req.End)
	if err != nil {
		return nil, err
	}

	res, err := getAllDailyQuote(ctx, inst.ID, req.Fresh)
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

	resp := &QueryResp{
		Instrument: inst,
		Data:       data,
	}

	return resp, nil
}
