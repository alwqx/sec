package sina

import (
	"fmt"

	"github.com/alwqx/sec/types"
)

// BasicSecurity 基本证券信息
type BasicSecurity struct {
	Name         string             `json:"name"`          // 证券名称
	SecurityType types.SecurityType `json:"security_type"` // 证券类型：股票 stock，基金 fund
	Code         string             `json:"code"`          // 证券代码
	ExCode       string             `json:"ex_code"`       // 证券带交易所编码 SH600036
	ExChange     string             `json:"exchange"`      // 交易所
}

// BasicCorp 公司基本信息
type BasicCorp struct {
	Code            string // 证券代码
	ExCode          string // 带交易所的证券代码
	HistoryName     string // 简称历史
	Name            string
	EnName          string
	ExChange        string  // 交易所
	Price           float64 // 发行价格
	Date            string  // 上市时间
	WebSite         string  // 公司网址
	RegisterAddress string  // 注册地址
	BusinessAddress string  // 办公地址
	MainBusiness    string  // 主营业务
}

type CorpProfile struct {
	Code            string
	ExCode          string
	Name            string  // 公司名称
	HistoryName     string  // 简称历史
	ListingPrice    float64 // 发行价格
	ListingDate     string  // 上市时间
	Category        string  // 行业分类
	WebSite         string  // 公司网址
	RegisterAddress string  // 注册地址
	BusinessAddress string  // 办公地址
	MainBusiness    string  // 主营业务
	Description     string  // 公司简介，目前只有港股有
	Current         float64 // 当前价格，没有成交价时是昨收
	PriceIsYClose   bool    // Current 是否为昨收
	PB              float64 // 市净率
	PeTTM           float64 // 市盈率TTM
	MarketCap       float64 // 总市值
	TradedMarketCap float64 // 流通市值
	HKTrust         bool    // 港股交易所买卖基金、房地产投资信托等，不是公司，没有公司资料
}

// SecurityQuote 证券行情
type SecurityQuote struct {
	TradeDate string  `json:"trade_date"` // 交易日期 "2023-06-02"
	Code      string  `json:"code"`
	ExCode    string  `json:"ex_code"`
	Name      string  `json:"name"`
	Current   float64 `json:"current"`  // 当前价格
	Open      float64 `json:"open"`     // 开盘价
	YClose    float64 `json:"yclose"`   // 上个交易日收盘价
	High      float64 `json:"high"`     // 最高价
	Low       float64 `json:"low"`      // 最低价
	Volume    float64 `json:"volume"`   // 成交金额 单位：元
	TurnOver  int64   `json:"turnover"` // 成交数量 单位：股
	Time      string  `json:"time"`     // 行情时间 "15:00:01"
	// TradeDate 和 Time 是交易所当地时间，Timezone 是它的 IANA 时区：A 股 Asia/Shanghai、港股 Asia/Hong_Kong、美股 America/New_York。
	Timezone string `json:"timezone"`
}

// EffectivePrice 返回可用的价格。没有成交价（如开盘前）时用昨收，并把第二个返回值设为 true；都没有时返回 0。
func (q *SecurityQuote) EffectivePrice() (float64, bool) {
	if q.Current > 0 {
		return q.Current, false
	}
	if q.YClose > 0 {
		return q.YClose, true
	}
	return 0, false
}

// 分红送转信息
type Dividend struct {
	PublicDate     string  `json:"public_date"`     // 公告日期
	RecordDate     string  `json:"record_date"`     // 登记日期
	DividendedDate string  `json:"dividended_date"` // 除息日期
	Shares         float64 `json:"shares"`          // 送股数量
	AddShares      float64 `json:"add_shares"`      // 转增股票数量
	Bonus          float64 `json:"bonus"`           // 红利
}

// PriceText 返回当前价格的文字，用昨收时加以说明。
func (p *CorpProfile) PriceText() string {
	if p.PriceIsYClose {
		return fmt.Sprintf("%.2f（昨收，当前无成交价）", p.Current)
	}
	return fmt.Sprintf("%.2f", p.Current)
}
