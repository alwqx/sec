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

type InfoOptions struct {
	Code     string // 证券代码 600036
	ExCode   string // 带交易所前缀的证券代码  SH600036
	Dividend bool   // 是否显示分红送转信息，true显示，false不显示
}
