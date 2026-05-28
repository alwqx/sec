package eastmoney

import (
	"fmt"
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
}

type GetQuoteHistoryReq struct {
	Code       string
	MarketCode int        // 市场 1 上证，2 深证
	FQT        FuQuanType // 复权类型 0不复权 1前复权 2后复权，默认不复权
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
