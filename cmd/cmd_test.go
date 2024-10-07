package cmd

import (
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
)

func TestHumanNum(t *testing.T) {
	require.EqualValues(t, " - ", humanNum(-1))
	require.EqualValues(t, " - ", humanNum(0.0))
	require.EqualValues(t, "1.00万", humanNum(10000))
	require.EqualValues(t, "10.09万", humanNum(100900))
	require.EqualValues(t, "1000.09亿", humanNum(100009000009))
}

func TestPrintSecs(t *testing.T) {
	// 1. empty
	printSecs(nil)
	printSecs([]sina.BasicSecurity{})

	// 2. common
	secs := []sina.BasicSecurity{
		{
			Name:   "龙芯中科",
			ExCode: "SH688047",
		},
		{
			Name:   "立讯精密",
			ExCode: "SZ002475",
		},
	}
	printSecs(secs)
}

func TestPrintDividends(t *testing.T) {
	// 1. nil or empty
	printDividends(nil)
	printDividends([]sina.Dividend{})

	dids := []sina.Dividend{
		{
			PublicDate:     "2024-07-04",
			RecordDate:     "2024-07-10",
			DividendedDate: "2024-07-11",
			Shares:         12,
			AddShares:      2.45,
			Bonus:          2.3,
		},
		{
			PublicDate:     "2023-07-04",
			RecordDate:     "2023-07-10",
			DividendedDate: "2023-07-11",
			Shares:         22,
			AddShares:      3.05,
			Bonus:          0.3,
		},
	}
	printDividends(dids)
}
