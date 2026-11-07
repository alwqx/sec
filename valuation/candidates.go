package valuation

import (
	"strings"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
)

// Candidates 返回按 key 搜索到的证券里要依次尝试估值的几只，前一只没有估值数据（ErrNoData）时试下一只，sec valuation 和 sec mcp 共用。
// 同一数字可能同时是指数和股票（000001 是上证指数和平安银行），所以纯数字代码要试完所有沪深北结果。
// 其他情况只取搜索的第一条：00700 的第二条是模塑科技（000700），不能因为腾讯不支持就换成别的公司。
func Candidates(secs []*sina.BasicSecurity, key string) []*sina.BasicSecurity {
	same := []*sina.BasicSecurity{}
	for _, sec := range secs {
		if strings.EqualFold(sec.ExCode, key) {
			return []*sina.BasicSecurity{sec}
		}
		if sec.Code == key && types.IsACode(sec.ExCode) {
			same = append(same, sec)
		}
	}
	if len(same) > 0 {
		return same
	}
	return secs[:1]
}
