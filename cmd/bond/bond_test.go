package bond

import (
	"fmt"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/bond"
	"github.com/alwqx/sec/utils"
)

func TestPrintBondYield(t *testing.T) {
	// 1. nil data
	printBondYield(nil)

	// 2. empty data
	printBondYield([]*bond.BondYieldItem{})

	// 3. first trading day (no previous data, YClose = -1)
	date1, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-01")
	fmt.Println("first day (no previous):")
	printBondYield([]*bond.BondYieldItem{
		{
			Date:     "2026-05-01",
			DateTime: date1,
			BC1Month: 3.71,
			BC3Month: 3.68,
			BC6Month: 3.71,
			BC5Year:  4.02,
			BC10Year: 4.39,
			YClose:   -1,
		},
	})

	// 4. yield up (red)
	date2, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-04")
	fmt.Println("yield up (red):")
	printBondYield([]*bond.BondYieldItem{
		{
			Date:       "2026-05-04",
			DateTime:   date2,
			BC1Month:   3.71,
			BC3Month:   3.70,
			BC6Month:   3.76,
			BC5Year:    4.08,
			BC10Year:   4.45,
			YClose:     4.39,
			Change:     0.06,
			ChangeRate: 0.01367,
		},
	})

	// 5. yield down (green)
	date3, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-06")
	fmt.Println("yield down (green):")
	printBondYield([]*bond.BondYieldItem{
		{
			Date:       "2026-05-06",
			DateTime:   date3,
			BC1Month:   3.70,
			BC3Month:   3.69,
			BC6Month:   3.74,
			BC5Year:    3.99,
			BC10Year:   4.36,
			YClose:     4.43,
			Change:     -0.07,
			ChangeRate: -0.01580,
		},
	})
}

func TestPrintBondHistory(t *testing.T) {
	// 1. nil data
	printBondHistory(nil)

	// 2. empty data
	printBondHistory([]*bond.BondYieldItem{})

	// 3. single item with no previous data
	date1, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-01")
	fmt.Println("single item (no previous):")
	printBondHistory([]*bond.BondYieldItem{
		{
			Date:     "2026-05-01",
			DateTime: date1,
			BC1Month: 3.71,
			BC3Month: 3.68,
			BC6Month: 3.71,
			BC5Year:  4.02,
			BC10Year: 4.39,
			YClose:   -1,
		},
	})

	// 4. multi-day history covering all color states: up / down / first-day(no-prev)
	date2, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-04")
	date3, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-05")
	date4, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-06")
	date5, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-07")
	date6, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-08")

	data := []*bond.BondYieldItem{
		{
			Date:     "2026-05-01",
			DateTime: date1,
			BC1Month: 3.71, BC3Month: 3.68, BC6Month: 3.71,
			BC5Year: 4.02, BC10Year: 4.39,
			YClose: -1,
		},
		{
			Date: "2026-05-04", DateTime: date2,
			BC1Month: 3.71, BC3Month: 3.70, BC6Month: 3.76,
			BC5Year: 4.08, BC10Year: 4.45,
			YClose: 4.39, Change: 0.06, ChangeRate: 0.01367,
		},
		{
			Date: "2026-05-05", DateTime: date3,
			BC1Month: 3.70, BC3Month: 3.69, BC6Month: 3.75,
			BC5Year: 4.08, BC10Year: 4.43,
			YClose: 4.45, Change: -0.02, ChangeRate: -0.00449,
		},
		{
			Date: "2026-05-06", DateTime: date4,
			BC1Month: 3.70, BC3Month: 3.69, BC6Month: 3.74,
			BC5Year: 3.99, BC10Year: 4.36,
			YClose: 4.43, Change: -0.07, ChangeRate: -0.01580,
		},
		{
			Date: "2026-05-07", DateTime: date5,
			BC1Month: 3.72, BC3Month: 3.69, BC6Month: 3.74,
			BC5Year: 4.04, BC10Year: 4.41,
			YClose: 4.36, Change: 0.05, ChangeRate: 0.01147,
		},
		{
			Date: "2026-05-08", DateTime: date6,
			BC1Month: 3.71, BC3Month: 3.69, BC6Month: 3.74,
			BC5Year: 4.02, BC10Year: 4.38,
			YClose: 4.41, Change: -0.03, ChangeRate: -0.00680,
		},
	}
	fmt.Println("multi-day history (up/down/no-prev):")
	printBondHistory(data)
}

// TestPrintBondHistoryEdgeCases 覆盖边界场景
func TestPrintBondHistoryEdgeCases(t *testing.T) {
	date0, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-01")
	date1, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-04")
	date2, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-05")
	date3, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-06")
	date4, _ := time.Parse(utils.LayoutYYMMDD, "2026-05-07")

	// 1. flat: yield unchanged from previous day (ChangeRate == 0, no color)
	t.Run("unchanged yield (flat)", func(t *testing.T) {
		fmt.Println("--- unchanged yield (flat, no color) ---")
		printBondHistory([]*bond.BondYieldItem{
			{
				Date: "2026-05-04", DateTime: date1,
				BC1Month: 3.70, BC3Month: 3.69, BC6Month: 3.74,
				BC5Year: 4.00, BC10Year: 4.40,
				YClose: 4.40, Change: 0.0, ChangeRate: 0.0,
			},
		})
	})

	// 2. very small increase (borderline positive ChangeRate)
	t.Run("tiny increase", func(t *testing.T) {
		fmt.Println("--- tiny increase (0.1 bp, red) ---")
		printBondHistory([]*bond.BondYieldItem{
			{
				Date: "2026-05-04", DateTime: date1,
				BC1Month: 3.70, BC3Month: 3.69, BC6Month: 3.74,
				BC5Year: 4.00, BC10Year: 4.401,
				YClose: 4.40, Change: 0.001, ChangeRate: 0.000227,
			},
		})
	})

	// 3. very small decrease (borderline negative ChangeRate)
	t.Run("tiny decrease", func(t *testing.T) {
		fmt.Println("--- tiny decrease (0.1 bp, green) ---")
		printBondHistory([]*bond.BondYieldItem{
			{
				Date: "2026-05-04", DateTime: date1,
				BC1Month: 3.70, BC3Month: 3.69, BC6Month: 3.74,
				BC5Year: 4.00, BC10Year: 4.399,
				YClose: 4.40, Change: -0.001, ChangeRate: -0.000227,
			},
		})
	})

	// 4. large change (+50 bp)
	t.Run("large increase", func(t *testing.T) {
		fmt.Println("--- large increase (+50 bp, red) ---")
		printBondHistory([]*bond.BondYieldItem{
			{
				Date: "2026-05-05", DateTime: date2,
				BC1Month: 4.20, BC3Month: 4.19, BC6Month: 4.24,
				BC5Year: 4.50, BC10Year: 4.90,
				YClose: 4.40, Change: 0.50, ChangeRate: 0.1136,
			},
		})
	})

	// 5. large change (-50 bp)
	t.Run("large decrease", func(t *testing.T) {
		fmt.Println("--- large decrease (-50 bp, green) ---")
		printBondHistory([]*bond.BondYieldItem{
			{
				Date: "2026-05-06", DateTime: date3,
				BC1Month: 3.20, BC3Month: 3.19, BC6Month: 3.24,
				BC5Year: 3.50, BC10Year: 3.90,
				YClose: 4.40, Change: -0.50, ChangeRate: -0.1136,
			},
		})
	})

	// 6. mixed: first day (no prev) + flat + up + down together
	t.Run("mixed all states", func(t *testing.T) {
		fmt.Println("--- mixed: no-prev + flat + up + down ---")
		printBondHistory([]*bond.BondYieldItem{
			{
				Date: "2026-05-01", DateTime: date0,
				BC1Month: 3.71, BC3Month: 3.68, BC6Month: 3.71,
				BC5Year: 4.02, BC10Year: 4.39,
				YClose: -1, // no previous
			},
			{
				Date: "2026-05-04", DateTime: date1,
				BC1Month: 3.71, BC3Month: 3.70, BC6Month: 3.76,
				BC5Year: 4.08, BC10Year: 4.39,
				YClose: 4.39, Change: 0.0, ChangeRate: 0.0, // flat
			},
			{
				Date: "2026-05-05", DateTime: date2,
				BC1Month: 3.72, BC3Month: 3.71, BC6Month: 3.77,
				BC5Year: 4.10, BC10Year: 4.45,
				YClose: 4.39, Change: 0.06, ChangeRate: 0.01367, // up
			},
			{
				Date: "2026-05-06", DateTime: date3,
				BC1Month: 3.70, BC3Month: 3.69, BC6Month: 3.75,
				BC5Year: 4.05, BC10Year: 4.41,
				YClose: 4.45, Change: -0.04, ChangeRate: -0.00889, // down
			},
			{
				Date: "2026-05-07", DateTime: date4,
				BC1Month: 3.69, BC3Month: 3.68, BC6Month: 3.74,
				BC5Year: 4.03, BC10Year: 4.41,
				YClose: 4.41, Change: 0.0, ChangeRate: 0.0, // flat again
			},
		})
	})

	// 7. zero yield values (edge case, should not panic)
	t.Run("zero yields", func(t *testing.T) {
		fmt.Println("--- zero yields ---")
		printBondHistory([]*bond.BondYieldItem{
			{
				Date: "2026-05-04", DateTime: date1,
				BC1Month: 0, BC3Month: 0, BC6Month: 0,
				BC5Year: 0, BC10Year: 0,
				YClose: -1,
			},
			{
				Date: "2026-05-05", DateTime: date2,
				BC1Month: 0.01, BC3Month: 0.01, BC6Month: 0.01,
				BC5Year: 0.02, BC10Year: 0.02,
				YClose: 0.0, Change: 0.02, ChangeRate: 0.0, // YClose=0 would cause Inf for rate in provider, but here we test print
			},
		})
	})
}
