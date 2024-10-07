package quote

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
)

func TestPrintQuote(t *testing.T) {
	// 涨
	quote1 := &sina.SecurityQuote{
		Name:      "龙芯中科",
		Code:      "688047",
		ExCode:    "SH688047",
		TradeDate: "2024-09-30",
		Time:      "15:00:01",
		Current:   119.62,
		YClose:    99.68,
		Open:      106,
		High:      119.62,
		Low:       104.5,
		Volume:    938310086.000,
		TurnOver:  8256723,
	}
	fmt.Println("上涨")
	printQuote([]*sina.SecurityQuote{quote1})

	// 跌
	quote2 := &sina.SecurityQuote{
		Name:      "龙芯中科",
		Code:      "688047",
		ExCode:    "SH688047",
		TradeDate: "2024-09-30",
		Time:      "15:00:01",
		Current:   119.62,
		YClose:    199.68,
		Open:      106,
		High:      119.62,
		Low:       104.5,
		Volume:    938310086.000,
		TurnOver:  8256723,
	}
	fmt.Println("下跌")
	printQuote([]*sina.SecurityQuote{quote2})

	// 平
	quote3 := &sina.SecurityQuote{
		Name:      "龙芯中科",
		Code:      "688047",
		ExCode:    "SH688047",
		TradeDate: "2024-09-30",
		Time:      "15:00:01",
		Current:   119.62,
		YClose:    119.62,
		Open:      106,
		High:      119.62,
		Low:       104.5,
		Volume:    938310086.000,
		TurnOver:  8256723,
	}
	fmt.Println("不变")
	printQuote([]*sina.SecurityQuote{quote1, quote2, quote3})

	// 3. 从 string 解析
	str := `[
	{
		"TradeDate": "2024-09-30",
		"Code": "688047",
		"ExCode": "SH688047",
		"Name": "龙芯中科",
		"Current": 119.62,
		"Open": 106,
		"YClose": 99.68,
		"High": 119.62,
		"Low": 104.5,
		"Volume": 938310086,
		"TurnOver": 8256723,
		"Time": "15:00:01"
	},
	{
		"TradeDate": "2024-09-30",
		"Code": "002475",
		"ExCode": "SZ002475",
		"Name": "立讯精密",
		"Current": 43.46,
		"Open": 42,
		"YClose": 40.48,
		"High": 43.95,
		"Low": 41.01,
		"Volume": 6841666995.79,
		"TurnOver": 160489237,
		"Time": "15:00:00"
	}
]`
	var quotes []*sina.SecurityQuote
	err := json.Unmarshal([]byte(str), &quotes)
	require.Nil(t, err)
	printQuote(quotes)
}
