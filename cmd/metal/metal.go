package metal

import (
	"log/slog"
	"time"

	"github.com/alwqx/sec/provider/metal"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

func NewMetalCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "metal",
		Aliases: []string{"m"},
		Short:   "Print quote of precious metal(default au999)",
		RunE:    MetalHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")

	return rootCmd
}

// MetalHandler 打印贵金属最新行情数据，默认 Au999
func MetalHandler(cmd *cobra.Command, args []string) error {
	end := time.Now()
	req := &metal.QueryAu999Req{
		Start: end.Add(-10 * 24 * time.Hour).Format(utils.LayoutYYMMDD),
		End:   end.Format(utils.LayoutYYMMDD),
	}
	resp, err := metal.QueryAu999(cmd.Context(), req)
	if err != nil {
		return err
	}
	num := len(resp.Data)
	if num == 0 {
		slog.Warn("no data")
	} else {
		printAu999History(resp.Data[num-1:])
	}

	return nil
}
