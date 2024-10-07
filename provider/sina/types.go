package sina

import "github.com/alwqx/sec/types"

const (
	MultiSearchMaxNum = 8 // 单次支持最多查询的证券数量
)

// BasicSecurity 基本证券信息
type BasicSecurity struct {
	Name         string             // 证券名称
	SecurityType types.SecurityType // 证券类型：股票 stock，基金 fund
	Code         string             // 证券代码
	ExCode       string             // 证券带交易所编码 SH600036
	ExChange     string             // 交易所
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
	MainBussiness   string  // 主营业务
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
	Current         float64 // 当前价格
	PB              float64 // 市净率
	PeTTM           float64 // 市盈率TTM
	MarketCap       float64 // 总市值
	TradedMarketCap float64 // 流通市值
}

// SecurityQuote 证券行情
type SecurityQuote struct {
	TradeDate string // 交易日期 "2023-06-02"
	Code      string
	ExCode    string
	Name      string
	Current   float64 // 当前价格
	Open      float64
	YClose    float64 // 上个交易日收盘价
	High      float64
	Low       float64
	Volume    float64 // 成交金额 单位：元
	TurnOver  int64   // 成交数量 单位：股
	Time      string  // 交易日期 "2023-06-02"
}

// 分红送转信息
type Dividend struct {
	PublicDate     string  // 公告日期
	RecordDate     string  // 登记日期
	DividendedDate string  // 除息日期
	Shares         float64 // 送股数量
	AddShares      float64 // 转增股票数量
	Bonus          float64 // 红利
}
