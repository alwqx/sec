package sina

import "github.com/alwqx/sec/types"

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
