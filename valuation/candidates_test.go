package valuation

import (
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
)

func TestCandidates(t *testing.T) {
	index := &sina.BasicSecurity{Code: "000001", ExCode: "SH000001"}
	stock := &sina.BasicSecurity{Code: "000001", ExCode: "SZ000001"}
	fund := &sina.BasicSecurity{Code: "000001", ExCode: "OF000001"}
	require.Equal(t, []*sina.BasicSecurity{index, stock}, Candidates([]*sina.BasicSecurity{index, stock, fund}, "000001"), "指数没有数据时继续试股票")
	require.Equal(t, []*sina.BasicSecurity{index}, Candidates([]*sina.BasicSecurity{index, stock}, "sh000001"), "带交易所时只查它")

	tencent := &sina.BasicSecurity{Code: "00700", ExCode: "HK00700"}
	other := &sina.BasicSecurity{Code: "000700", ExCode: "SZ000700"}
	require.Equal(t, []*sina.BasicSecurity{tencent}, Candidates([]*sina.BasicSecurity{tencent, other}, "00700"), "不能换成别的公司")

	bank := &sina.BasicSecurity{Code: "600036", ExCode: "SH600036"}
	require.Equal(t, []*sina.BasicSecurity{bank}, Candidates([]*sina.BasicSecurity{bank, {Code: "601398", ExCode: "SH601398"}}, "招商银行"))
}
