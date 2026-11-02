package bond

import (
	"github.com/alwqx/sec/provider/bond"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// yieldRow 是 --json 输出，上游缺失的期限是 null，真实的 0% 保留为 0。
type yieldRow struct {
	Date   string              `json:"date"`
	Yields map[string]*float64 `json:"yields"` // 键是期限，如 1m、10y，单位 %
}

func printYieldJSON(cmd *cobra.Command, items []*bond.BondYieldItem) error {
	rows := make([]yieldRow, 0, len(items))
	for _, item := range items {
		values := map[string]float64{
			"1m": item.BC1Month, "3m": item.BC3Month, "6m": item.BC6Month, "1y": item.BC1Year, "2y": item.BC2Year, "3y": item.BC3Year,
			"5y": item.BC5Year, "7y": item.BC7Year, "10y": item.BC10Year, "20y": item.BC20Year, "30y": item.BC30Year,
		}
		yields := make(map[string]*float64, len(values))
		for tenor, value := range values {
			if item.MissingTenors[tenor] {
				yields[tenor] = nil
				continue
			}
			yields[tenor] = &value
		}
		rows = append(rows, yieldRow{item.Date, yields})
	}
	return utils.PrintJSON(cmd.OutOrStdout(), map[string]any{"results": rows})
}
