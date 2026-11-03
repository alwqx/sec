package metal

import (
	"fmt"
	"log/slog"

	"github.com/alwqx/sec/provider/metal"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// historyDays 是只指定 --end 时往前取的日历天数。
const historyDays = 30

func NewMetalCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "metal",
		Aliases: []string{"m"},
		Short:   "Print daily quotes of gold (Au99.99) or silver (Ag99.99) on the Shanghai Gold Exchange: the latest day, or a date range with --begin/--end",
		RunE:    MetalHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().Bool("json", false, "Output JSON")
	rootCmd.Flags().StringP("inst", "i", "", "SGE instrument, case-insensitive, alias in brackets: "+metal.InstrumentList()+"; default Au99.99")
	rootCmd.Flags().StringP("begin", "b", "", "Begin date 20250101; with --begin or --end, print every day in the range")
	rootCmd.Flags().StringP("end", "e", "", fmt.Sprintf("End date 20250131, default today; with only --end, the range starts %d days earlier", historyDays))

	return rootCmd
}

// queryRange 返回查询区间（YYYY-MM-DD）。没有指定 --begin、--end 时查最近 10 天，只输出最后一个交易日，
// 休市时也能取到最近一天；ranged 为 true 时输出区间内的每一天。
func queryRange(cmd *cobra.Command) (start, end string, ranged bool, err error) {
	beginText, _ := cmd.Flags().GetString("begin")
	endText, _ := cmd.Flags().GetString("end")
	ranged = beginText != "" || endText != ""
	days := historyDays
	if !ranged {
		days = 10
	}
	beginTime, endTime, err := utils.ParseRange(beginText, endText, days)
	if err != nil {
		return "", "", false, err
	}
	return beginTime.Format(utils.LayoutYYMMDD), endTime.Format(utils.LayoutYYMMDD), ranged, nil
}

// MetalHandler 打印贵金属日行情，默认 Au99.99 最近一个交易日；指定区间时打印区间内每一天。
func MetalHandler(cmd *cobra.Command, args []string) error {
	req := &metal.QueryReq{}
	req.Inst, _ = cmd.Flags().GetString("inst")
	var (
		ranged bool
		err    error
	)
	if req.Start, req.End, ranged, err = queryRange(cmd); err != nil {
		return err
	}
	resp, err := metal.Query(cmd.Context(), req)
	if err != nil {
		return err
	}
	items := resp.Data
	if !ranged && len(items) > 0 {
		items = items[len(items)-1:]
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return printDailyJSON(cmd, resp.Instrument, items)
	}
	if len(items) == 0 {
		slog.Warn("no data")
		return nil
	}
	printDailyHistory(cmd.OutOrStdout(), resp.Instrument, items)
	return nil
}
