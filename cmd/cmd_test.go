package cmd

import (
	"testing"

	"github.com/alwqx/sec/provider/sina"
)

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

func TestVersionHandler(t *testing.T) {
	// version.Version=
	versionHandler(nil, nil)
}
