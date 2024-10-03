package sina

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
