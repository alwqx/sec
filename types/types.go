package types

type SecurityType string

const (
	SecurityTypeFund  SecurityType = "fund"
	SecurityTypeStock SecurityType = "stock"

	// 交易所
	ExChangeSse    = "sse"    // 上交所
	ExChangeSzse   = "szse"   // 深交所
	ExChangeBse    = "bse"    // 北交所
	ExChangeHKex   = "hk"     // 香港交所
	ExChangeNyse   = "ny"     // 纽约交所
	ExChangeNasdaq = "nasdaq" // 纳斯达克
)

type SinaSecurityProfile struct {
	Code            string // 证券代码
	ExCode          string // 带交易所的证券代码
	Name            string
	EnName          string
	ExChange        string  // 交易所
	Price           float64 // 发行价格
	Date            string  // 上市时间
	WebSite         string  // 公司网址
	RegisterAddress string  // 注册地址
	WorkAddress     string  // 办公地址
	MainBussiness   string  // 主营业务
}

type Quote struct {
	TradeDate string // 交易日期 "2023-06-02"
	Code      string
	Name      string
	Current   float64 // 当前价格
	Open      float64
	Close     float64
	High      float64
	Low       float64
	Turn      float64 // 换手率
	Money     float64 // 成交金额 单位：元
	Volume    int64   // 成交数量 单位：股
}

type SinaQuote struct {
	TradeDate string // 交易日期 "2023-06-02"
	Code      string
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

type SinaProfile struct {
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
