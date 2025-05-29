package types

import (
	"encoding/json"
	"fmt"
	"strings"
)

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

func JSONify(data interface{}) {
	b, _ := json.MarshalIndent(data, "", "\t")
	fmt.Println(string(b))
}

func HumanNum(cap float64) (res string) {
	if cap <= 0.0 {
		res = " - "
	} else if cap > 100_000_000.0 {
		res = fmt.Sprintf("%-.2f亿", cap/100_000_000.0)
	} else {
		res = fmt.Sprintf("%-.2f万", cap/10_000.0)
	}
	return
}

// IsACode 判断证券代码是否是 A 股
func IsACode(exCode string) (res bool) {
	lowCode := strings.ToLower(exCode)
	res = strings.HasPrefix(lowCode, "sh") || strings.HasPrefix(lowCode, "sz") || strings.HasPrefix(lowCode, "bj")
	return
}

// IsHCode 判断证券代码是否是 h 股
func IsHCode(exCode string) (res bool) {
	lowCode := strings.ToLower(exCode)
	res = strings.HasPrefix(lowCode, "hk")
	return
}
