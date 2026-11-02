package eastmoney

import (
	"fmt"
	"strings"
	"time"
)

const (
	TimeYYMMDD = "20060102"
)

type MarketType int

func (m MarketType) String() string {
	var res string
	switch m {
	case MarketTypeSzSe:
		res = "SZ"
	case MarketTypeSse:
		res = "SH"
	case MarketTypeNasdaq:
		res = "$"
	case MarketTypeHK:
		return "HK"
	default:
		res = fmt.Sprintf("unknown %d", m)
	}

	return res
}

type FuQuanType int

// Quote 基本行情
type Quote struct {
	Date       time.Time  `json:"date"`        // 交易日期
	Code       string     `json:"code"`        // 证券代码
	Name       string     `json:"name"`        // 证券名称
	Market     MarketType `json:"market"`      // 证券市场
	Open       float64    `json:"open"`        // 开盘价
	Close      float64    `json:"close"`       // 收盘价
	High       float64    `json:"high"`        // 最高价
	Low        float64    `json:"low"`         // 最低价
	Volume     int64      `json:"volume"`      // 成交量
	TurnOver   float64    `json:"turn_over"`   // 成交额
	Amplitude  float64    `json:"amplitude"`   // 振幅
	ChangeRate float64    `json:"change_rate"` // 涨跌幅
	Change     float64    `json:"change"`      // 涨跌额
	Velocity   float64    `json:"velocity"`    // TurnOver Rate 换手率
	Fqt        int        `json:"fqt"`         // 复权类型
	NoTurnover bool       `json:"-"`           // 数据源没有成交额和换手率，不能把 0 当成真实值
	NoChange   bool       `json:"-"`           // 没有前一日收盘价，涨跌和振幅未知，不能把 0 当成真实值
}

type GetQuoteHistoryReq struct {
	Code       string
	MarketCode int        // 市场 1 上证，2 深证
	FQT        FuQuanType // 复权类型 0不复权 1前复权 2后复权，默认不复权
	Freq       Freq       // K 线周期，默认日线
	Begin      string     // 开始时间 19000101 格式
	End        string     // 结束时间 20500101 格式
}

// QuoteHistoryResp 东方财富 K 线历史接口返回数据结构
type QuoteHistoryResp struct {
	Rc     int               `json:"rc"`
	Rt     int               `json:"rt"`
	Svr    int64             `json:"svr"`
	Lt     int               `json:"lt"`
	Full   int               `json:"full"`
	Dlmkts string            `json:"dlmkts"`
	Data   *QuoteHistoryData `json:"data"`
}

// QuoteHistoryData 东方财富 K 线历史接口中 data 字段结构体
type QuoteHistoryData struct {
	Code       string     `json:"code"`
	Market     MarketType `json:"market"`
	Name       string     `json:"name"`
	Decimal    int        `json:"decimal"` // 小数点精确度
	Dktotal    int        `json:"dktotal"` // 总数据条数
	PreKPrice  float64    `json:"preKPrice"`
	PrePrice   float64    `json:"prePrice"`
	QtMiscType int        `json:"qtMiscType"`
	Version    int        `json:"version"`
	Klines     []string   `json:"klines"`
}

// KLineQuote K 线结构体
type KLineQuote struct {
	Date       time.Time
	Open       float64
	Close      float64
	High       float64
	Low        float64
	Volume     int64   // 成交量
	TurnOver   float64 // 成交额
	Amplitude  float64 // 振幅
	ChangeRate float64 // 涨跌幅
	Change     float64 // 涨跌额
	Velocity   float64 // TurnOver Rate 换手率
}

// Freq 是 K 线周期。
type Freq string

const (
	FreqDay   Freq = "day"
	FreqWeek  Freq = "week"
	FreqMonth Freq = "month"
)

// ParseFreq 解析周期，空字符串是日线。
func ParseFreq(text string) (Freq, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "d", "day":
		return FreqDay, nil
	case "w", "week":
		return FreqWeek, nil
	case "m", "month":
		return FreqMonth, nil
	}
	return FreqDay, fmt.Errorf("不支持的周期 %s，可选 day、week、month", text)
}

// BarDays 返回一根 K 线大约跨越的自然日数，用来换算查询区间。
func (f Freq) BarDays() int {
	switch f {
	case FreqWeek:
		return 7
	case FreqMonth:
		return 31
	}
	return 1
}

// Label 返回周期的中文名称。
func (f Freq) Label() string {
	switch f {
	case FreqWeek:
		return "周"
	case FreqMonth:
		return "月"
	}
	return "日"
}

// klt 返回东方财富接口的周期参数。
func (f Freq) klt() string {
	switch f {
	case FreqWeek:
		return "102"
	case FreqMonth:
		return "103"
	}
	return "101"
}
