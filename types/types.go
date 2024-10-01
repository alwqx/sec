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
	ExChangeNasdaq = "dasdap" // 纳斯达克
)

type SinaSearchResult struct {
	Name         string       // 股票名称
	SecurityType SecurityType // 证券类型：股票 stock，基金 fund
	Code         string       // 股票代码
	ExCode       string       // 股票带交易所编码 SH600036
	ExChange     string       // 交易所
}
