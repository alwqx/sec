package metal

import (
	"testing"

	"github.com/alwqx/sec/provider/metal"
)

func TestPrintAu999History(t *testing.T) {
	// 1. nil data
	printAu999History(nil)

	// 2. empty data
	printAu999History([]*metal.DailyHQItem{})

	// 3. common data with 1 item
	data := []*metal.DailyHQItem{
		{
			Date:   "2026-04-24",
			Open:   1040,
			Close:  1033.25,
			Low:    1028,
			High:   1044,
			YClose: -1,
		},
		{
			Date:       "2026-04-27",
			Open:       1039.9,
			Close:      1037.21,
			Low:        1033,
			High:       1044,
			YClose:     1033.25,
			Change:     3.96,
			ChangeRate: 0.003817,
		},
		{
			Date:       "2026-04-28",
			Open:       1035,
			Close:      1020.73,
			Low:        1038.9,
			High:       1019,
			YClose:     1037.21,
			Change:     -16.48,
			ChangeRate: -0.016145,
		},
	}
	printAu999History(data)
}
